package importer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// The synthetic wiki's names. h1Alias is the alias in Wren's H1.
const (
	wren          = "Wren Placeholder"
	rowan         = "Rowan Placeholder"
	h1Alias       = "Ash Sample"
	unsureAlias   = "Maria de Placeholder"
	publicAlias   = "Rowan of the Glen"
	songTitle     = "Song of Testing"
	collidingSong = "Song of Testing!"
)

func page(id int64, title string, categories []string, wikitext string) mediawiki.Page {
	return mediawiki.Page{NS: mediawiki.NSMain, Title: title, PageID: id, Categories: categories, Wikitext: wikitext,
		URL: "https://wiki.example.org/wiki/" + strings.ReplaceAll(title, " ", "_"), RevID: id * 10}
}

// syntheticDump is a small wiki: two bards (one with an alias in its H1, one with a public and an unsure
// alias), a piece, a piece whose title slugifies alike, and the
// Collections page.
func syntheticDump() mediawiki.Dump {
	bards := []string{mediawiki.CategoryBards}
	piece := []string{mediawiki.CategoryPieces, mediawiki.CategorySongs}
	return mediawiki.Dump{
		Pages: []mediawiki.Page{
			page(10, wren, bards, "= "+wren+" ("+h1Alias+") =\nWren sings.\n"+
				"== Licensing & Etiquette ==\nAsk first at ash@example.org. [https://wren.example.org/terms Terms]\n"),
			page(11, rowan, append(bards, "In Memoriam"), "= "+rowan+" ("+publicAlias+", "+unsureAlias+") =\n"+
				"Deceased October 31, 2020.\n"),
			page(20, songTitle, piece, "{{Infobox Song\n|title="+songTitle+"\n|lyrics_by=[["+wren+"]]\n}}\n"+
				"Written for a revel.\n"+
				"== Lyrics ==\nThe version sung differs.\n"+
				"== Licensing & Etiquette ==\nCopyright held. [https://wren.example.org/terms Terms]\n"+
				"== Performances & Recordings ==\n* [https://www.youtube.com/watch?v=video000001 "+
				"Recording] of [["+wren+"]] at a Revel in 2020, recorded on YouTube\n"+
				"== Key Links ==\n* [https://blog.example.org/ash-sample.html Blog post]\n"+
				"* [https://www.youtube.com/watch?v=video000001&utm_source=x Same video]\n"),
			page(30, collidingSong, piece, "{{Infobox Song\n|lyrics_by=[["+wren+"]]\n}}\nAnother song.\n"),
			page(40, mediawiki.CollectionsPage, nil, "== S ==\n* [https://songs.example.org/book.pdf Songbook]\n"),
		},
		Categories: map[string]mediawiki.Category{},
		Redirects: []mediawiki.Redirect{
			{From: "Ash Sàmple", NS: mediawiki.NSMain, To: wren},
		},
		Activity: []mediawiki.PageActivity{
			{Title: songTitle, NS: mediawiki.NSMain, Revisions: 5, Users: map[string]int{"Wiki User 02": 1,
				"Wiki User 01": 3, "Wiki User 03": 1}},
		},
	}
}

func syntheticImport(t *testing.T) domain.CatalogueImport {
	t.Helper()
	imp, _ := Build(mediawiki.ParseCorpus(syntheticDump()), buildTime)

	return imp
}

func pieceTitled(t *testing.T, imp domain.CatalogueImport, title string) domain.ImportedPiece {
	t.Helper()
	for _, p := range imp.Pieces {
		if p.Doc.Title == title {
			return p
		}
	}
	require.Failf(t, "no piece", "%q", title)

	return domain.ImportedPiece{}
}

func personNamed(t *testing.T, imp domain.CatalogueImport, name string) domain.ImportedPerson {
	t.Helper()
	for _, p := range imp.Persons {
		if p.Doc.Name == name {
			return p
		}
	}
	require.Failf(t, "no person", "%q", name)

	return domain.ImportedPerson{}
}

func TestBuild_SyntheticValidates(t *testing.T) {
	imp := syntheticImport(t)

	require.NoError(t, imp.Validate())
}

