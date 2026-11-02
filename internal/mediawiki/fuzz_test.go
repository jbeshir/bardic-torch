package mediawiki

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
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
// word) up to the 2 MiB page cap, failing as soon as a size takes super-linearly longer than the size before it
// (linearGrowth). The quadratic code took 89 s, 15 s, 5 s and 8 s at 128–256 KiB, and hours at 2 MiB.
func TestHostileInput_QuadraticShapes(t *testing.T) {
	names := make([]string, 2000)
	for k := range names {
		names[k] = fmt.Sprintf("Name%04d Placeholder", k)
	}
	matcher := NewNameMatcher(names, nil)
	shapes := []struct {
		name  string
		parse func(size int)
	}{
		{"ParseValue", func(size int) { ParseValue(strings.Repeat("[http://a ", size/10)) }},
		{"WikitextToMarkdown", func(size int) { WikitextToMarkdown(strings.Repeat("[http://a x ''", size/14)) }},
		{"Words", func(size int) { Words(strings.Repeat("h", size)) }},
		{"NameMatcher.Find", func(size int) { matcher.Find(strings.Repeat("http", size/4)) }},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			if err := linearGrowth(t, MaxPageRecordBytes, s.parse); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// The growth budget of linearGrowth. Sizes grow by growthStep from 1/1024 of the largest, and each size has two
// budgets relative to CPU times measured in the same run (cpuWithin), of which it must meet the tighter (but
// never one under growthFloor):
//
//   - growthStepAllowance times the size before. A step of linear code takes about growthStep times as long as
//     the size before (at most 5.2x seen, above growthFloor/growthStepAllowance) and one of quadratic code
//     growthStep² times, so this catches quadratic work already present at small sizes.
//   - growthAnchorAllowance times the anchor's time, scaled linearly to the size; the anchor is the first size
//     that took at least growthAnchor. Linear code stays within 2.2x of that scaling (the race detector and cache
//     sizes slow large inputs more per byte than small ones), while quadratic code exceeds it by the size ratio
//     to the anchor, so this catches quadratic work that only shows at large sizes. Noise can only lengthen the
//     anchor, which loosens budgets rather than failing linear code.
//
// Being relative, the budgets hold on any machine and under the race detector, which makes the regexp-heavy
// parsers 15–25x slower. Below the floor, times grow unevenly (with scheduling, garbage collection, cache sizes
// and the regexp package backtracking short inputs instead of running its automaton).
//
// The race detector slows Go code but not the assembly of strings.Index and its kin. Repeated work done only by
// such assembly (a rescan through strings.IndexByte, say) hides under the slowed linear work up to the page cap,
// so only a run without -race catches it; make validate runs these tests without it.
const (
	growthStep            = 4
	growthSteps           = 5
	growthStepAllowance   = 10
	growthAnchorAllowance = 6
	growthAnchor          = 20 * time.Millisecond
	growthFloor           = time.Second
)

// linearGrowth runs parse at maxSize/growthStep^growthSteps, then at each size growthStep times larger up to
// maxSize, logging each time, and returns an error as soon as a size runs over its budget (see growthStep).
// The smallest size has no budget; at 1/1024 of the page cap, even the quadratic code above finished in
// milliseconds. A run over budget is left running in the background.
func linearGrowth(t *testing.T, maxSize int, parse func(size int)) error {
	t.Helper()
	size := maxSize
	for range growthSteps {
		size /= growthStep
	}
	runtime.GC()
	elapsed, _ := cpuWithin(t, time.Duration(math.MaxInt64), func() { parse(size) })
	var anchor time.Duration
	anchorSize := 0
	for size < maxSize {
		if anchorSize == 0 && elapsed >= growthAnchor {
			anchor, anchorSize = elapsed, size
		}
		previous := elapsed
		size *= growthStep
		budget := growthStepAllowance * previous
		if anchorSize > 0 {
			budget = min(budget, growthAnchorAllowance*anchor*time.Duration(size/anchorSize))
		}
		budget = max(budget, growthFloor)
		runtime.GC()
		var within bool
		if elapsed, within = cpuWithin(t, budget, func() { parse(size) }); !within {
			return fmt.Errorf("%d bytes ran over the %s budget (%s at %d bytes, anchor %s at %d bytes)", size,
				budget, previous, size/growthStep, anchor, anchorSize)
		}
		t.Logf("%d bytes: %s (budget %s)", size, elapsed, budget)
	}

	return nil
}

// cpuWithin runs fn and returns the CPU time the process spent until it returned, or reports false as soon as
// that has gone over budget, leaving fn running. CPU time rather than wall time, so that other processes on a
// busy machine (CI runs every package at once, and MySQL) do not count; no other test in this package runs in
// parallel, so the process's time is fn's and the garbage collector's.
func cpuWithin(t *testing.T, budget time.Duration, fn func()) (time.Duration, bool) {
	t.Helper()
	start := processCPUTime(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-done:
			elapsed := processCPUTime(t) - start

			return elapsed, elapsed <= budget
		case <-poll.C:
			if elapsed := processCPUTime(t) - start; elapsed > budget {
				return elapsed, false
			}
		}
	}
}

// processCPUTime is the user and system CPU time the test process has used.
func processCPUTime(t *testing.T) time.Duration {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatalf("getrusage: %v", err)
	}

	return time.Duration(usage.Utime.Nano() + usage.Stime.Nano())
}

