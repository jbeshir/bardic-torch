package command

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[CheckLinksRequest, CheckLinksResult] = (*CheckLinks)(nil)

// The link checker's schedule.
const (
	// LinkCheckInterval is how long after its last check a link is due again.
	LinkCheckInterval = 7 * 24 * time.Hour
	// ArchiveLookupInterval is how long after its last archive lookup a link without an archived copy is due again.
	ArchiveLookupInterval = 30 * 24 * time.Hour
	// deadAfterFailures is how many consecutive definite failures make a link dead.
	deadAfterFailures = 3
	// outageWindow is how many of a run's first checks, or archive lookups, decide whether the network or the
	// archive is down: if more than half of them fail transiently, the run stops. A run with fewer is never stopped.
	outageWindow = 20
)

// ErrLinkCheckOutage is a run stopped because most of its first checks, or archive lookups, failed transiently: the
// network or the archive is down, and recording the failures would only make links look worse than they are.
var ErrLinkCheckOutage = errors.New("most of the first checks failed transiently")

// CheckLinks checks the visible links due for a check, records each result, then looks up archived copies of the
// links due for an archive lookup (docs/PLAN.md §3, "cmd/check-links"). One run at a time holds the lock; every
// write is also a compare-and-set, so a run without the lock, or a link hidden or checked since it was selected,
// writes nothing. Logs name links by ID only, never by URL.
type CheckLinks struct {
	Locker  datasources.LinkCheckLocker
	Links   datasources.LinkChecker
	Prober  datasources.LinkProber
	Archive datasources.ArchiveLookup
	Now     func() time.Time
}

// CheckLinksRequest are the limits of a run.
type CheckLinksRequest struct {
	// Limit is the most links checked, and ArchiveLimit the most archive lookups made.
	Limit, ArchiveLimit int32
	// Workers is how many links are checked at once.
	Workers int
	// MaxDuration is how long the run checks and looks up: it then stops, recording what it finished.
	MaxDuration time.Duration
	// ArchiveSpacing is the least time between two archive lookups.
	ArchiveSpacing time.Duration
	// LockPingInterval is how often the run checks that it still holds the lock.
	LockPingInterval time.Duration
}

// CheckLinksResult is what a run did, in counts.
type CheckLinksResult struct {
	// LockHeld is whether another run held the lock, so this one did nothing.
	LockHeld bool
	// Outcomes counts the checks recorded, by outcome.
	Outcomes map[datasources.LinkOutcome]int
	// NotRecorded counts the checks not recorded: the link was hidden or checked since it was selected, or the write
	// failed.
	NotRecorded int
	// ArchiveLookups counts the archive lookups recorded, and ArchivesFound those that found a copy.
	ArchiveLookups, ArchivesFound int
	// StoppedAtMaxDuration is whether the run reached MaxDuration before it finished.
	StoppedAtMaxDuration bool
}

// probed is a link and what checking it found.
type probed struct {
	target  datasources.LinkCheckTarget
	outcome datasources.LinkOutcome
}

// Execute runs the checker once. It returns ErrLinkCheckOutage if most of the first checks or archive lookups
// failed transiently, and datasources.ErrLinkCheckLockLost if the lock was lost during the run; another run holding
// the lock is not an error.
func (c *CheckLinks) Execute(ctx context.Context, req CheckLinksRequest) (CheckLinksResult, error) {
	logger := domain.LoggerFromContext(ctx)
	lock, err := c.Locker.LockLinkCheck(ctx)
	if errors.Is(err, datasources.ErrLinkCheckInProgress) {
		logger.InfoContext(ctx, "another link check holds the lock; nothing to do")
		return CheckLinksResult{LockHeld: true}, nil
	}
	if err != nil {
		return CheckLinksResult{}, fmt.Errorf("taking the link check lock: %w", err)
	}
	defer lock.Release(ctx)

	work, stopWork := context.WithTimeout(ctx, req.MaxDuration)
	defer stopWork()
	lockLost := keepLock(work, stopWork, lock, req.LockPingInterval)

	result := CheckLinksResult{Outcomes: map[datasources.LinkOutcome]int{}}
	err = c.checkDue(ctx, work, req, &result)
	if err == nil {
		err = c.lookUpArchives(ctx, work, req, &result)
	}
	stopWork()
	if lostErr := <-lockLost; lostErr != nil {
		return result, lostErr
	}
	if ctx.Err() != nil {
		return result, fmt.Errorf("link check stopped: %w", ctx.Err())
	}
	result.StoppedAtMaxDuration = errors.Is(work.Err(), context.DeadlineExceeded)

	return result, err
}

