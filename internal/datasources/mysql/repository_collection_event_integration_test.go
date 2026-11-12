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

// recordFixture is a catalogue of collections and events where every citation and performance rule has
// something to leave out: hidden links, unapproved and no-page entities, and unapproved collections, terms and
// events.
type recordFixture struct {
	songbooks, eastmarchBook, supplement, appendix, orphan, pendingShelf, greatIndex catalogtest.Ref
	revel, pendingMoot, endlessFair                                                  catalogtest.Ref
}

// Fixture strings the tests compare more than once.
const (
	eastmarchBookName = "Eastmarch Songbook"
	kettleBookURL     = "https://example.org/eastmarch#kettle"
	aleTitle          = "The Ale Song"
	revelVideoURL     = "https://example.org/revel/video"
)

var records = catalogtest.NewShared(func(c *catalogtest.Catalog) recordFixture {
	eastmarch := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation, Name: eastmarchName}})
	westmarch := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation, Name: "Westmarch"},
		Unapproved: true})

	songbooks := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Songbooks of the Marches",
		Kind: domain.CollectionKindWiki}})
	pendingShelf := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Pending Shelf"},
		Unapproved: true})
	eastmarchBook := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{
		Name: eastmarchBookName, URL: new("https://example.org/eastmarch.pdf"), Kind: domain.CollectionKindSongbookPDF,
		DescriptionMD: new("Songs of the east."), RegionTermID: &eastmarch.ID, ParentID: &songbooks.ID,
		IsBardicResource: true, Enumerable: domain.CollectionEnumerablePDF,
	}})
	supplement := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{
		Name: "Eastmarch Supplement", ParentID: &eastmarchBook.ID, Kind: domain.CollectionKindHostedDocument}})
	c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Draft Supplement",
		ParentID: &eastmarchBook.ID}, Unapproved: true})
	appendix := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "The Appendix",
		ParentID: &eastmarchBook.ID, URL: new("https://example.org/appendix")}, SortName: "Appendix"})
	orphan := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Orphan Book",
		ParentID: &pendingShelf.ID, RegionTermID: &westmarch.ID}})

	revel := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: revelName,
		DescriptionMD: new("A winter gathering.")}})
	c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "The Green Hall", Kind: domain.EventKindVenue}})
	c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Autumn Moot"}})
	pendingMoot := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Secret Moot"}, Unapproved: true})

	at := func(year *int16, description string, performerIDs ...int64) domain.DocPerformance {
		return domain.DocPerformance{EventID: &revel.ID, Year: year, DescriptionVerbatim: description,
			PerformerIDs: performerIDs}
	}
	cited := func(url string, roles ...domain.LinkRole) domain.DocLink {
		return domain.DocLink{URL: url, CollectionID: &eastmarchBook.ID, Roles: roles}
	}

	pendingPerson := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Pending Rook",
		Links: []domain.DocPersonLink{{
			DocLink:      cited("https://example.org/rook", domain.LinkRoleWebsite),
			Performances: []domain.DocPersonPerformance{{DocPerformance: at(nil, "Rook at the revel")}},
		}}}, Unapproved: true})
	noPage := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: hollisName,
		Status: domain.PersonStatusPrefersNoPage,
		Links: []domain.DocPersonLink{{
			DocLink:      cited(hollisURL, domain.LinkRoleWebsite),
			Performances: []domain.DocPersonPerformance{{DocPerformance: at(nil, "Hollis at the revel")}},
		}}}})
	pendingPiece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "The Pewter Kettle",
		Links: []domain.DocPieceLink{{
			DocLink:      cited("https://example.org/pewter", domain.LinkRoleLyrics),
			Performances: []domain.DocPerformance{at(new(int16(2026)), "Pewter at the revel")},
		}}}, Unapproved: true})
	wren := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: wrenName,
		Links: []domain.DocPersonLink{{
			DocLink: domain.DocLink{URL: wrenURL, Title: new("Wren's songs"), Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
			Performances: []domain.DocPersonPerformance{
				{DocPerformance: at(new(int16(2023)), "Wren at the revel")},
				{PieceID: &pendingPiece.ID, DocPerformance: at(new(int16(2024)), "Wren sings the Pewter Kettle")},
			},
		}}}})

	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{
		Title: kettleTitle,
		Links: []domain.DocPieceLink{
			{DocLink: domain.DocLink{URL: kettleBookURL, Title: new("Kettle entry"), CollectionID: &eastmarchBook.ID,
				Locator: new("p. 12"), Roles: []domain.LinkRole{domain.LinkRoleLyrics, domain.LinkRoleSheetMusic}}},
			{DocLink: cited("https://example.org/eastmarch#hidden", domain.LinkRoleText)},
			{
				DocLink: domain.DocLink{URL: revelVideoURL, Title: new("At the revel"), Platform: new("example.org"),
					Roles: []domain.LinkRole{domain.LinkRolePerformance}},
				Performances: []domain.DocPerformance{
					at(nil, "Undated"),
					at(new(int16(2025)), revelPerformance, wren.ID, noPage.ID, pendingPerson.ID),
					at(new(int16(2020)), "Older"),
				},
			},
			{
				DocLink:      domain.DocLink{URL: "https://example.org/revel/hidden", Roles: []domain.LinkRole{domain.LinkRoleOther}},
				Performances: []domain.DocPerformance{at(new(int16(2025)), "Hidden performance")},
			},
		},
	}, SortTitle: "Copper Kettle"})
	c.SetLinkState("https://example.org/eastmarch#hidden", catalogtest.LinkState{IsHidden: true})
	c.SetLinkState("https://example.org/revel/hidden", catalogtest.LinkState{IsHidden: true})
	// The book's kettle entry is also attached to Wren: one more citation, with its own roles.
	c.Exec("INSERT INTO link_attachments (link_id, person_id, role, position) "+
		"SELECT id, ?, 'external_profile', 1 FROM links WHERE url = ?", wren.ID, kettleBookURL)
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: aleTitle,
		Links: []domain.DocPieceLink{{DocLink: cited("https://example.org/eastmarch#ale", domain.LinkRoleText)}}},
		SortTitle: "Ale Song"})

	greatIndex, endlessFair := seedLongRecords(c)

	return recordFixture{
		songbooks: songbooks, eastmarchBook: eastmarchBook, supplement: supplement, appendix: appendix, orphan: orphan,
		pendingShelf: pendingShelf, greatIndex: greatIndex,
		revel: revel, pendingMoot: pendingMoot, endlessFair: endlessFair,
	}
})

