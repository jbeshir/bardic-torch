package mediawiki

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func lyricLines(n int) string {
	lines := make([]string, n)
	for k := range lines {
		lines[k] = "placeholder lyric line"
	}

	return strings.Join(lines, "\n")
}

func TestDropInlineLyrics_Poems(t *testing.T) {
	for text, want := range map[string]string{
		"== Notes ==\nBefore.\n<poem>\n" + lyricLines(1) + "\n</poem>\nAfter.": "== Notes ==\nBefore.\n \nAfter.",
		"x <POEM style=\"a\">one\ntwo</Poem > y":                               "x   y",
		"== Lyrics ==\n<poem>\none\n== Notes ==\nunclosed":                     "== Lyrics ==\n ",
	} {
		got, had := DropInlineLyrics(text)

		assert.Equal(t, want, got, text)
		assert.True(t, had, text)
	}
}

func TestDropInlineLyrics_LyricsSectionLines(t *testing.T) {
	section := func(heading, body string) string { return "== " + heading + " ==\n" + body }
	lyrics := "first lyric line\n* bulleted lyric line\n\n: indented lyric line\n" +
		"* [https://example.org/ The lyrics]\n* Link\n[[Category:Songs]]\nlast lyric line"

	got, had := DropInlineLyrics(section(lyricsHeading, lyrics))
	assert.True(t, had)
	assert.Equal(t, "== Lyrics ==\n \n \n\n \n* [https://example.org/ The lyrics]\n* Link\n[[Category:Songs]]\n ", got,
		"lines with a link and hints stay")

	for _, kept := range []string{
		section("Text", "This version adds a verse.\n* [https://example.org/ Text]\nSung slowly."),
		section("Notes", lyricLines(6)),
	} {
		got, had := DropInlineLyrics(kept)
		assert.False(t, had, kept)
		assert.Equal(t, kept, got, "fewer than four lines are commentary; other sections are not lyrics")
	}
}
