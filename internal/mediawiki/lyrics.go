package mediawiki

import (
	"regexp"
	"slices"
	"sort"
	"strings"
)

// InlineLyricsMinLines is the number of lines from which text in a section is inline lyrics for the
// legacy_had_inline_lyrics flag: the poem lines of a section together, or the prose lines of a Lyrics or Text
// section.
const InlineLyricsMinLines = 4

// LyricsBlockKind is how a block of inline lyrics was written.
type LyricsBlockKind string

// The lyrics block kinds.
const (
	// LyricsPoem is a <poem> element, closed or not, with any attributes and in any case.
	LyricsPoem LyricsBlockKind = "poem"
	// LyricsProse is a run of prose lines in a Lyrics or Text section that has at least InlineLyricsMinLines.
	LyricsProse LyricsBlockKind = "prose"
	// LyricsComment is an HTML comment holding a <poem> tag.
	LyricsComment LyricsBlockKind = "comment"
)

// LyricsBlock is a span of a page's wikitext that holds lyrics: text[Start:End] is dropped. A poem's body (its
// text without the tags) is text[BodyStart:BodyEnd]; for other kinds the body is the whole block. Section is the
// index of the section (in PageSections) the block starts in and Lines the number of non-blank lines of its body.
type LyricsBlock struct {
	Kind      LyricsBlockKind
	Section   int
	Start     int
	End       int
	BodyStart int
	BodyEnd   int
	Lines     int
}

// CommentaryLine is a prose line of a Lyrics or Text section with fewer than InlineLyricsMinLines prose lines:
// version commentary, not lyrics. text[Start:End] is the line.
type CommentaryLine struct {
	Section int
	Start   int
	End     int
}

// InlineLyrics is where a page's wikitext holds lyrics. Blocks are sorted and do not overlap. HadInlineLyrics is
// the legacy_had_inline_lyrics flag: some section has at least InlineLyricsMinLines poem lines, or is a Lyrics or
// Text section with at least that many prose lines.
type InlineLyrics struct {
	Blocks          []LyricsBlock
	Commentary      []CommentaryLine
	HadInlineLyrics bool
}

var (
	poemOpen  = regexp.MustCompile(`(?i)<poem\b[^>]*>?`)
	poemClose = regexp.MustCompile(`(?i)</poem[` + jsSpaces + `]*>`)
)

// InlineLyricsBlocks finds the lyrics in a page's wikitext, which are never imported:
//   - every <poem> element, whatever its length, attributes or case, in every section; an unclosed one runs to the
//     end of its section, and a tag left open runs to the end of the text;
//   - every HTML comment holding a <poem> tag;
//   - in a Lyrics or Text section, the lines that are neither blank, a hint nor carrying a link: with at least
//     InlineLyricsMinLines of them they are lyrics (blocks), with fewer they are version commentary.
//
// Spans are offsets into wikitext.
func InlineLyricsBlocks(wikitext string) InlineLyrics {
	masked := MaskComments(wikitext)
	sections := PageSections(wikitext)
	tagged := mergeBlocks(append(poemBlocks(masked, sections), commentBlocks(wikitext, sections)...))
	blocks := append([]LyricsBlock(nil), tagged...)
	res := InlineLyrics{}
	poemLines := make([]int, len(sections))
	for _, b := range tagged {
		poemLines[b.Section] += b.Lines
	}
	for k, s := range sections {
		if poemLines[k] >= InlineLyricsMinLines {
			res.HadInlineLyrics = true
		}
		if !KeyOf(s).IsLyrics() {
			continue
		}
		prose := proseLines(masked, s, tagged)
		if len(prose) < InlineLyricsMinLines {
			for _, l := range prose {
				res.Commentary = append(res.Commentary, CommentaryLine{Section: k, Start: l.start, End: l.end})
			}

			continue
		}
		res.HadInlineLyrics = true
		blocks = append(blocks, proseBlocks(masked, k, prose)...)
	}
	sortBlocks(blocks)
	res.Blocks = blocks

	return res
}

// WithoutInlineLyrics returns wikitext with each block replaced by one space, so that no tag can be formed from
// the text on either side.
func WithoutInlineLyrics(wikitext string, blocks []LyricsBlock) string {
	sorted := append([]LyricsBlock(nil), blocks...)
	sortBlocks(sorted)
	var b strings.Builder
	last := 0
	for _, block := range sorted {
		start := min(max(block.Start, last), len(wikitext))
		end := min(max(block.End, start), len(wikitext))
		b.WriteString(wikitext[last:start])
		b.WriteByte(' ')
		last = end
	}
	b.WriteString(wikitext[last:])

	return b.String()
}

