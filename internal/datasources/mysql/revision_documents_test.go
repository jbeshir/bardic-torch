package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

func TestEncodeFlags_NilIsEmptyArray(t *testing.T) {
	encoded, err := encodeFlags(nil)
	require.NoError(t, err)
	assert.JSONEq(t, "[]", string(encoded))

	encoded, err = encodeFlags([]domain.Flag{{Code: domain.FlagPossibleDuplicate}})
	require.NoError(t, err)
	assert.JSONEq(t, `[{"code":"possible_duplicate","path":""}]`, string(encoded))
}

func TestDecodeJSONColumn_EmptyIsZero(t *testing.T) {
	flags, err := decodeJSONColumn[[]domain.Flag]("flags", nil)
	require.NoError(t, err)
	assert.NotNil(t, flags)
	assert.Empty(t, flags)

	flags, err = decodeJSONColumn[[]domain.Flag]("flags", []byte("null"))
	require.NoError(t, err)
	assert.NotNil(t, flags)

	keys, err := decodeJSONColumn[domain.ElementKeySet]("received keys", []byte{})
	require.NoError(t, err)
	assert.NotNil(t, keys)
	assert.Empty(t, keys)

	_, err = decodeJSONColumn[[]domain.Flag]("flags", []byte("{"))
	require.ErrorContains(t, err, "decoding flags")
}
