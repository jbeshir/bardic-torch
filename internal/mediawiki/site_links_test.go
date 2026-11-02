package mediawiki

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

const (
	performancesHeading = "Performances & Recordings"
	knownPerformances   = "Known Performances"
	greenDragon         = "Green Dragon"
	venuePageID         = 77
	videoOne            = "https://youtube.com/watch?v=video000001"
	videoTwo            = "https://youtube.com/watch?v=video000002"
	videoThree          = "https://youtube.com/watch?v=video000003"
	springRevel         = "Spring Revel"
	pennsic45           = "Pennsic 45"
)

// linksWiki parses the pieces, people and links of a fixture wiki.
func linksWiki(w *wikiFixture) (Links, People) {
	s := w.site()
	pieces := s.Pieces()
	people := ParsePeople(s, pieces)

	return ParseLinks(s, pieces, people), people
}

func pieceLinksOf(t *testing.T, l Links, title string) PieceLinks {
	t.Helper()
	for _, pl := range l.Pieces {
		if pl.Title == title {
			return pl
		}
	}
	t.Fatalf("no links for piece %q", title)

	return PieceLinks{}
}

func personLinksOf(t *testing.T, l Links, person PersonRef) PersonLinks {
	t.Helper()
	for _, pl := range l.Persons {
		if pl.Person == person {
			return pl
		}
	}
	t.Fatalf("no links for person %q", person)

	return PersonLinks{}
}

func linkOfURL(t *testing.T, links []LinkRecord, normalized string) LinkRecord {
	t.Helper()
	k := linkIndex(links, normalized)
	require.GreaterOrEqual(t, k, 0, normalized)

	return links[k]
}

// performanceWiki has a greenDragon, a piece with a performance of a bard at the greenDragon, and a bard page with
// performances of the piece and of a piece without a page.
func performanceWiki() *wikiFixture {
	w := standardWiki()
	w.page(Page{Title: greenDragon, PageID: venuePageID, Wikitext: "A greenDragon.", Categories: []string{CategoryVenue}})
	w.page(Page{Title: aSong, Wikitext: pieceWikitext(aSong, nil, performancesHeading,
		"* [https://youtu.be/video000001 Recording by Rowan Placeholder on YouTube] of [["+wren+"]] at the [["+greenDragon+
			"]], 2010"), Categories: pieceCategories()})
	addBard(w, ash, knownPerformances, "* [https://www.youtube.com/watch?v=video000002 Video] of [["+ash+
		"]] singing [["+aSong+"]] at "+springRevel+"\n"+
		"* [https://youtu.be/video000003 Other] at Spring revel, 2012\n"+
		"* ["+videoOne+" Same video] singing [["+aSong+"]]\n"+
		"* Sung at Pennsic, no recording")

	return w
}

func TestPlacePerformance_PiecePage(t *testing.T) {
	l, _ := linksWiki(performanceWiki())

	link := linkOfURL(t, pieceLinksOf(t, l, aSong).Links, videoOne)
	require.Len(t, link.Performances, 1)
	perf := link.Performances[0]
	assert.Equal(t, "[https://youtu.be/video000001 Recording by Rowan Placeholder on YouTube] of [["+wren+
		"]] at the [["+greenDragon+"]], 2010", perf.DescriptionVerbatim)
	assert.Equal(t, rowan, perf.Uploader)
	assert.Equal(t, aSong, perf.Page)
	assert.Equal(t, []PersonRef{wren}, perf.PerformerRefs)
	assert.Equal(t, greenDragon, perf.Event)
	assert.Equal(t, 2010, perf.Year)
}

func TestPlacePerformance_BardPageWithKnownPiece(t *testing.T) {
	l, _ := linksWiki(performanceWiki())

	piece := linkOfURL(t, pieceLinksOf(t, l, aSong).Links, videoTwo)
	assert.Equal(t, []domain.LinkRole{domain.LinkRolePerformance}, piece.Roles)
	require.Len(t, piece.Performances, 1, "recorded once, in the piece")
	assert.Equal(t, ash, piece.Performances[0].Page)
	assert.Equal(t, []PersonRef{ash}, piece.Performances[0].PerformerRefs)
	assert.Equal(t, springRevel, piece.Performances[0].Event)
	bard := linkOfURL(t, personLinksOf(t, l, ash).Links, videoTwo)
	assert.Equal(t, []domain.LinkRole{domain.LinkRolePerformance}, bard.Roles, "the bard keeps the link")
	assert.Empty(t, bard.Performances)
}

