package command_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

type fakeLinks struct {
	datasources.LinkChecks
	due, dueArchive           []datasources.DueLink
	checkedBefore, archiveCut time.Time
	checks                    map[uint64]check
	archives                  map[uint64]string
}

type check struct {
	liveness domain.Liveness
	failures uint8
}

func (f *fakeLinks) ListLinksDueForCheck(_ context.Context, before time.Time, _ int32) ([]datasources.DueLink, error) {
	f.checkedBefore = before
	return f.due, nil
}

func (f *fakeLinks) ListLinksDueForArchive(_ context.Context, before time.Time, _ int32) ([]datasources.DueLink, error) {
	f.archiveCut = before
	return f.dueArchive, nil
}

func (f *fakeLinks) UpdateLinkCheck(_ context.Context, id uint64, l domain.Liveness, n uint8, _ time.Time) error {
	f.checks[id] = check{l, n}
	return nil
}

func (f *fakeLinks) UpdateLinkArchive(_ context.Context, id uint64, archived string, _ time.Time) error {
	f.archives[id] = archived
	return nil
}

type fakeProber map[string]datasources.LinkOutcome

func (p fakeProber) Probe(_ context.Context, url string) datasources.LinkOutcome { return p[url] }

type fakeArchive struct{ asked []string }

func (a *fakeArchive) Available(_ context.Context, url string) (string, error) {
	a.asked = append(a.asked, url)
	if url == "https://down.example/" {
		return "", datasources.ErrArchiveUnavailable
	}
	if url == "https://has.example/" {
		return "https://web.archive.org/web/2020/https://has.example/", nil
	}
	return "", nil
}

func TestCheckLinksClassification(t *testing.T) {
	const (
		ok, gone, blocked, unreachable = datasources.LinkOutcomeOK, datasources.LinkOutcomeGone,
			datasources.LinkOutcomeBlocked, datasources.LinkOutcomeUnreachable
	)
	tests := []struct {
		name    string
		link    datasources.DueLink
		outcome datasources.LinkOutcome
		want    check
	}{
		{"ok resets failures", datasources.DueLink{Liveness: domain.LivenessUnknown, Failures: 2}, ok,
			check{domain.LivenessOK, 0}},
		{"first gone", datasources.DueLink{Liveness: domain.LivenessOK}, gone, check{domain.LivenessOK, 1}},
		{"second gone is not dead", datasources.DueLink{Liveness: domain.LivenessOK, Failures: 1}, gone,
			check{domain.LivenessOK, 2}},
		{"third gone is dead", datasources.DueLink{Liveness: domain.LivenessOK, Failures: 2}, gone,
			check{domain.LivenessDead, 3}},
		{"blocked", datasources.DueLink{Liveness: domain.LivenessOK, Failures: 1}, blocked,
			check{domain.LivenessBlocked, 1}},
		{"unreachable changes nothing", datasources.DueLink{Liveness: domain.LivenessOK, Failures: 1}, unreachable,
			check{domain.LivenessOK, 1}},
	}
	for _, tt := range tests {
		tt.link.ID, tt.link.URL = 1, "https://a.example/"
		links := &fakeLinks{due: []datasources.DueLink{tt.link}, checks: map[uint64]check{}}
		checker := &command.CheckLinks{Links: links, Prober: fakeProber{tt.link.URL: tt.outcome},
			Archive: &fakeArchive{}, Now: time.Now}
		_, err := checker.Execute(context.Background(), command.CheckLinksRequest{MaxDuration: time.Minute})
		require.NoError(t, err, tt.name)
		assert.Equal(t, tt.want, links.checks[1], tt.name)
	}
}

func TestCheckLinksArchive(t *testing.T) {
	now := time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC)
	links := &fakeLinks{
		dueArchive: []datasources.DueLink{{ID: 1, URL: "https://has.example/"}, {ID: 2, URL: "https://none.example/"},
			{ID: 3, URL: "https://down.example/"}},
		archives: map[uint64]string{},
	}
	archive := &fakeArchive{}
	checker := &command.CheckLinks{Links: links, Prober: fakeProber{}, Archive: archive, Now: func() time.Time { return now }}

	_, err := checker.Execute(context.Background(), command.CheckLinksRequest{MaxDuration: time.Minute})
	require.NoError(t, err)

	// Only links not looked up for 30 days are asked for, and what is found is stored; a failed lookup is not.
	assert.Equal(t, now.Add(-30*24*time.Hour), links.archiveCut)
	assert.Equal(t, now.Add(-7*24*time.Hour), links.checkedBefore)
	assert.Equal(t, map[uint64]string{1: "https://web.archive.org/web/2020/https://has.example/", 2: ""}, links.archives)
	assert.Len(t, archive.asked, 3)
}

func TestCheckLinksStopsAtMaxDuration(t *testing.T) {
	links := &fakeLinks{due: []datasources.DueLink{{ID: 1, URL: "https://a.example/"}}, checks: map[uint64]check{}}
	checker := &command.CheckLinks{Links: links, Prober: fakeProber{}, Archive: &fakeArchive{}, Now: time.Now}

	_, err := checker.Execute(context.Background(), command.CheckLinksRequest{MaxDuration: -time.Second})
	require.NoError(t, err)
	assert.Empty(t, links.checks)
}
