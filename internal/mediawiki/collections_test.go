package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

const (
	exampleSite     = "https://site.example.org/"
	aCollection     = "A collection"
	archiveList     = "https://archive.example.org/songs/old/list.html"
	archiveSongbook = "https://archive.example.org/songs/book.pdf"
)

// collectionsPage is a Collections page with the sections given as letter and body pairs.
func collectionsPage(sections ...string) Page {
	text := "Collections of songs.\n"
	for k := 0; k+1 < len(sections); k += 2 {
		text += "== " + sections[k] + " ==\n" + sections[k+1] + "\n"
	}

	return Page{Title: CollectionsPage, Wikitext: text}
}

func TestParseCollections_Entries(t *testing.T) {
	c := ParseCollections(collectionsPage(
		"A", "* [https://www.archive.example.org/songs/ Ash's Songbook - a big one] - kept since 1990\n"+
			"** [https://archive.example.org/songs/list.html Directory of songbooks]\n"+
			"* no link here\n",
		"B", "",
		"C", "* [https://c.example.org/ C &amp; D]\n",
	), TermPlan{})

	require.Len(t, c.Records, 3)
	assert.Equal(t, 3, c.Entries())
	first := c.Records[0]
	assert.Equal(t, CollectionRecord{
		Letter: "A", Label: "Ash's Songbook - a big one", URL: "https://www.archive.example.org/songs/",
		NormalizedURL: "https://archive.example.org/songs/", Note: "kept since 1990",
		Kind: domain.CollectionKindSongbookWebsite, Description: "a big one; kept since 1990",
	}, first)
	assert.Equal(t, "Ash's Songbook", first.Name())
	assert.Equal(t, domain.CollectionKindIndexOrLinkList, c.Records[1].Kind)
	assert.Equal(t, "C & D", c.Records[2].Label)
}

func TestParseCollections_Kinds(t *testing.T) {
	cases := []struct {
		url, label string
		kind       domain.CollectionKind
	}{
		{"https://www.youtube.com/playlist?list=PL1", "Performances", domain.CollectionKindYouTubePlaylist},
		{"https://www.youtube.com/@channel", "A channel", domain.CollectionKindYouTubeChannel},
		{"https://site.example.org/book.pdf", aCollection, domain.CollectionKindSongbookPDF},
		{"https://docs.google.com/document/d/1", aCollection, domain.CollectionKindGoogleDocSongbook},
		{"https://www.scribd.com/document/1", aCollection, domain.CollectionKindHostedDocument},
		{"https://wiki.example.org/index.php?title=Category:Songs", aCollection, domain.CollectionKindWikiCategory},
		{exampleSite, "The kingdom wiki", domain.CollectionKindWikiCategory},
		{"https://example.org/song_wiki/", "Song Wiki", domain.CollectionKindWiki},
		{"https://horntip.com/a/", "Old book", domain.CollectionKindArchivedSongbook},
		{exampleSite, "Bardic links", domain.CollectionKindIndexOrLinkList},
		{exampleSite, "A snogbook", domain.CollectionKindSongbookWebsite},
		{"https://ash.blogspot.com/", "Ash writes", domain.CollectionKindBlog},
		{exampleSite, "Bardic page", domain.CollectionKindWebsite},
	}
	for _, c := range cases {
		assert.Equal(t, c.kind, classifyCollection(c.url, c.label), c.url+" "+c.label)
	}
}

func TestParseCollections_DuplicateMerged(t *testing.T) {
	c := ParseCollections(collectionsPage(
		"H", "* [https://history.example.org/bardic/ History of the Kingdom]\n",
		"W", "* [https://www.history.example.org/bardic/ Kingdom history] - again\n",
	), TermPlan{})

	assert.Equal(t, 2, c.Entries())
	require.Len(t, c.Records, 1)
	assert.Equal(t, "History of the Kingdom", c.Records[0].Label)
	require.Len(t, c.Duplicates, 1)
	assert.Equal(t, "Kingdom history", c.Duplicates[0].Label)
	assert.Equal(t, "W", c.Duplicates[0].Letter)
}