func TestPlacePerformance_BardPageWithUnknownPiece(t *testing.T) {
	l, _ := linksWiki(performanceWiki())

	bard := linkOfURL(t, personLinksOf(t, l, ash).Links, videoThree)
	require.Len(t, bard.Performances, 1)
	assert.Equal(t, springRevel, bard.Performances[0].Event, "the event of the first spelling, by fold")
	assert.Equal(t, 2012, bard.Performances[0].Year)
	assert.Equal(t, -1, linkIndex(pieceLinksOf(t, l, aSong).Links, videoThree))
}

func TestPlacePerformance_RepeatedAndUnlinkedListed(t *testing.T) {
	l, _ := linksWiki(performanceWiki())

	assert.Len(t, linkOfURL(t, pieceLinksOf(t, l, aSong).Links, videoOne).Performances, 1,
		"the piece page records the video already")
	require.Len(t, l.RepeatedPerformances, 1)
	assert.Equal(t, ash, l.RepeatedPerformances[0].Page)
	assert.Empty(t, linkOfURL(t, personLinksOf(t, l, ash).Links, videoOne).Performances)
	require.Len(t, l.Unlinked, 1)
	assert.Equal(t, "Sung at Pennsic, no recording", l.Unlinked[0].DescriptionVerbatim)
}

// TestPlacePerformance_DedupedByURLAcrossPages: a bard page's item on a link a piece page records a performance
// on is that performance, whether or not its wording names the piece: it is not recorded again.
func TestPlacePerformance_DedupedByURLAcrossPages(t *testing.T) {
	w := performanceWiki()
	addBard(w, rowan, knownPerformances, "* ["+videoOne+" Video] of [["+rowan+"]] at the Spring Revel")

	l, _ := linksWiki(w)

	assert.Len(t, linkOfURL(t, pieceLinksOf(t, l, aSong).Links, videoOne).Performances, 1, "the piece page's only")
	assert.Empty(t, linkOfURL(t, personLinksOf(t, l, rowan).Links, videoOne).Performances)
	k := slices.IndexFunc(l.RepeatedPerformances, func(p Performance) bool { return p.Page == rowan })
	require.GreaterOrEqual(t, k, 0)
	assert.Equal(t, aSong, l.RepeatedPerformances[k].Piece, "listed with the piece page recording it")
}

// TestPlacePerformance_WorkIsNeverAPerformer: on a bard page, a link to a work of its works list, after a comma, is
// the performance's piece, not a performer, so no stub person has the work's title.
func TestPlacePerformance_WorkIsNeverAPerformer(t *testing.T) {
	w := standardWiki()
	addBard(w, rowan, songsSection, "* [["+unwritten+"]]", knownPerformances,
		"* ["+videoThree+" Video] performing his original piece, [["+unwritten+"]]")

	l, people := linksWiki(w)

	perf := linkOfURL(t, personLinksOf(t, l, rowan).Links, videoThree).Performances
	require.Len(t, perf, 1)
	assert.Equal(t, unwritten, perf[0].Piece)
	assert.Empty(t, perf[0].PerformerRefs)
	_, ok := people.Persons.Stub(unwritten)
	assert.False(t, ok, "a work is no person")
}

func TestEvents_DedupeByFold(t *testing.T) {
	l, _ := linksWiki(performanceWiki())

	assert.Equal(t, []EventRecord{
		{Name: greenDragon, Kind: domain.EventKindVenue, PageID: venuePageID, Pages: []string{aSong}},
		{Name: springRevel, Kind: domain.EventKindEvent, Pages: []string{ash}},
		{Name: "Pennsic", Kind: domain.EventKindEvent, Pages: []string{ash}},
	}, l.Events)
}

func TestEvents_VenuePage(t *testing.T) {
	w := standardWiki()
	w.page(Page{Title: greenDragon, PageID: venuePageID, Wikitext: "A greenDragon.", Categories: []string{CategoryVenue}})

	l, _ := linksWiki(w)

	assert.Equal(t, []EventRecord{{Name: greenDragon, Kind: domain.EventKindVenue, PageID: venuePageID}}, l.Events,
		"a greenDragon is an event even with no performance at it")
}

func TestEvents_SimilarNamesListed(t *testing.T) {
	w := standardWiki()
	w.page(Page{Title: aSong, Wikitext: pieceWikitext(aSong, nil, performancesHeading,
		"* ["+videoOne+" A] at Pennsic 45\n* ["+videoTwo+" B] at Pennsic 47\n* ["+videoThree+" C] at Spring War"),
		Categories: pieceCategories()})

	l, _ := linksWiki(w)

	assert.Len(t, l.Events, 3, "never merged")
	assert.Equal(t, []SimilarEventNames{{A: pennsic45, B: "Pennsic 47"}}, l.SimilarEvents)
}

