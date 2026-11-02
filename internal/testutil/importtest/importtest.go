// Package importtest gives tests the catalogue import of the committed scrubbed copy of the wiki crawl, built
// once per test binary.
package importtest

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/importer"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

// BuildTime is the time of every revision of the scrubbed import.
var BuildTime = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

var scrubbed struct {
	once   sync.Once
	err    error
	imp    []byte
	review importer.Review
}

// Scrubbed returns the import of the committed scrubbed copy and its review. Each call returns its own copy of
// the import, which the caller may change; the review is shared, and must not be changed.
func Scrubbed(t testing.TB) (domain.CatalogueImport, importer.Review) {
	t.Helper()
	scrubbed.once.Do(func() {
		var d mediawiki.Dump
		d, scrubbed.err = mediawiki.LoadDump(wikidumptest.Scrubbed(t))
		if scrubbed.err != nil {
			return
		}
		var imp domain.CatalogueImport
		imp, scrubbed.review = importer.Build(mediawiki.ParseCorpus(d), BuildTime)
		if scrubbed.err = imp.Validate(); scrubbed.err != nil {
			return
		}
		scrubbed.imp, scrubbed.err = json.Marshal(imp)
	})
	if scrubbed.err != nil {
		t.Fatalf("importtest: building the scrubbed import: %v", scrubbed.err)
	}
	var imp domain.CatalogueImport
	if err := json.Unmarshal(scrubbed.imp, &imp); err != nil {
		t.Fatalf("importtest: copying the scrubbed import: %v", err)
	}

	return imp, scrubbed.review
}
