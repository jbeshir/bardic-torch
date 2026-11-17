package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

const (
	exampleSongbook = "https://songs.example.org/book/"
	exampleVideo    = "https://www.youtube.com/watch?v=abcdefghijk"
	keyLinksHeading = "Key Links"
	page63          = "p. 63"
	songbook        = "Songbook"
	exampleA        = "https://example.org/one"
)

// linksOf extracts a parsed page's links with no collections or websites known.
func linksOf(wikitext string) PageLinks {
	return extractLinks(ParsePage(wikitext), linkContext{})
}

func rolesOf(links []LinkRecord) map[string][]domain.LinkRole {
	out := map[string][]domain.LinkRole{}
	for _, l := range links {
		out[l.URL] = l.Roles
	}

	return out
}

func TestExtractLinks_RoleBySection(t *testing.T) {
	headings := map[string]domain.LinkRole{
		"Lyrics":                           domain.LinkRoleLyrics,
		"text":                             domain.LinkRoleText,
		sheetMusicHeading:                  domain.LinkRoleSheetMusic,
		"Dancing Instructions":             domain.LinkRoleDanceInstructions,
		"Timeline of Text Sources":         domain.LinkRoleTextSource,
		"Translations":                     domain.LinkRoleTranslation,
		"Performances & Recordings":        domain.LinkRolePerformance,
		"Known Performances":               domain.LinkRolePerformance,
		"Licensing and Etiquette":          domain.LinkRoleLicensingSource,
		"Bard’s Website":                   domain.LinkRoleWebsite,
		"Bard's Websites":                  domain.LinkRoleWebsite,
		"Other Pages & External Resources": domain.LinkRoleExternalProfile,
		keyLinksHeading:                    domain.LinkRoleOther,
		"NOTES":                            domain.LinkRoleOther,
		"Something Else":                   domain.LinkRoleOther,
	}
	for heading, role := range headings {
		links := linksOf("== " + heading + " ==\n* [https://example.org/a Link]\n").Links
		require.Len(t, links, 1, heading)
		assert.Equal(t, []domain.LinkRole{role}, links[0].Roles, heading)
	}
}

func TestExtractLinks_KeyLinkOnCreditedWebsiteIsOfficial(t *testing.T) {
	ctx := linkContext{creditedSites: []string{"https://wren.example.org/"}}
	links := extractLinks(ParsePage("== Key Links ==\n* [http://www.wren.example.org/songs/a Lyrics]\n"+
		"* [https://wren.example.org.evil.example/ Other]\n"), ctx).Links

	require.Len(t, links, 2)
	assert.Equal(t, []domain.LinkRole{domain.LinkRoleOfficial}, links[0].Roles, "any scheme, under the site")
	assert.Equal(t, []domain.LinkRole{domain.LinkRoleOther}, links[1].Roles)
}

func TestExtractLinks_DedupeByNormalizedURLKeepsRoles(t *testing.T) {
	pl := linksOf("== Lyrics ==\n* [https://www.example.org/one?utm_source=x Words]\n" +
		"== Sheet Music ==\n* [https://example.org/one Score]\n" +
		"== Lyrics ==\n* [https://Example.org/one Again]\n")

	require.Len(t, pl.Links, 1)
	l := pl.Links[0]
	assert.Equal(t, "https://www.example.org/one?utm_source=x", l.URL, "the first URL as entered")
	assert.Equal(t, exampleA, l.NormalizedURL)
	assert.Equal(t, "Words", l.Title, "the first text")
	assert.Equal(t, []domain.LinkRole{domain.LinkRoleLyrics, domain.LinkRoleSheetMusic}, l.Roles)
	assert.Equal(t, []SectionKey{SectionLyrics, SectionSheetMusic}, l.Sections)
	assert.Len(t, pl.Occurrences, 3, "every occurrence is kept")
	assert.Empty(t, pl.RepeatedURLs(), "the URLs as entered differ")
}

func TestExtractLinks_RepeatedURLs(t *testing.T) {
	pl := linksOf("== Lyrics ==\n* [https://example.org/one A]\n* [https://example.org/b B]\n* [https://example.org/one C]\n")

	assert.Equal(t, []string{exampleA}, pl.RepeatedURLs())
}

func TestExtractLinks_Platform(t *testing.T) {
	cases := map[string]string{
		exampleVideo:                               PlatformYouTube,
		"https://youtu.be/abcdefghijk":             PlatformYouTube,
		"https://open.spotify.com/track/1":         PlatformSpotify,
		"https://wren.bandcamp.com/album/a":        PlatformBandcamp,
		"https://soundcloud.com/wren/a":            PlatformSoundCloud,
		"https://www.reverbnation.com/wren":        PlatformReverbNation,
		"https://www.facebook.com/wren":            PlatformFacebook,
		"https://en.wikipedia.org/wiki/A":          PlatformWikipedia,
		"https://www.gutenberg.org/ebooks/1":       PlatformGutenberg,
		"https://songs.example.org/files/Book.PDF": PlatformPDF,
		"https://www.songs.example.org/a.html":     "songs.example.org",
		"https://notyoutube.com/a":                 "notyoutube.com",
	}
	for raw, platform := range cases {
		links := linksOf("== Lyrics ==\n* [" + raw + " A]\n").Links
		require.Len(t, links, 1, raw)
		assert.Equal(t, platform, links[0].Platform, raw)
	}
}

