package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// checkNow is the time of the tests' runs, and checkStamp what a run records it as.
var (
	checkNow   = time.Date(2026, 11, 8, 3, 23, 45, 678, time.UTC)
	checkStamp = checkNow.Truncate(time.Second)
)

// lastWeek is a previous check's time.
var lastWeek = checkStamp.Add(-8 * 24 * time.Hour)

// linkURL is the URL of the test link with id; the tests' logs must never hold it.
func linkURL(id int64) string { return fmt.Sprintf("https://hidden-songs.example/song-%d", id) }

// fakeLinks is a link checker store holding due links, recording what is written. A write to a link in lose
// reports nothing written, as a compare-and-set that lost does.
type fakeLinks struct {
	mu             sync.Mutex
	due            []datasources.LinkCheckTarget
	archiveDue     []datasources.LinkArchiveTarget
	lose           map[int64]bool
	unavailable    map[int64]bool
	checks         []datasources.LinkCheckResult
	archives       []datasources.LinkArchiveResult
	archiveCutoffs []time.Time
}

func (f *fakeLinks) ListLinksDueForCheck(
	_ context.Context, checkedBefore time.Time, limit int32,
) ([]datasources.LinkCheckTarget, error) {
	if !checkedBefore.Equal(checkNow.Add(-LinkCheckInterval)) {
		return nil, errors.New("unexpected cutoff")
	}

	return f.due[:min(int(limit), len(f.due))], nil
}

func (f *fakeLinks) UpdateLinkCheck(_ context.Context, result datasources.LinkCheckResult) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unavailable[result.ID] {
		return false, datasources.ErrUnavailable
	}
	if f.lose[result.ID] {
		return false, nil
	}
	f.checks = append(f.checks, result)

	return true, nil
}

func (f *fakeLinks) ListLinksDueForArchive(
	_ context.Context, checkedBefore time.Time, limit int32,
) ([]datasources.LinkArchiveTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.archiveCutoffs = append(f.archiveCutoffs, checkedBefore)

	return f.archiveDue[:min(int(limit), len(f.archiveDue))], nil
}

func (f *fakeLinks) UpdateLinkArchive(_ context.Context, result datasources.LinkArchiveResult) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lose[result.ID] {
		return false, nil
	}
	f.archives = append(f.archives, result)

	return true, nil
}

// written returns the recorded checks by link ID.
func (f *fakeLinks) written() map[int64]datasources.LinkCheckResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	byID := map[int64]datasources.LinkCheckResult{}
	for _, check := range f.checks {
		byID[check.ID] = check
	}

	return byID
}

// fakeProber answers each URL with its outcome, OK by default. If hang is set, a check waits for its context to
// end instead.
type fakeProber struct {
	outcomes map[string]datasources.LinkOutcome
	hang     bool
}

func (p *fakeProber) Probe(ctx context.Context, url string) datasources.LinkOutcome {
	if p.hang {
		<-ctx.Done()
		return datasources.LinkOutcomeUnreachable
	}
	if outcome, ok := p.outcomes[url]; ok {
		return outcome
	}

	return datasources.LinkOutcomeOK
}

// heldLock returns a locker whose lock is taken once, pings fine and is released once.
func heldLock(t *testing.T) *mocks.LinkCheckLocker {
	t.Helper()
	lock := mocks.NewLinkCheckLock(t)
	lock.EXPECT().Ping(mock.Anything).Return(nil).Maybe()
	lock.EXPECT().Release(mock.Anything).Return().Once()
	locker := mocks.NewLinkCheckLocker(t)
	locker.EXPECT().LockLinkCheck(mock.Anything).Return(lock, nil).Once()

	return locker
}

// testRequest is a run's limits in the tests.
func testRequest() CheckLinksRequest {
	return CheckLinksRequest{
		Limit: 400, ArchiveLimit: 200, Workers: 4, MaxDuration: time.Minute, ArchiveSpacing: 0,
		LockPingInterval: time.Minute,
	}
}

// noArchive is an archive that must not be asked.
func noArchive(t *testing.T) datasources.ArchiveLookup { return mocks.NewArchiveLookup(t) }

// newCheckLinks returns a run over links and prober.
func newCheckLinks(
	t *testing.T, links *fakeLinks, prober datasources.LinkProber, archive datasources.ArchiveLookup,
) *CheckLinks {
	t.Helper()

	return &CheckLinks{Locker: heldLock(t), Links: links, Prober: prober, Archive: archive,
		Now: func() time.Time { return checkNow }}
}