func TestBuild_RevisionsAreImportApprovedWithContributors(t *testing.T) {
	imp := syntheticImport(t)

	p := pieceTitled(t, imp, songTitle)
	assert.Equal(t, domain.ImportedRevision{ID: p.Revision.ID, Summary: domain.ImportSummaryPage(200),
		Contributors: []string{"Wiki User 01", "Wiki User 02", "Wiki User 03"}, CreatedAt: buildTime}, p.Revision,
		"contributors by edit count, then name")
	assert.Empty(t, personNamed(t, imp, rowan).Revision.Contributors, "a page with no activity credits no one")
}

func TestBuild_SlugCollisionsStable(t *testing.T) {
	imp := syntheticImport(t)

	assert.Equal(t, "song-of-testing", pieceTitled(t, imp, songTitle).Slug)
	assert.Equal(t, "song-of-testing-2", pieceTitled(t, imp, collidingSong).Slug, "the later page ID gets -2")
	again := syntheticImport(t)
	assert.Equal(t, imp.Pieces, again.Pieces)
}

func TestBuild_LinkStatesOnePerNormalizedURL(t *testing.T) {
	imp := syntheticImport(t)

	seen := map[string]domain.ImportedLink{}
	for _, l := range imp.Links {
		u, err := domain.NormalizeURL(l.Link.URL)
		require.NoError(t, err)
		require.NotContains(t, seen, u)
		seen[u] = l
	}
	for _, p := range imp.Pieces {
		for _, l := range p.Doc.Links {
			u, err := domain.NormalizeURL(l.URL)
			require.NoError(t, err)
			row := seen[u].Link
			row.Roles = l.Roles
			assert.Equal(t, row, l.DocLink, "a document's link carries its row's fields")
		}
	}
	song := pieceTitled(t, imp, songTitle).Doc
	video := 0
	for _, l := range song.Links {
		if strings.Contains(l.URL, "video000001") {
			video++
			assert.ElementsMatch(t, []domain.LinkRole{domain.LinkRolePerformance, domain.LinkRoleOther}, l.Roles)
		}
	}
	assert.Equal(t, 1, video, "two spellings of one URL are one link with both roles")
}

// TestBuild_PersonStatusSource: a status is sourced to the link its date is read from; with no such link, to the wiki
// page.
func TestBuild_PersonStatusSource(t *testing.T) {
	imp := syntheticImport(t)

	doc := personNamed(t, imp, rowan).Doc
	assert.Equal(t, domain.PersonStatusDeceased, doc.Status)
	require.NotNil(t, doc.StatusSourceURL)
	assert.Equal(t, "https://wiki.example.org/wiki/Rowan_Placeholder", *doc.StatusSourceURL, "no link: the page")
	assert.Nil(t, personNamed(t, imp, wren).Doc.StatusSourceURL, "an active person needs no source")

	d := syntheticDump()
	obituary := "https://news.example.org/obituary-placeholder"
	d.Pages = append(d.Pages,
		page(12, "Elm Placeholder", []string{mediawiki.CategoryBards, "In Memoriam"}, "= Elm Placeholder =\n"+
			"== Notes ==\n* ["+obituary+" Deceased October 31, 2020]\n"))
	imp, _ = Build(mediawiki.ParseCorpus(d), buildTime)

	assert.Equal(t, obituary, *personNamed(t, imp, "Elm Placeholder").Doc.StatusSourceURL, "the obituary")
}

func TestBuild_LegacyColumns(t *testing.T) {
	imp := syntheticImport(t)

	p := pieceTitled(t, imp, songTitle)
	require.NotNil(t, p.Legacy)
	require.NotNil(t, p.Legacy.PageID)
	assert.Equal(t, uint32(20), *p.Legacy.PageID)
	assert.Equal(t, songTitle, p.Legacy.Title)
	bard := personNamed(t, imp, wren)
	require.NotNil(t, bard.Legacy)
	assert.Equal(t, uint32(10), *bard.Legacy.PageID)
}

