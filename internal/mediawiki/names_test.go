package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWords_FoldedAndLocated(t *testing.T) {
	text := "Ó Brien's  wren_PLACEHOLDER-x"

	words := Words(text)

	var folded []string
	for _, w := range words {
		folded = append(folded, w.Folded)
		assert.NotEmpty(t, text[w.Start:w.End])
	}
	assert.Equal(t, []string{"o", "brien", "s", "wren", "placeholder", "x"}, folded)
	assert.Equal(t, "PLACEHOLDER", text[words[4].Start:words[4].End])
}
