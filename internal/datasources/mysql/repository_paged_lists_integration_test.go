//go:build integration

package mysql_test

import (
	"fmt"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// creditFixture is a catalogue whose pieces' credits name every kind of person a credit line must leave out, and
// where one person is credited on several pieces.
type creditFixture struct {
	// manyHands credits, in order: a no-page person, Ash, Wren (lyrics), an unapproved person, Birch, Wren (tune),
	// Cedar, Dun, Elm and Fir.
	manyHands catalogtest.Ref
	// echo is a variant of manyHands; ashOnly and ashAndWren are Ash's other public works.
	echo, ashOnly, ashAndWren catalogtest.Ref
	// ash has a hidden legal alias.
	ash, wren, noPage, pending catalogtest.Ref
	// reels tags manyHands and ashOnly.
	reels catalogtest.Ref
}

// Names in the credit fixture.
const (
	manyHandsTitle    = "Many Hands"
	ashAloneTitle     = "Ash Alone"
	bothTogetherTitle = "Both Together"
	unknownPersonSlug = "no-such-person"
	ashName           = "Ash Tallow"
	ashLegalName      = "Ashley Hiddenname"
	creditWren        = "Wren Credited"
	noPageCredit      = "Quiet Credited"
	pendingCredit     = "Pending Credited"
)

var creditCatalogue = catalogtest.NewShared(func(c *catalogtest.Catalog) creditFixture {
	person := func(name string) catalogtest.Ref {
		return c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: name}})
	}
	ash := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: ashName,
		Aliases: []domain.DocPersonAlias{{Name: ashLegalName, Kind: domain.PersonAliasKindLegal}}}})
	wren := person(creditWren)
	noPage := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: noPageCredit,
		Status: domain.PersonStatusPrefersNoPage}})
	pending := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: pendingCredit}, Unapproved: true})
	birch, cedar, dun, elm, fir := person("Birch"), person("Cedar"), person("Dun"), person("Elm"), person("Fir")
	reels := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetForm, Name: "Reels"}})

	credit := func(p catalogtest.Ref, role domain.CreditRole) domain.DocCredit {
		return domain.DocCredit{PersonID: &p.ID, Role: role}
	}
	manyHands := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: manyHandsTitle,
		Terms: []domain.DocPieceTerm{{TermID: reels.ID, IsPrimary: true}},
		Credits: []domain.DocCredit{
			credit(noPage, domain.CreditRoleLyrics), credit(ash, domain.CreditRoleLyrics),
			credit(wren, domain.CreditRoleLyrics), credit(pending, domain.CreditRoleTune),
			{Role: domain.CreditRoleTune, Attribution: domain.AttributionTraditional},
			credit(birch, domain.CreditRoleMusic), credit(wren, domain.CreditRoleTune), credit(cedar, domain.CreditRoleMusic),
			credit(dun, domain.CreditRoleArrangement), credit(elm, domain.CreditRoleArrangement),
			credit(fir, domain.CreditRoleTranslation),
		}}})
	echo := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Echo of Many Hands", Kind: domain.PieceKindPoem,
		Relations: []domain.DocPieceRelation{{ToPieceID: manyHands.ID, Type: domain.PieceRelationTypeVariantOf}}}})
	ashOnly := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: ashAloneTitle, Kind: domain.PieceKindPoem,
		Terms:   []domain.DocPieceTerm{{TermID: reels.ID, IsPrimary: true}},
		Credits: []domain.DocCredit{credit(ash, domain.CreditRoleAuthor)}}})
	ashAndWren := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: bothTogetherTitle,
		Credits: []domain.DocCredit{credit(wren, domain.CreditRoleTune), credit(ash, domain.CreditRoleLyrics)}}})
	// Credits that no list may follow: a pending piece crediting Ash, and a piece crediting only persons a credit
	// line leaves out.
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Ash Unreleased",
		Credits: []domain.DocCredit{credit(ash, domain.CreditRoleLyrics)}}, Unapproved: true})
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Quiet Song",
		Credits: []domain.DocCredit{credit(noPage, domain.CreditRoleLyrics), credit(pending, domain.CreditRoleTune)}}})

	return creditFixture{manyHands: manyHands, echo: echo, ashOnly: ashOnly, ashAndWren: ashAndWren,
		ash: ash, wren: wren, noPage: noPage, pending: pending, reels: reels}
})

