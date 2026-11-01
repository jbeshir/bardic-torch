package importer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// lyricMarker is the word every placeholder line of the scrubbed copy's dropped lyrics has.
const lyricMarker = "scrubbedlyric"

func TestBuild_ValidatesOutput(t *testing.T) {
	imp, _ := scrubbedImport(t)

	require.NoError(t, imp.Validate())
}

func TestBuild_ResearchCounts(t *testing.T) {
	c := scrubbedCorpus(t)
	imp, _ := Build(c, BuildOptions{Now: buildTime})

	assert.Equal(t, researchCounts, importCounts(c, imp))
}

func TestBuild_TermClosureIncludesSelfRows(t *testing.T) {
	imp, _ := scrubbedImport(t)

	self := map[int64]bool{}
	for _, a := range imp.TermAncestors {
		if a.Depth == 0 {
			assert.Equal(t, a.AncestorID, a.DescendantID)
			self[a.DescendantID] = true
		}
	}
	require.NotEmpty(t, imp.Terms)
	for _, term := range imp.Terms {
		assert.True(t, self[term.ID], "term %d has its own row", term.ID)
	}
	assert.Greater(t, len(imp.TermAncestors), len(imp.Terms), "and rows for its ancestors")
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

func TestBuild_ConservationSourceDefaultsToLegacyURL(t *testing.T) {
	imp, _ := scrubbedImport(t)

	assessed := 0
	for _, p := range imp.Pieces {
		c := p.Doc.Conservation
		if c.Status == domain.ConservationStatusUnassessed {
			assert.Nil(t, c.SourceURL, p.Doc.Title)

			continue
		}
		assessed++
		require.NotNil(t, c.SourceURL, p.Doc.Title)
		assert.Contains(t, *c.SourceURL, "/wiki/", "%s: the source is the wiki page", p.Doc.Title)
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

// TestBuild_NoPlaceholderInPublicFields checks that no name placeholder of the scrubbed copy, in any of its forms,
// is in a public field: only hidden aliases, legacy columns and hidden links may hold one.
func TestBuild_NoPlaceholderInPublicFields(t *testing.T) {
	imp, _ := scrubbedImport(t)

	assert.Empty(t, placeholderForm.FindAllString(publicJSON(t, imp), -1))
	encoded, err := json.Marshal(imp)
	require.NoError(t, err)
	assert.NotEmpty(t, placeholderForm.FindAllString(string(encoded), -1),
		"the hidden aliases still hold their placeholders")
}

func TestBuild_ContributorsAreCreditedPerPage(t *testing.T) {
	imp, review := scrubbedImport(t)

	credited := 0
	for _, p := range imp.Pieces {
		if p.Doc.IsStub {
			assert.Empty(t, p.Revision.Contributors, "a stub has no page to credit")
			assert.Equal(t, domain.ImportSummaryStub, p.Revision.Summary)
		} else if len(p.Revision.Contributors) > 0 {
			credited++
		}
	}
	assert.Equal(t, 142, credited, "every piece page has contributors")
	assert.NotEmpty(t, review.Contributors)
}
