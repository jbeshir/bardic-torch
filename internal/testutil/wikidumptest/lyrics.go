package wikidumptest

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// LyricRunLines is the fewest consecutive prose lines of a Lyrics or Text section that are lyrics; a shorter run
// is commentary, which the importer keeps as version notes (docs/PLAN.md §7).
const LyricRunLines = 4

var (
	// oraclePoem is a <poem> element, in any case, closed or not (to the end of the text).
	oraclePoem = regexp.MustCompile(`(?is)<poem\b[^>]*>.*?(?:</poem>|\z)`)
	// oracleHeading is a heading line and its name.
	oracleHeading = regexp.MustCompile(`^=+\s*(.*?)\s*=+\s*$`)
)

// OracleLyrics returns the spans of a page's inline lyrics, found with simple rules of their own rather than the
// detector under test (mediawiki.InlineLyricsBlocks): every <poem> element, and in each section named in sections
// every run of LyricRunLines or more consecutive lines outside them that are not blank and carry no link.
func OracleLyrics(wikitext string, sections []string) [][]int {
	var out [][]int
	for _, m := range oraclePoem.FindAllStringIndex(wikitext, -1) {
		out = append(out, []int{m[0], m[1]})
	}
	poems := slices.Clone(out)
	inPoem := func(start, end int) bool {
		return slices.ContainsFunc(poems, func(p []int) bool { return start < p[1] && p[0] < end })
	}
	in := false
	var run [][]int
	flush := func() {
		if len(run) >= LyricRunLines {
			out = append(out, []int{run[0][0], run[len(run)-1][1]})
		}
		run = nil
	}
	at := 0
	for _, line := range strings.SplitAfter(wikitext, "\n") {
		trimmed := strings.TrimRight(line, "\n")
		switch h := oracleHeading.FindStringSubmatch(trimmed); {
		case h != nil:
			flush()
			in = slices.ContainsFunc(sections, func(name string) bool { return strings.EqualFold(name, h[1]) })
		case in && strings.TrimSpace(trimmed) != "" && !strings.Contains(trimmed, "[") &&
			!inPoem(at, at+len(trimmed)):
			run = append(run, []int{at, at + len(trimmed)})
		default:
			flush()
		}
		at += len(line)
	}
	flush()

	return out
}

// InlineTextSections returns, by page ID, the sections the reference analysis (analysis/pieces.json, its
// inline_text) found inline text in.
func InlineTextSections(t testing.TB, analysis fs.FS) map[int64][]string {
	t.Helper()
	var reference struct {
		Pieces []struct {
			PageID     int64 `json:"pageid"`
			InlineText []struct {
				Section string `json:"section"`
			} `json:"inline_text"`
		} `json:"pieces"`
	}
	data, err := fs.ReadFile(analysis, "pieces.json")
	NoError(t, "reading the reference pieces", err)
	NoError(t, "decoding the reference pieces", json.Unmarshal(data, &reference))
	out := map[int64][]string{}
	for _, p := range reference.Pieces {
		for _, it := range p.InlineText {
			out[p.PageID] = append(out[p.PageID], it.Section)
		}
	}

	return out
}

// OutsideSpans returns the parts of text outside the spans (which may overlap), each a text of its own, so that
// no word sequence runs across a span.
func OutsideSpans(text string, spans [][]int) []string {
	sorted := slices.Clone(spans)
	slices.SortFunc(sorted, func(a, b []int) int { return a[0] - b[0] })
	var out []string
	at := 0
	for _, sp := range sorted {
		if sp[0] > at {
			out = append(out, text[at:sp[0]])
		}
		at = max(at, sp[1])
	}

	return append(out, text[at:])
}
