package mediawiki

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

// oracleDeviations are the ways this package deliberately differs from the reference scripts, whose output
// (analysis/*.json) the research oracle tests compare against. Each oracle comparison that allows a difference
// names its entry (deviation). Any other difference needs a plan amendment and an entry here.
var oracleDeviations = map[string]string{
	"bards.aliases.every_group": "aliases come from every H1 parenthetical and the legal/mundane/aka markers; the " +
		"reference takes only the last parenthetical, so the oracle's aliases must be a subset of ours",
	"bards.aliases.title_lead": "an H1 parenthetical that is the page title is not an alias, and the H1's lead is " +
		"one instead; the reference keeps the title as an alias",
	"lyrics.poem_forms": "every <poem\\b[^>]*> element in any case, closed or not, and every comment holding one, " +
		"is a lyrics block; the reference counts only closed <poem>…</poem> (the dump has no other form)",
	"lyrics.prose_lines": "a Lyrics/Text section's prose lines are every line that is not blank, a hint or " +
		"carrying a link, whatever its first character; the reference skips lines starting with * # : { | } !",
	"pages.test_type": "the template test page Roses of the East is typed test; the reference types it other",
	"links.dedupe": "an external URL repeated on a page is one link with several roles (chunk 6); the reference " +
		"lists each occurrence",
	"sections.single_line_headings": "Sections is hand-coded, RE2 having no backreference, and a heading never " +
		"spans two lines; the reference's \\s may cross a line break (the dump has no such heading)",
	"titles.parentheticals": "a title field that is the page title and a trailing parenthetical gives the " +
		"parenthetical's parts (a subtitle, or alternates when \"/\" splits it); the reference splits only at \"/\"",
	"titles.h1_alternates": "the H1, parentheticals included, is an alternate title (piece H1 parentheticals name " +
		"the piece, never a person); the reference lists the H1 apart",
	"titles.rejected": "an alternate that is another page's title or a category or facet word is rejected and " +
		"listed, not imported (Fight for the Midrealm, Birthday Dirge); the reference keeps it",
	"titles.unparsed_prose": "only lead lines starting \"Also known as\" are parsed; other lines naming titles are " +
		"listed for review; the reference matches \"also known as\" anywhere in the line",
	"titles.quotes_and_ampersands": "quotes around a title are dropped and \" & \" separates titles in an " +
		"\"Also known as\" line; the reference keeps the quotes and splits only at \",\" and \"or\"",
	"titles.redirects": "redirects to a piece are its aliases of kind redirect; the reference lists them only in " +
		"redirects.json",
	"sections.masked_comments": "sections are found with comments masked by spaces, not removed, so offsets are " +
		"offsets into the wikitext; the dump's headings are the same either way",
}

// deviation returns the reason for a deviation the comparison relies on.
func deviation(t *testing.T, key string) string {
	t.Helper()
	reason, ok := oracleDeviations[key]
	wikidumptest.Equal(t, wikidumptest.Location{Field: "oracleDeviations." + key, Offset: -1}, true, ok)

	return reason
}

type oracleLink struct {
	Kind     WikilinkKind `json:"kind"`
	Target   string       `json:"target"`
	Display  *string      `json:"display"`
	Fragment string       `json:"fragment"`
}

type oracleExternal struct {
	URL       string  `json:"url"`
	Text      *string `json:"text"`
	Bracketed bool    `json:"bracketed"`
	Line      string  `json:"line"`
}

type oracleItem struct {
	Depth int    `json:"depth"`
	Raw   string `json:"raw"`
	Text  string `json:"text"`
}

type oracleSection struct {
	Level         int              `json:"level"`
	Heading       *string          `json:"heading"`
	Key           SectionKey       `json:"key"`
	Status        string           `json:"status"`
	Items         []oracleItem     `json:"items"`
	ExternalLinks []oracleExternal `json:"external_links"`
	Wikilinks     []oracleLink     `json:"wikilinks"`
	PlainText     string           `json:"plain_text"`
	PoemLines     int              `json:"poem_lines"`
}

type oracleValueItem struct {
	Type      ValueItemType `json:"type"`
	Target    string        `json:"target"`
	Display   string        `json:"display"`
	URL       string        `json:"url"`
	Label     string        `json:"label"`
	Qualifies string        `json:"qualifies"`
	Depth     int           `json:"depth"`
}

type oracleParam struct {
	Raw    string `json:"raw"`
	Parsed struct {
		Raw         string            `json:"raw"`
		Items       []oracleValueItem `json:"items"`
		Separators  []string          `json:"separators"`
		FreeText    *string           `json:"free_text"`
		Placeholder bool              `json:"placeholder"`
	} `json:"parsed"`
}