// manyHandsNames is Many Hands' credit line.
var manyHandsNames = []string{ashName, creditWren, "Birch", "Cedar", "Dun"}

// TestRepository_PieceSummaryCreditNames checks the credit line of every summary a read shows of a piece: named
// credits only, to public persons with a page, in credit order, each person once, at most five, by their name.
func TestRepository_PieceSummaryCreditNames(t *testing.T) {
	t.Parallel()

	db, fx := creditCatalogue.Get(t)
	repo := mysql.New(db)
	ctx := t.Context()

	pieces, err := repo.ListPublicPieces(ctx, datasources.PieceQuery{Sort: domain.PieceSortTitle,
		Page: domain.Page{Limit: 10}})
	require.NoError(t, err)
	byTitle := map[string][]string{}
	for _, p := range pieces.Pieces {
		byTitle[p.Title] = p.CreditNames
	}
	assert.Equal(t, map[string][]string{
		manyHandsTitle: manyHandsNames, ashAloneTitle: {ashName}, bothTogetherTitle: {creditWren, ashName},
		"Echo of Many Hands": nil, "Quiet Song": nil,
	}, byTitle)

	piece, err := repo.GetPublicPiece(ctx, fx.manyHands.Slug)
	require.NoError(t, err)
	assert.Equal(t, manyHandsNames, piece.CreditNames)

	echo, err := repo.GetPublicPiece(ctx, fx.echo.Slug)
	require.NoError(t, err)
	require.Len(t, echo.Relations, 1)
	assert.Equal(t, manyHandsNames, echo.Relations[0].Piece.CreditNames)

	term, err := repo.GetPublicTerm(ctx, fx.reels.Slug, domain.Page{Limit: 10})
	require.NoError(t, err)
	require.Len(t, term.Pieces, 2)
	assert.Equal(t, []string{ashName}, term.Pieces[0].CreditNames, term.Pieces[0].Title)
	assert.Equal(t, manyHandsNames, term.Pieces[1].CreditNames, term.Pieces[1].Title)

	ash, err := repo.GetPublicPerson(ctx, fx.ash.Slug)
	require.NoError(t, err)
	works := map[string][]string{}
	for _, w := range ash.Profile.Works {
		works[w.Piece.Title] = w.Piece.CreditNames
	}
	assert.Equal(t, map[string][]string{
		manyHandsTitle: manyHandsNames, ashAloneTitle: {ashName}, bothTogetherTitle: {creditWren, ashName},
	}, works)

	// A revision's snapshot shows the related piece's current summary.
	revision, err := repo.GetPublicRevision(ctx, fx.echo.RevisionID)
	require.NoError(t, err)
	pieceRevision, ok := revision.(domain.PieceRevision)
	require.True(t, ok)
	require.Len(t, pieceRevision.Snapshot.Relations, 1)
	assert.Equal(t, manyHandsNames, pieceRevision.Snapshot.Relations[0].Piece.CreditNames)
	manyHandsRevision, err := repo.GetPublicRevision(ctx, fx.manyHands.RevisionID)
	require.NoError(t, err)
	entity, ok := manyHandsRevision.(domain.PieceRevision)
	require.True(t, ok)
	assert.Equal(t, manyHandsNames, entity.Entity.CreditNames)

	for _, names := range [][]string{byTitle[manyHandsTitle], piece.CreditNames} {
		assert.NotContains(t, names, noPageCredit)
		assert.NotContains(t, names, pendingCredit)
		assert.NotContains(t, names, ashLegalName)
	}
}

