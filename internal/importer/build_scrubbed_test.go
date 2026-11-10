package importer

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// lyricMarker is the word every placeholder line of the scrubbed copy's dropped lyrics has.
const lyricMarker = "scrubbedlyric"

func TestBuild_ValidatesOutput(t *testing.T) {
	imp, _ := scrubbedImport(t)

	require.NoError(t, imp.Validate())
}

func TestBuild_ResearchCounts(t *testing.T) {
	c := scrubbedCorpus(t)
	imp, _ := Build(c, buildTime)

	assert.Equal(t, researchCounts, importCounts(c, imp))
}

func TestBuild_TermClosureIncludesSelfRows(t *testing.T) {
	imp, _ := scrubbedImport(t)
	ancestors, err := imp.TermAncestors()
	require.NoError(t, err)

	self := map[int64]bool{}
	for _, a := range ancestors {
		if a.Depth == 0 {
			assert.Equal(t, a.AncestorID, a.DescendantID)
			self[a.DescendantID] = true
		}
	}
	require.NotEmpty(t, imp.Terms)
	for _, term := range imp.Terms {
		assert.True(t, self[term.ID], "term %d has its own row", term.ID)
	}
	assert.Greater(t, len(ancestors), len(imp.Terms), "and rows for its ancestors")
}

func TestBuild_SortKeys(t *testing.T) {
	imp, _ := scrubbedImport(t)

	for _, p := range imp.Pieces {
		assert.Equal(t, domain.SortTitle(p.Doc.Title), p.SortTitle)
		assert.Equal(t, domain.SortLetter(p.SortTitle), p.SortLetter)
	}
	for _, p := range imp.Persons {
		assert.Equal(t, p.Doc.Name, p.SortName, "persons sort by their name as written")
		assert.Equal(t, domain.SortLetter(p.SortName), p.SortLetter)
	}
	for _, c := range imp.Collections {
		assert.Equal(t, domain.SortTitle(c.Doc.Name), c.SortName)
	}
	threeWords := pieceTitled(t, imp, "Three Words")
	assert.Equal(t, domain.Letter("T"), threeWords.SortLetter)
}

func TestBuild_ConservationSourceIsThePiecesWikiPage(t *testing.T) {
	imp, _ := scrubbedImport(t)
	pageURLs := map[string]string{}
	for _, p := range scrubbedCorpus(t).Pieces {
		pageURLs[p.Title] = p.URL
	}

	assessed := 0
	for _, p := range imp.Pieces {
		c := p.Doc.Conservation
		if c.Status == domain.ConservationStatusUnassessed {
			assert.Nil(t, c.SourceURL, p.Doc.Title)

			continue
		}
		assessed++
		require.NotNil(t, c.SourceURL, p.Doc.Title)
		require.NotNil(t, p.Legacy, p.Doc.Title)
		assert.Equal(t, pageURLs[p.Legacy.Title], *c.SourceURL, "%s: the source is the piece's own wiki page",
			p.Doc.Title)
	}
	assert.Equal(t, 11, assessed)
}

// TestBuild_NoLyricMarkerAnywhere checks that no dropped lyric line of the scrubbed copy reaches any field of the
// import, public or not, or the review.
func TestBuild_NoLyricMarkerAnywhere(t *testing.T) {
	imp, review := scrubbedImport(t)

	encoded, err := json.Marshal(imp)
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(string(encoded)), lyricMarker)
	assert.NotContains(t, strings.ToLower(string(RenderReview(review))), lyricMarker)
}

// TestBuild_ContributorsAreCreditedPerPage: a piece page's revision credits its contributors, and a stub piece's
// those of the bard pages listing it, from which it comes.
// shareTracking is a query parameter that can tie a shared link to its sharer.
var shareTracking = regexp.MustCompile("[?&](?:si|utm_[a-z]+|fbclid|igshid|ab_channel|feature|pp)=")

// TestBuild_URLsWithoutTrackingParameters: no imported link URL, nor any URL of a performance description, keeps a
// tracking parameter, though the wiki writes many.
func TestBuild_URLsWithoutTrackingParameters(t *testing.T) {
	imp, _ := scrubbedImport(t)
	written := 0
	for _, pl := range scrubbedCorpus(t).Links.Pieces {
		for _, o := range pl.Occurrences {
			if shareTracking.MatchString(o.URL) {
				written++
			}
		}
	}
	require.Positive(t, written)

	for _, l := range imp.Links {
		assert.NotRegexp(t, shareTracking, l.Link.URL)
	}
	for _, p := range imp.Pieces {
		for _, l := range p.Doc.Links {
			assert.NotRegexp(t, shareTracking, l.URL)
			for _, perf := range l.Performances {
				for _, e := range mediawiki.FindExternalLinks(perf.DescriptionVerbatim) {
					assert.NotRegexp(t, shareTracking, e.URL, p.Doc.Title)
				}
			}
		}
	}
}

func TestBuild_ContributorsAreCreditedPerPage(t *testing.T) {
	imp, review := scrubbedImport(t)
	c := scrubbedCorpus(t)
	activity := map[string][]string{}
	for _, a := range c.Dump.Activity {
		if a.NS == mediawiki.NSMain {
			activity[a.Title] = slices.Collect(maps.Keys(a.Users))
		}
	}
	listers := map[string][]string{}
	for _, sp := range c.People.StubPieces {
		for _, cr := range sp.Credits {
			listers[sp.Title] = append(listers[sp.Title], activity[string(cr.Person)]...)
		}
	}

	credited := 0
	for _, p := range imp.Pieces {
		if !p.Doc.IsStub {
			if len(p.Revision.Contributors) > 0 {
				credited++
			}

			continue
		}
		assert.Equal(t, domain.ImportSummaryStub, p.Revision.Summary)
		require.NotNil(t, p.Legacy)
		want := slices.Compact(slices.Sorted(slices.Values(listers[p.Legacy.Title])))
		assert.NotEmpty(t, want, p.Doc.Title)
		assert.ElementsMatch(t, want, p.Revision.Contributors, "%s: the listing bard pages' contributors", p.Doc.Title)
	}
	assert.Equal(t, 142, credited, "every piece page has contributors")
	assert.NotEmpty(t, review.Contributors)
}
