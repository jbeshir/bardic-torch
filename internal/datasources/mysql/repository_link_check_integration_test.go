//go:build integration

package mysql_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// linkCheckFixture is an empty catalogue for the link checker's reads and writes.
type linkCheckFixture struct {
	db   *sql.DB
	repo *mysql.Repository
	now  time.Time
}

func newLinkCheckFixture(t *testing.T) linkCheckFixture {
	t.Helper()
	db := catalogtest.Writable(t)

	return linkCheckFixture{db: db, repo: mysql.New(db), now: time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)}
}

// linkRow is a links row to insert: the zero value is a visible, never checked link.
type linkRow struct {
	hidden           bool
	liveness         string
	failures         int
	lastCheckedAt    *time.Time
	archivedURL      *string
	archiveCheckedAt *time.Time
}

// insert stores a link with a URL numbered after its ID, returning the ID.
func (f linkCheckFixture) insert(t *testing.T, row linkRow) int64 {
	t.Helper()
	if row.liveness == "" {
		row.liveness = "unknown"
	}
	res, err := f.db.ExecContext(t.Context(), "INSERT INTO links (url, normalized_url, liveness, liveness_failures, "+
		"last_checked_at, archived_url, archive_checked_at, is_hidden) VALUES ('https://example.org/x', "+
		"UUID(), ?, ?, ?, ?, ?, ?)", row.liveness, row.failures, row.lastCheckedAt, row.archivedURL,
		row.archiveCheckedAt, row.hidden)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), "UPDATE links SET url = ?, normalized_url = ? WHERE id = ?",
		fmt.Sprintf("https://example.org/%d", id), fmt.Sprintf("https://example.org/%d", id), id)
	require.NoError(t, err)

	return id
}

// state returns the checker's columns of the link id.
func (f linkCheckFixture) state(t *testing.T, id int64) (liveness string, failures int, checked, archiveChecked *time.Time,
	archived *string) {
	t.Helper()
	var (
		lc, ac sql.NullTime
		au     sql.NullString
	)
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT liveness, liveness_failures, last_checked_at, "+
		"archive_checked_at, archived_url FROM links WHERE id = ?", id).Scan(&liveness, &failures, &lc, &ac, &au))
	if lc.Valid {
		checked = &lc.Time
	}
	if ac.Valid {
		archiveChecked = &ac.Time
	}
	if au.Valid {
		archived = &au.String
	}

	return liveness, failures, checked, archiveChecked, archived
}

func TestRepository_ListLinksDueForCheck(t *testing.T) {
	t.Parallel()

	f := newLinkCheckFixture(t)
	cutoff := f.now.Add(-7 * 24 * time.Hour)
	at := func(d time.Duration) *time.Time { return new(cutoff.Add(d)) }

	f.insert(t, linkRow{hidden: true})
	f.insert(t, linkRow{lastCheckedAt: at(time.Hour)})
	f.insert(t, linkRow{lastCheckedAt: at(0)})
	older := f.insert(t, linkRow{lastCheckedAt: at(-48 * time.Hour), liveness: livenessOK, failures: 2})
	oldest := f.insert(t, linkRow{lastCheckedAt: at(-72 * time.Hour)})
	sameAge := f.insert(t, linkRow{lastCheckedAt: at(-48 * time.Hour)})
	neverB := f.insert(t, linkRow{})
	neverA := f.insert(t, linkRow{liveness: livenessDead})

	cases := []struct {
		name  string
		limit int32
		want  []int64
	}{
		{name: "never checked first, then the oldest, then by ID; hidden, recent and cutoff skipped", limit: 100,
			want: []int64{neverB, neverA, oldest, older, sameAge}},
		{name: "limit", limit: 3, want: []int64{neverB, neverA, oldest}},
		{name: "limit of one", limit: 1, want: []int64{neverB}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.repo.ListLinksDueForCheck(t.Context(), cutoff, tc.limit)
			require.NoError(t, err)
			ids := make([]int64, 0, len(got))
			for _, target := range got {
				ids = append(ids, target.ID)
			}
			assert.Equal(t, tc.want, ids)
		})
	}

	got, err := f.repo.ListLinksDueForCheck(t.Context(), cutoff, 100)
	require.NoError(t, err)
	byID := map[int64]datasources.LinkCheckTarget{}
	for _, target := range got {
		byID[target.ID] = target
	}
	assert.Equal(t, datasources.LinkCheckTarget{ID: older, URL: fmt.Sprintf("https://example.org/%d", older),
		Liveness: domain.LivenessOK, Failures: 2, LastCheckedAt: at(-48 * time.Hour)}, byID[older])
	assert.Nil(t, byID[neverA].LastCheckedAt)
	assert.Equal(t, domain.LivenessDead, byID[neverA].Liveness)
}

