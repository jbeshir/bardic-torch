//go:build integration

package mysql_test

import (
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepository_ListPublicPieces_Text (A3.7) searches titles, notes and aliases, on a private database, both as
// built and after the FULLTEXT tables are rebuilt with ALTER TABLE ... FORCE, which must not change the results:
// the indexes are built with the server's default stopwords, which a rebuild keeps.
func TestRepository_ListPublicPieces_Text(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)
	piece := func(slug, title, sortTitle string, notes *string, aliases ...string) {
		doc := domain.PieceDocument{Title: title, NotesMD: notes}
		for _, alias := range aliases {
			doc.Aliases = append(doc.Aliases, domain.DocPieceAlias{Text: alias, Kind: domain.PieceAliasKindAltTitle})
		}
		c.Piece(catalogtest.Piece{Doc: doc, Slug: slug, SortTitle: sortTitle})
	}
	piece(textRoverSlug, "The Wild Rover", "Wild Rover", nil, "Rover of the Hills")
	piece(textEireSlug, "Éire Abú", "", nil, "Ireland's Call")
	piece("theatre", "Theatre of Ghosts", "", nil, "Ghost Play")
	piece("oneill", "O'Neill's March", "", nil)
	piece("oisin", "Oisín's Lay", "", nil, "Lay of Oisín")
	// The wanderer is only in an alias of the morning glory, three times over, and in the lamplighter's notes once.
	piece("morning-glory", "Morning Glory", "", nil, "Wanderer's Hymn, Wanderer's Rest and Wanderer's Road")
	piece(textLampSlug, "Lamplighter", "", new("A wanderer's tune from the hills."))
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Unapproved Rover"}, Slug: "unapproved-rover",
		Unapproved: true})

	repo := mysql.New(db)
	cases := []struct {
		name string
		q    string
		sort domain.PieceSort
		want []string
	}{
		{name: "title", q: "rover", want: []string{textRoverSlug}},
		{name: "alias", q: "ireland", want: []string{textEireSlug}},
		{name: "notes", q: "tune", want: []string{textLampSlug}},
		{name: "notes_and_alias", q: "hills", sort: domain.PieceSortTitle, want: []string{textLampSlug, textRoverSlug}},
		{name: "accent_insensitive", q: "eire", want: []string{textEireSlug}},
		{name: "apostrophe", q: "O'Neill", want: []string{"oneill"}},
		{name: "stopword_dropped", q: "the wild", want: []string{textRoverSlug}},
		{name: "every_token_required", q: "wild ghosts", want: []string{}},
		// Operators are inert: as words, "-rover" would exclude the rover and "ghost*" match nothing more.
		{name: "operators_inert", q: `+wild -rover`, want: []string{textRoverSlug}},
		{name: "quotes_and_groups_inert", q: `"(ghosts)" ~theatre <`, want: []string{"theatre"}},
		// Only stopwords or short words: a prefix of the title or an alias, by sort title.
		{name: "stopword_prefix", q: "the", want: []string{"theatre", textRoverSlug}},
		{name: "short_prefix", q: "Oi", want: []string{"oisin"}},
		{name: "accent_insensitive_prefix", q: "ei", want: []string{textEireSlug}},
		{name: "alias_prefix", q: "Ro", want: []string{textRoverSlug}},
		// The alias-only hit, with the higher score, ranks before the notes hit; by title it is after.
		{name: "alias_scores", q: "wanderer", want: []string{"morning-glory", textLampSlug}},
		{name: "alias_scores_by_title", q: "wanderer", sort: domain.PieceSortTitle,
			want: []string{textLampSlug, "morning-glory"}},
	}
	check := func(t *testing.T) {
		t.Helper()

		for _, tc := range cases {
			search := mustSearch(t, tc.q)
			sort := tc.sort
			if sort == "" {
				sort = domain.PieceSortRelevance
			}
			got, err := repo.ListPublicPieces(t.Context(), datasources.PieceQuery{Search: &search, Sort: sort,
				Page: domain.Page{Limit: 10}})
			require.NoError(t, err, tc.name)
			slugs := []string{}
			for _, p := range got.Pieces {
				slugs = append(slugs, p.Slug)
			}
			assert.Equal(t, tc.want, slugs, tc.name)
			assert.Equal(t, int64(len(tc.want)), got.Total, tc.name)
		}
	}

	t.Run("as_built", check)
	for _, table := range []string{"pieces", "piece_aliases"} {
		_, err := db.ExecContext(t.Context(), "ALTER TABLE "+table+" FORCE")
		require.NoError(t, err)
	}
	t.Run("after_rebuild", check)
}

// TestSearchStatements_ExecutionTimeLimit checks that the hint every search statement carries has the server stop
// a statement after its limit, and that the error is reported as datasources.ErrQueryTimeout.
func TestSearchStatements_ExecutionTimeLimit(t *testing.T) {
	t.Parallel()

	db, _ := searches.Get(t)
	start := time.Now()
	err := mysql.RunSlowStatementWithHint(t.Context(), db)
	require.ErrorIs(t, err, datasources.ErrQueryTimeout)
	assert.Less(t, time.Since(start), 10*time.Second)
}

// Slugs of the text search fixture used more than twice.
const (
	textRoverSlug = "wild-rover"
	textEireSlug  = "eire"
	textLampSlug  = "lamplighter"
)
