package mediawiki

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

func TestWikitextToMarkdown(t *testing.T) {
	for in, want := range map[string]string{
		"'''Bold''' and ''italic'' and '''''both'''''":      "**Bold** and *italic* and ***both***",
		"[https://example.invalid/a A page]":                "[A page](https://example.invalid/a)",
		"[https://example.invalid/a]":                       "<https://example.invalid/a>",
		"See https://example.invalid/a.":                    "See <https://example.invalid/a>.",
		"[https://example.invalid/a_(b) Text]":              "[Text](<https://example.invalid/a_(b)>)",
		"[[A Song]] and [[A Song|the song]]":                "A Song and the song",
		"[[:Category:Warlike|Warlike]] songs[[Category:X]]": "Warlike songs",
		"[[File:Picture.png|thumb]]Text":                    "Text",
		"* One\n** Two\n# Three":                            "- One\n  - Two\n1. Three",
		"Line one<br>Line two":                              "Line one\\\nLine two",
		"A *star*, an_underscore, [brackets] and 1 < 2 > 0": `A \*star\*, an\_underscore, \[brackets\] and 1 \< 2 \> 0`,
		"# not a heading":                                   "1. not a heading",
		"- dash\n1. number":                                 `\- dash` + "\n" + `1\. number`,
		"Kept<!-- a comment -->{{small|gone}} &amp; done":   "Kept & done",
		"<span style=\"x\">Inside</span>":                   "Inside",
		"One\n\n\n\nTwo":                                    "One\n\nTwo",
	} {
		assert.Equal(t, want, WikitextToMarkdown(in), in)
	}
}

// lyricMarker marks a synthetic lyric line; it must never reach any field of a record.
const lyricMarker = "scrubbedlyric"

// markedLyricLines are n synthetic lyric lines carrying lyricMarker.
func markedLyricLines(n int) string {
	lines := make([]string, n)
	for k := range lines {
		lines[k] = lyricMarker + " line " + string(rune('a'+k))
	}

	return strings.Join(lines, "\n")
}

// assertNoLyricMarker checks every string reachable from v by reflection.
func assertNoLyricMarker(t *testing.T, v reflect.Value, path string) {
	t.Helper()
	kind := v.Kind()
	if kind == reflect.String {
		assert.NotContains(t, v.String(), lyricMarker, path)
		assert.NotContains(t, strings.ToLower(v.String()), "<poem", path)
	}
	if kind == reflect.Struct {
		for k := range v.NumField() {
			assertNoLyricMarker(t, v.Field(k), path+"."+v.Type().Field(k).Name)
		}
	}
	if kind == reflect.Slice || kind == reflect.Array {
		for k := range v.Len() {
			assertNoLyricMarker(t, v.Index(k), path)
		}
	}
	if (kind == reflect.Pointer || kind == reflect.Interface) && !v.IsNil() {
		assertNoLyricMarker(t, v.Elem(), path)
	}
	if kind == reflect.Map {
		for _, key := range v.MapKeys() {
			assertNoLyricMarker(t, key, path)
			assertNoLyricMarker(t, v.MapIndex(key), path)
		}
	}
}

func TestPieceFields_InlineLyricsDropped(t *testing.T) {
	text := pieceWikitext(aSong, [][2]string{{titleParam, aSong}},
		lyricsHeading, "<poem>\n"+markedLyricLines(6)+"\n</poem>\n* [https://example.invalid/lyrics Lyrics]",
		"Notes", "A note.\n<poem style=\"x\">\n"+markedLyricLines(2)+"\n</poem>",
		"Licensing & Etiquette", "The words are old.\n<POEM>\n"+markedLyricLines(3))
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text, Categories: pieceCategories()})

	assertNoLyricMarker(t, reflect.ValueOf(p), "PieceRecord")
	assert.True(t, p.Lyrics.HadInlineLyrics)
	assert.Equal(t, []DroppedLyricsSection{{Heading: lyricsHeading, Lines: 6}, {Heading: "Notes", Lines: 2},
		{Heading: "Licensing & Etiquette", Lines: 3}}, p.Lyrics.Sections)
	assert.Equal(t, "A note.", p.Content.NotesMD)
	assert.Equal(t, "The words are old.", p.Content.LicensingNotesMD)
	assert.Empty(t, p.Content.VersionNotesMD)
}

func TestPieceFields_ProseLyricsDropped(t *testing.T) {
	text := pieceWikitext(aSong, nil, lyricsHeading,
		"* [https://example.invalid/lyrics Lyrics]\n"+markedLyricLines(2)+"\n: "+lyricMarker+" indented\n* "+
			lyricMarker+" bulleted")
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text, Categories: pieceCategories()})

	assertNoLyricMarker(t, reflect.ValueOf(p), "PieceRecord")
	assert.True(t, p.Lyrics.HadInlineLyrics)
	assert.Empty(t, p.Content.VersionNotesMD)
}

