package mediawiki

import (
	"strings"
	"testing"
	"time"
)

// fuzzSeeds are the shapes of wikitext the fuzz tests start from.
var fuzzSeeds = []string{
	"",
	"= Title (Wren Placeholder) =\n{{Infobox song\n| lyrics_by = [[Wren Placeholder]] (verses) / anonymous\n}}\n" +
		"== Lyrics ==\n<poem>\nplaceholder lyric line\n</poem>\nprose\n* [https://example.org/ link]",
	"== a = b ==\n=x=\n====\n======= seven =======",
	"<POEM style=x>unclosed\n== Next ==\n<!-- <poem> --> <poem",
	"{{a|{{b|[[c|d]]}}|e=f}} {{{p}}} {{unclosed",
	"x=https://example.org/ \"https://example.org/ [https://example.org/ t] www.example.org",
	"[[:Category:X|(Y)]], [ext]; a & b and c with d <br/> e (f (g))",
	strings.Repeat("{{[[(", 40),
	"<nowiki><poem>x</poem></nowiki> <!-- a --> <poem>\n</poem>",
}

func addSeeds(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
}

func FuzzFindTemplates(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, text string) {
		last := 0
		for _, tpl := range FindTemplates(text) {
			if tpl.Start < last || tpl.End > len(text) || tpl.Start >= tpl.End {
				t.Fatalf("template span [%d, %d) out of order or bounds (after %d, length %d)", tpl.Start, tpl.End,
					last, len(text))
			}
			if text[tpl.Start:tpl.End] != tpl.Raw || !strings.HasPrefix(tpl.Raw, "{{") {
				t.Fatalf("template at %d is not its span", tpl.Start)
			}
			last = tpl.End
		}
	})
}

func FuzzParseValue(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, raw string) {
		v := ParseValue(raw)
		if len(v.Raw) > len(raw) {
			t.Fatalf("value longer than its input")
		}
		for _, it := range v.Items {
			if it.Depth < 0 || it.Depth > MaxNestingDepth {
				t.Fatalf("item depth %d outside [0, %d]", it.Depth, MaxNestingDepth)
			}
			if (it.Qualifies != "") && it.Depth == 0 {
				t.Fatalf("a qualifying item has no depth")
			}
		}
	})
}

func FuzzSections(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, text string) {
		sections := Sections(text)
		if len(sections) == 0 || sections[0].Level != 0 || sections[0].Start != 0 {
			t.Fatalf("no lead section")
		}
		end := 0
		for k, s := range sections {
			checkSection(t, k, s, end, len(text))
			end = s.End
		}
		if end != len(text) {
			t.Fatalf("sections end at %d, not %d", end, len(text))
		}
	})
}

func checkSection(t *testing.T, k int, s Section, previousEnd, length int) {
	t.Helper()
	if s.Start != previousEnd || s.BodyStart < s.Start || s.End < s.BodyStart || s.End > length {
		t.Fatalf("section %d [%d, %d, %d) out of order or bounds (previous end %d, length %d)", k, s.Start,
			s.BodyStart, s.End, previousEnd, length)
	}
	if k > 0 && (s.Level < 1 || s.Level > 6) {
		t.Fatalf("section %d has level %d", k, s.Level)
	}
}

func FuzzInlineLyricsBlocks(f *testing.F) {
	addSeeds(f)
	f.Fuzz(func(t *testing.T, text string) {
		lyrics := InlineLyricsBlocks(text)
		last := 0
		for _, b := range lyrics.Blocks {
			checkBlock(t, b, last, len(text))
			last = b.End
		}
		for _, c := range lyrics.Commentary {
			if c.Start < 0 || c.End > len(text) || c.Start > c.End || overlapsBlock(lyrics.Blocks, c.Start, c.End) {
				t.Fatalf("commentary [%d, %d) out of bounds or inside a block", c.Start, c.End)
			}
		}
		if poemOpen.MatchString(WithoutInlineLyrics(text, lyrics.Blocks)) {
			t.Fatalf("a <poem tag survives removing the lyrics")
		}
	})
}

func checkBlock(t *testing.T, b LyricsBlock, previousEnd, length int) {
	t.Helper()
	if b.Start < previousEnd || b.End > length || b.Start > b.End || b.BodyStart < b.Start || b.BodyEnd > b.End ||
		b.BodyStart > b.BodyEnd {
		t.Fatalf("block [%d, %d) body [%d, %d) out of order or bounds (after %d, length %d)", b.Start, b.End,
			b.BodyStart, b.BodyEnd, previousEnd, length)
	}
}

// TestHostileInput_TimeBudget runs every parser over 1 MB of unclosed {{[[( nesting, which a parser that
// backtracks or rescans would take far longer than the budget over.
func TestHostileInput_TimeBudget(t *testing.T) {
	hostile := strings.Repeat("{{[[(", 1<<20/5)
	page := Page{PageID: 1, Wikitext: "= T (" + hostile + ") =\n== Lyrics ==\n" + hostile + "\n<poem>" + hostile}
	start := time.Now()

	FindTemplates(hostile)
	FindWikilinks(hostile)
	FindExternalLinks(hostile)
	Sections(hostile)
	ParseValue(hostile)
	ParsePage(page.Wikitext)
	InlineLyricsBlocks(page.Wikitext)
	BardAliases(page)
	PersonNameCandidates(page, testMaxYear)
	ParsePerformanceText(hostile, testMaxYear)
	NewNameMatcher([]string{wren}).Find(hostile)
	RepairMarkup(hostile)
	FindMarkupIssues(hostile)

	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("parsing 1 MB of hostile input took %s, over the 20s budget", elapsed)
	}
}
