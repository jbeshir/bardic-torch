package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSortTitle(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ title, want string }{
		{"The Parting Glass", "Parting Glass"},
		{"A Grazing Mace", "Grazing Mace"},
		{"An Dro", "Dro"},
		{"the lowlands", "lowlands"},
		{"THE  Wide Sea", "Wide Sea"},
		{"'Tis Pity", "Tis Pity"},
		{"\"The Kettle\"", "Kettle\""},
		{"...And Then", "And Then"},
		{"Theodora", "Theodora"},
		{"Another Round", "Another Round"},
		{"Anvil Song", "Anvil Song"},
		{"Ale", "Ale"},
		{"The", "The"},
		{"A ", "A "},
		{"An Rí", "Rí"},
		{"1588", "1588"},
		{"?!", "?!"},
		{"", ""},
	} {
		assert.Equal(t, tc.want, SortTitle(tc.title), tc.title)
	}
}

func TestSortLetter(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		key  string
		want Letter
	}{
		{"Kettle", "K"},
		{"kettle", "K"},
		{"Éamon", "E"},
		{"ł", "L"}, {"Ł", "L"},
		{"ø", "O"}, {"Ø", "O"},
		{"đ", "D"}, {"Đ", "D"},
		{"ð", "D"}, {"Ð", "D"},
		{"æ", "A"}, {"Æ", "A"},
		{"œ", "O"}, {"Œ", "O"},
		{"ß", "S"}, {"ẞ", "S"},
		{"þ", "T"}, {"Þ", "T"},
		{"ﬁ", "F"},
		{"7 Swans", LetterOther},
		{"'Tis", LetterOther},
		{"日本", LetterOther},
		{"Ωmega", LetterOther},
		{"", LetterOther},
	} {
		assert.Equal(t, tc.want, SortLetter(tc.key), tc.key)
	}
}

func TestSortLetter_MatchesParseLetter(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"", "a", "Z", "é", "ß", "日", "1", "\xff", "-", "ǅ", "İ", "ı"} {
		letter := SortLetter(key)
		parsed, err := ParseLetter(string(letter))
		require.NoError(t, err, key)
		assert.Equal(t, letter, parsed)
	}
}

func TestFoldLetters(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Astrid Aelfric Thorsson", FoldLetters("Astrið Ælfric Þorsson"))
	assert.Equal(t, "Cafe fiddle strasse", FoldLetters("Café ﬁddle straße"))
	assert.Equal(t, "Nihon 日本", FoldLetters("Nihon 日本"))
}