func TestPerformers_ResolveToBardsAndStubs(t *testing.T) {
	w := standardWiki()
	w.page(indexedBard(heatherBard, heatherBard+" ("+marionAlias+")"))
	w.page(Page{Title: aSong, Wikitext: pieceWikitext(aSong, nil, performancesHeading,
		"* ["+videoOne+" Video] of [[Marian of Placeholderdale]], [["+rowan+"]] and [["+anotherSong+"]]"),
		Categories: pieceCategories()})
	w.page(Page{Title: anotherSong, Wikitext: pieceWikitext(anotherSong, nil), Categories: pieceCategories()})

	l, people := linksWiki(w)

	perf := linkOfURL(t, pieceLinksOf(t, l, aSong).Links, videoOne).Performances[0]
	assert.Equal(t, []PersonRef{heatherBard, rowan}, perf.PerformerRefs, "a piece link names no performer")
	_, ok := people.Persons.Stub(rowan)
	assert.True(t, ok, "an unmatched performer is a stub person")
	_, ok = people.Persons.Stub(anotherSong)
	assert.False(t, ok)
	require.Len(t, people.Persons.Matches, 1)
	assert.Equal(t, MatchFuzzy, people.Persons.Matches[0].Match.Kind, "listed for review")
}

func TestParseLinks_CanonicalPerURL(t *testing.T) {
	w := standardWiki()
	w.page(Page{Title: aSong, Wikitext: pieceWikitext(aSong, nil, lyricsHeading,
		"* [https://songs.example.org/book.pdf Songbook, p. 3]"), Categories: pieceCategories()})
	w.page(Page{Title: anotherSong, Wikitext: pieceWikitext(anotherSong, nil, sheetMusicHeading,
		"* [http://songs.example.org/book.pdf Score]"), Categories: pieceCategories()})

	l, _ := linksWiki(w)

	first := pieceLinksOf(t, l, aSong)
	other := linkOfURL(t, pieceLinksOf(t, l, anotherSong).Links, "http://songs.example.org/book.pdf")
	assert.Equal(t, []domain.LinkRole{domain.LinkRoleSheetMusic}, other.Roles, "roles stay the document's")
	assert.Equal(t, "Score", other.Title)
	assert.Len(t, l.Canonical, 2, "two normalised URLs (no scheme upgrade)")

	w.page(Page{Title: "Third Song", Wikitext: pieceWikitext("Third Song", nil, sheetMusicHeading,
		"* [https://www.songs.example.org/book.pdf Score]"), Categories: pieceCategories()})
	l, _ = linksWiki(w)

	third := pieceLinksOf(t, l, "Third Song").Links[0]
	assert.Equal(t, songbookPDF, third.URL, "the canonical URL as first entered")
	assert.Empty(t, third.Title, "pages disagree on the title, and the link is in no collection")
	assert.Empty(t, third.Locator, "nor is another page's locator given")
	assert.Equal(t, third.LinkFields, pieceLinksOf(t, l, aSong).Links[0].LinkFields,
		"every document carries the same fields")
	assert.Equal(t, []LinkConflict{{NormalizedURL: songbookPDF,
		Owner:          LinkOwner{PieceID: pieceLinksOf(t, l, "Third Song").PageID},
		CanonicalOwner: LinkOwner{PieceID: first.PageID}, Fields: []string{"url", "title", "locator"},
		Own: LinkFields{URL: "https://www.songs.example.org/book.pdf",
			NormalizedURL: songbookPDF,
			Title:         "Score", Platform: PlatformPDF}}}, l.Conflicts)
}

