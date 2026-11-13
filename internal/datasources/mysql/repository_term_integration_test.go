//go:build integration

package mysql_test

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// termFixture is a term tree where every count and member rule has something to leave out: an unapproved term
// with an approved child (both tagging pieces), an unapproved root, an unapproved affiliation, an unapproved
// piece, an unapproved person, a no-page person, and a piece tagged with both a term and its child.
type termFixture struct {
	spears, pikes, gloomy, southmarch catalogtest.Ref
}

// Fixture strings the tests compare more than once.
const (
	ballad = "Ballad of Blades"
	cedar  = "Cedar Vane"
)

var terms = catalogtest.NewShared(func(c *catalogtest.Catalog) termFixture {
	merry := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetMood, Name: "Merry"}})
	gloomy := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetMood, Name: "Gloomy"},
		Unapproved: true})

	war := c.Term(catalogtest.Term{Doc: domain.TermDocument{
		Facet: domain.FacetTopic, Name: warName, DescriptionMD: new("Songs of war."),
	}})
	c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Peace"}})
	swords := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: swordsName,
		ParentID: &war.ID, Position: 2}})
	shields := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Shields (Topic)",
		DisplayName: "Shields", ParentID: &war.ID, Position: 1}, Slug: "shields"})
	c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Bucklers", ParentID: &shields.ID}})
	sabres := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Sabres",
		ParentID: &swords.ID}})
	spears := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Spears",
		ParentID: &war.ID}, Unapproved: true})
	pikes := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Pikes",
		ParentID: &spears.ID}})

	northmarch := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation, Name: northmarchName}})
	barony := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation,
		Name: "Northmarch Barony", ParentID: &northmarch.ID}})
	southmarch := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation, Name: "Southmarch"},
		Unapproved: true})
	c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Patriotic",
		RelatedAffiliationID: &northmarch.ID}})
	c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Loyal",
		RelatedAffiliationID: &southmarch.ID}})

	tagged := func(title string, termIDs ...int64) catalogtest.Piece {
		return catalogtest.Piece{Doc: domain.PieceDocument{Title: title, TermIDs: termIDs}}
	}
	c.Piece(tagged(ballad, swords.ID, war.ID))
	sabre := tagged("The Sabre Dance", sabres.ID)
	sabre.SortTitle = "Sabre Dance"
	c.Piece(sabre)
	c.Piece(tagged("Shield Song", shields.ID))
	c.Piece(tagged("Pike March", pikes.ID))
	c.Piece(tagged("Spear Song", spears.ID))
	c.Piece(tagged("Merry Jig", merry.ID))
	c.Piece(tagged("Gloomy Air", gloomy.ID))
	pending := tagged("War Draft", war.ID)
	pending.Unapproved = true
	c.Piece(pending)

	affiliated := func(name string, termIDs ...int64) catalogtest.Person {
		return catalogtest.Person{Doc: domain.PersonDocument{Name: name, AffiliationIDs: termIDs}}
	}
	c.Person(affiliated("Ash Moor", northmarch.ID))
	c.Person(affiliated("Birch Lowe", barony.ID))
	c.Person(affiliated(cedar, northmarch.ID, barony.ID))
	c.Person(affiliated("Dell Southby", southmarch.ID))
	noPage := affiliated(hollisName, northmarch.ID)
	noPage.Doc.Status = domain.PersonStatusPrefersNoPage
	c.Person(noPage)
	pendingPerson := affiliated("Pending Rook", northmarch.ID)
	pendingPerson.Unapproved = true
	c.Person(pendingPerson)

	return termFixture{spears: spears, pikes: pikes, gloomy: gloomy, southmarch: southmarch}
})

func termRepository(t *testing.T) (*mysql.Repository, termFixture) {
	t.Helper()

	db, fx := terms.Get(t)
	return mysql.New(db), fx
}

func termSummary(slug string, facet domain.Facet, displayName string) domain.TermSummary {
	return domain.TermSummary{Slug: slug, Facet: facet, DisplayName: displayName}
}