// keepLock pings lock every interval until work ends, stopping the work if the lock is lost. The channel it returns
// yields the error that lost the lock, or nil, once the pings have stopped.
func keepLock(
	work context.Context, stopWork context.CancelFunc, lock datasources.LinkCheckLock, interval time.Duration,
) <-chan error {
	lost := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				lost <- nil
				return
			case <-ticker.C:
				if err := lock.Ping(work); err != nil && work.Err() == nil {
					domain.LoggerFromContext(work).ErrorContext(work, "link check lock lost; stopping")
					stopWork()
					lost <- fmt.Errorf("checking the link check lock: %w", err)
					return
				}
			}
		}
	}()

	return lost
}

// checkDue checks the links due and records each result, until they are done or work ends. Results come in as
// checks finish; the first outageWindow are held back until they show the network is up.
func (c *CheckLinks) checkDue(ctx, work context.Context, req CheckLinksRequest, result *CheckLinksResult) error {
	targets, err := c.Links.ListLinksDueForCheck(ctx, c.Now().Add(-LinkCheckInterval), req.Limit)
	if err != nil {
		return fmt.Errorf("listing links due for a check: %w", err)
	}

	probing, stopProbing := context.WithCancel(work)
	results := c.probeAll(probing, targets, req.Workers)
	defer func() {
		stopProbing()
		for range results {
		}
	}()

	windowSize := min(outageWindow, len(targets))
	window := make([]probed, 0, windowSize)
	for p := range results {
		if probing.Err() != nil {
			// A check cut short by the end of the run says nothing about the link.
			continue
		}
		if len(window) < windowSize {
			if window = append(window, p); len(window) < windowSize {
				continue
			}
			if err := c.recordWindow(ctx, window, result); err != nil {
				return err
			}
			continue
		}
		if err := c.record(ctx, p, result); err != nil {
			return err
		}
	}
	if len(window) < windowSize {
		// The run ended before the window filled: what finished is recorded unless it shows an outage.
		return c.recordWindow(ctx, window, result)
	}

	return nil
}

// recordWindow records the first checks of a run, unless there are outageWindow of them and more than half failed
// transiently. A run with fewer checks is never stopped: its transient failures change nothing but the check time.
func (c *CheckLinks) recordWindow(ctx context.Context, window []probed, result *CheckLinksResult) error {
	unreachable := 0
	for _, p := range window {
		if p.outcome == datasources.LinkOutcomeUnreachable {
			unreachable++
		}
	}
	if len(window) == outageWindow && unreachable*2 > len(window) {
		domain.LoggerFromContext(ctx).ErrorContext(ctx, "most of the first link checks failed transiently; "+
			"stopping without recording them", "unreachable", unreachable, "checked", len(window))
		return fmt.Errorf("%w: %d of the first %d checks", ErrLinkCheckOutage, unreachable, len(window))
	}
	for _, p := range window {
		if err := c.record(ctx, p, result); err != nil {
			return err
		}
	}

	return nil
}

