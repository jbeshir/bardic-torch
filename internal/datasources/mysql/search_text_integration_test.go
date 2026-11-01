//go:build integration

package mysql_test

import (
	"strings"
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

// TestRepository_ListPublicPieces_Text searches titles, notes and aliases, on a private database, both as
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
	piece(textHeronSlug, "The Wild Heron", "Wild Heron", nil, "Heron of the Hills")
	piece(textEireSlug, "Éire Abú", "", nil, "Ireland's Call")
	piece("theatre", "Theatre of Ghosts", "", nil, "Ghost Play")
	piece("oneill", "O'Neill's March", "", nil)
	piece("oisin", "Oisín's Lay", "", nil, "Lay of Oisín")
	// The wanderer is only in an alias of the morning glory, three times over, and in the lamplighter's notes once.
	piece("morning-glory", "Morning Glory", "", nil, "Wanderer's Hymn, Wanderer's Rest and Wanderer's Road")
	piece(textLampSlug, "Lamplighter", "", new("A wanderer's tune from the hills."))
	// InnoDB indexes "ember_glow" as one word, and never indexes a word longer than 84 characters.
	piece("ember-glow", "Ember_Glow Reel", "", nil)
	piece("long-word", longWord+" Reel", "", nil)
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Unapproved Heron"}, Slug: "unapproved-heron",
		Unapproved: true})

	repo := mysql.New(db)
	cases := []struct {
		name string
		q    string
		sort domain.PieceSort
		want []string
	}{
		{name: "title", q: "heron", want: []string{textHeronSlug}},
		{name: "alias", q: "ireland", want: []string{textEireSlug}},
		{name: "notes", q: "tune", want: []string{textLampSlug}},
		{name: "notes_and_alias", q: "hills", sort: domain.PieceSortTitle, want: []string{textLampSlug, textHeronSlug}},
		{name: "accent_insensitive", q: "eire", want: []string{textEireSlug}},
		{name: "apostrophe", q: "O'Neill", want: []string{"oneill"}},
		{name: "stopword_dropped", q: "the wild", want: []string{textHeronSlug}},
		{name: "every_token_required", q: "wild ghosts", want: []string{}},
		// Operators are inert: as words, "-heron" would exclude the heron and "ghost*" match nothing more.
		{name: "operators_inert", q: `+wild -heron`, want: []string{textHeronSlug}},
		{name: "quotes_and_groups_inert", q: `"(ghosts)" ~theatre <`, want: []string{"theatre"}},
		// Only stopwords or short words: a prefix of the title or an alias, by sort title.
		{name: "stopword_prefix", q: "the", want: []string{"theatre", textHeronSlug}},
		{name: "short_prefix", q: "Oi", want: []string{"oisin"}},
		{name: "accent_insensitive_prefix", q: "ei", want: []string{textEireSlug}},
		{name: "alias_prefix", q: "He", want: []string{textHeronSlug}},
		// The alias-only hit, with the higher score, ranks before the notes hit; by title it is after.
		{name: "alias_scores", q: "wanderer", want: []string{"morning-glory", textLampSlug}},
		{name: "alias_scores_by_title", q: "wanderer", sort: domain.PieceSortTitle,
			want: []string{textLampSlug, "morning-glory"}},
		// An underscore joins words, as in the index; a word too long to be indexed falls back to a prefix match;
		// a query of combining marks alone has no word, and its prefix match finds nothing.
		{name: "underscore_joins", q: "ember_glow", want: []string{"ember-glow"}},
		{name: "too_long_word_prefix", q: longWord, want: []string{"long-word"}},
		{name: "too_long_word_ignored", q: longWord + " reel", sort: domain.PieceSortTitle,
			want: []string{"ember-glow", "long-word"}},
		{name: "marks_only", q: "\u0301\u0301\u0301", want: []string{}},
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
	textHeronSlug = "wild-heron"
	textEireSlug  = "eire"
	textLampSlug  = "lamplighter"
)

// TestRepository_ListPublicPersons_AliasMatchesUnscored checks that a person matched only by a public alias
// scores 0, ranking after every name match and then by sort name, however often the alias repeats the word: the
// alias index holds hidden aliases too, and a score over it would depend on them.
func TestRepository_ListPublicPersons_AliasMatchesUnscored(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Zed Moor", Aliases: []domain.DocPersonAlias{
		{Name: "Smith of Smithfield, Smith of Moor", Kind: domain.PersonAliasKindNickname, IsPublic: true},
		{Name: "Smith Smithson", Kind: domain.PersonAliasKindLegal},
		{Name: "Old Smith", Kind: domain.PersonAliasKindLegal},
	}}})
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Ada Moor", Aliases: []domain.DocPersonAlias{
		{Name: "The Smith of Moor", Kind: domain.PersonAliasKindNickname, IsPublic: true},
	}}})
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Wynn Smith"}})

	search := mustSearch(t, "smith")
	got, err := mysql.New(db).ListPublicPersons(t.Context(), datasources.PersonQuery{Search: &search,
		Sort: domain.PersonSortRelevance, Page: domain.Page{Limit: 10}})
	require.NoError(t, err)
	slugs := []string{}
	for _, p := range got.Persons {
		slugs = append(slugs, p.Slug)
	}
	assert.Equal(t, []string{"wynn-smith", "ada-moor", "zed-moor"}, slugs)
}

// longWord is a word longer than InnoDB's longest FULLTEXT token (innodb_ft_max_token_size).
var longWord = "Lo" + strings.Repeat("o", domain.MaxSearchTokenLength) + "ng"