type oraclePiece struct {
	PageID     int64                  `json:"pageid"`
	H1         *string                `json:"h1"`
	Sections   []oracleSection        `json:"sections"`
	Infobox    map[string]oracleParam `json:"infobox"`
	ParamOrder []string               `json:"infobox_param_order"`
	InlineText json.RawMessage        `json:"inline_text"`
}

type oracleBard struct {
	PageID  int64    `json:"pageid"`
	Title   string   `json:"title"`
	Aliases []string `json:"aliases"`
}

type oracle struct {
	pages  map[int64]Page
	pieces []oraclePiece
	bards  []oracleBard
}

func loadOracle(t *testing.T) oracle {
	t.Helper()
	dumpFS, analysisFS := wikidumptest.Research(t)
	dump, err := LoadDump(dumpFS)
	wikidumptest.NoError(t, "dump", err)
	var pieces struct {
		Pieces []oraclePiece `json:"pieces"`
	}
	wikidumptest.NoError(t, "pieces.json", decodeAnalysis(analysisFS, "pieces.json", &pieces))
	var bards struct {
		Bards []oracleBard `json:"bards"`
	}
	wikidumptest.NoError(t, "bards.json", decodeAnalysis(analysisFS, "bards.json", &bards))
	pages := map[int64]Page{}
	for _, p := range dump.Pages {
		if p.NS == NSMain {
			pages[p.PageID] = p
		}
	}

	return oracle{pages: pages, pieces: pieces.Pieces, bards: bards.Bards}
}

func decodeAnalysis(fsys fs.FS, name string, v any) error {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}

	return json.Unmarshal(b, v)
}

func TestResearchOracle_PageTypes(t *testing.T) {
	o := loadOracle(t)
	deviation(t, "pages.test_type")
	want := map[int64]PageType{}
	for _, p := range o.pieces {
		want[p.PageID] = PageTypePiece
	}
	for _, b := range o.bards {
		want[b.PageID] = PageTypeBard
	}
	for id, p := range o.pages {
		got := TypePage(p)
		if got != PageTypePiece && got != PageTypeBard {
			got = ""
		}
		wikidumptest.Equal(t, wikidumptest.Location{PageID: id, Field: "type", Offset: -1}, want[id], got)
	}
	wikidumptest.Equal(t, wikidumptest.Location{Field: "test page type", Offset: -1}, PageTypeTest,
		TypePage(Page{Title: testPage}))
}

func TestResearchOracle_Sections(t *testing.T) {
	o := loadOracle(t)
	deviation(t, "sections.single_line_headings")
	for _, piece := range o.pieces {
		parsed := ParsePage(o.pages[piece.PageID].Wikitext)
		at := wikidumptest.Location{PageID: piece.PageID, Field: "sections.count", Offset: -1}
		if !wikidumptest.Equal(t, at, len(piece.Sections), len(parsed.Sections)) {
			continue
		}
		poemLines := poemLinesBySection(o.pages[piece.PageID].Wikitext)
		for k, want := range piece.Sections {
			compareSection(t, piece.PageID, k, want, parsed.Sections[k], poemLines[k])
		}
	}
}

func poemLinesBySection(wikitext string) map[int]int {
	lines := map[int]int{}
	for _, b := range InlineLyricsBlocks(wikitext).Blocks {
		if b.Kind == LyricsPoem {
			lines[b.Section] += b.Lines
		}
	}

	return lines
}

func compareSection(t *testing.T, pageID int64, k int, want oracleSection, got PageSection, poemLines int) {
	t.Helper()
	at := func(field string) wikidumptest.Location {
		return wikidumptest.Location{PageID: pageID, Field: "sections." + field, Offset: k}
	}
	wikidumptest.Equal(t, at("level"), want.Level, got.Level)
	wikidumptest.Equal(t, at("heading"), deref(want.Heading), got.Heading)
	wikidumptest.Equal(t, at("key"), want.Key, got.Key)
	wikidumptest.Equal(t, at("status"), want.Status == emptyWord, got.Content.Empty())
	gotItems := make([]oracleItem, 0, len(got.Content.Items))
	for _, it := range got.Content.Items {
		gotItems = append(gotItems, oracleItem(it))
	}
	wikidumptest.Equal(t, at("items"), want.Items, gotItems)
	gotExternal := make([]oracleExternal, 0, len(got.Content.ExternalLinks))
	for _, e := range got.Content.ExternalLinks {
		gotExternal = append(gotExternal, oracleExternal{URL: e.URL, Text: optional(e.Text), Bracketed: e.Bracketed,
			Line: e.Line})
	}
	wikidumptest.Equal(t, at("external_links"), want.ExternalLinks, gotExternal)
	gotLinks := make([]oracleLink, 0, len(got.Content.Wikilinks))
	for _, l := range got.Content.Wikilinks {
		link := oracleLink{Kind: l.Kind, Target: l.Target, Fragment: l.Fragment}
		if l.Piped {
			link.Display = &l.Display
		}
		gotLinks = append(gotLinks, link)
	}
	wikidumptest.Equal(t, at("wikilinks"), want.Wikilinks, gotLinks)
	wikidumptest.Equal(t, at("plain_text"), want.PlainText, truncateUTF16(got.Content.PlainText, 4000))
	wikidumptest.Equal(t, at("poem_lines"), want.PoemLines, poemLines)
}

