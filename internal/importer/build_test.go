package importer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// The synthetic wiki's names. hiddenAlias is the legal name in Wren's H1; its surname is hiddenSurname.
const (
	wren          = "Wren Placeholder"
	rowan         = "Rowan Placeholder"
	hiddenAlias   = "Ash Hiddenname"
	hiddenSurname = "Hiddenname"
	unsureAlias   = "Maria de Placeholder"
	publicAlias   = "Rowan of the Glen"
	songTitle     = "Song of Testing"
	collidingSong = "Song of Testing!"
)

func page(id int64, title string, categories []string, wikitext string) mediawiki.Page {
	return mediawiki.Page{NS: mediawiki.NSMain, Title: title, PageID: id, Categories: categories, Wikitext: wikitext,
		URL: "https://wiki.example.org/wiki/" + strings.ReplaceAll(title, " ", "_"), RevID: id * 10}
}

// syntheticDump is a small wiki: two bards (one with a legal alias in its H1, one with a public and an unsure
// alias), a piece naming the legal alias in every kind of free text, a piece whose title slugifies alike, and the
// Collections page.
func syntheticDump() mediawiki.Dump {
	bards := []string{mediawiki.CategoryBards}
	piece := []string{mediawiki.CategoryPieces, mediawiki.CategorySongs}
	return mediawiki.Dump{
		Pages: []mediawiki.Page{
			page(10, wren, bards, "= "+wren+" ("+hiddenAlias+") =\nWren sings. Mr. "+hiddenSurname+" wrote back.\n"+
				"== Licensing & Etiquette ==\nAsk first at ash@example.org. [https://wren.example.org/terms Terms]\n"),
			page(11, rowan, append(bards, "In Memoriam"), "= "+rowan+" ("+publicAlias+", "+unsureAlias+") =\n"+
				"Deceased October 31, 2020.\n"),
			page(20, songTitle, piece, "{{Infobox Song\n|title="+songTitle+"\n|other_titles="+hiddenAlias+
				"'s Song\n|lyrics_by=[["+wren+"]]\n}}\nWritten for "+hiddenAlias+".\n"+
				"== Lyrics ==\nThe version "+hiddenAlias+" sang differs.\n"+
				"== Licensing & Etiquette ==\nCopyright "+hiddenAlias+". [https://wren.example.org/terms Terms]\n"+
				"== Performances & Recordings ==\n* [https://www.youtube.com/watch?v=video000001 "+hiddenAlias+
				" sings] of [["+wren+"]] at "+hiddenAlias+" Revel in 2020, recorded by "+hiddenAlias+" on YouTube\n"+
				"== Key Links ==\n* [https://blog.example.org/ash-hiddenname.html Blog post]\n"+
				"* [https://www.youtube.com/watch?v=video000001&utm_source=x Same video]\n"),
			page(30, collidingSong, piece, "{{Infobox Song\n|lyrics_by=[["+wren+"]]\n}}\nAnother song.\n"),
			page(40, mediawiki.CollectionsPage, nil, "== S ==\n* [https://songs.example.org/book.pdf "+
				hiddenAlias+" Songbook]\n"),
		},
		Categories: map[string]mediawiki.Category{},
		Redirects: []mediawiki.Redirect{
			{From: "Ash Hiddénname", NS: mediawiki.NSMain, To: wren},
		},
		Activity: []mediawiki.PageActivity{
			{Title: songTitle, NS: mediawiki.NSMain, Revisions: 5, Users: map[string]int{"Wiki User 02": 1,
				"Wiki User 01": 3, "Wiki User 03": 1}},
		},
	}
}

func syntheticImport(t *testing.T) (domain.CatalogueImport, Review) {
	t.Helper()

	return Build(mediawiki.ParseCorpus(syntheticDump()), buildTime)
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
	imp, _ := syntheticImport(t)

	require.NoError(t, imp.Validate())
}

