//go:build integration

package mysql_test

import (
	"sync"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// report reports url as reporterID, returning the receipt and whether the report is new.
func (f *editingFixture) report(t *testing.T, reporterID, url string) (domain.LinkReportReceipt, bool, error) {
	t.Helper()

	return f.repo.InsertLinkReport(t.Context(), datasources.NewLinkReport{ReporterUserID: reporterID, URL: url,
		Reason: domain.LinkReportReasonBroken, Message: new("The page is gone."), Now: f.now})
}

// insertOpenReport stores an open report of the link linkID by reporterID directly, returning its ID.
func (f *editingFixture) insertOpenReport(t *testing.T, linkID int64, reporterID string) int64 {
	t.Helper()
	result, err := f.db.ExecContext(t.Context(), "INSERT INTO link_reports (link_id, reporter_user_id, reason, "+
		"created_at) VALUES (?, ?, 'other', ?)", linkID, reporterID, f.now)
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)

	return id
}

// linkID returns the ID of the link with url.
func (f *editingFixture) linkID(t *testing.T, url string) int64 {
	t.Helper()
	link, err := f.repo.GetModerationLink(t.Context(), url)
	require.NoError(t, err)

	return link.LinkID
}

// TestRepository_LinkReports walks the link report queries on one fixture, each step on the state the previous
// left: reporting a visible link (and only a visible one), the repeat and cap rules, the inbox, resolving with
// and without hiding the link, and hiding and showing a link.
func TestRepository_LinkReports(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	lyrics, site, hidden := f.linkID(t, songLyricsURL), f.linkID(t, morwenSiteURL), f.linkID(t, songHiddenURL)

	// A hidden link, an unknown URL and a string that is not a URL are all the same not found.
	for _, url := range []string{songHiddenURL, morwenHidden, "https://example.org/nothing-here", "not a url"} {
		_, _, err := f.report(t, wrenUserID, url)
		require.ErrorIs(t, err, datasources.ErrNotFound, url)
	}

	wren, created, err := f.report(t, wrenUserID, songLyricsURL)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, domain.LinkReportReceipt{ID: wren.ID, Status: domain.LinkReportStatusOpen, CreatedAt: f.now}, wren)
	again, created, err := f.report(t, wrenUserID, "https://EXAMPLE.org/wren/lyrics")
	require.NoError(t, err)
	assert.False(t, created, "a repeat by the same reporter, in another spelling, returns the open report")
	assert.Equal(t, wren, again)
	ash, created, err := f.report(t, ashUserID, songLyricsURL)
	require.NoError(t, err)
	assert.True(t, created, "another reporter's report of the same link is their own")

	// Wren reaches the cap of open reports; a repeat is still answered with the open report.
	for range domain.MaxOpenLinkReports - 1 {
		f.insertOpenReport(t, hidden, wrenUserID)
	}
	_, _, err = f.report(t, wrenUserID, morwenSiteURL)
	require.ErrorIs(t, err, datasources.ErrReportLimitReached)
	again, created, err = f.report(t, wrenUserID, songLyricsURL)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, wren, again)

	t.Run("inbox", func(t *testing.T) {
		open := domain.LinkReportStatusOpen
		list, err := f.repo.ListLinkReports(t.Context(),
			datasources.LinkReportQuery{Status: &open, Page: domain.Page{Limit: 2}})
		require.NoError(t, err)
		assert.Equal(t, int64(domain.MaxOpenLinkReports+1), list.Total)
		require.Len(t, list.Reports, 2)
		first := list.Reports[0]
		assert.Equal(t, wren.ID, first.ID, "oldest first, then by ID")
		assert.Equal(t, ash.ID, list.Reports[1].ID)
		assert.Equal(t, domain.ModerationAuthor{ID: wrenUserID, DisplayName: wrenDisplay, Role: domain.RoleContributor},
			first.Reporter)
		assert.Equal(t, domain.LinkReportReasonBroken, first.Reason)
		assert.Equal(t, new("The page is gone."), first.Message)
		assert.Equal(t, lyrics, first.Link.LinkID)
		assert.Equal(t, int64(2), first.Link.OpenReportCount)
		assert.Nil(t, first.ResolvedBy)

		dismissed := domain.LinkReportStatusDismissed
		list, err = f.repo.ListLinkReports(t.Context(),
			datasources.LinkReportQuery{Status: &dismissed, Page: domain.Page{Limit: 10}})
		require.NoError(t, err)
		assert.Equal(t, domain.LinkReportList{Reports: []domain.LinkReport{}, Total: 0}, list)
	})

	t.Run("dismiss", func(t *testing.T) {
		note := "The page works."
		report, err := f.repo.ResolveLinkReport(t.Context(), datasources.ResolveLinkReportRequest{
			ModeratorUserID: rowanUserID, ReportID: ash.ID, Status: domain.LinkReportStatusDismissed, Note: &note,
			Now: f.now,
		})
		require.NoError(t, err)
		assert.Equal(t, domain.LinkReportStatusDismissed, report.Status)
		assert.Equal(t, &note, report.Note)
		assert.Equal(t, &f.now, report.ResolvedAt)
		require.NotNil(t, report.ResolvedBy)
		assert.Equal(t, rowanUserID, report.ResolvedBy.ID)
		assert.Equal(t, domain.RoleModerator, report.ResolvedBy.Role)
		assert.False(t, report.Link.IsHidden)

		_, err = f.repo.ResolveLinkReport(t.Context(), datasources.ResolveLinkReportRequest{
			ModeratorUserID: rowanUserID, ReportID: ash.ID, Status: domain.LinkReportStatusResolved, Now: f.now,
		})
		require.ErrorIs(t, err, datasources.ErrReportNotOpen)
		for _, id := range []int64{0, 1 << 40} {
			_, err = f.repo.ResolveLinkReport(t.Context(), datasources.ResolveLinkReportRequest{
				ModeratorUserID: rowanUserID, ReportID: id, Status: domain.LinkReportStatusResolved, Now: f.now,
			})
			require.ErrorIs(t, err, datasources.ErrNotFound, id)
		}
	})

	t.Run("resolve_and_hide", func(t *testing.T) {
		other := f.insertOpenReport(t, lyrics, ashUserID)
		report, err := f.repo.ResolveLinkReport(t.Context(), datasources.ResolveLinkReportRequest{
			ModeratorUserID: rowanUserID, ReportID: wren.ID, Status: domain.LinkReportStatusResolved, HideLink: true,
			Now: f.now,
		})
		require.NoError(t, err)
		assert.Equal(t, domain.LinkReportStatusResolved, report.Status)
		assert.Nil(t, report.Note, "the resolved report keeps the moderator's note, none here")
		assert.True(t, report.Link.IsHidden)
		assert.Equal(t, int64(0), report.Link.OpenReportCount, "hiding resolved the link's other open report")
		assert.Equal(t, domain.LinkHiddenNote, reportNote(t, f, other))

		_, _, err = f.report(t, ashUserID, songLyricsURL)
		require.ErrorIs(t, err, datasources.ErrNotFound, "a hidden link cannot be reported")
	})

	t.Run("set_hidden", func(t *testing.T) {
		shown, err := f.repo.SetLinkHidden(t.Context(), datasources.SetLinkHiddenRequest{
			ModeratorUserID: rowanUserID, LinkID: lyrics, Hidden: false, Now: f.now,
		})
		require.NoError(t, err)
		assert.False(t, shown.IsHidden)
		_, created, err := f.report(t, ashUserID, songLyricsURL)
		require.NoError(t, err)
		assert.True(t, created, "a shown link can be reported again")

		open := f.insertOpenReport(t, site, ashUserID)
		hiddenSite, err := f.repo.SetLinkHidden(t.Context(), datasources.SetLinkHiddenRequest{
			ModeratorUserID: rowanUserID, LinkID: site, Hidden: true, Now: f.now,
		})
		require.NoError(t, err)
		assert.True(t, hiddenSite.IsHidden)
		assert.Equal(t, morwenSiteURL, hiddenSite.URL)
		assert.Equal(t, int64(0), hiddenSite.OpenReportCount)
		assert.Equal(t, domain.LinkHiddenNote, reportNote(t, f, open))

		_, err = f.repo.SetLinkHidden(t.Context(), datasources.SetLinkHiddenRequest{
			ModeratorUserID: rowanUserID, LinkID: 1 << 40, Hidden: true, Now: f.now,
		})
		require.ErrorIs(t, err, datasources.ErrNotFound)
	})
}

