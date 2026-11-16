package mysql

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnumOf(t *testing.T) {
	t.Parallel()

	c := &catalogueReader{}
	assert.Equal(t, domain.FacetMood, enumOf[domain.Facet](c, queries.TermsFacetMood))
	require.NoError(t, c.err)

	// A column converted to the wrong domain type fails the read, though the value is a valid one of the column.
	c = &catalogueReader{}
	enumOf[domain.PieceKind](c, queries.TermsFacetMood)
	require.ErrorIs(t, c.err, domain.ErrUnknownEnumValue)

	c = &catalogueReader{}
	enumOf[domain.PieceKind](c, queries.PiecesKind("ballad"))
	require.ErrorIs(t, c.err, domain.ErrUnknownEnumValue)

	c = &catalogueReader{}
	assert.Nil(t, nullEnumOf[domain.LinkPlaceType](c, queries.LinksPlaceType(""), false))
	require.NoError(t, c.err)
}
