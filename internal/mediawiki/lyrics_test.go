package mediawiki

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lyricLines(n int) string {
	lines := make([]string, n)
	for k := range lines {
		lines[k] = "placeholder lyric line"
	}

	return strings.Join(lines, "\n")
}

func TestInlineLyricsBlocks_PoemAnyLength(t *testing.T) {
	for lines, flagged := range map[int]bool{1: false, 3: false, 4: true, 60: true} {
		text := "== Notes ==\nBefore.\n<poem>\n" + lyricLines(lines) + "\n</poem>\nAfter."
		got := InlineLyricsBlocks(text)

		require.Len(t, got.Blocks, 1, "every poem is dropped, whatever its length")
		b := got.Blocks[0]
		assert.Equal(t, LyricsPoem, b.Kind)
		assert.Equal(t, 1, b.Section)
		assert.Equal(t, lines, b.Lines)
		assert.Equal(t, strings.Index(text, "<poem>"), b.Start)
		assert.Equal(t, strings.Index(text, "</poem>")+len("</poem>"), b.End)
		assert.Equal(t, flagged, got.HadInlineLyrics, lines)
		assert.Equal(t, "== Notes ==\nBefore.\n \nAfter.", WithoutInlineLyrics(text, got.Blocks))
	}
}

func TestInlineLyricsBlocks_PoemForms(t *testing.T) {
	tests := map[string]struct {
		text      string
		wantSpans []string
	}{
		"attributes and case": {
			text:      "x <POEM style=\"a\">one</Poem > y",
			wantSpans: []string{"<POEM style=\"a\">one</Poem >"},
		},
		"unclosed runs to the end of its section": {
			text:      "== Lyrics ==\n<poem>\none\ntwo\n== Notes ==\nkept",
			wantSpans: []string{"<poem>\none\ntwo\n"},
		},
		"a tag left open runs to the end of the text": {
			text:      "a <poem class=x\n== Notes ==\nalso dropped",
			wantSpans: []string{"<poem class=x\n== Notes ==\nalso dropped"},
		},
		"closed in a later section": {
			text:      "<poem>\na\n== Head ==\nb\n</poem> kept",
			wantSpans: []string{"<poem>\na\n== Head ==\nb\n</poem>"},
		},
		"inside nowiki it still shows": {
			text:      "<nowiki><poem>one</poem></nowiki>",
			wantSpans: []string{"<poem>one</poem>"},
		},
		"not a poem tag": {
			text: "<poems>x</poems> <poemx>",
		},
		"self-closing tag": {
			text:      "a <poem/> b\n== Next ==\nc",
			wantSpans: []string{"<poem/> b\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := InlineLyricsBlocks(tt.text)
			var spans []string
			for _, b := range got.Blocks {
				spans = append(spans, tt.text[b.Start:b.End])
			}
			assert.Equal(t, tt.wantSpans, spans)
			assert.False(t, poemOpen.MatchString(WithoutInlineLyrics(tt.text, got.Blocks)))
		})
	}
}

func TestInlineLyricsBlocks_FlagCountsLinesPerSection(t *testing.T) {
	two := "<poem>\n" + lyricLines(2) + "\n</poem>\n"
	same := InlineLyricsBlocks("== Notes ==\n" + two + two)
	assert.Len(t, same.Blocks, 2)
	assert.True(t, same.HadInlineLyrics, "2 + 2 poem lines in one section")

	apart := InlineLyricsBlocks("== Notes ==\n" + two + "== Other ==\n" + two)
	assert.Len(t, apart.Blocks, 2)
	assert.False(t, apart.HadInlineLyrics, "2 poem lines in each of two sections")
}

func TestInlineLyricsBlocks_CommentBeforePoem(t *testing.T) {
	text := "== Lyrics ==\n<!-- a comment, which shifts nothing -->\n<poem>\n" + lyricLines(4) + "\n</poem>\n" +
		"* [https://example.org/ Lyrics]"
	got := InlineLyricsBlocks(text)

	require.Len(t, got.Blocks, 1)
	b := got.Blocks[0]
	assert.Equal(t, strings.Index(text, "<poem>"), b.Start, "spans are offsets into the original text")
	assert.Equal(t, "<poem>\n"+lyricLines(4)+"\n</poem>", text[b.Start:b.End])
	assert.True(t, got.HadInlineLyrics)
	assert.Empty(t, got.Commentary)
	assert.Equal(t, "== Lyrics ==\n<!-- a comment, which shifts nothing -->\n \n* [https://example.org/ Lyrics]",
		WithoutInlineLyrics(text, got.Blocks))
}

func TestInlineLyricsBlocks_CommentHoldingPoem(t *testing.T) {
	text := "before <!-- old: <poem>\none\n</poem> --> after <!-- plain comment -->"
	got := InlineLyricsBlocks(text)

	require.Len(t, got.Blocks, 1)
	assert.Equal(t, LyricsComment, got.Blocks[0].Kind)
	assert.Equal(t, "<!-- old: <poem>\none\n</poem> -->", text[got.Blocks[0].Start:got.Blocks[0].End])
	assert.False(t, got.HadInlineLyrics)
	assert.Equal(t, "before   after <!-- plain comment -->", WithoutInlineLyrics(text, got.Blocks))
}

