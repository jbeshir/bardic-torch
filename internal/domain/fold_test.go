package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFoldName_ApostrophesAndAccents(t *testing.T) {
	for in, want := range map[string]string{
		"Lasair inghen ui h’Airt": "lasair inghen ui hairt",
		"Lasair inghen ui h'Airt": "lasair inghen ui hairt",
		"  Thórvé   Skáld  ":      "thorve skald",
		"Ælfric ʼOʼ `Brien‘":      "aelfric o brien",
		"Astrið Æsa":              "astrid aesa",
		"WREN\tPLACEHOLDER":       "wren placeholder",
	} {
		assert.Equal(t, want, FoldName(in), in)
	}
}

func TestDuplicateKey(t *testing.T) {
	t.Parallel()

	for _, group := range [][]string{
		{"The Wren's Song", "The Wrens Song", "the wren’s song", "The Wrenʼs Song", "The Wren‘s Song",
			"The Wren`s Song", "The Wren´s Song", "THE WRENS SONG", "  The   Wrens\tSong "},
		{"Ástrið Placeholder", "Astrid Placeholder", "ASTRID PLACEHOLDER"},
		{"Łukasz Ørn", "Lukasz Orn"},
		{"Æsa Groß", "Aesa Gross"},
		{"Café Morwen", "Cafe\u0301 Morwen", "cafe morwen"},
	} {
		for _, s := range group {
			assert.Equal(t, DuplicateKey(group[0]), DuplicateKey(s), s)
		}
	}
	assert.Equal(t, "the wrens song", DuplicateKey("The Wren's Song"))
	assert.Equal(t, "lukasz orn", DuplicateKey("Łukasz Ørn"))
	assert.NotEqual(t, DuplicateKey("The Wren Song"), DuplicateKey("The Wrens Song"))
	assert.NotEqual(t, DuplicateKey("Morwen"), DuplicateKey("Mor wen"), "spaces still separate words")
}

func TestDisplayNameKey(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"Morwen", "MORWEN", "Mor wen", "Mor.wen", "Mor-wen!", "  mor_wen ", "Mórwen", "Mor'wen"} {
		assert.Equal(t, "morwen", DisplayNameKey(s), s)
	}
	assert.Equal(t, "bard0a1b2c3d", DisplayNameKey("Bard 0a1b2c3d"))
	assert.Equal(t, "aesagross", DisplayNameKey("Æsa Groß"))
	assert.NotEqual(t, DisplayNameKey("Morwen"), DisplayNameKey("Morwenna"))
}
