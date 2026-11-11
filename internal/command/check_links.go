package command

import (
	"context"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[CheckLinksRequest, Empty] = (*CheckLinks)(nil)

// The link checker's schedule (docs/PLAN.md §5.5).
const (
	// linkBatch is the most links checked, and the most archive lookups made, in a run.
	linkBatch = 400
	// linkCheckInterval is how long after its last check a link is due again.
	linkCheckInterval = 7 * 24 * time.Hour
	// archiveLookupInterval is how long after its last archive lookup a link is due again.
	archiveLookupInterval = 30 * 24 * time.Hour
	// deadAfterFailures is how many consecutive 404, 410 or NXDOMAIN answers make a link dead.
	deadAfterFailures = 3
)

// CheckLinksRequest is the request for the CheckLinks command.
type CheckLinksRequest struct {
	// MaxDuration is how long the run may take; it then stops with what it finished recorded.
	MaxDuration time.Duration
}

// CheckLinks checks the visible links due for a check, records each result, then looks up archived copies of the
// links due for it. Logs name links by ID only, never by URL.
type CheckLinks struct {
	Links   datasources.LinkChecks
	Prober  datasources.LinkProber
	Archive datasources.ArchiveLookup
	Now     func() time.Time
}

// Execute runs the checker once.
func (c *CheckLinks) Execute(ctx context.Context, req CheckLinksRequest) (Empty, error) {
	work, stop := context.WithTimeout(ctx, req.MaxDuration)
	defer stop()

	if err := c.checkDue(work); err != nil {
		return Empty{}, err
	}

	return Empty{}, c.lookUpArchives(work)
}

// checkDue checks the links due and records each result, until they are done or ctx ends.
func (c *CheckLinks) checkDue(ctx context.Context) error {
	links, err := c.Links.ListLinksDueForCheck(ctx, c.Now().Add(-linkCheckInterval), linkBatch)
	if err != nil {
		return fmt.Errorf("listing links due for a check: %w", err)
	}
	logger := domain.LoggerFromContext(ctx)
	for _, link := range links {
		outcome := c.Prober.Probe(ctx, link.URL)
		if ended(ctx) {
			// A check cut short by the end of the run says nothing about the link.
			break
		}
		liveness, failures := nextLiveness(link, outcome)
		if err := c.Links.UpdateLinkCheck(ctx, link.ID, liveness, failures, c.Now().UTC()); err != nil {
			return fmt.Errorf("recording a link check: %w", err)
		}
		logger.DebugContext(ctx, "link checked", "link_id", link.ID, "outcome", string(outcome))
	}

	return nil
}

// nextLiveness returns a link's liveness and consecutive failures after a check found outcome: a success resets
// the failures, a third failure in a row makes the link dead, a refusal makes it blocked and resets the failures,
// and an inconclusive check changes nothing.
func nextLiveness(link datasources.DueLink, outcome datasources.LinkOutcome) (domain.Liveness, uint8) {
	switch outcome {
	case datasources.LinkOutcomeOK:
		return domain.LivenessOK, 0
	case datasources.LinkOutcomeGone:
		failures := link.Failures + 1
		if failures >= deadAfterFailures {
			return domain.LivenessDead, failures
		}
		return link.Liveness, failures
	case datasources.LinkOutcomeBlocked:
		return domain.LivenessBlocked, 0
	case datasources.LinkOutcomeUnreachable:
	}

	return link.Liveness, link.Failures
}

// lookUpArchives looks up an archived copy of each link due, until they are done or ctx ends. A lookup that fails
// is not recorded, so the link stays due.
func (c *CheckLinks) lookUpArchives(ctx context.Context) error {
	links, err := c.Links.ListLinksDueForArchive(ctx, c.Now().Add(-archiveLookupInterval), linkBatch)
	if err != nil {
		return fmt.Errorf("listing links due for an archive lookup: %w", err)
	}
	logger := domain.LoggerFromContext(ctx)
	for _, link := range links {
		archived, err := c.Archive.Available(ctx, link.URL)
		if ended(ctx) {
			break
		}
		if err != nil {
			logger.WarnContext(ctx, "archive lookup failed", "link_id", link.ID)
			continue
		}
		if err := c.Links.UpdateLinkArchive(ctx, link.ID, archived, c.Now().UTC()); err != nil {
			return fmt.Errorf("recording an archive lookup: %w", err)
		}
	}

	return nil
}

// ended reports whether ctx has ended, which is how a run reaches its time limit.
func ended(ctx context.Context) bool { return ctx.Err() != nil }
