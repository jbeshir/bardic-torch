//go:build integration

package mysql_test

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// facetSlugs are the slugs of the facet fixture's terms, one per term facet but affiliation.
const (
	siegeSlug     = "siege"
	rousingSlug   = "rousing"
	tudorSlug     = "tudor-era"
	highlandSlug  = "highland"
	oldTongueSlug = "old-tongue"
	heroCycleSlug = "hero-cycle"
	roundSlug     = "round"
	seasonalSlug  = "seasonal"
)

// The facet fixture's pieces' slugs.
const (
	alphaSlug   = "alpha-tune"
	betaSlug    = "beta-tune"
	gammaSlug   = "gamma-tune"
	deltaSlug   = "delta-tune"
	epsilonSlug = "epsilon-tune"
)

// facetPieces are the facet fixture's public pieces by slug, with the terms they are tagged with.
var facetPieces = map[string][]string{
	alphaSlug:   {siegeSlug, rousingSlug, tudorSlug, highlandSlug},
	betaSlug:    {siegeSlug, tudorSlug, oldTongueSlug, heroCycleSlug},
	gammaSlug:   {rousingSlug, highlandSlug, roundSlug, seasonalSlug},
	deltaSlug:   {tudorSlug, rousingSlug, roundSlug},
	epsilonSlug: nil,
}

// facetSearches holds one term in each term facet but affiliation, and pieces tagged with several of them, so
// that every facet's column in the search statements is exercised by a real server.
var facetSearches = catalogtest.NewShared(func(c *catalogtest.Catalog) struct{} {
	facets := map[string]domain.Facet{
		siegeSlug: domain.FacetTopic, rousingSlug: domain.FacetMood, tudorSlug: domain.FacetOrigin,
		highlandSlug: domain.FacetCulture, oldTongueSlug: domain.FacetLanguage,
		heroCycleSlug: domain.FacetLegendaria, roundSlug: domain.FacetForm, seasonalSlug: domain.FacetOther,
	}
	names := map[string]string{
		siegeSlug: "Siege", rousingSlug: "Rousing", tudorSlug: "Tudor Era", highlandSlug: "Highland",
		oldTongueSlug: "Old Tongue", heroCycleSlug: "Hero Cycle", roundSlug: "Round", seasonalSlug: "Seasonal",
	}
	refs := map[string]catalogtest.Ref{}
	for slug, facet := range facets {
		refs[slug] = c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: facet, Name: names[slug]}, Slug: slug})
	}
	for slug, tagged := range facetPieces {
		doc := domain.PieceDocument{Title: facetTitle(slug)}
		for _, term := range tagged {
			doc.Terms = append(doc.Terms, domain.DocPieceTerm{TermID: refs[term].ID})
		}
		c.Piece(catalogtest.Piece{Doc: doc, Slug: slug})
	}
	return struct{}{}
})

// facetTitle is the title of the facet fixture piece with slug.
func facetTitle(slug string) string {
	return map[string]string{
		alphaSlug: "Alpha Tune", betaSlug: "Beta Tune", gammaSlug: "Gamma Tune",
		deltaSlug: "Delta Tune", epsilonSlug: "Epsilon Tune",
	}[slug]
}

func facetSummaries(slugs ...string) []domain.PieceSummary {
	summaries := []domain.PieceSummary{}
	for _, slug := range slugs {
		summaries = append(summaries, domain.PieceSummary{Slug: slug, Title: facetTitle(slug),
			Kind: domain.PieceKindSong, ConservationStatus: domain.ConservationStatusUnassessed})
	}
	return summaries
}

// TestRepository_ListPublicPieces_EachTermFacet selects one term of each term facet in turn, and checks the
// pieces, which are hand-computed from facetPieces.
func TestRepository_ListPublicPieces_EachTermFacet(t *testing.T) {
	t.Parallel()

	db, _ := facetSearches.Get(t)
	repo := mysql.New(db)

	for _, tc := range []struct {
		term string
		want []string
	}{
		{siegeSlug, []string{alphaSlug, betaSlug}},
		{rousingSlug, []string{alphaSlug, deltaSlug, gammaSlug}},
		{tudorSlug, []string{alphaSlug, betaSlug, deltaSlug}},
		{highlandSlug, []string{alphaSlug, gammaSlug}},
		{oldTongueSlug, []string{betaSlug}},
		{heroCycleSlug, []string{betaSlug}},
		{roundSlug, []string{deltaSlug, gammaSlug}},
		{seasonalSlug, []string{gammaSlug}},
	} {
		t.Run(tc.term, func(t *testing.T) {
			t.Parallel()

			list, err := repo.ListPublicPieces(t.Context(), datasources.PieceQuery{TermSlugs: []string{tc.term},
				Page: domain.Page{Limit: domain.DefaultPageLimit}})
			require.NoError(t, err)
			assert.Equal(t, facetSummaries(tc.want...), list.Pieces)
			assert.Equal(t, int64(len(tc.want)), list.Total)
		})
	}
}

// TestRepository_ListPublicPieces_ThreeTermFacets selects an origin, a topic and a mood term: only Alpha Tune has
// all three. Each term's count leaves out only its own facet's selection, and a term of an unselected facet is
// counted among the pieces that match all three.
func TestRepository_ListPublicPieces_ThreeTermFacets(t *testing.T) {
	t.Parallel()

	db, _ := facetSearches.Get(t)
	repo := mysql.New(db)

	list, err := repo.ListPublicPieces(t.Context(), datasources.PieceQuery{
		TermSlugs: []string{tudorSlug, siegeSlug, rousingSlug}, Page: domain.Page{Limit: domain.DefaultPageLimit}})
	require.NoError(t, err)

	assert.Equal(t, domain.PieceList{
		Pieces: facetSummaries(alphaSlug),
		Total:  1,
		Facets: domain.PieceFacets{
			// Siege: matching tudor and rousing leaves alpha and delta, of which alpha has the siege. Rousing:
			// tudor and siege leave alpha and beta, of which alpha is rousing. Tudor: siege and rousing leave
			// alpha. Highland is not selected, so it is counted among alpha alone.
			Terms:                termCounts(highlandSlug, 1, rousingSlug, 1, siegeSlug, 1, tudorSlug, 1),
			Kinds:                kindCounts(domain.PieceKindSong, 1),
			ConservationStatuses: statusCounts(domain.ConservationStatusUnassessed, 1),
			Letters:              letters("A", 1),
		},
	}, list)

	// Without the siege selection, Delta Tune (tudor and rousing, not siege) is found too. Tudor is counted among the
	// rousing pieces (alpha, delta, gamma), rousing among the tudor ones (alpha, beta, delta), and the unselected
	// terms among alpha and delta.
	list, err = repo.ListPublicPieces(t.Context(), datasources.PieceQuery{
		TermSlugs: []string{tudorSlug, rousingSlug}, Page: domain.Page{Limit: domain.DefaultPageLimit}})
	require.NoError(t, err)
	assert.Equal(t, facetSummaries(alphaSlug, deltaSlug), list.Pieces)
	assert.Equal(t, termCounts(highlandSlug, 1, roundSlug, 1, rousingSlug, 2, siegeSlug, 1, tudorSlug, 2),
		list.Facets.Terms)
}