// TestLinearGrowth_FailsQuadratic pins that linearGrowth passes linear code and fails quadratic code in whatever
// mode the tests run (CI runs them with -race). The quadratic function stops once linearGrowth has returned, so
// it leaves nothing running.
func TestLinearGrowth_FailsQuadratic(t *testing.T) {
	const maxSize = 1 << 20
	text := strings.Repeat("a", maxSize)
	var count atomic.Int64
	linear := func(size int) {
		for range 1000 {
			count.Add(int64(strings.Count(text[:size], "b")))
		}
	}
	if err := linearGrowth(t, maxSize, linear); err != nil {
		t.Fatalf("linear: %v", err)
	}
	var stop atomic.Bool
	quadratic := func(size int) {
		for i := 0; i < size && !stop.Load(); i++ {
			count.Add(int64(strings.Count(text[i:size], "b")))
		}
	}
	err := linearGrowth(t, maxSize, quadratic)
	stop.Store(true)
	if err == nil {
		t.Fatal("quadratic: linearGrowth returned no error")
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

// TestHostileInput_TimeBudget runs every parser over unclosed {{[[( nesting up to 1 MB, failing as soon as a
// parser takes super-linearly longer at a size than at the size before it (linearGrowth), as one that
// backtracks or rescans would.
func TestHostileInput_TimeBudget(t *testing.T) {
	hostile := func(size int) string { return strings.Repeat("{{[[(", size/5) }
	page := func(size int) Page {
		h := hostile(size)

		return Page{PageID: 1, Wikitext: "= T (" + h + ") =\n== Lyrics ==\n" + h + "\n<poem>" + h}
	}
	parsers := []struct {
		name  string
		parse func(size int)
	}{
		{"FindTemplates", func(size int) { FindTemplates(hostile(size)) }},
		{"FindWikilinks", func(size int) { FindWikilinks(hostile(size)) }},
		{"FindExternalLinks", func(size int) { FindExternalLinks(hostile(size)) }},
		{"Sections", func(size int) { Sections(hostile(size)) }},
		{"ParseValue", func(size int) { ParseValue(hostile(size)) }},
		{"ParsePage", func(size int) { ParsePage(page(size).Wikitext) }},
		{"InlineLyricsBlocks", func(size int) { InlineLyricsBlocks(page(size).Wikitext) }},
		{"BardAliases", func(size int) { BardAliases(page(size)) }},
		{"PersonNameCandidates", func(size int) { PersonNameCandidates(page(size), testMaxYear) }},
		{"ParsePerformanceText", func(size int) { ParsePerformanceText(hostile(size), testMaxYear) }},
		{"NameMatcher.Find", func(size int) { NewNameMatcher([]string{wren}, []string{wren}).Find(hostile(size)) }},
		{"RepairMarkup", func(size int) { RepairMarkup(hostile(size)) }},
		{"FindMarkupIssues", func(size int) { FindMarkupIssues(hostile(size)) }},
	}
	for _, p := range parsers {
		t.Run(p.name, func(t *testing.T) {
			if err := linearGrowth(t, 1<<20, p.parse); err != nil {
				t.Fatal(err)
			}
		})
	}
}