func TestResearchOracle_Infoboxes(t *testing.T) {
	o := loadOracle(t)
	for _, piece := range o.pieces {
		boxes := ParsePage(o.pages[piece.PageID].Wikitext).Infoboxes
		at := func(field string, k int) wikidumptest.Location {
			return wikidumptest.Location{PageID: piece.PageID, Field: "infobox." + field, Offset: k}
		}
		if !wikidumptest.Equal(t, at("present", -1), piece.Infobox != nil, len(boxes) > 0) || len(boxes) == 0 {
			continue
		}
		box := boxes[0]
		names := make([]string, 0, len(box.Params))
		for _, p := range box.Params {
			names = append(names, p.Name)
		}
		wikidumptest.Equal(t, at("param_order", -1), piece.ParamOrder, names)
		for k, name := range piece.ParamOrder {
			want := piece.Infobox[name]
			got, _ := box.Param(name)
			wikidumptest.Equal(t, at("raw", k), want.Raw, got.Raw)
			wikidumptest.Equal(t, at("value.raw", k), want.Parsed.Raw, got.Value.Raw)
			wikidumptest.Equal(t, at("value.placeholder", k), want.Parsed.Placeholder, got.Value.Placeholder)
			wikidumptest.Equal(t, at("value.free_text", k), deref(want.Parsed.FreeText), got.Value.FreeText)
			wikidumptest.Equal(t, at("value.separators", k), nonNil(want.Parsed.Separators), nonNil(got.Value.Separators))
			gotItems := make([]oracleValueItem, 0, len(got.Value.Items))
			for _, it := range got.Value.Items {
				gotItems = append(gotItems, oracleValueItem(it))
			}
			wikidumptest.Equal(t, at("value.items", k), want.Parsed.Items, gotItems)
		}
	}
}

func TestResearchOracle_InlineLyricsFlag(t *testing.T) {
	o := loadOracle(t)
	deviation(t, "lyrics.poem_forms")
	deviation(t, "lyrics.prose_lines")
	flagged := 0
	for _, piece := range o.pieces {
		want := string(piece.InlineText) != "null"
		got := InlineLyricsBlocks(o.pages[piece.PageID].Wikitext).HadInlineLyrics
		wikidumptest.Equal(t, wikidumptest.Location{PageID: piece.PageID, Field: "inline_lyrics", Offset: -1}, want, got)
		if got {
			flagged++
		}
	}
	wikidumptest.Equal(t, wikidumptest.Location{Field: "inline_lyrics.pages", Offset: -1}, 11, flagged)
}

func TestResearchOracle_BardAliases(t *testing.T) {
	o := loadOracle(t)
	deviation(t, "bards.aliases.every_group")
	deviation(t, "bards.aliases.title_lead")
	for _, bard := range o.bards {
		got := map[string]bool{}
		for _, a := range BardAliases(o.pages[bard.PageID]) {
			got[FoldName(a.Name)] = true
		}
		for k, alias := range bard.Aliases {
			if FoldName(alias) == FoldName(bard.Title) {
				continue
			}
			wikidumptest.Equal(t, wikidumptest.Location{PageID: bard.PageID, Field: "aliases", Offset: k}, true,
				got[FoldName(alias)])
		}
	}
}

func TestResearchOracle_MaskedCommentsKeepHeadings(t *testing.T) {
	o := loadOracle(t)
	deviation(t, "sections.masked_comments")
	for id, p := range o.pages {
		stripped := Sections(MaskNowiki(StripComments(p.Wikitext)))
		masked := PageSections(p.Wikitext)
		headings := func(ss []Section) []string {
			out := make([]string, 0, len(ss))
			for _, s := range ss {
				out = append(out, strings.Repeat("=", s.Level)+s.Heading)
			}

			return out
		}
		wikidumptest.Equal(t, wikidumptest.Location{PageID: id, Field: "headings", Offset: -1}, headings(stripped),
			headings(masked))
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

func optional(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

// truncateUTF16 cuts s to at most n UTF-16 code units, as the reference's String.prototype.slice does.
func truncateUTF16(s string, n int) string {
	units := 0
	for k, r := range s {
		size := 1
		if r > 0xffff {
			size = 2
		}
		if units+size > n {
			return s[:k]
		}
		units += size
	}

	return s
}