func TestBuild_RevisionsAreImportApprovedWithContributors(t *testing.T) {
	imp, _ := syntheticImport(t)

	p := pieceTitled(t, imp, songTitle)
	assert.Equal(t, domain.ImportedRevision{ID: p.Revision.ID, Summary: domain.ImportSummaryPage(200),
		Contributors: []string{"Wiki User 01", "Wiki User 02", "Wiki User 03"}, CreatedAt: buildTime}, p.Revision,
		"contributors by edit count, then name")
	assert.Empty(t, personNamed(t, imp, rowan).Revision.Contributors, "a page with no activity credits no one")
}

func TestBuild_SlugCollisionsStable(t *testing.T) {
	imp, _ := syntheticImport(t)

	assert.Equal(t, "song-of-testing", pieceTitled(t, imp, songTitle).Slug)
	assert.Equal(t, "song-of-testing-2", pieceTitled(t, imp, collidingSong).Slug, "the later page ID gets -2")
	again, _ := syntheticImport(t)
	assert.Equal(t, imp.Pieces, again.Pieces)
}

func TestBuild_LegalAliasHidden(t *testing.T) {
	imp, review := syntheticImport(t)

	aliases := personNamed(t, imp, wren).Doc.Aliases
	require.Len(t, aliases, 1, "the redirect folding to the H1 alias is the same alias")
	assert.Equal(t, domain.DocPersonAlias{Name: hiddenAlias, Kind: domain.PersonAliasKindLegal, IsPublic: false},
		aliases[0])
	assert.Contains(t, review.Aliases[0].Text, hiddenAlias, "unsure classifications come first")
}

func TestBuild_RedirectEqualToHiddenAliasHidden(t *testing.T) {
	d := syntheticDump()
	d.Pages[0].Wikitext = "= " + wren + " (" + hiddenAlias + ") =\n"
	d.Redirects = []mediawiki.Redirect{{From: "Ash Hiddénname", NS: mediawiki.NSMain, To: wren},
		{From: "Ash Hiddenname-Placeholder", NS: mediawiki.NSMain, To: wren}}

	imp, _ := Build(mediawiki.ParseCorpus(d), buildTime)

	for _, a := range personNamed(t, imp, wren).Doc.Aliases {
		assert.False(t, a.IsPublic, "%q: a redirect folding to, or holding, a hidden alias is hidden", a.Name)
	}
}

func TestBuild_UnsureAliasHiddenAndListed(t *testing.T) {
	imp, review := syntheticImport(t)

	assert.ElementsMatch(t, []domain.DocPersonAlias{
		{Name: publicAlias, Kind: domain.PersonAliasKindSCA, IsPublic: true},
		{Name: unsureAlias, Kind: domain.PersonAliasKindSCA, IsPublic: false},
	}, personNamed(t, imp, rowan).Doc.Aliases)
	var unsure []string
	for _, it := range review.Aliases {
		if strings.Contains(it.Text, "unsure") {
			unsure = append(unsure, it.Text)
		}
	}
	assert.Len(t, unsure, 2, "the legal and the shared-particle alias")
	assert.Len(t, review.Aliases, 3, "every alias of the import is listed")
}