// target is a due link.
func target(id int64, liveness domain.Liveness, failures uint8, last *time.Time) datasources.LinkCheckTarget {
	return datasources.LinkCheckTarget{ID: id, URL: linkURL(id), Liveness: liveness, Failures: failures,
		LastCheckedAt: last}
}

// logContext returns a context logging JSON at debug to logs.
func logContext(t *testing.T, logs *bytes.Buffer) context.Context {
	t.Helper()

	return domain.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(logs,
		&slog.HandlerOptions{Level: slog.LevelDebug})))
}

func TestCheckLinks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		target   datasources.LinkCheckTarget
		outcome  datasources.LinkOutcome
		lose     bool
		want     *datasources.LinkCheckResult
		wantWarn bool
	}{
		{name: "ok resets failures", target: target(1, domain.LivenessUnknown, 2, &lastWeek),
			outcome: datasources.LinkOutcomeOK,
			want:    &datasources.LinkCheckResult{Liveness: domain.LivenessOK, Failures: 0}},
		{name: "first failure keeps liveness", target: target(1, domain.LivenessOK, 0, nil),
			outcome: datasources.LinkOutcomeGone,
			want:    &datasources.LinkCheckResult{Liveness: domain.LivenessOK, Failures: 1}},
		{name: "second failure keeps liveness", target: target(1, domain.LivenessOK, 1, &lastWeek),
			outcome: datasources.LinkOutcomeGone,
			want:    &datasources.LinkCheckResult{Liveness: domain.LivenessOK, Failures: 2}},
		{name: "third failure is dead", target: target(1, domain.LivenessOK, 2, &lastWeek),
			outcome: datasources.LinkOutcomeGone,
			want:    &datasources.LinkCheckResult{Liveness: domain.LivenessDead, Failures: 3}},
		{name: "failure count saturates", target: target(1, domain.LivenessDead, 255, &lastWeek),
			outcome: datasources.LinkOutcomeGone,
			want:    &datasources.LinkCheckResult{Liveness: domain.LivenessDead, Failures: 255}},
		{name: "dead link that works again is ok", target: target(1, domain.LivenessDead, 5, &lastWeek),
			outcome: datasources.LinkOutcomeOK,
			want:    &datasources.LinkCheckResult{Liveness: domain.LivenessOK, Failures: 0}},
		{name: "blocked keeps count", target: target(1, domain.LivenessOK, 2, &lastWeek),
			outcome: datasources.LinkOutcomeBlocked,
			want:    &datasources.LinkCheckResult{Liveness: domain.LivenessBlocked, Failures: 2}},
		{name: "guard refusal is blocked and warned", target: target(1, domain.LivenessUnknown, 1, nil),
			outcome: datasources.LinkOutcomeRefused, wantWarn: true,
			want: &datasources.LinkCheckResult{Liveness: domain.LivenessBlocked, Failures: 1}},
		{name: "transient failure changes nothing but the time", target: target(1, domain.LivenessOK, 1, &lastWeek),
			outcome: datasources.LinkOutcomeUnreachable,
			want:    &datasources.LinkCheckResult{Liveness: domain.LivenessOK, Failures: 1}},
		{name: "compare-and-set that loses is skipped", target: target(1, domain.LivenessOK, 0, &lastWeek),
			outcome: datasources.LinkOutcomeGone, lose: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			links := &fakeLinks{due: []datasources.LinkCheckTarget{tc.target}, lose: map[int64]bool{1: tc.lose}}
			prober := &fakeProber{outcomes: map[string]datasources.LinkOutcome{linkURL(1): tc.outcome}}
			var logs bytes.Buffer
			result, err := newCheckLinks(t, links, prober, noArchive(t)).Execute(logContext(t, &logs), testRequest())
			require.NoError(t, err)

			if tc.want == nil {
				assert.Empty(t, links.written())
				assert.Equal(t, 1, result.NotRecorded)
				return
			}
			want := *tc.want
			want.ID, want.CheckedAt, want.ExpectedLastCheckedAt = 1, checkStamp, tc.target.LastCheckedAt
			want.ProbedURL = linkURL(1)
			assert.Equal(t, map[int64]datasources.LinkCheckResult{1: want}, links.written())
			assert.Equal(t, map[datasources.LinkOutcome]int{tc.outcome: 1}, result.Outcomes)
			assert.Equal(t, tc.wantWarn, strings.Contains(logs.String(), `"level":"WARN"`), logs.String())
		})
	}
}