func TestInlineLyricsBlocks_ProseRule(t *testing.T) {
	section := func(heading, body string) string { return "== " + heading + " ==\n" + body }
	lyrics := "first lyric line\n* bulleted lyric line\n\n: indented lyric line\n{{template line}}\n" +
		"* [https://example.org/ The lyrics]\n* Link\n[[Category:Songs]]\nlast lyric line"

	got := InlineLyricsBlocks(section(lyricsHeading, lyrics))
	assert.True(t, got.HadInlineLyrics, "every non-link, non-hint line counts, whatever its first character")
	text := section(lyricsHeading, lyrics)
	var spans []string
	for _, b := range got.Blocks {
		assert.Equal(t, LyricsProse, b.Kind)
		spans = append(spans, text[b.Start:b.End])
	}
	assert.Equal(t, []string{
		"first lyric line\n* bulleted lyric line\n\n: indented lyric line\n{{template line}}",
		"last lyric line",
	}, spans)
	assert.Equal(t, "== Lyrics ==\n \n* [https://example.org/ The lyrics]\n* Link\n[[Category:Songs]]\n ",
		WithoutInlineLyrics(text, got.Blocks))

	short := section("Text", "This version adds a verse.\n* [https://example.org/ Text]\nSung slowly.")
	got = InlineLyricsBlocks(short)
	assert.False(t, got.HadInlineLyrics)
	assert.Empty(t, got.Blocks)
	var commentary []string
	for _, c := range got.Commentary {
		assert.Equal(t, 1, c.Section)
		commentary = append(commentary, short[c.Start:c.End])
	}
	assert.Equal(t, []string{"This version adds a verse.", "Sung slowly."}, commentary)

	notes := InlineLyricsBlocks(section("Notes", lyricLines(6)))
	assert.False(t, notes.HadInlineLyrics, "the prose rule is for Lyrics and Text sections only")
	assert.Empty(t, notes.Blocks)
	assert.Empty(t, notes.Commentary)
}

func TestInlineLyricsBlocks_PoemLinesAreNotProse(t *testing.T) {
	text := "== Lyrics ==\nintro line\n<poem>\n" + lyricLines(5) + "\n</poem>\noutro line"
	got := InlineLyricsBlocks(text)

	require.Len(t, got.Blocks, 1)
	assert.Equal(t, LyricsPoem, got.Blocks[0].Kind)
	assert.Len(t, got.Commentary, 2)
}

func TestWithoutInlineLyrics_RemovesSpansOnly(t *testing.T) {
	text := "keep one <poem>drop</poem> keep two <poem>drop</poem>"
	blocks := InlineLyricsBlocks(text).Blocks
	reversed := []LyricsBlock{blocks[1], blocks[0]}

	assert.Equal(t, "keep one   keep two  ", WithoutInlineLyrics(text, reversed))
	assert.Equal(t, blocks[0].Start, reversed[1].Start, "the blocks given are not reordered")
	assert.Equal(t, text, WithoutInlineLyrics(text, nil))
	assert.Equal(t, "<po em>", WithoutInlineLyrics("<po<poem>x</poem>em>", InlineLyricsBlocks("<po<poem>x</poem>em>").Blocks),
		"removing a block cannot form a tag")
}

func TestTextLines(t *testing.T) {
	for _, tt := range []struct {
		line string
		want int
	}{
		{"", 0},
		{"  ", 0},
		{"one line", 1},
		{"a b<br>c d<br/>e f<br />g h<BR>i j", 5},
		{"<br>a<br><br>  <br>b<br>", 2},
		{"a <brx> b <b>c</b>", 1},
		{"a<br", 1},
	} {
		assert.Equal(t, tt.want, TextLines(tt.line), tt.line)
	}
}

// TestInlineLyricsBlocks_LinesJoinedByBreaks: lines joined by <br> on one line count as lines, so one line holding
// a verse is lyrics.
func TestInlineLyricsBlocks_LinesJoinedByBreaks(t *testing.T) {
	joined := strings.ReplaceAll(lyricLines(5), "\n", "<br>")
	text := "== " + lyricsHeading + " ==\n" + joined + "\n"

	got := InlineLyricsBlocks(text)
	assert.True(t, got.HadInlineLyrics)
	require.Len(t, got.Blocks, 1)
	assert.Equal(t, LyricsProse, got.Blocks[0].Kind)
	assert.Equal(t, 5, got.Blocks[0].Lines)
	assert.Empty(t, got.Commentary)
	assert.NotContains(t, WithoutInlineLyrics(text, got.Blocks), "placeholder")

	short := InlineLyricsBlocks("== " + lyricsHeading + " ==\nThis version<br>adds a verse.\n")
	assert.False(t, short.HadInlineLyrics, "two lines are version commentary")
	assert.Len(t, short.Commentary, 1)
}
