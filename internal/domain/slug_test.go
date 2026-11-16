package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSlug(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"a", "0", "copper-kettle", "12th-century-topic", strings.Repeat("a", MaxSlugLength)} {
		assert.True(t, IsSlug(s), s)
	}
	for _, s := range []string{
		"", "-", "Copper-kettle", "copper--kettle", "-copper", "copper-", "copper kettle", "copper_kettle",
		"café", "copper/kettle", "copper%20kettle", "copper\n", strings.Repeat("a", MaxSlugLength+1),
	} {
		assert.False(t, IsSlug(s), s)
	}
}

func TestSlugify(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("word-", 40) // 200 characters
	for _, tc := range []struct{ name, want string }{
		{"The Copper Pot", "the-copper-pot"},
		{"Chanson à boire", "chanson-a-boire"},
		{"Déjà Vu", "deja-vu"},
		{"Brennan’s Reel", "brennan-s-reel"},
		{"Astrið", "astrid"},
		{"Łukasz Ørsted", "lukasz-orsted"},
		{"Ælfric Þorsson", "aelfric-thorsson"},
		{"Œuvre Straße", "oeuvre-strasse"},
		{"Ðuro Đorđe", "duro-dorde"},
		{"ﬁddler", "fiddler"},
		{"  --Hey, ho!! (the wind & the rain)--  ", "hey-ho-the-wind-the-rain"},
		{"12th Century", "12th-century"},
		{"½", "1-2"},
		{"日本", ""},
		{"", ""},
		{"!!!", ""},
		{long, strings.TrimSuffix(long[:190], "-")},
		{strings.Repeat("a", 300), strings.Repeat("a", MaxSlugLength)},
		{strings.Repeat("a", 190) + " b", strings.Repeat("a", 190)},
		{strings.Repeat("a", 191) + " b", strings.Repeat("a", 191)},
	} {
		got := Slugify(tc.name)
		assert.Equal(t, tc.want, got, tc.name)
		if got != "" {
			assert.True(t, IsSlug(got), tc.name)
		}
	}
}

// TestSlugify_AlwaysIsSlugOrEmpty fuzzes Slugify: its result is a slug or empty.
func TestSlugify_AlwaysIsSlugOrEmpty(t *testing.T) {
	t.Parallel()

	inputs := []string{"", "-", "a-", "-a", "a--b", "ǅemal", "İstanbul", "ﬃ", "á́", "\xff\xfe", "x y z"}
	for i := range 2000 {
		// Deterministic pseudo-random strings over a mix of ASCII, Latin, punctuation and other scripts.
		alphabet := []rune("aZ09 -_.,'’()ßæøłðþœéÅ日ﬁ́\t")
		var b strings.Builder
		for j := range (i*7)%250 + 1 {
			b.WriteRune(alphabet[(i*31+j*17)%len(alphabet)])
		}
		inputs = append(inputs, b.String())
	}
	for _, s := range inputs {
		if got := Slugify(s); got != "" {
			assert.True(t, IsSlug(got), "%q → %q", s, got)
		}
	}
}
