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

func TestSlugAllocator_Collisions(t *testing.T) {
	t.Parallel()

	var a SlugAllocator
	assert.Equal(t, "kettle", a.Allocate("Kettle", SlugEntityTypePiece, 1))
	assert.Equal(t, "kettle-2", a.Allocate("Kettle!", SlugEntityTypePiece, 2))
	assert.Equal(t, "kettle-3", a.Allocate("kettle", SlugEntityTypePiece, 3))
	assert.Equal(t, "kettle-2-2", a.Allocate("Kettle 2", SlugEntityTypePiece, 4))
	// Slugs are unique per entity type only.
	assert.Equal(t, "kettle", a.Allocate("Kettle", SlugEntityTypePerson, 1))
}

func TestSlugAllocator_EmptyFallsBackToEntityID(t *testing.T) {
	t.Parallel()

	var a SlugAllocator
	assert.Equal(t, "piece-7", a.Allocate("日本", SlugEntityTypePiece, 7))
	assert.Equal(t, "person-7", a.Allocate("", SlugEntityTypePerson, 7))
	assert.Equal(t, "event-12", a.Allocate("!!!", SlugEntityTypeEvent, 12))
	assert.Equal(t, "term-3", a.Allocate("", SlugEntityTypeTerm, 3))
	// A name whose slug is a fallback slug taken before gets a suffix.
	assert.Equal(t, "piece-7-2", a.Allocate("Piece 7", SlugEntityTypePiece, 8))
}

func TestSlugAllocator_SuffixFitsMaxLength(t *testing.T) {
	t.Parallel()

	var a SlugAllocator
	unbroken := strings.Repeat("a", MaxSlugLength)
	assert.Equal(t, unbroken, a.Allocate(unbroken, SlugEntityTypePiece, 1))
	second := a.Allocate(unbroken, SlugEntityTypePiece, 2)
	assert.Equal(t, strings.Repeat("a", MaxSlugLength-2)+"-2", second)

	words := strings.Repeat("abcd-", 38) + "x" // 191 characters
	assert.Equal(t, words, a.Allocate(words, SlugEntityTypePiece, 3))
	third := a.Allocate(words, SlugEntityTypePiece, 4)
	assert.Equal(t, strings.TrimSuffix(strings.Repeat("abcd-", 37), "-")+"-abcd-2", third)
	for n := 5; n < 15; n++ {
		slug := a.Allocate(words, SlugEntityTypePiece, int64(n))
		assert.True(t, IsSlug(slug), slug)
	}
}

func TestEntitySlug(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "the-wren-s-song", EntitySlug("The Wren's Song", SlugEntityTypePiece, 3, 1))
	assert.Equal(t, "the-wren-s-song-2", EntitySlug("The Wren's Song", SlugEntityTypePiece, 3, 2))
	assert.Equal(t, "term-3", EntitySlug("…", SlugEntityTypeTerm, 3, 1))
	assert.Equal(t, "term-3-12", EntitySlug("…", SlugEntityTypeTerm, 3, 12))
}
