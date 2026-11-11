//go:build integration

package mysql_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
)

func TestRepository_LinkReport_OneOpenPerUserAndLinkAndHideLink(t *testing.T) {
	db := mysqltest.NewDatabase(t)
	repo := mysql.New(db)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	const url = "https://example.org/kettle"
	catalogtest.New(t, db).Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Tin Kettle",
		Links: []domain.DocPieceLink{{DocLink: domain.DocLink{URL: url, Roles: []domain.LinkRole{domain.LinkRoleLyrics}}}}}})
	ensureUser(t, repo, "auth0|reporter", "Morwen")
	ensureUser(t, repo, "auth0|mod", "Mod")
	report := datasources.NewLinkReport{ReporterUserID: "auth0|reporter", NormalizedURL: url,
		Reason: domain.LinkReportReasonCopyright, Now: now}

	id, err := repo.InsertLinkReport(t.Context(), report)
	require.NoError(t, err)
	_, err = repo.InsertLinkReport(t.Context(), report)
	require.ErrorIs(t, err, datasources.ErrAlreadyReported)

	require.NoError(t, repo.CloseLinkReport(t.Context(), datasources.Closing{ModeratorUserID: "auth0|mod", ID: id,
		Status: domain.ReportStatusResolved, Now: now}, true))
	link, err := repo.GetModerationLink(t.Context(), url)
	require.NoError(t, err)
	assert.True(t, link.IsHidden)
	require.Len(t, link.Attachments, 1)
	assert.Equal(t, "tin-kettle", link.Attachments[0].Slug)
	_, err = repo.InsertLinkReport(t.Context(), report)
	require.ErrorIs(t, err, datasources.ErrNotFound, "a hidden link is not reportable")
}

func TestRepository_ListContactRequests_PurgesAfter180Days(t *testing.T) {
	repo := mysql.New(mysqltest.NewDatabase(t))
	ensureUser(t, repo, "auth0|mod", "Mod")
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour

	resolvedAgo := func(days int) {
		id, err := repo.InsertContactRequest(t.Context(), datasources.NewContactRequest{Kind: domain.ContactKindGeneral,
			Email: "wren@example.org", Message: "Hello.", Now: now.Add(-300 * day)})
		require.NoError(t, err)
		require.NoError(t, repo.CloseContactRequest(t.Context(), datasources.Closing{ModeratorUserID: "auth0|mod",
			ID: id, Status: domain.ReportStatusResolved, Now: now.Add(-time.Duration(days) * day)}))
	}
	resolvedAgo(181)
	resolvedAgo(179)

	list, err := repo.ListContactRequests(t.Context(), domain.ReportStatusResolved, domain.Page{Limit: 10}, now)
	require.NoError(t, err)
	require.Len(t, list.Requests, 2)
	// Oldest first: the one resolved 181 days ago, then the one resolved 179 days ago.
	old, recent := list.Requests[0], list.Requests[1]
	assert.True(t, old.Purged)
	assert.Nil(t, old.Email)
	assert.Nil(t, old.Message)
	assert.False(t, recent.Purged)
	assert.Equal(t, "wren@example.org", *recent.Email)
}
