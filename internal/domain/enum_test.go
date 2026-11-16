package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnums_ValidAndParse(t *testing.T) {
	t.Parallel()

	for _, reason := range NeedsWorkReasons() {
		assert.True(t, reason.Valid(), reason)
		parsed, err := ParseNeedsWorkReason(string(reason))
		require.NoError(t, err)
		assert.Equal(t, reason, parsed)
	}
	_, err := ParseNeedsWorkReason("orphaned")
	require.EqualError(t, err, "unknown needs-work reason [orphaned]")

	for _, kind := range pieceKindValues {
		assert.True(t, kind.Valid(), kind)
	}
	assert.False(t, PieceKind("ballad").Valid())
	assert.False(t, PieceKind("").Valid())
	assert.False(t, Facet("Mood").Valid(), "values are case-sensitive")
	_, err = ParseFacet("kind")
	require.EqualError(t, err, "unknown facet [kind]")
}