func TestExtractLinks_PlaceType(t *testing.T) {
	collections := NewCollectionMatcher([]CollectionRecord{
		{NormalizedURL: songbookPDF, Kind: domain.CollectionKindSongbookPDF},
		{NormalizedURL: "https://archive.example.org/songs/", Kind: domain.CollectionKindSongbookWebsite},
	})
	ctx := linkContext{collections: collections, bardSites: []string{"https://wren.example.org/"}}
	links := extractLinks(ParsePage("== Lyrics ==\n* ["+exampleVideo+" Video]\n"+
		"* [https://open.spotify.com/track/1 Track]\n* [https://songs.example.org/book.pdf#page=3 Book]\n"+
		"* [https://archive.example.org/songs/a.html Archive]\n* [https://wren.example.org/a Wren]\n"+
		"* [https://elsewhere.example.org/ Else]\n== Bard's Website ==\n* [https://site.example.org/ Site]\n"), ctx).Links

	got := map[string]domain.LinkPlaceType{}
	for _, l := range links {
		got[l.Title] = l.PlaceType
	}
	assert.Equal(t, map[string]domain.LinkPlaceType{
		"Video": domain.LinkPlaceTypeRecordingPlatform, "Track": domain.LinkPlaceTypeStreaming,
		"Book": domain.LinkPlaceTypeBook, "Archive": domain.LinkPlaceTypeCommunityArchive,
		"Wren": domain.LinkPlaceTypeAuthorSite, "Else": "", "Site": domain.LinkPlaceTypeAuthorSite,
	}, got)
}

func TestLocator(t *testing.T) {
	cases := []struct{ text, fragment, line, want string }{
		{"Songbook, p. 63", "", "", page63},
		{"Book page 63", "", "", page63},
		{"Book pg 63", "", "", page63},
		{songbook, "page=63", "", page63},
		{songbook, "zoom=1&page=7", "", "p. 7"},
		{songbook, "", "* [x Songbook], p.12", "p. 12"},
		{"Pennsic 46", "", "", ""},
		{songbook, "pages", "", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, Locator(c.text, c.fragment, c.line), c)
	}
}

func TestExtractLinks_Locator(t *testing.T) {
	links := linksOf("== Lyrics ==\n* [https://songs.example.org/book.pdf#page=63 Songbook]\n" +
		"* [https://songs.example.org/other.pdf Other], page 12\n" +
		"* [https://songs.example.org/a.pdf A] and [https://songs.example.org/b.pdf B], p. 5\n").Links

	require.Len(t, links, 4)
	assert.Equal(t, page63, links[0].Locator, "from the fragment")
	assert.Equal(t, "p. 12", links[1].Locator, "from the line")
	assert.Empty(t, links[2].Locator, "a line with two links locates neither")
	assert.Empty(t, links[3].Locator)
}

func TestExtractLinks_BareURL(t *testing.T) {
	pl := linksOf("== Performances & Recordings ==\n* Recorded at https://example.org/v.\n")

	require.Len(t, pl.Occurrences, 1)
	assert.False(t, pl.Occurrences[0].Bracketed)
	require.Len(t, pl.Links, 1)
	assert.Equal(t, "https://example.org/v", pl.Links[0].URL)
	assert.Empty(t, pl.Links[0].Title)
}

func TestExtractLinks_RepairedDoubleBracket(t *testing.T) {
	w := standardWiki()
	bard := parseFixtureBard(w, ash, bardWikitext(ash, "Bard's Website", "* [[https://ash.example.org/ Ash's site]]"))

	pl := extractLinks(bard.Parsed, linkContext{})

	require.Len(t, pl.Links, 1)
	assert.Equal(t, "https://ash.example.org/", pl.Links[0].NormalizedURL)
	assert.Equal(t, "Ash's site", pl.Links[0].Title)
	assert.Equal(t, []domain.LinkRole{domain.LinkRoleWebsite}, pl.Links[0].Roles)
}

func TestExtractLinks_UnnormalizableDropped(t *testing.T) {
	pl := linksOf("== Lyrics ==\n* [https://user:secret@example.org/a A]\n* [https://example.org/b B]\n")

	require.Len(t, pl.Links, 1)
	assert.Equal(t, "https://example.org/b", pl.Links[0].URL)
}

func TestExtractLinks_InfoboxLinksAreOccurrencesOnly(t *testing.T) {
	pl := linksOf(pieceWikitext(aSong, [][2]string{{lyricsByParam, "[https://en.wikipedia.org/wiki/A " + rowan + "]"}},
		lyricsHeading, "* [https://example.org/one A]"))

	assert.Equal(t, []LinkOccurrence{
		{URL: exampleA, Text: "A", Bracketed: true, Line: "* [https://example.org/one A]",
			Section: SectionLyrics},
		{URL: "https://en.wikipedia.org/wiki/A", Text: rowan, Bracketed: true, Section: "infobox:" + lyricsByParam},
	}, pl.Occurrences)
	assert.Equal(t, map[string][]domain.LinkRole{exampleA: {domain.LinkRoleLyrics}}, rolesOf(pl.Links))
}

func TestHasTrackingParameter(t *testing.T) {
	for raw, want := range map[string]bool{
		exampleVideo + "&list=PL1": true, "https://youtu.be/a?si=x": true, "https://example.org/?utm_medium=x": true,
		"https://example.org/?feature=share": true, "https://example.org/?pp=x": true, exampleVideo: false,
		"https://example.org/?ppx=1": false,
	} {
		assert.Equal(t, want, HasTrackingParameter(raw), raw)
	}
}