func TestRepository_UpdateLinkCheck_CompareAndSet(t *testing.T) {
	t.Parallel()

	f := newLinkCheckFixture(t)
	stamp := f.now.Add(-10 * 24 * time.Hour)
	checkedAt := f.now

	cases := []struct {
		name        string
		row         linkRow
		expected    *time.Time
		wantWritten bool
	}{
		{name: "matching stamp writes", row: linkRow{lastCheckedAt: &stamp}, expected: &stamp, wantWritten: true},
		{name: "never checked link with a nil stamp writes", row: linkRow{}, expected: nil, wantWritten: true},
		{name: "stale stamp writes nothing", row: linkRow{lastCheckedAt: &stamp},
			expected: new(stamp.Add(-time.Hour))},
		{name: "nil stamp on a checked link writes nothing", row: linkRow{lastCheckedAt: &stamp}},
		{name: "stamp on a never checked link writes nothing", row: linkRow{}, expected: &stamp},
		{name: "hidden link writes nothing", row: linkRow{hidden: true, lastCheckedAt: &stamp}, expected: &stamp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.row.archivedURL = new("https://web.archive.org/web/2020/https://example.org/kept")
			tc.row.liveness = livenessOK
			id := f.insert(t, tc.row)
			written, err := f.repo.UpdateLinkCheck(t.Context(), datasources.LinkCheckResult{ID: id,
				Liveness: domain.LivenessDead, Failures: 3, CheckedAt: checkedAt, ExpectedLastCheckedAt: tc.expected})
			require.NoError(t, err)
			assert.Equal(t, tc.wantWritten, written)

			liveness, failures, checked, archiveChecked, archived := f.state(t, id)
			if tc.wantWritten {
				assert.Equal(t, livenessDead, liveness)
				assert.Equal(t, 3, failures)
				assert.Equal(t, &checkedAt, checked)
			} else {
				assert.Equal(t, livenessOK, liveness)
				assert.Zero(t, failures)
				assert.Equal(t, tc.row.lastCheckedAt, checked)
			}
			assert.Nil(t, archiveChecked, "only the check columns are written")
			assert.Equal(t, tc.row.archivedURL, archived, "only the check columns are written")
		})
	}
}

func TestRepository_ListLinksDueForArchive(t *testing.T) {
	t.Parallel()

	f := newLinkCheckFixture(t)
	cutoff := f.now.Add(-30 * 24 * time.Hour)
	archived := new("https://web.archive.org/web/2020/https://example.org/x")

	f.insert(t, linkRow{hidden: true, liveness: livenessDead})
	f.insert(t, linkRow{archivedURL: archived, liveness: livenessDead})
	f.insert(t, linkRow{archiveCheckedAt: new(cutoff.Add(time.Hour))})
	f.insert(t, linkRow{archiveCheckedAt: new(cutoff)})
	staleDead := f.insert(t, linkRow{liveness: livenessDead, archiveCheckedAt: new(cutoff.Add(-24 * time.Hour))})
	neverOK := f.insert(t, linkRow{liveness: livenessOK})
	neverDead := f.insert(t, linkRow{liveness: livenessDead})
	stale := f.insert(t, linkRow{archiveCheckedAt: new(cutoff.Add(-48 * time.Hour))})

	cases := []struct {
		name  string
		limit int32
		want  []int64
	}{
		{name: "dead first, then never looked up, then oldest", limit: 100,
			want: []int64{neverDead, staleDead, neverOK, stale}},
		{name: "limit", limit: 2, want: []int64{neverDead, staleDead}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.repo.ListLinksDueForArchive(t.Context(), cutoff, tc.limit)
			require.NoError(t, err)
			ids := make([]int64, 0, len(got))
			for _, target := range got {
				ids = append(ids, target.ID)
				assert.Equal(t, fmt.Sprintf("https://example.org/%d", target.ID), target.URL)
			}
			assert.Equal(t, tc.want, ids)
		})
	}
}

func TestRepository_UpdateLinkArchive_NeverClears(t *testing.T) {
	t.Parallel()

	f := newLinkCheckFixture(t)
	first := new("https://web.archive.org/web/2020/https://example.org/x")
	checkedAt := f.now

	id := f.insert(t, linkRow{})
	written, err := f.repo.UpdateLinkArchive(t.Context(), datasources.LinkArchiveResult{ID: id, CheckedAt: checkedAt})
	require.NoError(t, err)
	assert.True(t, written)
	_, _, _, archiveChecked, archived := f.state(t, id)
	assert.Equal(t, &checkedAt, archiveChecked, "a lookup that found nothing is recorded")
	assert.Nil(t, archived)

	later := checkedAt.Add(24 * time.Hour)
	written, err = f.repo.UpdateLinkArchive(t.Context(), datasources.LinkArchiveResult{ID: id, ArchivedURL: first,
		CheckedAt: later})
	require.NoError(t, err)
	assert.True(t, written)
	_, _, _, archiveChecked, archived = f.state(t, id)
	assert.Equal(t, &later, archiveChecked)
	assert.Equal(t, first, archived)

	muchLater := later.Add(24 * time.Hour)
	written, err = f.repo.UpdateLinkArchive(t.Context(), datasources.LinkArchiveResult{ID: id, CheckedAt: muchLater})
	require.NoError(t, err)
	assert.True(t, written)
	_, _, _, archiveChecked, archived = f.state(t, id)
	assert.Equal(t, &muchLater, archiveChecked)
	assert.Equal(t, first, archived, "a lookup that found nothing never clears the copy")

	hidden := f.insert(t, linkRow{hidden: true})
	written, err = f.repo.UpdateLinkArchive(t.Context(), datasources.LinkArchiveResult{ID: hidden, ArchivedURL: first,
		CheckedAt: checkedAt})
	require.NoError(t, err)
	assert.False(t, written, "a hidden link is never written")
	_, _, _, archiveChecked, archived = f.state(t, hidden)
	assert.Nil(t, archiveChecked)
	assert.Nil(t, archived)
}

// livenessDead is the value of the liveness column of a link that no longer works.
const livenessDead = "dead"

// livenessOK is the value of the liveness column of a link that works.
const livenessOK = "ok"