// probeAll checks targets with workers checks at once, until they are done or ctx ends. The channel it returns
// yields each result and closes when every worker has finished.
func (c *CheckLinks) probeAll(ctx context.Context, targets []datasources.LinkCheckTarget, workers int) <-chan probed {
	jobs := make(chan datasources.LinkCheckTarget)
	go func() {
		defer close(jobs)
		for _, target := range targets {
			select {
			case jobs <- target:
			case <-ctx.Done():
				return
			}
		}
	}()

	results := make(chan probed)
	var wg sync.WaitGroup
	for range max(workers, 1) {
		wg.Go(func() {
			for target := range jobs {
				results <- probed{target: target, outcome: c.Prober.Probe(ctx, target.URL)}
			}
		})
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

// record writes what a check found, if the link is still as it was selected. A write that fails because the
// database is busy or unreachable is skipped, to be retried next run; any other failure stops the run.
func (c *CheckLinks) record(ctx context.Context, p probed, result *CheckLinksResult) error {
	logger := domain.LoggerFromContext(ctx)
	if p.outcome == datasources.LinkOutcomeRefused {
		logger.WarnContext(ctx, "link refused by the checker's address and scheme guard; recorded as blocked",
			"link_id", p.target.ID)
	}
	liveness, failures := nextLiveness(p.target, p.outcome)
	written, err := c.Links.UpdateLinkCheck(ctx, datasources.LinkCheckResult{
		ID: p.target.ID, Liveness: liveness, Failures: failures, CheckedAt: c.Now().UTC().Truncate(time.Second),
		ExpectedLastCheckedAt: p.target.LastCheckedAt,
	})
	switch {
	case errors.Is(err, datasources.ErrUnavailable):
		logger.WarnContext(ctx, "link check not recorded: the database is unavailable", "link_id", p.target.ID)
		result.NotRecorded++
		return nil
	case err != nil:
		return fmt.Errorf("recording a link check: %w", err)
	case !written:
		logger.InfoContext(ctx, "link hidden or checked since it was selected; not recorded", "link_id", p.target.ID)
		result.NotRecorded++
		return nil
	}
	logger.DebugContext(ctx, "link checked", "link_id", p.target.ID, "outcome", string(p.outcome),
		"liveness", string(liveness))
	result.Outcomes[p.outcome]++

	return nil
}

// nextLiveness returns a link's liveness and consecutive definite failures after a check found outcome. A success
// resets the failures; a definite failure adds one, and makes the link dead at deadAfterFailures; a refusal by the
// site or the guard makes it blocked and keeps the count; a transient failure changes nothing.
func nextLiveness(target datasources.LinkCheckTarget, outcome datasources.LinkOutcome) (domain.Liveness, uint8) {
	switch outcome {
	case datasources.LinkOutcomeOK:
		return domain.LivenessOK, 0
	case datasources.LinkOutcomeGone:
		failures := target.Failures
		if failures < math.MaxUint8 {
			failures++
		}
		if failures >= deadAfterFailures {
			return domain.LivenessDead, failures
		}
		return target.Liveness, failures
	case datasources.LinkOutcomeBlocked, datasources.LinkOutcomeRefused:
		return domain.LivenessBlocked, target.Failures
	case datasources.LinkOutcomeUnreachable:
		return target.Liveness, target.Failures
	default:
		return target.Liveness, target.Failures
	}
}

// lookUpArchives looks up an archived copy of each link due, one every ArchiveSpacing, until they are done or work
// ends. A lookup that fails is not recorded, so the link stays due.
func (c *CheckLinks) lookUpArchives(ctx, work context.Context, req CheckLinksRequest, result *CheckLinksResult) error {
	if ended(work) {
		return nil
	}
	targets, err := c.Links.ListLinksDueForArchive(ctx, c.Now().Add(-ArchiveLookupInterval), req.ArchiveLimit)
	if err != nil {
		return fmt.Errorf("listing links due for an archive lookup: %w", err)
	}

	failed := 0
	for i, target := range targets {
		if i > 0 && !pause(work, req.ArchiveSpacing) {
			return nil
		}
		archived, err := c.Archive.Available(work, target.URL)
		switch {
		case ended(work):
			return nil
		case err != nil:
			failed++
			domain.LoggerFromContext(ctx).WarnContext(ctx, "archive lookup failed", "link_id", target.ID)
		default:
			if err := c.recordArchive(ctx, target.ID, archived, result); err != nil {
				return err
			}
		}
		if i+1 == outageWindow && failed*2 > outageWindow {
			domain.LoggerFromContext(ctx).ErrorContext(ctx, "most of the first archive lookups failed; stopping",
				"failed", failed, "looked_up", outageWindow)
			return fmt.Errorf("%w: %d of the first %d archive lookups", ErrLinkCheckOutage, failed, outageWindow)
		}
	}

	return nil
}

// recordArchive writes an archive lookup's result, if the link is still visible.
func (c *CheckLinks) recordArchive(ctx context.Context, id int64, archived *string, result *CheckLinksResult) error {
	written, err := c.Links.UpdateLinkArchive(ctx, datasources.LinkArchiveResult{
		ID: id, ArchivedURL: archived, CheckedAt: c.Now().UTC().Truncate(time.Second),
	})
	switch {
	case errors.Is(err, datasources.ErrUnavailable):
		domain.LoggerFromContext(ctx).WarnContext(ctx, "archive lookup not recorded: the database is unavailable",
			"link_id", id)
		return nil
	case err != nil:
		return fmt.Errorf("recording an archive lookup: %w", err)
	case !written:
		domain.LoggerFromContext(ctx).InfoContext(ctx, "link hidden since it was selected; archive not recorded",
			"link_id", id)
		return nil
	}
	result.ArchiveLookups++
	if archived != nil {
		result.ArchivesFound++
	}

	return nil
}

// pause waits for d, returning false if ctx ends first.
func pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// ended reports whether ctx has ended.
func ended(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}