// TestRepository_ListPublicPieces_PersonFilter pages through a person's works with the person filter, which
// filters the facet counts too.
func TestRepository_ListPublicPieces_PersonFilter(t *testing.T) {
	t.Parallel()

	db, fx := creditCatalogue.Get(t)
	repo := mysql.New(db)
	ctx := t.Context()
	query := func(page domain.Page, terms ...string) datasources.PieceQuery {
		return datasources.PieceQuery{PersonSlug: &fx.ash.Slug, TermSlugs: terms, Sort: domain.PieceSortTitle,
			Page: page}
	}

	first, err := repo.ListPublicPieces(ctx, query(domain.Page{Limit: 2}))
	require.NoError(t, err)
	second, err := repo.ListPublicPieces(ctx, query(domain.Page{Limit: 2, Offset: 2}))
	require.NoError(t, err)

	// The unapproved piece crediting Ash is not listed or counted.
	var titles []string
	for _, p := range append(first.Pieces, second.Pieces...) {
		titles = append(titles, p.Title)
	}
	assert.Equal(t, []string{ashAloneTitle, bothTogetherTitle, manyHandsTitle}, titles)
	assert.Equal(t, int64(3), first.Total)
	assert.Equal(t, int64(3), second.Total)
	assert.Equal(t, first.Facets, second.Facets)
	assert.Equal(t, []domain.PieceKindCount{
		{Kind: domain.PieceKindSong, Count: 2}, {Kind: domain.PieceKindPoem, Count: 1},
	}, first.Facets.Kinds)
	assert.Equal(t, []domain.TermCount{{Slug: fx.reels.Slug, Count: 2}}, first.Facets.Terms)
	assert.Equal(t, []domain.LetterCount{{Letter: "A", Count: 1}, {Letter: "B", Count: 1}, {Letter: "M", Count: 1}},
		first.Facets.Letters)

	// With a term too, both must match; the term's own facet count ignores the term, not the person.
	withTerm, err := repo.ListPublicPieces(ctx, query(domain.Page{Limit: 10}, fx.reels.Slug))
	require.NoError(t, err)
	require.Len(t, withTerm.Pieces, 2)
	assert.Equal(t, ashAloneTitle, withTerm.Pieces[0].Title)
	assert.Equal(t, manyHandsTitle, withTerm.Pieces[1].Title)
	assert.Equal(t, []domain.TermCount{{Slug: fx.reels.Slug, Count: 2}}, withTerm.Facets.Terms)

	// Wren's works are hers alone.
	wren, err := repo.ListPublicPieces(ctx, datasources.PieceQuery{PersonSlug: &fx.wren.Slug,
		Sort: domain.PieceSortTitle, Page: domain.Page{Limit: 10}})
	require.NoError(t, err)
	assert.Equal(t, int64(2), wren.Total)
}

// TestRepository_ListPublicPieces_UnknownPerson checks that unknown and unapproved persons, and persons who prefer
// no page, give the same error, naming the slug as given, and that an unknown term is reported before them.
func TestRepository_ListPublicPieces_UnknownPerson(t *testing.T) {
	t.Parallel()

	db, fx := creditCatalogue.Get(t)
	repo := mysql.New(db)
	for _, slug := range []string{fx.noPage.Slug, fx.pending.Slug, unknownPersonSlug} {
		_, err := repo.ListPublicPieces(t.Context(), datasources.PieceQuery{PersonSlug: &slug,
			TermSlugs: []string{fx.reels.Slug}, Sort: domain.PieceSortTitle, Page: domain.Page{Limit: 10}})
		var unknown *datasources.UnknownPersonError
		require.ErrorAs(t, err, &unknown, slug)
		assert.Equal(t, slug, unknown.Slug)
	}

	_, err := repo.ListPublicPieces(t.Context(), datasources.PieceQuery{PersonSlug: &fx.noPage.Slug,
		TermSlugs: []string{"no-such-term"}, Sort: domain.PieceSortTitle, Page: domain.Page{Limit: 10}})
	var unknown *datasources.UnknownTermError
	require.ErrorAs(t, err, &unknown)
}

