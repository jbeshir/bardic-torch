package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

// researchCounts are the counts of the research report (§1, §3) that the real crawl and the scrubbed copy share.
var researchCounts = CorpusStats{
	PiecesByKind: map[domain.PieceKind]int{domain.PieceKindSong: 135, domain.PieceKindPoem: 4,
		domain.PieceKindStory: 2, domain.PieceKindDanceMusic: 1},
	Bards: 48, CollectionEntries: 62, Collections: 61, InlineLyricsPages: 11,
	RedirectsByNS:  map[int]int{NSMain: 6, NSCategory: 6},
	PieceRedirects: 5, BardRedirects: 1,
	RedLinksByNS:  map[int]int{NSMain: 180, NSCategory: 22, NSSpecial: 1},
	ExternalLinks: 691, TrackingParameterURLs: 112, PagesWithRepeatedURLs: 35, WorksWithoutPage: 78,
}

func TestCorpus_ResearchCounts(t *testing.T) {
	d, err := LoadDump(wikidumptest.Scrubbed(t))
	require.NoError(t, err)

	assert.Equal(t, researchCounts, Stats(ParseCorpus(d)))
}
