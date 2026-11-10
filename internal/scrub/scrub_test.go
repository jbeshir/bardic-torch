package scrub

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

func songPage(id int64, title, wikitext string) mediawiki.Page {
	return mediawiki.Page{NS: mediawiki.NSMain, Title: title, PageID: id, Wikitext: wikitext}
}

func TestScrub_PoemBodyKeepsTagsAndThreshold(t *testing.T) {
	short := "<poem>\nfirst real line\nsecond real line\n\nthird real line\n</poem>"
	long := "<poem style=\"x\">\n" + strings.Repeat("a real line of the song\n", 60) + "</POEM>"
	d := mediawiki.Dump{Pages: []mediawiki.Page{
		songPage(1, "Song of Testing", "== Notes ==\n"+short+"\n== Words ==\n"+long+"\n"),
		songPage(2, "Unclosed Song", "== Words ==\n<poem>\none\ntwo\n== Notes ==\nkept\n"),
	}}

	res := Scrub(d)

	three := strings.Repeat(LyricPlaceholderLine+"\n", 3)
	four := strings.Repeat(LyricPlaceholderLine+"\n", mediawiki.InlineLyricsMinLines)
	assert.Equal(t, "== Notes ==\n<poem>\n"+three+"</poem>\n== Words ==\n<poem style=\"x\">\n"+four+"</POEM>\n",
		res.Dump.Pages[0].Wikitext, "a long poem is cut to the threshold")
	assert.Equal(t, "== Words ==\n<poem>\n"+strings.Repeat(LyricPlaceholderLine+"\n", 2)+"== Notes ==\nkept\n",
		res.Dump.Pages[1].Wikitext, "an unclosed poem ends at the next heading")
	assert.Equal(t, 3, res.Stats.PoemBlocks)
	assert.NotContains(t, res.Dump.Pages[0].Wikitext, "real line")
}

func TestScrub_ProseLyricsBlockAndComment(t *testing.T) {
	prose := "== Lyrics ==\nverse one goes here\nverse two goes here\n\nverse three goes here\nverse four\n" +
		"verse five\n* [https://example.org/lyrics Full lyrics]\n"
	d := mediawiki.Dump{Pages: []mediawiki.Page{
		songPage(1, "Prose Song", prose),
		songPage(2, "Commented Song", "Intro <!-- <poem>\nhidden real line\n</poem> --> outro\n"),
	}}

	res := Scrub(d)

	assert.Equal(t, "== Lyrics ==\n"+strings.TrimSuffix(strings.Repeat(LyricPlaceholderLine+"\n", 4), "\n")+
		"\n* [https://example.org/lyrics Full lyrics]\n", res.Dump.Pages[0].Wikitext)
	assert.True(t, mediawiki.InlineLyricsBlocks(res.Dump.Pages[0].Wikitext).HadInlineLyrics)
	assert.Equal(t, "Intro "+commentPlaceholder+" outro\n", res.Dump.Pages[1].Wikitext)
	assert.Equal(t, Stats{ProseBlocks: 1, CommentBlocks: 1}, res.Stats)
}

func TestScrub_CategoryDescriptionsAndDenylist(t *testing.T) {
	lyric := "the quiet heron sings beneath a borrowed moon tonight"
	d := mediawiki.Dump{
		Pages: []mediawiki.Page{songPage(1, "Heron Song", "== Words ==\n<poem>\n"+lyric+"\n</poem>\n")},
		Categories: map[string]mediawiki.Category{
			"Herons": {Name: "Herons", DescriptionWikitext: "<poem>\n" + lyric + " again\n</poem>"},
		},
	}

	res := Scrub(d)

	assert.NotContains(t, res.Dump.Categories["Herons"].DescriptionWikitext, "heron")
	assert.NotEmpty(t, res.Denylist.Scan(lyric))
}

func TestScrub_DeterministicAndIdempotent(t *testing.T) {
	d := mediawiki.Dump{Pages: []mediawiki.Page{
		songPage(1, "Song", "== Words ==\n<poem>\none real line here\ntwo real line here\n</poem>\n"),
	}}

	once := Scrub(d)
	twice := Scrub(once.Dump)

	assert.Equal(t, once, Scrub(d))
	assert.Equal(t, once.Dump, twice.Dump)
	assert.Zero(t, twice.Denylist.Len(), "placeholder lines are not lyrics")
	require.NotEmpty(t, d.Pages[0].Wikitext)
	assert.Contains(t, d.Pages[0].Wikitext, "real line", "the input is not modified")
}
