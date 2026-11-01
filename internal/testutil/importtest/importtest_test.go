package importtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/importer"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

func TestScrubbed_CopiesTheBuiltImport(t *testing.T) {
	d, err := mediawiki.LoadDump(wikidumptest.Scrubbed(t))
	require.NoError(t, err)
	built, _ := importer.Build(mediawiki.ParseCorpus(d), importer.BuildOptions{Now: BuildTime})

	first, _ := Scrubbed(t)
	first.Pieces[0].Doc.Title = "Changed by the caller"
	second, _ := Scrubbed(t)

	assert.Equal(t, built, second, "a copy equals the import as built, and a caller's change does not reach it")
}