// TestBuild_FreeTextContainingHiddenAliasListed checks every kind of public free text: each occurrence of the
// hidden alias (and the honorific form of its surname) is withheld and listed, and a link whose URL holds it is
// hidden.
func TestBuild_FreeTextContainingHiddenAliasListed(t *testing.T) {
	imp, review := syntheticImport(t)

	public := strings.ToLower(publicJSON(t, imp))
	assert.NotContains(t, public, strings.ToLower(hiddenSurname))
	for _, field := range []string{"notes", "version notes", "licensing notes", "alias", "title", "description",
		"uploader"} {
		found := false
		for _, it := range review.HiddenNames {
			found = found || strings.Contains(it.Text, ", "+field+":")
		}
		assert.True(t, found, "a withheld %s is listed", field)
	}
	wrenDoc := personNamed(t, imp, wren).Doc
	require.NotNil(t, wrenDoc.NotesMD)
	assert.Equal(t, "Wren sings. "+domain.WithheldName+" wrote back.", *wrenDoc.NotesMD, "the honorific form is withheld")
	song := pieceTitled(t, imp, songTitle).Doc
	require.NotNil(t, song.NotesMD)
	assert.Equal(t, "Written for "+domain.WithheldName+".", *song.NotesMD)
	for _, a := range song.Aliases {
		assert.False(t, domain.HoldsWithheldText(a.Text), "an alias holding the hidden alias is dropped, not withheld")
	}

	assert.Empty(t, imp.Collections, "a collection named by the hidden alias is not imported")
	assert.Empty(t, imp.Events, "nor is an event")
	assert.True(t, containsKind(review.HiddenNames, "collection_withheld"), "the collection is listed")
	assert.True(t, containsKind(review.HiddenNames, "event_withheld"), "the event is listed")
	for _, l := range pieceTitled(t, imp, songTitle).Doc.Links {
		for _, p := range l.Performances {
			assert.Nil(t, p.Uploader, "a withheld uploader is left out, not a placeholder")
		}
	}

	hidden := 0
	for _, l := range imp.Links {
		if l.IsHidden {
			hidden++
			assert.Equal(t, "https://blog.example.org/ash-hiddenname.html", l.Link.URL)
		}
	}
	assert.Equal(t, 1, hidden, "the link whose URL holds the alias is hidden")
}

func TestBuild_EmailsWithheldAndListed(t *testing.T) {
	imp, review := syntheticImport(t)

	assert.NotContains(t, publicJSON(t, imp), "@example.org")
	notes := personNamed(t, imp, wren).Doc.LicensingNotesMD
	require.NotNil(t, notes)
	assert.Contains(t, *notes, domain.WithheldEmail)
	require.Len(t, review.Contacts, 1)
	assert.Contains(t, review.Contacts[0].Text, "ash@example.org", "the private review names the address")
}

func TestBuild_LinkStatesOnePerNormalizedURL(t *testing.T) {
	imp, _ := syntheticImport(t)

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

// TestBuild_ReviewQuotesTheOriginal: the review is private, so it quotes what it lists as the wiki writes it,
// though the public field withholds a name.
func TestBuild_ReviewQuotesTheOriginal(t *testing.T) {
	d := syntheticDump()
	item := "[https://www.youtube.com/watch?v=video000009 " + hiddenAlias + " sings]"
	d.Pages = append(d.Pages, page(21, "Song of Quoting", []string{mediawiki.CategoryPieces, mediawiki.CategorySongs},
		"{{Infobox Song\n|lyrics_by=[["+wren+"]]\n}}\n== Performances & Recordings ==\n* "+item+"\n"))
	imp, review := Build(mediawiki.ParseCorpus(d), buildTime)

	perf := pieceTitled(t, imp, "Song of Quoting").Doc.Links[0].Performances[0]
	assert.NotContains(t, perf.DescriptionVerbatim, hiddenAlias, "withheld in public")
	incomplete := keep(review.Performances, func(it Item) bool {
		return it.Kind == "incomplete" && it.Page == "Song of Quoting"
	})
	require.Len(t, incomplete, 1)
	assert.Contains(t, incomplete[0].Text, hiddenAlias, "named in the private review")
}

// TestBuild_PersonStatusSource: a status is sourced to the link its date is read from, unless that link is hidden
// for a legal name in its URL; with no such link, to the wiki page.
func TestBuild_PersonStatusSource(t *testing.T) {
	imp, _ := syntheticImport(t)

	doc := personNamed(t, imp, rowan).Doc
	assert.Equal(t, domain.PersonStatusDeceased, doc.Status)
	require.NotNil(t, doc.StatusSourceURL)
	assert.Equal(t, "https://wiki.example.org/wiki/Rowan_Placeholder", *doc.StatusSourceURL, "no link: the page")
	assert.Nil(t, personNamed(t, imp, wren).Doc.StatusSourceURL, "an active person needs no source")

	d := syntheticDump()
	obituary := "https://news.example.org/obituary-placeholder"
	d.Pages = append(d.Pages,
		page(12, "Elm Placeholder", []string{mediawiki.CategoryBards, "In Memoriam"}, "= Elm Placeholder =\n"+
			"== Notes ==\n* ["+obituary+" Deceased October 31, 2020]\n"),
		page(13, "Oak Placeholder", []string{mediawiki.CategoryBards, "In Memoriam"}, "= Oak Placeholder =\n"+
			"== Notes ==\n* [https://news.example.org/ash-hiddenname Deceased October 31, 2020]\n"))
	imp, _ = Build(mediawiki.ParseCorpus(d), buildTime)

	assert.Equal(t, obituary, *personNamed(t, imp, "Elm Placeholder").Doc.StatusSourceURL, "the obituary")
	assert.Equal(t, "https://wiki.example.org/wiki/Oak_Placeholder",
		*personNamed(t, imp, "Oak Placeholder").Doc.StatusSourceURL, "a hidden link is never the source")
}

func TestBuild_LegacyColumns(t *testing.T) {
	imp, _ := syntheticImport(t)

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
	imp, _ := syntheticImport(t)

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
		for _, name := range []string{wren, rowan, hiddenAlias, hiddenSurname, unsureAlias, publicAlias, songTitle} {
			assert.NotContains(t, s, name)
		}
	}
}

