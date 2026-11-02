package mediawiki

import (
	"fmt"
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
		v := referenceParse(raw)
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

// FuzzWikitextToMarkdown checks that the Markdown holds no links but converted ones: no image ("![" with the "!"
// unescaped), and every unescaped "<" (an autolink) and every link destination is an absolute http(s) URL.
func FuzzWikitextToMarkdown(f *testing.F) {
	addSeeds(f)
	f.Add("![https://example.invalid/x.png x] ![//example.invalid/y] [www.example.invalid t] <img src=x> \\![http://a b]")
	f.Add("0000[//\\![]")
	f.Fuzz(func(t *testing.T, text string) {
		md := WikitextToMarkdown(text)
		for i := 0; i < len(md); i++ {
			if !unescapedAt(md, i) {
				continue
			}
			switch {
			case strings.HasPrefix(md[i:], "!["):
				t.Fatalf("an image at %d", i)
			case md[i] == '<':
				i = skipDestination(t, md, i+1, '>')
			case strings.HasPrefix(md[i:], "](<"):
				i = skipDestination(t, md, i+3, '>')
			case strings.HasPrefix(md[i:], "]("):
				i = skipDestination(t, md, i+2, ')')
			}
		}
	})
}

// skipDestination checks that md[start:] starts with an absolute http(s) URL ending at closer, and returns the
// closer's offset.
func skipDestination(t *testing.T, md string, start int, closer byte) int {
	t.Helper()
	end := strings.IndexByte(md[start:], closer)
	if end < 0 || !strings.HasPrefix(md[start:], "http://") && !strings.HasPrefix(md[start:], "https://") {
		t.Fatalf("an HTML tag, or a link that is not to an absolute http(s) URL, at %d", start)
	}

	return start + end
}

// unescapedAt reports whether s[i] is not escaped: an even number of backslashes precede it.
func unescapedAt(s string, i int) bool {
	n := 0
	for k := i - 1; k >= 0 && s[k] == '\\'; k-- {
		n++
	}

	return n%2 == 0
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

// TestHostileInput_QuadraticShapes runs the shapes that once made a parser quadratic (an unclosed "[http://"
// before each later token; a token folded by string concatenation; every concatenated name searched in every
// word) at 256 KiB and at the 2 MiB page cap. Each must finish within its budget; the quadratic code took 89 s,
// 15 s, 5 s and 8 s at 128–256 KiB, and hours at 2 MiB.
func TestHostileInput_QuadraticShapes(t *testing.T) {
	names := make([]string, 2000)
	for k := range names {
		names[k] = fmt.Sprintf("Name%04d Placeholder", k)
	}
	matcher := NewNameMatcher(names, nil)
	shapes := map[string]func(size int){
		"ParseValue":         func(size int) { ParseValue(strings.Repeat("[http://a ", size/10)) },
		"WikitextToMarkdown": func(size int) { WikitextToMarkdown(strings.Repeat("[http://a x ''", size/14)) },
		"Words":              func(size int) { Words(strings.Repeat("h", size)) },
		"NameMatcher.Find":   func(size int) { matcher.Find(strings.Repeat("http", size/4)) },
	}
	for name, parse := range shapes {
		for _, size := range []int{256 << 10, MaxPageRecordBytes} {
			t.Run(fmt.Sprintf("%s/%d", name, size), func(t *testing.T) {
				withinBudget(t, 3*time.Second, func() { parse(size) })
			})
		}
	}
}

// withinBudget runs fn and fails the test as soon as it has run longer than budget.
func withinBudget(t *testing.T, budget time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(budget):
		t.Fatalf("took over the %s budget", budget)
	}
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
	NewNameMatcher([]string{wren}, []string{wren}).Find(hostile)
	RepairMarkup(hostile)
	FindMarkupIssues(hostile)

	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("parsing 1 MB of hostile input took %s, over the 20s budget", elapsed)
	}
}
