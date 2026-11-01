package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSearchQuery(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		q          string
		wantText   string
		wantTokens []string
	}{
		{name: "words", q: "Wild Rover", wantText: "Wild Rover", wantTokens: []string{testWild, testRover}},
		{name: "stopword_dropped", q: "The Wild Rover", wantText: "The Wild Rover", wantTokens: []string{testWild, testRover}},
		{name: "only_stopword", q: "the", wantText: "the"},
		{name: "only_short", q: "Oi", wantText: "Oi"},
		{name: "short_and_stopwords", q: "of the ox", wantText: "of the ox"},
		{name: "apostrophe_splits", q: "O'Neill", wantText: "O'Neill", wantTokens: []string{"neill"}},
		{name: "hyphen_splits", q: "Twenty-Two", wantText: "Twenty-Two", wantTokens: []string{"twenty", "two"}},
		{name: "accents_kept", q: "Éire", wantText: "Éire", wantTokens: []string{"éire"}},
		{name: "decomposed_accent_stripped", q: "E\u0301ire", wantText: "E\u0301ire", wantTokens: []string{"eire"}},
		{name: "marks_only_dropped", q: "\u0301\u0301\u0301 \u0300\u0301\u0302\u0303", wantText: "\u0301\u0301\u0301 \u0300\u0301\u0302\u0303"},
		{name: "marks_not_counted", q: "e\u0301\u0301\u0301", wantText: "e\u0301\u0301\u0301"},
		{name: "underscore_joins", q: testUnderscored, wantText: testUnderscored, wantTokens: []string{testUnderscored}},
		{name: "underscores_only_dropped", q: "___ wild", wantText: "___ wild", wantTokens: []string{testWild}},
		{name: "longest_token", q: strings.Repeat("x", MaxSearchTokenLength), wantText: strings.Repeat("x", MaxSearchTokenLength), wantTokens: []string{strings.Repeat("x", MaxSearchTokenLength)}},
		{name: "too_long_token_dropped", q: strings.Repeat("x", MaxSearchTokenLength+1) + " wild", wantText: strings.Repeat("x", MaxSearchTokenLength+1) + " wild", wantTokens: []string{testWild}},
		{name: "only_too_long_token", q: strings.Repeat("\u00e9", MaxSearchTokenLength+1), wantText: strings.Repeat("\u00e9", MaxSearchTokenLength+1)},
		{name: "digits", q: "16th century", wantText: "16th century", wantTokens: []string{"16th", "century"}},
		{name: "deduplicated", q: "Rover rover ROVER", wantText: "Rover rover ROVER", wantTokens: []string{testRover}},
		{
			name: "operators_inert", q: `+wild* -rover ~"grove" @8 (ash) <oak> 'elm'`,
			wantText:   `+wild* -rover ~"grove" @8 (ash) <oak> 'elm'`,
			wantTokens: []string{testWild, testRover, "grove", "ash", "oak", "elm"},
		},
		{
			name: "at_most_eight", q: "one two six ten red tan elm oak ash yew fir",
			wantText:   "one two six ten red tan elm oak ash yew fir",
			wantTokens: []string{"one", "two", "six", "ten", "red", "tan", "elm", "oak"},
		},
		{name: "spaces_collapsed", q: " \tthe \n wild\r\f", wantText: "the wild", wantTokens: []string{testWild}},
		{name: "only_punctuation", q: "%_!", wantText: "%_!"},
		{name: "non_ascii_space_kept", q: "the\u00a0end", wantText: "the\u00a0end", wantTokens: []string{"end"}},
		{name: "longest", q: strings.Repeat("é", MaxSearchLength), wantText: strings.Repeat("é", MaxSearchLength)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseSearchQuery(tc.q)
			require.NoError(t, err)
			assert.Equal(t, tc.wantText, got.Text)
			if len(tc.wantTokens) == 0 {
				assert.Empty(t, got.Tokens)
			} else {
				assert.Equal(t, tc.wantTokens, got.Tokens)
			}
		})
	}
}

func TestParseSearchQuery_Invalid(t *testing.T) {
	t.Parallel()

	for name, q := range map[string]string{
		"empty":        "",
		"spaces":       " \t\r\n\f",
		"too_long":     strings.Repeat("é", MaxSearchLength+1),
		"invalid_utf8": "wild\xff",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseSearchQuery(q)
			assert.Error(t, err)
		})
	}
}

func TestSearchStopwords(t *testing.T) {
	t.Parallel()

	assert.Contains(t, innoDBDefaultStopwords, "the")
	assert.IsIncreasing(t, innoDBDefaultStopwords)
	for _, word := range innoDBDefaultStopwords {
		if len(word) >= MinSearchTokenLength {
			assert.Empty(t, searchTokens(word), word)
		}
	}
}

func TestParseLetter(t *testing.T) {
	t.Parallel()

	for c := 'A'; c <= 'Z'; c++ {
		letter, err := ParseLetter(string(c))
		require.NoError(t, err)
		assert.Equal(t, Letter(string(c)), letter)
	}
	letter, err := ParseLetter("0")
	require.NoError(t, err)
	assert.Equal(t, LetterOther, letter)

	for _, invalid := range []string{"", "a", "1", "AB", "Ä", " A", "#"} {
		_, err := ParseLetter(invalid)
		assert.Error(t, err, "[%s]", invalid)
	}
}

// Search tokens several cases expect.
const (
	testRover = "rover"
	testWild  = "wild"
	// testUnderscored is one word, as InnoDB's parser counts underscores as word characters.
	testUnderscored = "foo_bar"
)