var (
	warSummary        = termSummary("war", domain.FacetTopic, warName)
	swordsSummary     = termSummary("swords", domain.FacetTopic, swordsName)
	northmarchSummary = termSummary("northmarch", domain.FacetAffiliation, northmarchName)
	baronySummary     = termSummary("northmarch-barony", domain.FacetAffiliation, "Northmarch Barony")

	bladesSummary = domain.PieceSummary{Slug: "ballad-of-blades", Title: ballad, Kind: domain.PieceKindSong,
		ConservationStatus: domain.ConservationStatusUnassessed}
	sabreSummary = domain.PieceSummary{Slug: "the-sabre-dance", Title: "The Sabre Dance", Kind: domain.PieceKindSong,
		ConservationStatus: domain.ConservationStatusUnassessed}
	shieldSummary = domain.PieceSummary{Slug: "shield-song", Title: "Shield Song", Kind: domain.PieceKindSong,
		ConservationStatus: domain.ConservationStatusUnassessed}
)

func listItem(summary domain.TermSummary, name string, parent *string, pieces, persons int64) domain.TermListItem {
	return domain.TermListItem{TermSummary: summary, Name: name, ParentSlug: parent,
		TermCounts: domain.TermCounts{PieceCount: pieces, PersonCount: persons}}
}

// TestRepository_ListPublicTerms checks the tree order (facets in enum order, then each term before its children and
// before its next sibling, siblings by position then display name) and the counts: with descendants, distinct,
// without unapproved pieces, no-page or unapproved persons, or anything tagged with a pending term or a term under
// one, which are not listed.
func TestRepository_ListPublicTerms(t *testing.T) {
	t.Parallel()

	repo, _ := termRepository(t)
	list, err := repo.ListPublicTerms(t.Context(), nil)
	require.NoError(t, err)

	assert.Equal(t, []domain.TermListItem{
		listItem(termSummary("merry", domain.FacetMood, "Merry"), "Merry", nil, 1, 0),
		listItem(termSummary("loyal", domain.FacetTopic, "Loyal"), "Loyal", nil, 0, 0),
		listItem(termSummary("patriotic", domain.FacetTopic, "Patriotic"), "Patriotic", nil, 0, 0),
		listItem(termSummary("peace", domain.FacetTopic, "Peace"), "Peace", nil, 0, 0),
		// The ballad is tagged with War and Swords but counts once; the pikes under pending Spears do not count.
		listItem(warSummary, warName, nil, 3, 0),
		listItem(termSummary("shields", domain.FacetTopic, "Shields"), "Shields (Topic)", new("war"), 1, 0),
		listItem(termSummary("bucklers", domain.FacetTopic, "Bucklers"), "Bucklers", new("shields"), 0, 0),
		listItem(swordsSummary, swordsName, new("war"), 2, 0),
		listItem(termSummary("sabres", domain.FacetTopic, "Sabres"), "Sabres", new("swords"), 1, 0),
		// Cedar is affiliated with both but counts once.
		listItem(northmarchSummary, northmarchName, nil, 0, 3),
		listItem(baronySummary, "Northmarch Barony", new("northmarch"), 0, 2),
	}, list)
}

func TestRepository_ListPublicTerms_Facet(t *testing.T) {
	t.Parallel()

	repo, _ := termRepository(t)
	affiliation := domain.FacetAffiliation
	list, err := repo.ListPublicTerms(t.Context(), &affiliation)
	require.NoError(t, err)
	assert.Equal(t, []domain.TermListItem{
		listItem(northmarchSummary, northmarchName, nil, 0, 3),
		listItem(baronySummary, "Northmarch Barony", new("northmarch"), 0, 2),
	}, list)

	legendaria := domain.FacetLegendaria
	list, err = repo.ListPublicTerms(t.Context(), &legendaria)
	require.NoError(t, err)
	assert.Empty(t, list)
}

var firstPage = domain.Page{Limit: domain.DefaultPageLimit}

// TestRepository_GetPublicTerm reads a term with its description, public children with counts and pieces tagged with
// it or a public descendant, each once.
func TestRepository_GetPublicTerm(t *testing.T) {
	t.Parallel()

	repo, _ := termRepository(t)
	term, err := repo.GetPublicTerm(t.Context(), "war", firstPage)
	require.NoError(t, err)
	clearUpdatedAt(t, &term.UpdatedAt)

	assert.Equal(t, domain.Term{
		TermSummary:   warSummary,
		Name:          warName,
		DescriptionMD: new("Songs of war."),
		Ancestors:     []domain.TermSummary{},
		// By position; not the pending Spears.
		Children: []domain.TermChild{
			{Term: termSummary("shields", domain.FacetTopic, "Shields"), TermCounts: domain.TermCounts{PieceCount: 1}},
			{Term: swordsSummary, TermCounts: domain.TermCounts{PieceCount: 2}},
		},
		TermCounts: domain.TermCounts{PieceCount: 3},
		Pieces:     []domain.PieceSummary{bladesSummary, sabreSummary, shieldSummary},
	}, term)
}