func sortBlocks(blocks []LyricsBlock) {
	sort.SliceStable(blocks, func(a, b int) bool { return blocks[a].Start < blocks[b].Start })
}

// sectionAt returns the index of the section containing offset.
func sectionAt(sections []Section, offset int) int {
	k := sort.Search(len(sections), func(k int) bool { return sections[k].Start > offset })

	return max(k-1, 0)
}

func poemBlocks(masked string, sections []Section) []LyricsBlock {
	closes := poemClose.FindAllStringIndex(masked, -1)
	var out []LyricsBlock
	for at := 0; at < len(masked); {
		loc := poemOpen.FindStringIndex(masked[at:])
		if loc == nil {
			break
		}
		open, bodyStart := at+loc[0], at+loc[1]
		k := sectionAt(sections, open)
		end, bodyEnd := max(sections[k].End, bodyStart), max(sections[k].End, bodyStart)
		if c := sort.Search(len(closes), func(c int) bool { return closes[c][0] >= bodyStart }); c < len(closes) {
			bodyEnd, end = closes[c][0], closes[c][1]
		}
		out = append(out, LyricsBlock{
			Kind: LyricsPoem, Section: k, Start: open, End: end, BodyStart: bodyStart, BodyEnd: bodyEnd,
			Lines: countLines(masked[bodyStart:bodyEnd]),
		})
		at = max(end, open+1)
	}

	return out
}

var poemTag = regexp.MustCompile(`(?i)<poem\b`)

func commentBlocks(wikitext string, sections []Section) []LyricsBlock {
	var out []LyricsBlock
	for _, loc := range comment.FindAllStringIndex(wikitext, -1) {
		if poemTag.MatchString(wikitext[loc[0]:loc[1]]) {
			out = append(out, LyricsBlock{
				Kind: LyricsComment, Section: sectionAt(sections, loc[0]), Start: loc[0], End: loc[1],
				BodyStart: loc[0], BodyEnd: loc[1],
			})
		}
	}

	return out
}

// mergeBlocks returns blocks sorted, with overlapping ones merged into the first, keeping its kind and section.
func mergeBlocks(blocks []LyricsBlock) []LyricsBlock {
	blocks = slices.Clone(blocks)
	sortBlocks(blocks)
	var out []LyricsBlock
	for _, b := range blocks {
		if n := len(out); n > 0 && b.Start < out[n-1].End {
			last := &out[n-1]
			if b.End > last.End {
				last.End = b.End
				last.BodyEnd = max(last.BodyEnd, b.BodyEnd)
			}
			if b.Section == last.Section {
				last.Lines += b.Lines
			}

			continue
		}
		out = append(out, b)
	}

	return out
}

func countLines(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if trimSpace(l) != "" {
			n++
		}
	}

	return n
}

type lineSpan struct{ start, end int }

// proseLines returns the lines of section s, outside blocks, that are not blank, not hints and carry no link.
func proseLines(masked string, s Section, blocks []LyricsBlock) []lineSpan {
	var out []lineSpan
	for start := s.BodyStart; start < s.End; {
		end := strings.IndexByte(masked[start:s.End], '\n')
		if end < 0 {
			end = s.End
		} else {
			end += start
		}
		line := masked[start:end]
		if trimSpace(line) != "" && !IsHint(line) && !overlapsBlock(blocks, start, end) && !carriesLink(line) {
			out = append(out, lineSpan{start: start, end: end})
		}
		start = end + 1
	}

	return out
}

// overlapsBlock reports whether [start, end) overlaps one of blocks, which are sorted and do not overlap.
func overlapsBlock(blocks []LyricsBlock, start, end int) bool {
	k := sort.Search(len(blocks), func(k int) bool { return blocks[k].End > start })

	return k < len(blocks) && blocks[k].Start < end
}

func carriesLink(line string) bool {
	masked := MaskNowiki(line)

	return len(FindWikilinks(masked)) > 0 || len(FindExternalLinks(masked)) > 0
}

// proseBlocks joins prose lines that only blank lines separate into blocks.
func proseBlocks(masked string, section int, lines []lineSpan) []LyricsBlock {
	var out []LyricsBlock
	for _, l := range lines {
		if n := len(out); n > 0 && trimSpace(masked[out[n-1].End:l.start]) == "" {
			out[n-1].End, out[n-1].BodyEnd = l.end, l.end
			out[n-1].Lines++

			continue
		}
		out = append(out, LyricsBlock{
			Kind: LyricsProse, Section: section, Start: l.start, End: l.end, BodyStart: l.start, BodyEnd: l.end,
			Lines: 1,
		})
	}

	return out
}