// TestCheckLinks_OutageAbort checks that a run whose first 20 checks are more than half transient failures records
// none of them, and that one with exactly half records them all.
func TestCheckLinks_OutageAbort(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		links       int
		unreachable int
		wantAbort   bool
	}{
		{name: "11 of the first 20", links: 30, unreachable: 11, wantAbort: true},
		{name: "10 of the first 20", links: 30, unreachable: 10},
		{name: "all of a run under 20", links: 19, unreachable: 19},
		{name: "1 of a small run", links: 3, unreachable: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			links := &fakeLinks{}
			prober := &fakeProber{outcomes: map[string]datasources.LinkOutcome{}}
			for id := range int64(tc.links) {
				links.due = append(links.due, target(id+1, domain.LivenessOK, 0, nil))
				if id < int64(tc.unreachable) {
					prober.outcomes[linkURL(id+1)] = datasources.LinkOutcomeUnreachable
				}
			}
			// With one worker the results come in the order of the due links, so the window is the first 20.
			req := testRequest()
			req.Workers = 1
			result, err := newCheckLinks(t, links, prober, noArchive(t)).Execute(t.Context(), req)
			if tc.wantAbort {
				require.ErrorIs(t, err, ErrLinkCheckOutage)
				assert.Empty(t, links.written(), "nothing is recorded")
				assert.Empty(t, links.archiveCutoffs, "no archive lookups follow")
				return
			}
			require.NoError(t, err)
			assert.Len(t, links.written(), tc.links)
			assert.Equal(t, tc.unreachable, result.Outcomes[datasources.LinkOutcomeUnreachable])
		})
	}
}

// TestCheckLinks_MaxDurationStops checks that a run stops at its maximum duration, recording no check it cut
// short, and makes no archive lookups.
func TestCheckLinks_MaxDurationStops(t *testing.T) {
	t.Parallel()

	links := &fakeLinks{due: []datasources.LinkCheckTarget{target(1, domain.LivenessOK, 0, nil)}}
	req := testRequest()
	req.MaxDuration = 50 * time.Millisecond

	result, err := newCheckLinks(t, links, &fakeProber{hang: true}, noArchive(t)).Execute(t.Context(), req)
	require.NoError(t, err)
	assert.True(t, result.StoppedAtMaxDuration)
	assert.Empty(t, links.written())
	assert.Empty(t, links.archiveCutoffs)
}

func TestCheckLinks_LockHeldElsewhereDoesNothing(t *testing.T) {
	t.Parallel()

	locker := mocks.NewLinkCheckLocker(t)
	locker.EXPECT().LockLinkCheck(mock.Anything).Return(nil, datasources.ErrLinkCheckInProgress)
	run := &CheckLinks{Locker: locker, Links: mocks.NewLinkChecker(t), Prober: mocks.NewLinkProber(t),
		Archive: noArchive(t), Now: func() time.Time { return checkNow }}

	result, err := run.Execute(t.Context(), testRequest())
	require.NoError(t, err)
	assert.True(t, result.LockHeld)
}

// TestCheckLinks_LockLostStops checks that a run whose lock ping fails stops and fails.
func TestCheckLinks_LockLostStops(t *testing.T) {
	t.Parallel()

	lock := mocks.NewLinkCheckLock(t)
	lock.EXPECT().Ping(mock.Anything).Return(datasources.ErrLinkCheckLockLost).Once()
	lock.EXPECT().Release(mock.Anything).Return().Once()
	locker := mocks.NewLinkCheckLocker(t)
	locker.EXPECT().LockLinkCheck(mock.Anything).Return(lock, nil)
	links := &fakeLinks{due: []datasources.LinkCheckTarget{target(1, domain.LivenessOK, 0, nil)}}
	req := testRequest()
	req.LockPingInterval = 20 * time.Millisecond

	run := &CheckLinks{Locker: locker, Links: links, Prober: &fakeProber{hang: true}, Archive: noArchive(t),
		Now: func() time.Time { return checkNow }}
	_, err := run.Execute(t.Context(), req)
	require.ErrorIs(t, err, datasources.ErrLinkCheckLockLost)
	assert.Empty(t, links.written())
}

func TestCheckLinks_UnavailableWriteIsSkipped(t *testing.T) {
	t.Parallel()

	links := &fakeLinks{
		due:         []datasources.LinkCheckTarget{target(1, domain.LivenessOK, 0, nil), target(2, domain.LivenessOK, 0, nil)},
		unavailable: map[int64]bool{1: true},
	}
	result, err := newCheckLinks(t, links, &fakeProber{}, noArchive(t)).Execute(t.Context(), testRequest())
	require.NoError(t, err)
	assert.Equal(t, 1, result.NotRecorded)
	assert.Contains(t, links.written(), int64(2))
}

