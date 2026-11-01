package scrub

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadScrubbed(t *testing.T) mediawiki.Dump {
	t.Helper()
	d, err := mediawiki.LoadDump(wikidumptest.Scrubbed(t))
	require.NoError(t, err)
	require.NotEmpty(t, d.Pages)

	return d
}

// texts returns every wikitext of d with where it comes from.
func texts(d mediawiki.Dump) map[string]string {
	out := map[string]string{}
	for _, p := range d.Pages {
		out[p.Title] = p.Wikitext
	}
	for name, c := range d.Categories {
		out["category description "+name] = c.DescriptionWikitext
	}

	return out
}

var wikiUserPlaceholder = regexp.MustCompile(`^Wiki User \d{2}$`)

// TestScrubbedDumpStructuralPrivacy checks the committed copy without the private crawl: what would be a leak if
// the scrubber had missed it.
// poemTags are the tags of a <poem> element.
var poemTags = regexp.MustCompile(`(?i)</?poem\b[^>]*>`)

func TestScrubbedDumpStructuralPrivacy(t *testing.T) {
	d := loadScrubbed(t)

	for where, text := range texts(d) {
		requirePlaceholderLyrics(t, where, text)
		requireNoContactDetails(t, where, text)
	}
	for _, p := range d.Pages {
		for _, block := range wikidumptest.OracleLyrics(p.Wikitext, []string{"Lyrics", "Text"}) {
			body := poemTags.ReplaceAllString(p.Wikitext[block[0]:block[1]], "")
			assert.True(t, onlyPlaceholders(body), "%s: a lyrics run at byte %d (found without the detector) is not "+
				"placeholder lines", p.Title, block[0])
		}
	}
	for _, p := range d.Pages {
		if mediawiki.TypePage(p) != mediawiki.PageTypeBard {
			continue
		}
		for _, h1 := range mediawiki.BardAliases(p) {
			if h1.Source == mediawiki.AliasFromH1Parenthetical && !mediawiki.ClassifyPersonAlias(h1).Public() {
				assert.True(t, mediawiki.IsPlaceholderName(h1.Name) || slices.Contains(mediawiki.PublicNames, h1.Name),
					"%s: hidden H1 alias is not a placeholder", p.Title)
			}
		}
	}
	phrases, _ := collectPhrases(d.Pages, d.Redirects)
	assert.Empty(t, phrases, "possible legal names that are not placeholders")
	for _, a := range d.Activity {
		for user := range a.Users {
			assert.Regexp(t, wikiUserPlaceholder, user, a.Title)
		}
	}
}

// requirePlaceholderLyrics checks that every lyrics block of text is placeholder lines.
func requirePlaceholderLyrics(t *testing.T, where, text string) {
	t.Helper()
	for _, block := range mediawiki.InlineLyricsBlocks(text).Blocks {
		body := text[block.BodyStart:block.BodyEnd]
		if block.Kind == mediawiki.LyricsComment {
			assert.Equal(t, commentPlaceholder, body, where)

			continue
		}
		assert.True(t, onlyPlaceholders(body), "%s: lyrics block at byte %d is not placeholder lines", where,
			block.Start)
	}
}

// requireNoContactDetails checks that text has no email but placeholders, and no phone number or honorific name
// in prose but placeholders.
func requireNoContactDetails(t *testing.T, where, text string) {
	t.Helper()
	for _, e := range mediawiki.EmailAddress.FindAllStringIndex(text, -1) {
		assert.True(t, strings.HasSuffix(text[e[0]:e[1]], placeholderDomain),
			"%s: email at byte %d is not a placeholder", where, e[0])
	}
	s := scrubber{}
	s.prose(text, phonePattern, func(m []int, text string) string {
		p := text[m[0]:m[1]]
		assert.True(t, len(phoneDigits.FindAllString(p, -1)) < phoneMinDigits || isPhonePlaceholder(p),
			"%s: phone number at byte %d", where, m[0])

		return p
	})
	s.prose(text, mediawiki.HonorificName, func(m []int, text string) string {
		assert.True(t, startsWithPlaceholder(text[m[2]:]), "%s: honorific at byte %d is not a placeholder", where,
			m[0])

		return text[m[0]:m[1]]
	})
}

// TestScrubbedDumpIsAFixedPoint: scrubbing the committed copy changes nothing, so no rule finds anything left.
func TestScrubbedDumpIsAFixedPoint(t *testing.T) {
	d := loadScrubbed(t)

	res, err := Scrub(d)
	require.NoError(t, err)

	want, err := mediawiki.EncodeDump(d)
	require.NoError(t, err)
	got, err := mediawiki.EncodeDump(res.Dump)
	require.NoError(t, err)
	for name := range want {
		assert.Equal(t, string(want[name]), string(got[name]), "scrubbing %s again changes it", name)
	}
	assert.Zero(t, res.Denylist.Len())
}