// reportNote returns the resolution note of the link report id, failing the test if it has none.
func reportNote(t *testing.T, f *editingFixture, id int64) string {
	t.Helper()
	var note *string
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT resolution_note FROM link_reports WHERE id = ?", id).
		Scan(&note))
	require.NotNil(t, note)

	return *note
}

// TestRepository_LinkHideResolveRace hides a link while resolving one of its reports with hide_link, 50 times,
// both released together. Both lock the link row first, so they never deadlock: setLinkHidden always
// succeeds, and resolveLinkReport succeeds unless the hide committed first and resolved the report, when it gets
// ErrReportNotOpen. Either way the link ends hidden and the report resolved.
func TestRepository_LinkHideResolveRace(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	link := f.linkID(t, songLyricsURL)

	for i := range 50 {
		_, err := f.repo.SetLinkHidden(t.Context(), datasources.SetLinkHiddenRequest{
			ModeratorUserID: rowanUserID, LinkID: link, Hidden: false, Now: f.now,
		})
		require.NoError(t, err)
		report := f.insertOpenReport(t, link, wrenUserID)

		var (
			start               sync.WaitGroup
			done                sync.WaitGroup
			hideErr, resolveErr error
		)
		start.Add(1)
		done.Add(2)
		go func() {
			defer done.Done()
			start.Wait()
			_, hideErr = f.repo.SetLinkHidden(t.Context(), datasources.SetLinkHiddenRequest{
				ModeratorUserID: rowanUserID, LinkID: link, Hidden: true, Now: f.now,
			})
		}()
		go func() {
			defer done.Done()
			start.Wait()
			_, resolveErr = f.repo.ResolveLinkReport(t.Context(), datasources.ResolveLinkReportRequest{
				ModeratorUserID: rowanUserID, ReportID: report, Status: domain.LinkReportStatusResolved,
				Note: new("Taken down."), HideLink: true, Now: f.now,
			})
		}()
		start.Done()
		done.Wait()

		require.NoError(t, hideErr, "run %d", i)
		note := reportNote(t, f, report)
		if resolveErr != nil {
			require.ErrorIs(t, resolveErr, datasources.ErrReportNotOpen, "run %d", i)
			assert.Equal(t, domain.LinkHiddenNote, note, "run %d: the hide resolved the report first", i)
		} else {
			assert.Equal(t, "Taken down.", note, "run %d", i)
		}
		hiddenLink, err := f.repo.GetModerationLink(t.Context(), songLyricsURL)
		require.NoError(t, err)
		assert.True(t, hiddenLink.IsHidden, "run %d", i)
		assert.Equal(t, int64(0), hiddenLink.OpenReportCount, "run %d", i)
	}
}