func TestCheckLinks_ArchiveLookupForDeadLinks(t *testing.T) {
	t.Parallel()

	found := "https://web.archive.org/web/2024/https://hidden-songs.example/song-11"
	archive := mocks.NewArchiveLookup(t)
	archive.EXPECT().Available(mock.Anything, linkURL(11)).Return(&found, nil).Once()
	archive.EXPECT().Available(mock.Anything, linkURL(12)).Return(nil, nil).Once()
	archive.EXPECT().Available(mock.Anything, linkURL(13)).Return(nil, datasources.ErrArchiveUnavailable).Once()
	archive.EXPECT().Available(mock.Anything, linkURL(14)).Return(&found, nil).Once()
	links := &fakeLinks{
		archiveDue: []datasources.LinkArchiveTarget{
			{ID: 11, URL: linkURL(11)}, {ID: 12, URL: linkURL(12)}, {ID: 13, URL: linkURL(13)},
			{ID: 14, URL: linkURL(14)},
		},
		lose: map[int64]bool{14: true},
	}

	var logs bytes.Buffer
	result, err := newCheckLinks(t, links, &fakeProber{}, archive).Execute(logContext(t, &logs), testRequest())
	require.NoError(t, err)
	assert.Equal(t, []time.Time{checkNow.Add(-ArchiveLookupInterval)}, links.archiveCutoffs)
	assert.Equal(t, []datasources.LinkArchiveResult{
		{ID: 11, ArchivedURL: &found, CheckedAt: checkStamp},
		{ID: 12, CheckedAt: checkStamp},
	}, links.archives, "a failed lookup is not recorded, so the link stays due")
	assert.Equal(t, 2, result.ArchiveLookups)
	assert.Equal(t, 1, result.ArchivesFound)
	assert.NotContains(t, logs.String(), "hidden-songs.example")
}

func TestCheckLinks_ArchiveOutageStops(t *testing.T) {
	t.Parallel()

	archive := mocks.NewArchiveLookup(t)
	links := &fakeLinks{}
	for id := range int64(25) {
		links.archiveDue = append(links.archiveDue, datasources.LinkArchiveTarget{ID: id + 1, URL: linkURL(id + 1)})
		if id < 9 {
			archive.EXPECT().Available(mock.Anything, linkURL(id+1)).Return(nil, nil).Once()
			continue
		}
		if id < 20 {
			archive.EXPECT().Available(mock.Anything, linkURL(id+1)).Return(nil, datasources.ErrArchiveUnavailable).Once()
		}
	}

	_, err := newCheckLinks(t, links, &fakeProber{}, archive).Execute(t.Context(), testRequest())
	require.ErrorIs(t, err, ErrLinkCheckOutage, "11 of the first 20 lookups failed")
	assert.Len(t, links.archives, 9, "the lookups that worked are kept; none after the 20th is made")
}

// TestCheckLinks_LogsNoURL runs every kind of outcome at debug and checks that the logs name links by ID only.
func TestCheckLinks_LogsNoURL(t *testing.T) {
	t.Parallel()

	outcomes := []datasources.LinkOutcome{
		datasources.LinkOutcomeOK, datasources.LinkOutcomeGone, datasources.LinkOutcomeUnreachable,
		datasources.LinkOutcomeBlocked, datasources.LinkOutcomeRefused,
	}
	links := &fakeLinks{lose: map[int64]bool{6: true}, unavailable: map[int64]bool{7: true}}
	prober := &fakeProber{outcomes: map[string]datasources.LinkOutcome{}}
	for i := range int64(7) {
		links.due = append(links.due, target(i+1, domain.LivenessOK, 2, nil))
		prober.outcomes[linkURL(i+1)] = outcomes[int(i)%len(outcomes)]
	}
	archive := mocks.NewArchiveLookup(t)
	archive.EXPECT().Available(mock.Anything, linkURL(8)).Return(nil, datasources.ErrArchiveUnavailable)
	archive.EXPECT().Available(mock.Anything, linkURL(9)).Return(nil, nil)
	archive.EXPECT().Available(mock.Anything, linkURL(10)).Return(nil, nil)
	links.archiveDue = []datasources.LinkArchiveTarget{
		{ID: 8, URL: linkURL(8)}, {ID: 9, URL: linkURL(9)}, {ID: 10, URL: linkURL(10)},
	}

	var logs bytes.Buffer
	_, err := newCheckLinks(t, links, prober, archive).Execute(logContext(t, &logs), testRequest())
	require.NoError(t, err)
	for id := int64(1); id <= 8; id++ {
		assert.Contains(t, logs.String(), fmt.Sprintf(`"link_id":%d`, id))
	}
	assert.NotContains(t, logs.String(), "hidden-songs.example")
	assert.NotContains(t, logs.String(), "song-")
}
