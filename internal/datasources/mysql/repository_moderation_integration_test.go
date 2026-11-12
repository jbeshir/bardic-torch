//go:build integration

package mysql_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
)

func TestRepository_ModerationLink_FoundByURLAndHidden(t *testing.T) {
	db := mysqltest.NewDatabase(t)
	repo := mysql.New(db)
	const url = "https://example.org/kettle"
	catalogtest.New(t, db).Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Tin Kettle",
		Links: []domain.DocPieceLink{{DocLink: domain.DocLink{URL: url, Roles: []domain.LinkRole{domain.LinkRoleLyrics}}}}}})

	link, err := repo.GetModerationLink(t.Context(), url)
	require.NoError(t, err)
	require.False(t, link.IsHidden)
	require.Len(t, link.Attachments, 1)
	assert.Equal(t, "tin-kettle", link.Attachments[0].Slug)

	var id int64
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT id FROM links WHERE url = ?", url).Scan(&id))
	require.NoError(t, repo.SetLinkHidden(t.Context(), id, true))
	link, err = repo.GetModerationLink(t.Context(), url)
	require.NoError(t, err)
	assert.True(t, link.IsHidden)
}