// seedLongRecords seeds a collection with one more citation, and an event with one more performance, than their
// pages show.
func seedLongRecords(c *catalogtest.Catalog) (catalogtest.Ref, catalogtest.Ref) {
	index := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "The Great Index"}})
	fair := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Endless Fair"}})

	links := make([]domain.DocPieceLink, manyItems)
	performances := make([]domain.DocPerformance, manyItems)
	for i := range manyItems {
		links[i] = domain.DocPieceLink{DocLink: domain.DocLink{URL: fmt.Sprint("https://example.org/index/", i),
			CollectionID: &index.ID, Roles: []domain.LinkRole{domain.LinkRoleText}}}
		performances[i] = domain.DocPerformance{EventID: &fair.ID, Year: new(int16(1800 + i)),
			DescriptionVerbatim: fmt.Sprint("Round ", i)}
	}
	links[0].Performances = performances
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Endless Round", Links: links}})

	return index, fair
}

func recordRepository(t *testing.T) (*mysql.Repository, recordFixture) {
	t.Helper()

	db, fx := records.Get(t)
	return mysql.New(db), fx
}

var (
	plainKettleSummary = domain.PieceSummary{Slug: "the-copper-kettle", Title: kettleTitle, Kind: domain.PieceKindSong,
		ConservationStatus: domain.ConservationStatusUnassessed}
	plainWrenSummary = domain.PersonSummary{Slug: "wren-of-the-vale", Name: wrenName,
		Kind: domain.PersonKindCommunityBard}
)