func TestParseCollections_UnnormalizableDropped(t *testing.T) {
	c := ParseCollections(collectionsPage("A", "* [https://user:pw@example.org/ A]\n"), TermPlan{})

	assert.Empty(t, c.Records)
	require.Len(t, c.Dropped, 1)
	assert.Equal(t, 1, c.Entries())
}

func TestParseCollections_Region(t *testing.T) {
	w := standardWiki()
	w.category("West Kingdom", CategorySCAKingdom)
	w.page(Page{Title: ash, Categories: []string{CategoryBards, CategorySCABards, "West Kingdom"}})
	terms := w.site().Terms

	c := ParseCollections(collectionsPage("A",
		"* [https://a.example.org/ Songs of Calontir]\n"+
			"* [https://b.example.org/ History of the Kingdom of the West]\n"+
			"* [https://c.example.org/ Calontir and West Kingdom songs]\n"+
			"* [https://d.example.org/ Calontirish songs]\n"), terms)

	require.Len(t, c.Records, 4)
	assert.Equal(t, kingdom, c.Records[0].RegionTerm)
	assert.Equal(t, "West Kingdom", c.Records[1].RegionTerm)
	assert.Empty(t, c.Records[2].RegionTerm, "a label naming two kingdoms has no region")
	assert.Empty(t, c.Records[3].RegionTerm, "kingdoms are whole words")
}

func collectionMatcher() CollectionMatcher {
	return NewCollectionMatcher([]CollectionRecord{
		{NormalizedURL: "https://archive.example.org/songs/index.html", Kind: domain.CollectionKindWebsite},
		{NormalizedURL: archiveList, Kind: domain.CollectionKindSongbookWebsite},
		{NormalizedURL: "https://archive.example.org/songs/old/more.html", Kind: domain.CollectionKindBlog},
		{NormalizedURL: archiveSongbook, Kind: domain.CollectionKindSongbookPDF},
		{NormalizedURL: "https://root.example.org/index.html", Kind: domain.CollectionKindWebsite},
		{NormalizedURL: "https://youtube.com/@channel/videos", Kind: domain.CollectionKindWebsite},
		{NormalizedURL: "https://index.example.org/a/links.html", Kind: domain.CollectionKindIndexOrLinkList},
	})
}

func TestMatchCollection_ExactFirst(t *testing.T) {
	m := collectionMatcher()

	assert.Equal(t, CollectionMatch{URL: archiveSongbook},
		m.Match(archiveSongbook))
	assert.Equal(t, CollectionMatch{URL: archiveSongbook},
		m.Match("https://archive.example.org/songs/book.pdf#page=3"), "a fragment locates a place in the document")
	assert.Equal(t, CollectionMatch{URL: "https://index.example.org/a/links.html"},
		m.Match("https://index.example.org/a/links.html"), "any kind matches exactly")
	assert.Equal(t, CollectionMatch{URL: archiveList},
		m.Match(archiveList), "an exact match beats a tie")
}

func TestMatchCollection_LongestPrefix(t *testing.T) {
	m := collectionMatcher()

	assert.Equal(t, CollectionMatch{URL: "https://archive.example.org/songs/index.html"},
		m.Match("https://archive.example.org/songs/a.html"))
	assert.Equal(t, CollectionMatch{}, m.Match("https://other.example.org/songs/a.html"), "another host")
	assert.Equal(t, CollectionMatch{}, m.Match("https://archive.example.org/other/a.html"))
}

func TestMatchCollection_TieMatchesNone(t *testing.T) {
	m := collectionMatcher()

	assert.Equal(t, CollectionMatch{}, m.Match("https://archive.example.org/songs/old/a.html"))
}

func TestMatchCollection_NoPrefixMatch(t *testing.T) {
	m := collectionMatcher()

	assert.Equal(t, CollectionMatch{}, m.Match("https://root.example.org/a.html"), "a prefix of no path segment")
	assert.Equal(t, CollectionMatch{}, m.Match("https://youtube.com/@channel/other"), "a shared document host")
	assert.Equal(t, CollectionMatch{}, m.Match("https://index.example.org/a/b.html"), "a link list's kind")
}