// TestParseLinks_PageSpecificFieldsNeverFromAnotherPage: pages citing a PDF at different pages get a link each,
// with its own locator; a title the pages disagree on is the collection's name; a description is only kept when
// every page gives it.
func TestParseLinks_PageSpecificFieldsNeverFromAnotherPage(t *testing.T) {
	w := standardWiki()
	w.page(Page{Title: CollectionsPage, Wikitext: "== S ==\n* [" + songbookPDF + " Placeholder Songbook]\n"})
	w.page(Page{Title: aSong, Wikitext: pieceWikitext(aSong, nil, lyricsHeading,
		"* Lyrics are on page 2 of ["+songbookPDF+" the songbook]"), Categories: pieceCategories()})
	w.page(Page{Title: anotherSong, Wikitext: pieceWikitext(anotherSong, nil, lyricsHeading,
		"* Lyrics are on page 14 of ["+songbookPDF+" the songbook]"), Categories: pieceCategories()})
	w.page(Page{Title: "Third Song", Wikitext: pieceWikitext("Third Song", nil, lyricsHeading,
		"* ["+songbookPDF+" Third Song in the songbook]"), Categories: pieceCategories()})

	l, _ := linksWiki(w)

	two, fourteen := pieceLinksOf(t, l, aSong).Links[0], pieceLinksOf(t, l, anotherSong).Links[0]
	assert.Equal(t, songbookPDF+"#page=2", two.URL)
	assert.Equal(t, "p. 2", two.Locator)
	assert.Equal(t, songbookPDF+"#page=14", fourteen.URL)
	assert.Equal(t, "p. 14", fourteen.Locator)
	assert.Equal(t, songbookPDF, fourteen.CollectionURL, "still in the collection")
	assert.Equal(t, "Lyrics are on page 14 of", fourteen.Description, "the line's text around the link")
	third := pieceLinksOf(t, l, "Third Song").Links[0]
	assert.Equal(t, songbookPDF, third.URL, "a link with no page keeps the PDF's URL")
	assert.Empty(t, third.Locator)
	assert.Empty(t, l.Conflicts, "each page's link is its own")

	w.page(Page{Title: "Fourth Song", Wikitext: pieceWikitext("Fourth Song", nil, lyricsHeading,
		"* Also here: ["+songbookPDF+" Fourth Song in the songbook]"), Categories: pieceCategories()})
	l, _ = linksWiki(w)

	third = pieceLinksOf(t, l, "Third Song").Links[0]
	assert.Equal(t, "Placeholder Songbook", third.Title, "the pages disagree: the collection's name")
	assert.Empty(t, third.Description, "only one page gives a description")
	require.Len(t, l.Conflicts, 1)
	assert.Equal(t, []string{"title", "description"}, l.Conflicts[0].Fields)
	assert.Equal(t, "Fourth Song in the songbook", l.Conflicts[0].Own.Title)
}

func TestParseLinks_ProfileLinksOfInfoboxCredits(t *testing.T) {
	w := standardWiki()
	historical := "Placeholder Historical"
	addPiece(w, aSong, [][2]string{{lyricsByParam, "[https://en.wikipedia.org/wiki/Placeholder " + historical + "]"}})

	l, _ := linksWiki(w)

	assert.Empty(t, pieceLinksOf(t, l, aSong).Links, "not the piece's")
	pl := personLinksOf(t, l, PersonRef(historical))
	assert.Zero(t, pl.PageID)
	require.Len(t, pl.Links, 1)
	assert.Equal(t, LinkFields{URL: "https://en.wikipedia.org/wiki/Placeholder",
		NormalizedURL: "https://en.wikipedia.org/wiki/Placeholder", Title: historical, Platform: PlatformWikipedia},
		pl.Links[0].LinkFields)
	assert.Equal(t, []domain.LinkRole{domain.LinkRoleExternalProfile}, pl.Links[0].Roles)
}

func TestParseLinks_CollectionsAndWebsites(t *testing.T) {
	w := standardWiki()
	w.page(Page{Title: CollectionsPage, Wikitext: "== S ==\n* [https://archive.example.org/songs/index.html " +
		"Song archive]\n"})
	addBard(w, ash, "Bard's Website", "* [https://ash.example.org/ Ash's site]")
	addPiece(w, aSong, [][2]string{{lyricsByParam, "[[" + ash + "]]"}})
	p, _ := w.site().Page(aSong)
	p.Wikitext += "== Key Links ==\n* [https://ash.example.org/songs/a.html Official]\n== Lyrics ==\n" +
		"* [https://archive.example.org/songs/a.html#page=4 Archive]\n"
	w.pages[len(w.pages)-1] = p

	l, _ := linksWiki(w)

	links := pieceLinksOf(t, l, aSong).Links
	require.Len(t, links, 2)
	assert.Equal(t, []domain.LinkRole{domain.LinkRoleOfficial}, links[0].Roles)
	assert.Equal(t, domain.LinkPlaceTypeAuthorSite, links[0].PlaceType)
	assert.Equal(t, "https://archive.example.org/songs/index.html", links[1].CollectionURL)
	assert.Equal(t, domain.LinkPlaceTypeCommunityArchive, links[1].PlaceType)
	assert.Equal(t, "p. 4", links[1].Locator)
	assert.Len(t, l.Collections.Records, 1)
}