// TestRepository_GetPublicCollection reads a collection with its public region, parent and children, and its
// citations: one per visible link and public piece or person, with the roles of that attachment, by sort key.
func TestRepository_GetPublicCollection(t *testing.T) {
	t.Parallel()

	repo, fx := recordRepository(t)
	collection, err := repo.GetPublicCollection(t.Context(), fx.eastmarchBook.ID)
	require.NoError(t, err)
	clearUpdatedAt(t, &collection.UpdatedAt)

	assert.Equal(t, domain.Collection{
		CollectionSummary: domain.CollectionSummary{
			ID: fx.eastmarchBook.ID, Name: eastmarchBookName, Kind: domain.CollectionKindSongbookPDF,
			URL: new("https://example.org/eastmarch.pdf"), IsBardicResource: true,
		},
		DescriptionMD: new("Songs of the east."),
		Enumerable:    domain.CollectionEnumerablePDF,
		Region:        &domain.TermSummary{Slug: "eastmarch", Facet: domain.FacetAffiliation, DisplayName: eastmarchName},
		Parent: &domain.CollectionSummary{ID: fx.songbooks.ID, Name: "Songbooks of the Marches",
			Kind: domain.CollectionKindWiki},
		// By sort name; not the unapproved draft.
		Children: []domain.CollectionSummary{
			{ID: fx.appendix.ID, Name: "The Appendix", Kind: domain.CollectionKindOther,
				URL: new("https://example.org/appendix")},
			{ID: fx.supplement.ID, Name: "Eastmarch Supplement", Kind: domain.CollectionKindHostedDocument},
		},
		Citations: []domain.Citation{
			{Piece: &domain.PieceSummary{Slug: "the-ale-song", Title: aleTitle, Kind: domain.PieceKindSong,
				ConservationStatus: domain.ConservationStatusUnassessed},
				URL: "https://example.org/eastmarch#ale", Roles: []domain.LinkRole{domain.LinkRoleText}},
			{Piece: &plainKettleSummary, URL: kettleBookURL, Title: new("Kettle entry"), Locator: new("p. 12"),
				Roles: []domain.LinkRole{domain.LinkRoleLyrics, domain.LinkRoleSheetMusic}},
			{Person: &hollisSummary, URL: hollisURL, Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
			{Person: &plainWrenSummary, URL: kettleBookURL, Title: new("Kettle entry"), Locator: new("p. 12"),
				Roles: []domain.LinkRole{domain.LinkRoleExternalProfile}},
		},
		CitationsTotal: 4,
	}, collection)
}

// TestRepository_GetPublicCollection_NonPublicReferences checks that an unapproved parent and region are left
// out.
func TestRepository_GetPublicCollection_NonPublicReferences(t *testing.T) {
	t.Parallel()

	repo, fx := recordRepository(t)
	collection, err := repo.GetPublicCollection(t.Context(), fx.orphan.ID)
	require.NoError(t, err)
	clearUpdatedAt(t, &collection.UpdatedAt)

	assert.Nil(t, collection.Parent)
	assert.Nil(t, collection.Region)
	assert.Empty(t, collection.Children)
	assert.Empty(t, collection.Citations)
	assert.Zero(t, collection.CitationsTotal)
}

// TestRepository_GetPublicCollection_Cap checks that citations stop at the cap, with a total that counts every
// one.
func TestRepository_GetPublicCollection_Cap(t *testing.T) {
	t.Parallel()

	repo, fx := recordRepository(t)
	collection, err := repo.GetPublicCollection(t.Context(), fx.greatIndex.ID)
	require.NoError(t, err)
	clearUpdatedAt(t, &collection.UpdatedAt)

	assert.Len(t, collection.Citations, domain.MaxNestedListItems)
	assert.Equal(t, int64(manyItems), collection.CitationsTotal)
}

func TestRepository_GetPublicCollection_NotFound(t *testing.T) {
	t.Parallel()

	repo, fx := recordRepository(t)
	for _, id := range []int64{fx.pendingShelf.ID, 0, -1, 1 << 40} {
		_, err := repo.GetPublicCollection(t.Context(), id)
		require.ErrorIs(t, err, datasources.ErrNotFound, id)
	}
}

var (
	autumnSummary = domain.EventSummary{Slug: "autumn-moot", Name: "Autumn Moot", Kind: domain.EventKindEvent}
	fairSummary   = domain.EventSummary{Slug: "endless-fair", Name: "Endless Fair", Kind: domain.EventKindEvent}
	revelSummary  = domain.EventSummary{Slug: revelSlug, Name: revelName, Kind: domain.EventKindEvent}
	hallSummary   = domain.EventSummary{Slug: "the-green-hall", Name: "The Green Hall", Kind: domain.EventKindVenue}
)

// TestRepository_ListPublicEvents checks the order by name, the kind filter, pagination and totals, without the
// unapproved event.
func TestRepository_ListPublicEvents(t *testing.T) {
	t.Parallel()

	repo, _ := recordRepository(t)
	venue, event := domain.EventKindVenue, domain.EventKindEvent
	cases := []struct {
		name string
		kind *domain.EventKind
		page domain.Page
		want domain.EventList
	}{
		{
			name: "every_kind",
			page: firstPage,
			want: domain.EventList{
				Events: []domain.EventSummary{autumnSummary, fairSummary, revelSummary, hallSummary}, Total: 4,
			},
		},
		{name: "venues", kind: &venue, page: firstPage, want: domain.EventList{
			Events: []domain.EventSummary{hallSummary}, Total: 1,
		}},
		{name: "events_page", kind: &event, page: domain.Page{Limit: 2, Offset: 1}, want: domain.EventList{
			Events: []domain.EventSummary{fairSummary, revelSummary}, Total: 3,
		}},
		{name: "past_the_end", page: domain.Page{Limit: 2, Offset: 10}, want: domain.EventList{
			Events: []domain.EventSummary{}, Total: 4,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			list, err := repo.ListPublicEvents(t.Context(), tc.kind, tc.page)
			require.NoError(t, err)
			assert.Equal(t, tc.want, list)
		})
	}
}

// TestRepository_GetPublicEvent reads an event's performances newest first, unknown years last, without those
// on hidden links, of unapproved pieces, or recorded only by unapproved entities' links, and with public
// performers only.
func TestRepository_GetPublicEvent(t *testing.T) {
	t.Parallel()

	repo, fx := recordRepository(t)
	event, err := repo.GetPublicEvent(t.Context(), fx.revel.Slug)
	require.NoError(t, err)
	clearUpdatedAt(t, &event.UpdatedAt)

	revelLink := domain.PerformanceLink{URL: revelVideoURL, Title: new("At the revel"), Platform: new("example.org")}
	assert.Equal(t, domain.Event{
		EventSummary:  revelSummary,
		DescriptionMD: new("A winter gathering."),
		Performances: []domain.EventPerformance{
			{Piece: &plainKettleSummary, Link: revelLink, Year: new(int16(2025)),
				Performers: []domain.PersonSummary{plainWrenSummary, hollisSummary}, DescriptionVerbatim: revelPerformance},
			{Link: domain.PerformanceLink{URL: wrenURL, Title: new("Wren's songs")}, Year: new(int16(2023)),
				DescriptionVerbatim: "Wren at the revel"},
			{Piece: &plainKettleSummary, Link: revelLink, Year: new(int16(2020)), DescriptionVerbatim: "Older"},
			{Link: domain.PerformanceLink{URL: hollisURL}, DescriptionVerbatim: "Hollis at the revel"},
			{Piece: &plainKettleSummary, Link: revelLink, DescriptionVerbatim: "Undated"},
		},
		PerformancesTotal: 5,
	}, event)
}

// TestRepository_GetPublicEvent_Cap checks that performances stop at the cap, newest first, with a total that
// counts every one.
func TestRepository_GetPublicEvent_Cap(t *testing.T) {
	t.Parallel()

	repo, fx := recordRepository(t)
	event, err := repo.GetPublicEvent(t.Context(), fx.endlessFair.Slug)
	require.NoError(t, err)
	clearUpdatedAt(t, &event.UpdatedAt)

	require.Len(t, event.Performances, domain.MaxNestedListItems)
	assert.Equal(t, int64(manyItems), event.PerformancesTotal)
	assert.Equal(t, new(int16(1800+manyItems-1)), event.Performances[0].Year)
	assert.Equal(t, new(int16(1801)), event.Performances[domain.MaxNestedListItems-1].Year)
}

func TestRepository_GetPublicEvent_NotFound(t *testing.T) {
	t.Parallel()

	repo, fx := recordRepository(t)
	for _, slug := range []string{fx.pendingMoot.Slug, "no-such-event"} {
		_, err := repo.GetPublicEvent(t.Context(), slug)
		require.ErrorIs(t, err, datasources.ErrNotFound, slug)
	}
}