func TestRepository_GetPublicTerm_Ancestors(t *testing.T) {
	t.Parallel()

	repo, _ := termRepository(t)
	term, err := repo.GetPublicTerm(t.Context(), "sabres", firstPage)
	require.NoError(t, err)
	clearUpdatedAt(t, &term.UpdatedAt)

	assert.Equal(t, []domain.TermSummary{warSummary, swordsSummary}, term.Ancestors)
	assert.Empty(t, term.Children)
	assert.Equal(t, []domain.PieceSummary{sabreSummary}, term.Pieces)
}

// TestRepository_GetPublicTerm_RelatedAffiliation checks that a related affiliation is shown only if public.
func TestRepository_GetPublicTerm_RelatedAffiliation(t *testing.T) {
	t.Parallel()

	repo, _ := termRepository(t)
	patriotic, err := repo.GetPublicTerm(t.Context(), "patriotic", firstPage)
	require.NoError(t, err)
	assert.Equal(t, &northmarchSummary, patriotic.RelatedAffiliation)

	loyal, err := repo.GetPublicTerm(t.Context(), "loyal", firstPage)
	require.NoError(t, err)
	assert.Nil(t, loyal.RelatedAffiliation)
}

// TestRepository_GetPublicTerm_Affiliation checks that an affiliation term lists the public persons with a page
// affiliated with it or a descendant, each once, and no pieces.
func TestRepository_GetPublicTerm_Affiliation(t *testing.T) {
	t.Parallel()

	repo, _ := termRepository(t)
	term, err := repo.GetPublicTerm(t.Context(), "northmarch", firstPage)
	require.NoError(t, err)
	clearUpdatedAt(t, &term.UpdatedAt)

	assert.Equal(t, domain.Term{
		TermSummary: northmarchSummary,
		Name:        northmarchName,
		Ancestors:   []domain.TermSummary{},
		Children:    []domain.TermChild{{Term: baronySummary, TermCounts: domain.TermCounts{PersonCount: 2}}},
		TermCounts:  domain.TermCounts{PersonCount: 3},
		Persons: []domain.PersonSummary{
			{Slug: "ash-moor", Name: "Ash Moor", Kind: domain.PersonKindCommunityBard},
			{Slug: "birch-lowe", Name: "Birch Lowe", Kind: domain.PersonKindCommunityBard},
			{Slug: "cedar-vane", Name: cedar, Kind: domain.PersonKindCommunityBard},
		},
	}, term)
}

// TestRepository_GetPublicTerm_Pages checks that pages of members are stable and concatenate to the whole list.
func TestRepository_GetPublicTerm_Pages(t *testing.T) {
	t.Parallel()

	repo, _ := termRepository(t)
	var pieces []domain.PieceSummary
	for offset := int32(0); offset < 6; offset += 2 {
		term, err := repo.GetPublicTerm(t.Context(), "war", domain.Page{Limit: 2, Offset: offset})
		require.NoError(t, err)
		assert.Equal(t, int64(3), term.PieceCount)
		pieces = append(pieces, term.Pieces...)
	}
	assert.Equal(t, []domain.PieceSummary{bladesSummary, sabreSummary, shieldSummary}, pieces)

	term, err := repo.GetPublicTerm(t.Context(), "northmarch", domain.Page{Limit: 1, Offset: 2})
	require.NoError(t, err)
	clearUpdatedAt(t, &term.UpdatedAt)
	assert.Equal(t, []domain.PersonSummary{{Slug: "cedar-vane", Name: cedar, Kind: domain.PersonKindCommunityBard}},
		term.Persons)
}

// TestRepository_GetPublicTerm_NotFound checks that unapproved terms, terms under them and unknown terms are not
// found alike.
func TestRepository_GetPublicTerm_NotFound(t *testing.T) {
	t.Parallel()

	repo, fx := termRepository(t)
	for _, slug := range []string{fx.spears.Slug, fx.pikes.Slug, fx.gloomy.Slug, fx.southmarch.Slug, "no-such-term"} {
		_, err := repo.GetPublicTerm(t.Context(), slug, firstPage)
		require.ErrorIs(t, err, datasources.ErrNotFound, slug)
	}
}
