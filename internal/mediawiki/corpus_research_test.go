package mediawiki

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

func TestCorpus_ResearchCountsOnRealDump(t *testing.T) {
	dump, _ := wikidumptest.Research(t)
	d, err := LoadDump(dump)
	wikidumptest.NoError(t, "loading the dump", err)

	wikidumptest.Equal(t, wikidumptest.Location{Field: "corpus stats", Offset: -1}, researchCounts,
		Stats(ParseCorpus(d)))
}
