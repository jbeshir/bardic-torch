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
