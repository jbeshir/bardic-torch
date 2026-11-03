package scrub

import (
	"strings"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// LyricPlaceholderLine is the line that replaces each line of inline lyrics, at most
// mediawiki.InlineLyricsMinLines of them per block. Leak tests look for its first word.
const LyricPlaceholderLine = "scrubbedlyric placeholder line"

// commentPlaceholder replaces an HTML comment holding a <poem> tag; it still holds one, so it is still a block.
const commentPlaceholder = "<!-- <poem>" + LyricPlaceholderLine + "</poem> -->"

// lyrics returns text with each inline lyrics block replaced: a poem's body (its tags are kept) and a prose block
// by min(lines, mediawiki.InlineLyricsMinLines) placeholder lines, which keeps every section's classification,
// and a comment by commentPlaceholder. It appends the dropped bodies (but those that are placeholders already)
// to bodies and the text outside the blocks to outside.
func (s *scrubber) lyrics(text string, bodies, outside []string) (string, []string, []string) {
	found := mediawiki.InlineLyricsBlocks(text)
	outside = append(outside, mediawiki.WithoutInlineLyrics(text, found.Blocks))
	var b strings.Builder
	last := 0
	for _, block := range found.Blocks {
		if block.Start < last {
			continue
		}
		b.WriteString(text[last:block.Start])
		body := text[block.Start:block.End]
		switch block.Kind {
		case mediawiki.LyricsPoem:
			s.stats.PoemBlocks++
			body = text[block.BodyStart:block.BodyEnd]
			b.WriteString(text[block.Start:block.BodyStart])
			b.WriteString("\n" + placeholderLines(block.Lines))
			b.WriteString(text[block.BodyEnd:block.End])
		case mediawiki.LyricsProse:
			s.stats.ProseBlocks++
			b.WriteString(strings.TrimSuffix(placeholderLines(block.Lines), "\n"))
		case mediawiki.LyricsComment:
			s.stats.CommentBlocks++
			b.WriteString(commentPlaceholder)
		}
		if !onlyPlaceholders(body) {
			bodies = append(bodies, body)
		}
		last = block.End
	}
	b.WriteString(text[last:])

	return b.String(), bodies, outside
}

// placeholderLines is min(n, mediawiki.InlineLyricsMinLines) placeholder lines, each ending in a newline.
func placeholderLines(n int) string {
	return strings.Repeat(LyricPlaceholderLine+"\n", min(n, mediawiki.InlineLyricsMinLines))
}

// onlyPlaceholders reports whether body is a placeholder block already.
func onlyPlaceholders(body string) bool {
	if body == commentPlaceholder {
		return true
	}
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(line); line != "" && line != LyricPlaceholderLine {
			return false
		}
	}

	return true
}
