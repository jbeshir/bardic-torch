package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPersonIsStub(t *testing.T) {
	t.Parallel()

	assert.False(t, PersonIsStub(false, PersonStatusActive))
	assert.False(t, PersonIsStub(false, PersonStatusDeceased))
	assert.True(t, PersonIsStub(true, PersonStatusActive))
	assert.True(t, PersonIsStub(false, PersonStatusPrefersNoPage))
}
