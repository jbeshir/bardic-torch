package mediawiki

import (
	"regexp"
	"strings"
)

// InlineLyricsMinLines is the number of lines without a link from which a Lyrics or Text section's text is lyrics
// rather than version commentary.
const InlineLyricsMinLines = 4

// poemBlock is a <poem> element in any case and with any attributes; one left open runs to the end of the text.
var poemBlock = regexp.MustCompile(`(?is)<poem\b.*?(?:</poem\s*>|$)`)

// DropInlineLyrics returns wikitext without its inline lyrics, and whether it had any. The site shows links to
// lyrics, not lyrics, so the import drops every <poem> block, and the lines without a link of a Lyrics or Text
// section when there are InlineLyricsMinLines or more of them (fewer are version commentary, which is kept).
func DropInlineLyrics(wikitext string) (string, bool) {
	text := cutSpans(wikitext, poemBlock.FindAllStringIndex(MaskComments(wikitext), -1))
	masked := MaskComments(text)
	var verses [][]int
	for _, s := range PageSections(text) {
		if !KeyOf(s).IsLyrics() {
			continue
		}
		var plain [][]int
		for _, l := range lineSpans(masked, s.BodyStart, s.End) {
			line := masked[l.start:l.end]
			if trimSpace(line) != "" && !IsHint(line) && !carriesLink(line) {
				plain = append(plain, []int{l.start, l.end})
			}
		}
		if len(plain) >= InlineLyricsMinLines {
			verses = append(verses, plain...)
		}
	}
	out := cutSpans(text, verses)

	return out, out != wikitext
}

// cutSpans returns text with each of spans (sorted, not overlapping) replaced by one space, so that no tag can be
// formed from the text on either side.
func cutSpans(text string, spans [][]int) string {
	var b strings.Builder
	last := 0
	for _, s := range spans {
		b.WriteString(text[last:s[0]])
		b.WriteByte(' ')
		last = s[1]
	}
	b.WriteString(text[last:])

	return b.String()
}

type lineSpan struct{ start, end int }

// lineSpans are the lines of text[start:end], as offsets into text.
func lineSpans(text string, start, end int) []lineSpan {
	var out []lineSpan
	for start < end {
		next := strings.IndexByte(text[start:end], '\n')
		if next < 0 {
			next = end
		} else {
			next += start
		}
		out = append(out, lineSpan{start: start, end: next})
		start = next + 1
	}

	return out
}

func carriesLink(line string) bool {
	masked := MaskNowiki(line)

	return len(FindWikilinks(masked)) > 0 || len(FindExternalLinks(masked)) > 0
}