// pageSize pages through the capped lists in pages smaller than their cap.
const pageSize = 100

// collectPages reads every page of a list of total items, pageSize at a time, checking each page's total.
func collectPages[T any](t *testing.T, total int32, read func(page domain.Page) ([]T, int64, error)) []T {
	t.Helper()

	var all []T
	for offset := int32(0); offset <= total; offset += pageSize {
		items, gotTotal, err := read(domain.Page{Limit: pageSize, Offset: offset})
		require.NoError(t, err)
		require.Equal(t, int64(total), gotTotal, "offset %d", offset)
		all = append(all, items...)
	}
	require.Len(t, all, int(total))

	return all
}

// TestRepository_ListPersonPerformances_Paged pages through every performance of a person whose page caps them:
// newest year first, each once.
func TestRepository_ListPersonPerformances_Paged(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	all := collectPages(t, manyItems, func(page domain.Page) ([]domain.PersonPerformance, int64, error) {
		list, err := repo.ListPublicPersonPerformances(t.Context(), fx.prolific.Slug, page)
		return list.Performances, list.Total, err
	})
	for i, performance := range all {
		assert.Equal(t, new(int16(1800+manyItems-1-i)), performance.Year)
		assert.Equal(t, fmt.Sprint("Take ", manyItems-1-i), performance.DescriptionVerbatim)
	}

	past, err := repo.ListPublicPersonPerformances(t.Context(), fx.prolific.Slug,
		domain.Page{Limit: pageSize, Offset: domain.MaxPageOffset})
	require.NoError(t, err)
	assert.Empty(t, past.Performances)
	assert.Equal(t, int64(manyItems), past.Total)
}

// TestRepository_ListCollectionCitations_Paged pages through every citation of a collection whose page caps them.
func TestRepository_ListCollectionCitations_Paged(t *testing.T) {
	t.Parallel()

	repo, fx := recordRepository(t)
	all := collectPages(t, manyItems, func(page domain.Page) ([]domain.Citation, int64, error) {
		list, err := repo.ListPublicCollectionCitations(t.Context(), fx.greatIndex.ID, page)
		return list.Citations, list.Total, err
	})
	seen := map[string]bool{}
	for _, citation := range all {
		assert.False(t, seen[citation.URL], citation.URL)
		seen[citation.URL] = true
	}
	assert.Len(t, seen, manyItems)
}

// TestRepository_ListEventPerformances_Paged pages through every performance at an event whose page caps them.
func TestRepository_ListEventPerformances_Paged(t *testing.T) {
	t.Parallel()

	repo, fx := recordRepository(t)
	all := collectPages(t, manyItems, func(page domain.Page) ([]domain.EventPerformance, int64, error) {
		list, err := repo.ListPublicEventPerformances(t.Context(), fx.endlessFair.Slug, page)
		return list.Performances, list.Total, err
	})
	for i, performance := range all {
		assert.Equal(t, new(int16(1800+manyItems-1-i)), performance.Year)
	}
}

// capPage is the page of a paged list that a capped list is.
var capPage = domain.Page{Limit: domain.MaxNestedListItems}

