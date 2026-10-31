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
