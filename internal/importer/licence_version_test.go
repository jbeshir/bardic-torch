package importer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImporter_LeavesLicenceVersionNull checks that the importer infers no licence from prose, so no licence
// version either (docs/PLAN.md §7).
func TestImporter_LeavesLicenceVersionNull(t *testing.T) {
	imp, _ := scrubbedImport(t)

	require.NotEmpty(t, imp.Pieces)
	for _, piece := range imp.Pieces {
		assert.Nil(t, piece.Doc.Licence, piece.Slug)
		assert.Nil(t, piece.Doc.LicenceVersion, piece.Slug)
	}
}
