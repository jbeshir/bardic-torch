package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCollationKey(t *testing.T) {
	assert.Equal(t, CollationKey("Astrið Ælfric"), CollationKey("astrid aelfric"))
	assert.NotEqual(t, CollationKey("O'Brien"), CollationKey("OBrien"), "apostrophes count, as in the collation")
}
