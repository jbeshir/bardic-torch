package mediawiki

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// scrubbedLinks parses the links of the committed scrubbed dump.
func scrubbedLinks(t *testing.T) (Links, People) {
	t.Helper()
	s := scrubbedSite(t)
	pieces := s.Pieces()
	people := ParsePeople(s, pieces)

	return ParseLinks(s, pieces, people), people
}

func TestScrubbedLinks_ResearchCounts(t *testing.T) {
	l, _ := scrubbedLinks(t)

	c := l.Collections
	assert.Equal(t, 62, c.Entries())
	assert.Len(t, c.Records, 61)
	assert.Empty(t, c.Dropped)
	require.Len(t, c.Duplicates, 1)
	assert.Equal(t, "https://history.westkingdom.org/bardic/bardic_list.php", c.Duplicates[0].NormalizedURL)
	nested, notBardic := 0, 0
	kinds := map[domain.CollectionKind]int{}
	for _, r := range append(append([]CollectionRecord(nil), c.Records...), c.Duplicates...) {
		if r.Depth > 1 {
			nested++
			assert.NotEmpty(t, r.ParentURL)
		}
		if !r.IsBardicResource {
			notBardic++
		}
		kinds[r.Kind]++
	}
	assert.Equal(t, 2, nested)
	assert.Equal(t, 1, notBardic)
	assert.Equal(t, map[domain.CollectionKind]int{
		domain.CollectionKindIndexOrLinkList: 7, domain.CollectionKindSongbookPDF: 11,
		domain.CollectionKindSongbookWebsite: 18, domain.CollectionKindWebsite: 12,
		domain.CollectionKindWikiCategory: 2, domain.CollectionKindYouTubeChannel: 2,
		domain.CollectionKindGoogleDocSongbook: 1, domain.CollectionKindWiki: 2, domain.CollectionKindBlog: 2,
		domain.CollectionKindArchivedSongbook: 2, domain.CollectionKindHostedDocument: 2,
		domain.CollectionKindYouTubePlaylist: 1,
	}, kinds)

	assert.Equal(t, 112, l.TrackingParameterURLs())
	assert.Len(t, l.PagesWithRepeatedURLs(), 35)
	assert.Len(t, l.BareURLs(), 2)
	assert.Empty(t, l.Unlinked)
}

func TestScrubbedLinks_EventsAndPerformers(t *testing.T) {
	l, people := scrubbedLinks(t)

	require.NotEmpty(t, l.Events)
	assert.Equal(t, EventRecord{Name: greenDragon, Kind: domain.EventKindVenue, PageID: 131, Pages: l.Events[0].Pages},
		l.Events[0], "the Green Dragon venue page is the first event")
	assert.NotEmpty(t, l.Events[0].Pages)
	assert.Contains(t, l.SimilarEvents, SimilarEventNames{A: pennsic45, B: "Pennsic 47"})

	marian := false
	for _, m := range people.Persons.Matches {
		if m.Mention.Name.Name == "Marian of Heatherdale" {
			marian = true
			assert.Equal(t, MatchFuzzy, m.Match.Kind)
			assert.Equal(t, PersonRef("Heather Dale"), m.Match.Person)
		}
	}
	assert.True(t, marian, "the performer resolves to Heather Dale through the Marion alias")
	_, ok := people.Persons.Stub("Marian of Heatherdale")
	assert.False(t, ok)
}

func TestScrubbedLinks_BardPerformanceOnPiece(t *testing.T) {
	l, _ := scrubbedLinks(t)
	const video = "https://youtube.com/watch?v=oDRTvZHFl9o"

	bard := linkOfURL(t, personLinksOf(t, l, "Dahrien Cordell").Links, video)
	assert.Equal(t, []domain.LinkRole{domain.LinkRolePerformance}, bard.Roles, "the repaired double-bracket link")
	assert.Empty(t, bard.Performances)
	piece := linkOfURL(t, pieceLinksOf(t, l, "Rite of Passage").Links, video)
	assert.Contains(t, piece.Roles, domain.LinkRolePerformance)
	assert.NotEmpty(t, piece.Performances)
}

func TestScrubbedLinks_CanonicalAndCollections(t *testing.T) {
	l, _ := scrubbedLinks(t)

	canonical := map[string]LinkFields{}
	for _, f := range l.Canonical {
		_, dup := canonical[f.NormalizedURL]
		assert.False(t, dup, f.NormalizedURL)
		canonical[f.NormalizedURL] = f
	}
	check := func(links []LinkRecord) {
		for _, link := range links {
			assert.Equal(t, canonical[link.NormalizedURL], link.LinkFields, "every document carries the canonical link")
			if link.Platform == PlatformYouTube && link.CollectionURL != "" {
				assert.Equal(t, link.NormalizedURL, link.CollectionURL, "YouTube links match collections exactly only")
			}
			for _, p := range link.Performances {
				assert.NotContains(t, strings.ToLower(p.DescriptionVerbatim), "scrubbedlyric")
			}
		}
	}
	ties := 0
	for _, pl := range l.Pieces {
		check(pl.Links)
		for _, link := range pl.Links {
			if len(link.CollectionTied) > 0 {
				ties++
			}
		}
	}
	for _, pl := range l.Persons {
		check(pl.Links)
	}
	assert.Positive(t, ties, "links under two collections' shared prefix tie")
	assert.NotEmpty(t, l.Conflicts)
}