func TestBuild_SummariesAreNeutral(t *testing.T) {
	imp := syntheticImport(t)

	var summaries []string
	for _, p := range imp.Pieces {
		summaries = append(summaries, p.Revision.Summary)
	}
	for _, p := range imp.Persons {
		summaries = append(summaries, p.Revision.Summary)
	}
	for _, c := range imp.Collections {
		summaries = append(summaries, c.Revision.Summary)
	}
	for _, e := range imp.Events {
		summaries = append(summaries, e.Revision.Summary)
	}
	require.NotEmpty(t, summaries)
	for _, s := range summaries {
		assert.True(t, domain.IsImportSummary(s), s)
	}
}

func TestBuild_Deterministic(t *testing.T) {
	c := scrubbedCorpus(t)
	a, ca := Build(c, buildTime)
	b, cb := Build(c, buildTime)

	assert.Equal(t, a, b)
	assert.Equal(t, ca, cb)
}

func TestBuild_StringsAreNFC(t *testing.T) {
	d := syntheticDump()
	d.Pages[3].Wikitext = "{{Infobox Song\n}}\nCafé notes.\n"

	imp, _ := Build(mediawiki.ParseCorpus(d), buildTime)

	notes := pieceTitled(t, imp, collidingSong).Doc.NotesMD
	require.NotNil(t, notes)
	assert.Equal(t, "Café notes.", *notes)
	require.NoError(t, imp.Validate())
}

// TestBuild_ControlCharactersRemoved plants NUL, ESC and a bidirectional override in a piece's notes, a
// link's title and its URL: the text loses them, the URL has them percent-encoded (and a "www." URL is stored
// with https), Validate (which rejects any control character in any string) passes.
func TestBuild_ControlCharactersRemoved(t *testing.T) {
	d := syntheticDump()
	d.Pages[3].Wikitext = "{{Infobox Song\n}}\nNotes\x1b[31m with an \u202eoverride.\x00\n" +
		"== Key Links ==\n* [https://example.org/a\x1bb Link \u202etitle]\n* [www.example.org/c Site]\n"

	imp, _ := Build(mediawiki.ParseCorpus(d), buildTime)

	require.NoError(t, imp.Validate())
	doc := pieceTitled(t, imp, collidingSong).Doc
	require.NotNil(t, doc.NotesMD)
	assert.Equal(t, "Notes\\[31m with an override.", *doc.NotesMD)
	require.Len(t, doc.Links, 2)
	assert.Equal(t, "https://example.org/a%1Bb", doc.Links[0].URL)
	assert.Equal(t, "https://www.example.org/c", doc.Links[1].URL, "a www. URL is stored absolute")
	require.NotNil(t, doc.Links[0].Title)
	assert.Equal(t, "Link title", *doc.Links[0].Title)
}

// TestBuild_ControlCharactersRemovedFromNamesAndContributors plants ESC and a bidirectional override in a bard's
// public H1 alias, a page title (the piece's legacy title) and a wiki user name (a revision contributor): each is
// imported without them, and the import validates (Validate rejects any control character).
func TestBuild_ControlCharactersRemovedFromNamesAndContributors(t *testing.T) {
	d := syntheticDump()
	d.Pages[1].Wikitext = "= " + rowan + " (Rowan of the\x1b Glen, " + unsureAlias + ") =\nDeceased October 31, " +
		"2020.\n"
	d.Pages[3].Title = "Song of\u202e Testing!"
	d.Activity = append(d.Activity, mediawiki.PageActivity{Title: d.Pages[3].Title, NS: mediawiki.NSMain,
		Revisions: 1, Users: map[string]int{"Wiki\x1b User\u202e 04": 1}})

	imp, _ := Build(mediawiki.ParseCorpus(d), buildTime)

	require.NoError(t, imp.Validate())
	var aliases []string
	for _, a := range personNamed(t, imp, rowan).Doc.Aliases {
		aliases = append(aliases, a.Name)
	}
	assert.Contains(t, aliases, publicAlias)
	piece := pieceTitled(t, imp, collidingSong)
	require.NotNil(t, piece.Legacy)
	assert.Equal(t, collidingSong, piece.Legacy.Title)
	assert.Equal(t, []string{"Wiki User 04"}, piece.Revision.Contributors)
}