func TestPieceFields_VersionNotesKept(t *testing.T) {
	text := pieceWikitext(aSong, nil, lyricsHeading,
		"There are two versions; see [https://example.invalid/v the archive].\n"+
			"* [https://example.invalid/lyrics Lyrics]\n"+
			"'''Note:''' Please ensure lyrics are only included here if they are in the public domain.\n"+
			"The second verse is often left out.")
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text, Categories: pieceCategories()})

	assert.False(t, p.Lyrics.HadInlineLyrics)
	assert.Equal(t, "There are two versions; see [the archive](https://example.invalid/v).\n"+
		"The second verse is often left out.", p.Content.VersionNotesMD)
}

func TestPieceFields_NotesFromLeadNotesAndOtherSections(t *testing.T) {
	text := "Lead text.\n" + pieceWikitext(aSong, [][2]string{{titleParam, aSong}},
		"Key Links", "* [https://example.invalid/a A link]",
		"Notes", "A '''note'''.\n----\n[[Category:Warlike]]",
		"Translations of Note", "Into Welsh.",
		lyricsHeading, "* [https://example.invalid/l Lyrics]") +
		"=== Chorus ===\nNested under the lyrics, never a note.\n" +
		"== Timeline of Text Sources ==\n* 1590: [https://example.invalid/s A source]\n"
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text, Categories: pieceCategories()})

	assert.Equal(t, "Lead text.\n\nA **note**.\n\n### Translations of Note\n\nInto Welsh.", p.Content.NotesMD)
	assert.Equal(t, "- 1590: [A source](https://example.invalid/s)", p.Content.HistoricalSourceMD)
}

func TestPieceFields_LicensingHintsStripped(t *testing.T) {
	text := pieceWikitext(aSong, nil, "Licensing & Etiquette",
		"Information about licensing, permissions, and performance etiquette should always be sourced from the "+
			"author.\n* (Add sourced information here)\n* Link\nPerform freely, says [https://example.invalid/p the "+
			"author].")
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text, Categories: pieceCategories()})

	assert.Equal(t, "Perform freely, says [the author](https://example.invalid/p).", p.Content.LicensingNotesMD)
	assert.Equal(t, domain.LicensingStatusKnown, p.Content.LicensingStatus)

	unsourced := parseFixturePiece(standardWiki(), Page{Title: aSong, Categories: pieceCategories(),
		Wikitext: pieceWikitext(aSong, nil, "Licensing & Etiquette", "Perform freely.")})
	assert.Equal(t, domain.LicensingStatusUnknown, unsourced.Content.LicensingStatus, "a claim needs a source")

	unknown := parseFixturePiece(standardWiki(), Page{Title: aSong, Categories: pieceCategories(),
		Wikitext: pieceWikitext(aSong, nil, "Licensing & Etiquette",
			"We cannot find a central page with this bard's preferences or licensing.\n"+
				"[https://example.invalid/p A page]")})
	assert.Equal(t, domain.LicensingStatusUnknown, unknown.Content.LicensingStatus)
	assert.Equal(t, "[A page](https://example.invalid/p)", unknown.Content.LicensingNotesMD)
}

func TestPieceFields_PublicDomainIsAReviewFactNotALicence(t *testing.T) {
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Categories: pieceCategories(),
		Wikitext: pieceWikitext(aSong, nil, "Licensing & Etiquette", "The song is in the Public Domain.")})

	assert.True(t, p.Content.SaysPublicDomain)
	assert.Equal(t, "The song is in the Public Domain.", p.Content.LicensingNotesMD)
	assert.Equal(t, domain.LicensingStatusUnknown, p.Content.LicensingStatus)
}

func TestPieceFields_PeriodFromCenturyOrigin(t *testing.T) {
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: pieceWikitext(aSong, nil),
		Categories: pieceCategories(cat13thCentury, "16th Century", "10th Century (Topic)")})

	start, end := int16(1201), int16(1600)
	assert.Equal(t, &start, p.Content.PeriodStart)
	assert.Equal(t, &end, p.Content.PeriodEnd)
	assert.Equal(t, []string{cat13thCentury, "16th Century"}, p.Content.PeriodCategories,
		"a topic century is what the piece is about, not when it is from")

	none := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: pieceWikitext(aSong, nil),
		Categories: pieceCategories("10th Century (Topic)")})
	assert.Nil(t, none.Content.PeriodStart)
}

// TestScrubbedPieces_NoLyricMarkerInAnyField checks every field of every piece of the committed scrubbed copy,
// whose inline lyrics are placeholder lines carrying the marker.
func TestScrubbedPieces_NoLyricMarkerInAnyField(t *testing.T) {
	for _, p := range scrubbedSite(t).Pieces() {
		assertNoLyricMarker(t, reflect.ValueOf(p), p.Title)
	}
}