func TestBuild_Deterministic(t *testing.T) {
	c := scrubbedCorpus(t)
	a, ra := Build(c, buildTime)
	b, rb := Build(c, buildTime)

	assert.Equal(t, a, b)
	assert.Equal(t, RenderReview(ra), RenderReview(rb))
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

// TestBuild_ControlCharactersRemovedAndListed plants NUL, ESC and a bidirectional override in a piece's notes, a
// link's title and its URL: the text loses them, the URL has them percent-encoded (and a "www." URL is stored
// with https), Validate (which rejects any control character in any string) passes, and the review lists them.
func TestBuild_ControlCharactersRemovedAndListed(t *testing.T) {
	d := syntheticDump()
	d.Pages[3].Wikitext = "{{Infobox Song\n}}\nNotes\x1b[31m with an \u202eoverride.\x00\n" +
		"== Key Links ==\n* [https://example.org/a\x1bb Link \u202etitle]\n* [www.example.org/c Site]\n"

	imp, review := Build(mediawiki.ParseCorpus(d), buildTime)

	require.NoError(t, imp.Validate())
	doc := pieceTitled(t, imp, collidingSong).Doc
	require.NotNil(t, doc.NotesMD)
	assert.Equal(t, "Notes\\[31m with an override.", *doc.NotesMD)
	require.Len(t, doc.Links, 2)
	assert.Equal(t, "https://example.org/a%1Bb", doc.Links[0].URL)
	assert.Equal(t, "https://www.example.org/c", doc.Links[1].URL, "a www. URL is stored absolute")
	require.NotNil(t, doc.Links[0].Title)
	assert.Equal(t, "Link title", *doc.Links[0].Title)
	assert.True(t, containsKind(review.Markup, "text_characters"), "the removals are listed")
}

func TestBuild_PublicAliasHoldingHiddenAliasHidden(t *testing.T) {
	// The alias has a confident SCA form, so the classifier alone makes it public; it holds the hidden alias.
	sca := hiddenAlias + " of the Vale"
	require.True(t, mediawiki.ClassifyPersonAlias(mediawiki.AliasCandidate{Name: sca,
		Source: mediawiki.AliasFromH1Parenthetical}).Public())
	d := syntheticDump()
	d.Pages[0].Wikitext = "= " + wren + " (" + hiddenAlias + ", " + sca + ") =\nWren sings.\n"

	imp, review := Build(mediawiki.ParseCorpus(d), buildTime)

	var got []domain.DocPersonAlias
	for _, a := range personNamed(t, imp, wren).Doc.Aliases {
		if a.Name == sca {
			got = append(got, a)
		}
	}
	require.Len(t, got, 1)
	assert.False(t, got[0].IsPublic, "a public alias holding a hidden alias is hidden")
	assert.True(t, containsKind(review.HiddenNames, "public_alias"), "and listed")
}

func containsKind(items []Item, kind string) bool {
	for _, it := range items {
		if it.Kind == kind {
			return true
		}
	}

	return false
}

func TestBuild_StubPersonWithLegalNameNotCreated(t *testing.T) {
	// A credit names someone the wiki gives as a legal name elsewhere: no person is created, the credit keeps that
	// someone is named, and the review lists it.
	legal := "Hazel Legalname"
	d := syntheticDump()
	d.Pages = append(d.Pages, page(50, "Song of Hiding", []string{mediawiki.CategoryPieces, mediawiki.CategorySongs},
		"{{Infobox Song\n|lyrics_by="+legal+"\n}}\nThe writer's legal name is "+legal+".\n"))

	imp, review := Build(mediawiki.ParseCorpus(d), buildTime)

	require.NoError(t, imp.Validate())
	for _, p := range imp.Persons {
		assert.NotContains(t, p.Doc.Name, "Legalname")
		assert.False(t, domain.HoldsWithheldText(p.Doc.Name), "no person is named by a placeholder")
	}
	credits := pieceTitled(t, imp, "Song of Hiding").Doc.Credits
	require.Len(t, credits, 1)
	assert.Equal(t, domain.AttributionUnknown, credits[0].Attribution)
	assert.Nil(t, credits[0].PersonID)
	require.NotNil(t, credits[0].Note)
	assert.Equal(t, domain.WithheldName, *credits[0].Note)
	assert.True(t, containsKind(review.HiddenNames, "stub_withheld"))
}

// TestBuild_UnsureNamePublishedAsPersonKeptInText: an unsure private name that the import publishes as a stub
// person's name is not withheld from free text, and each occurrence is listed; an unsure name it does not publish,
// and the legal name, still are.
func TestBuild_UnsureNamePublishedAsPersonKeptInText(t *testing.T) {
	sage := "Sage Unsure"
	d := syntheticDump()
	d.Pages = append(d.Pages, page(50, "Song of Sharing", []string{mediawiki.CategoryPieces, mediawiki.CategorySongs},
		"{{Infobox Song\n|lyrics_by="+sage+"\n}}\nWritten by "+sage+" for Rowan Unsure and "+hiddenAlias+".\n"))
	c := mediawiki.ParseCorpus(d)
	c.PrivateNames = append(c.PrivateNames, mediawiki.PrivateName{Text: sage}, mediawiki.PrivateName{Text: rowanUnsure})

	imp, review := Build(c, buildTime)

	require.NoError(t, imp.Validate())
	personNamed(t, imp, sage)
	doc := pieceTitled(t, imp, "Song of Sharing").Doc
	require.NotNil(t, doc.NotesMD)
	assert.Equal(t, "Written by "+sage+" for "+domain.WithheldName+" and "+domain.WithheldName+".", *doc.NotesMD)
	var published []string
	for _, it := range review.HiddenNames {
		if it.Kind == "free_text_published" {
			published = append(published, it.Text)
		}
	}
	require.Len(t, published, 1, "the kept occurrence is listed")
	assert.Contains(t, published[0], `"`+sage+`"`)
	assert.NotContains(t, strings.ToLower(publicJSON(t, imp)), strings.ToLower(hiddenSurname))
}

// TestBuild_CollectionWithLegalNameInURLNotImported: a collection whose name holds no private name but whose URL
// holds the hidden alias is not imported, and is listed.
func TestBuild_CollectionWithLegalNameInURLNotImported(t *testing.T) {
	d := syntheticDump()
	d.Pages[4].Wikitext = "== S ==\n* [https://songs.example.org/ash-hiddenname/book.pdf Placeholder Songbook]\n" +
		"* [https://songs.example.org/other/book.pdf Other Songbook]\n"

	imp, review := Build(mediawiki.ParseCorpus(d), buildTime)

	require.NoError(t, imp.Validate())
	var names []string
	for _, c := range imp.Collections {
		names = append(names, c.Doc.Name)
	}
	assert.Equal(t, []string{"Other Songbook"}, names, "only the collection whose URL names no one is imported")
	assert.True(t, containsKind(review.HiddenNames, "collection_withheld"), "the collection is listed")
	assert.NotContains(t, strings.ToLower(publicJSON(t, imp)), strings.ToLower(hiddenSurname))
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
