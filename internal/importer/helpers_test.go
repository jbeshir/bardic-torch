package importer

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

// buildTime is the revision time of every test import.
var buildTime = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// scrubbed is the committed scrubbed dump parsed and built once, as every test only reads it.
var scrubbed struct {
	once   sync.Once
	err    error
	corpus mediawiki.Corpus
	imp    domain.CatalogueImport
}

func loadScrubbed(t *testing.T) {
	t.Helper()
	scrubbed.once.Do(func() {
		var d mediawiki.Dump
		d, scrubbed.err = mediawiki.LoadDump(wikidumptest.Scrubbed(t))
		scrubbed.corpus = mediawiki.ParseCorpus(d)
		scrubbed.imp, _ = Build(scrubbed.corpus, buildTime)
	})
	require.NoError(t, scrubbed.err)
}

// scrubbedCorpus is the committed scrubbed dump, parsed.
func scrubbedCorpus(t *testing.T) mediawiki.Corpus {
	t.Helper()
	loadScrubbed(t)

	return scrubbed.corpus
}

// scrubbedImport is the import of the committed scrubbed dump.
func scrubbedImport(t *testing.T) domain.CatalogueImport {
	t.Helper()
	loadScrubbed(t)

	return scrubbed.imp
}