// TestRepository_PagedListsMatchCappedLists checks that the first page of each paged list, of the cap's size, is
// exactly the list the entity's page shows, with the same total, for long and short lists alike; and that a person
// who prefers no page has an empty list, as their page has no profile.
func TestRepository_PagedListsMatchCappedLists(t *testing.T) {
	t.Parallel()

	detailRepo, detail := detailRepository(t)
	recordRepo, record := recordRepository(t)
	ctx := t.Context()

	cases := map[string]func(t *testing.T){
		"person_prolific": func(t *testing.T) { assertPersonPerformancesMatch(t, detailRepo, detail.prolific.Slug) },
		"person_wren":     func(t *testing.T) { assertPersonPerformancesMatch(t, detailRepo, detail.wren.Slug) },
		"person_no_page": func(t *testing.T) {
			person, err := detailRepo.GetPublicPerson(ctx, detail.noPage.Slug)
			require.NoError(t, err)
			require.Nil(t, person.Profile)
			list, err := detailRepo.ListPublicPersonPerformances(ctx, detail.noPage.Slug, capPage)
			require.NoError(t, err)
			assert.Equal(t, domain.PersonPerformanceList{Performances: []domain.PersonPerformance{}}, list)
		},
		"collection_long":  func(t *testing.T) { assertCitationsMatch(t, recordRepo, record.greatIndex.ID) },
		"collection_short": func(t *testing.T) { assertCitationsMatch(t, recordRepo, record.eastmarchBook.ID) },
		"event_long":       func(t *testing.T) { assertEventPerformancesMatch(t, recordRepo, record.endlessFair.Slug) },
		"event_short":      func(t *testing.T) { assertEventPerformancesMatch(t, recordRepo, record.revel.Slug) },
	}
	for name, check := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			check(t)
		})
	}
}

func assertPersonPerformancesMatch(t *testing.T, repo *mysql.Repository, slug string) {
	t.Helper()

	person, err := repo.GetPublicPerson(t.Context(), slug)
	require.NoError(t, err)
	require.NotNil(t, person.Profile)
	list, err := repo.ListPublicPersonPerformances(t.Context(), slug, capPage)
	require.NoError(t, err)
	require.NotEmpty(t, list.Performances)
	assert.Equal(t, domain.PersonPerformanceList{Performances: person.Profile.Performances,
		Total: person.Profile.PerformancesTotal}, list)
}

func assertCitationsMatch(t *testing.T, repo *mysql.Repository, id int64) {
	t.Helper()

	collection, err := repo.GetPublicCollection(t.Context(), id)
	require.NoError(t, err)
	list, err := repo.ListPublicCollectionCitations(t.Context(), id, capPage)
	require.NoError(t, err)
	require.NotEmpty(t, list.Citations)
	assert.Equal(t, domain.CitationList{Citations: collection.Citations, Total: collection.CitationsTotal}, list)
}

func assertEventPerformancesMatch(t *testing.T, repo *mysql.Repository, slug string) {
	t.Helper()

	event, err := repo.GetPublicEvent(t.Context(), slug)
	require.NoError(t, err)
	list, err := repo.ListPublicEventPerformances(t.Context(), slug, capPage)
	require.NoError(t, err)
	require.NotEmpty(t, list.Performances)
	assert.Equal(t, domain.EventPerformanceList{Performances: event.Performances, Total: event.PerformancesTotal}, list)
}

// TestRepository_PagedLists_NotFound checks that the paged lists of entities that are not public are not found,
// like those of unknown ones.
func TestRepository_PagedLists_NotFound(t *testing.T) {
	t.Parallel()

	detailRepo, detail := detailRepository(t)
	recordRepo, record := recordRepository(t)
	ctx := t.Context()
	page := domain.Page{Limit: 10}

	for _, slug := range []string{detail.pendingWren.Slug, unknownPersonSlug} {
		_, err := detailRepo.ListPublicPersonPerformances(ctx, slug, page)
		require.ErrorIs(t, err, datasources.ErrNotFound, slug)
	}
	for _, id := range []int64{record.pendingShelf.ID, 0, -1, 999999999} {
		_, err := recordRepo.ListPublicCollectionCitations(ctx, id, page)
		require.ErrorIs(t, err, datasources.ErrNotFound, id)
	}
	for _, slug := range []string{record.pendingMoot.Slug, "no-such-event"} {
		_, err := recordRepo.ListPublicEventPerformances(ctx, slug, page)
		require.ErrorIs(t, err, datasources.ErrNotFound, slug)
	}
}
