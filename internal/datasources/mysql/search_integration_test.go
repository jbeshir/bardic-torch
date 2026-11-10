//go:build integration

package mysql_test

import (
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The search fixture's piece slugs. Its pieces, with their facets (terms with descendants in brackets):
//
//	armada       1588 Armada       0  song                endangered             war, 16th-century
//	ash-grove    The Ash Grove     A  song + poem         low_risk               war, swords
//	eire-abu     Éire Abú          E  instrumental        unassessed             sea, 16th-century
//	iron-crown   Iron Crown        I  poem                endangered       stub  rapiers, mournful
//	oi-polloi    Oi Polloi         O  comedy              unassessed             mournful
//	pike-march   Pike March        P  song                unassessed             pikes (under unapproved spears)
//	salt-road    Salt Road         S  story + song        critically_endangered  sea, warlike
//	salt-road-2  Salt Road         S  poem                unassessed             (none)
//	masks        Theatre of Masks  T  dance_music         restricted       stub  (none)
//	wild-heron   The Wild Heron    W  song                endangered             swords, warlike
//
// The topics are war > swords > rapiers, and sea; the moods warlike and mournful; the origin 16th-century. An
// unapproved piece is tagged with war and warlike, and northmarch is an affiliation.
const (
	armadaSlug     = "armada"
	ashGroveSlug   = "ash-grove"
	eireAbuSlug    = "eire-abu"
	ironCrownSlug  = "iron-crown"
	oiPolloiSlug   = "oi-polloi"
	pikeMarchSlug  = "pike-march"
	saltRoadSlug   = "salt-road"
	saltRoad2Slug  = "salt-road-2"
	masksSlug      = "masks"
	wildHeronSlug  = "wild-heron"
	warSlug        = "war"
	swordsSlug     = "swords"
	rapiersSlug    = "rapiers"
	seaSlug        = "sea"
	warlikeSlug    = "warlike"
	mournfulSlug   = "mournful"
	centurySlug    = "16th-century"
	northmarchSlug = "northmarch"
	ravenmoorSlug  = "ravenmoor"
	saltRoadTitle  = "Salt Road"
	bardicWikiName = "Bardic Wiki"
)

// searchPieces are the summaries of the search fixture's public pieces, by slug.
var searchPieces = map[string]domain.PieceSummary{}

// searchFixture holds the refs the person and collection tests compare by.
type searchFixture struct {
	folio, balladIndex, bardicWiki, bardicBlog, northmarchBook catalogtest.Ref
}

var searches = catalogtest.NewShared(func(c *catalogtest.Catalog) searchFixture {
	term := func(facet domain.Facet, name, slug string, parent *catalogtest.Ref, unapproved bool) catalogtest.Ref {
		doc := domain.TermDocument{Facet: facet, Name: name}
		if parent != nil {
			doc.ParentID = &parent.ID
		}
		return c.Term(catalogtest.Term{Doc: doc, Slug: slug, Unapproved: unapproved})
	}
	war := term(domain.FacetTopic, warName, warSlug, nil, false)
	swords := term(domain.FacetTopic, swordsName, swordsSlug, &war, false)
	rapiers := term(domain.FacetTopic, "Rapiers", rapiersSlug, &swords, false)
	sea := term(domain.FacetTopic, "Sea", seaSlug, nil, false)
	spears := term(domain.FacetTopic, "Spears", "spears", nil, true)
	pikes := term(domain.FacetTopic, "Pikes", "pikes", &spears, false)
	warlike := term(domain.FacetMood, "Warlike", warlikeSlug, nil, false)
	mournful := term(domain.FacetMood, "Mournful", mournfulSlug, nil, false)
	century := term(domain.FacetOrigin, "16th Century", centurySlug, nil, false)
	northmarch := term(domain.FacetAffiliation, northmarchName, northmarchSlug, nil, false)
	ravenmoor := term(domain.FacetAffiliation, "Ravenmoor", ravenmoorSlug, &northmarch, false)
	term(domain.FacetAffiliation, "Southmarch", "southmarch", nil, true)

	piece := func(slug, title, sortTitle, letter string, kind domain.PieceKind, secondary []domain.PieceKind,
		status domain.ConservationStatus, stub, unapproved bool, terms ...catalogtest.Ref,
	) {
		doc := domain.PieceDocument{Title: title, Kind: kind, SecondaryKinds: secondary,
			Conservation: domain.DocConservation{Status: status}, IsStub: stub}
		for _, t := range terms {
			doc.Terms = append(doc.Terms, domain.DocPieceTerm{TermID: t.ID})
		}
		c.Piece(catalogtest.Piece{Doc: doc, Slug: slug, SortTitle: sortTitle, SortLetter: letter,
			Unapproved: unapproved})
		if !unapproved {
			searchPieces[slug] = domain.PieceSummary{Slug: slug, Title: title, Kind: kind,
				SecondaryKinds: secondary, ConservationStatus: status, IsStub: stub}
		}
	}
	poem := []domain.PieceKind{domain.PieceKindPoem}
	song := []domain.PieceKind{domain.PieceKindSong}
	piece(wildHeronSlug, "The Wild Heron", "Wild Heron", "W", domain.PieceKindSong, nil,
		domain.ConservationStatusEndangered, false, false, swords, warlike)
	piece(ashGroveSlug, "The Ash Grove", "Ash Grove", "A", domain.PieceKindSong, poem,
		domain.ConservationStatusLowRisk, false, false, war, swords)
	piece(ironCrownSlug, "Iron Crown", "", "I", domain.PieceKindPoem, nil,
		domain.ConservationStatusEndangered, true, false, rapiers, mournful)
	piece(saltRoadSlug, saltRoadTitle, "", "S", domain.PieceKindStory, song,
		domain.ConservationStatusCriticallyEndangered, false, false, sea, warlike)
	piece(eireAbuSlug, "Éire Abú", "", "E", domain.PieceKindInstrumental, nil,
		domain.ConservationStatusUnassessed, false, false, sea, century)
	piece(armadaSlug, "1588 Armada", "", "0", domain.PieceKindSong, nil,
		domain.ConservationStatusEndangered, false, false, war, century)
	piece(oiPolloiSlug, "Oi Polloi", "", "O", domain.PieceKindComedy, nil,
		domain.ConservationStatusUnassessed, false, false, mournful)
	piece(masksSlug, "Theatre of Masks", "", "T", domain.PieceKindDanceMusic, nil,
		domain.ConservationStatusRestricted, true, false)
	piece(pikeMarchSlug, "Pike March", "", "P", domain.PieceKindSong, nil,
		domain.ConservationStatusUnassessed, false, false, pikes)
	piece(saltRoad2Slug, saltRoadTitle, "", "S", domain.PieceKindPoem, nil,
		domain.ConservationStatusUnassessed, false, false)
	piece("pending-ballad", "Pending Ballad", "", "P", domain.PieceKindSong, nil,
		domain.ConservationStatusEndangered, false, true, war, warlike)

	seedSearchPersons(c, northmarch, ravenmoor)

	collection := func(name, sortName, letter string, kind domain.CollectionKind, unapproved bool) catalogtest.Ref {
		return c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: name, Kind: kind},
			SortName: sortName, SortLetter: letter, Unapproved: unapproved})
	}
	// Unapproved, so never listed or counted.
	collection("Draft Shelf", "", "D", domain.CollectionKindWiki, true)

	return searchFixture{
		northmarchBook: collection("The Northmarch Songbook", "Northmarch Songbook", "N",
			domain.CollectionKindSongbookPDF, false),
		balladIndex: collection("The Ballad Index", "Ballad Index", "B", domain.CollectionKindIndexOrLinkList, false),
		bardicWiki:  collection(bardicWikiName, "", "B", domain.CollectionKindWiki, false),
		folio:       collection("2nd Folio", "", "0", domain.CollectionKindArchivedSongbook, false),
		bardicBlog:  collection(bardicWikiName, "", "B", domain.CollectionKindBlog, false),
	}
})

// The search fixture's person slugs and names. Its persons:
//
//	nine-ravens  9 Ravens          0  group                                   no roles
//	ashlin-tallow Ashlin Tallow   A  historical       songwriter             northmarch
//	hollis-fenn  Hollis Fenn       H  community_bard   singer, no page        northmarch; public alias Owl of the Moor
//	lantern-band The Lantern Band  L  group            musician
//	wren         Wren of the Vale  W  community_bard   singer, poet           ravenmoor (under northmarch); public
//	                                                                          alias Linnet Wren, hidden Zephyrine Marker
//
// An unapproved singer is affiliated with northmarch.
const (
	// sqlShaped is an input that would change a statement if it reached the SQL text.
	sqlShaped       = "' OR 1=1 --"
	nineRavensSlug  = "nine-ravens"
	ashTallowSlug   = "ashlin-tallow"
	hollisFennSlug  = "hollis-fenn"
	lanternBandSlug = "lantern-band"
	wrenSlug        = "wren"
)

// searchPersons are the summaries of the search fixture's public persons, by slug.
var searchPersons = map[string]domain.PersonSummary{}

func seedSearchPersons(c *catalogtest.Catalog, northmarch, ravenmoor catalogtest.Ref) {
	person := func(slug, sortName, letter string, doc domain.PersonDocument, unapproved bool) {
		c.Person(catalogtest.Person{Doc: doc, Slug: slug, SortName: sortName, SortLetter: letter,
			Unapproved: unapproved})
		if !unapproved {
			searchPersons[slug] = domain.PersonSummary{Slug: slug, Name: doc.Name, Kind: doc.Kind,
				IsStub: domain.PersonIsStub(doc.IsStub, doc.Status)}
		}
	}
	person(wrenSlug, "", "W", domain.PersonDocument{Name: wrenName, Kind: domain.PersonKindCommunityBard,
		Roles:          []domain.PersonRole{domain.PersonRoleSinger, domain.PersonRolePoet},
		AffiliationIDs: []int64{ravenmoor.ID},
		Aliases: []domain.DocPersonAlias{
			{Name: "Linnet Wren", Kind: domain.PersonAliasKindStage},
		}}, false)
	person(ashTallowSlug, "", "A", domain.PersonDocument{Name: "Ashlin Tallow", Kind: domain.PersonKindHistorical,
		Roles: []domain.PersonRole{domain.PersonRoleSongwriter}, AffiliationIDs: []int64{northmarch.ID}}, false)
	person(lanternBandSlug, "Lantern Band", "L", domain.PersonDocument{Name: "The Lantern Band",
		Kind: domain.PersonKindGroup, Roles: []domain.PersonRole{domain.PersonRoleMusician}}, false)
	person(hollisFennSlug, "", "H", domain.PersonDocument{Name: hollisName, Kind: domain.PersonKindCommunityBard,
		Status: domain.PersonStatusPrefersNoPage, Roles: []domain.PersonRole{domain.PersonRoleSinger},
		AffiliationIDs: []int64{northmarch.ID},
		Aliases:        []domain.DocPersonAlias{{Name: "Owl of the Moor", Kind: domain.PersonAliasKindStage}},
	}, false)
	person(nineRavensSlug, "", "0", domain.PersonDocument{Name: "9 Ravens", Kind: domain.PersonKindGroup}, false)
	person("pending-rook", "", "P", domain.PersonDocument{Name: "Pending Rook", Kind: domain.PersonKindCommunityBard,
		Roles: []domain.PersonRole{domain.PersonRoleSinger}, AffiliationIDs: []int64{northmarch.ID}}, true)
}

// pieceSummaries returns the search fixture's summaries of slugs, in order.
func pieceSummaries(slugs ...string) []domain.PieceSummary {
	summaries := []domain.PieceSummary{}
	for _, slug := range slugs {
		summaries = append(summaries, searchPieces[slug])
	}
	return summaries
}

func personSummaries(slugs ...string) []domain.PersonSummary {
	summaries := []domain.PersonSummary{}
	for _, slug := range slugs {
		summaries = append(summaries, searchPersons[slug])
	}
	return summaries
}

func letters(counts ...any) []domain.LetterCount {
	letterCounts := []domain.LetterCount{}
	for i := 0; i+1 < len(counts); i += 2 {
		letterCounts = append(letterCounts, domain.LetterCount{
			Letter: domain.Letter(counts[i].(string)), Count: int64(counts[i+1].(int)),
		})
	}
	return letterCounts
}

func termCounts(counts ...any) []domain.TermCount {
	terms := []domain.TermCount{}
	for i := 0; i+1 < len(counts); i += 2 {
		terms = append(terms, domain.TermCount{Slug: counts[i].(string), Count: int64(counts[i+1].(int))})
	}
	return terms
}

func kindCounts(counts ...any) []domain.PieceKindCount {
	kinds := []domain.PieceKindCount{}
	for i := 0; i+1 < len(counts); i += 2 {
		kinds = append(kinds, domain.PieceKindCount{Kind: counts[i].(domain.PieceKind), Count: int64(counts[i+1].(int))})
	}
	return kinds
}

func statusCounts(counts ...any) []domain.ConservationStatusCount {
	statuses := []domain.ConservationStatusCount{}
	for i := 0; i+1 < len(counts); i += 2 {
		statuses = append(statuses, domain.ConservationStatusCount{
			ConservationStatus: counts[i].(domain.ConservationStatus), Count: int64(counts[i+1].(int)),
		})
	}
	return statuses
}

// everyPieceFacets are the facet counts of the search fixture's every public piece.
var everyPieceFacets = domain.PieceFacets{
	// Pike March is tagged only with a term under an unapproved one, so it counts under no term; the Ash Grove,
	// tagged with war and its child swords, counts once under war.
	Terms: termCounts(centurySlug, 2, mournfulSlug, 2, rapiersSlug, 1, seaSlug, 2, swordsSlug, 3, warSlug, 4,
		warlikeSlug, 2),
	Kinds: kindCounts(domain.PieceKindSong, 5, domain.PieceKindPoem, 3, domain.PieceKindStory, 1,
		domain.PieceKindDanceMusic, 1, domain.PieceKindInstrumental, 1, domain.PieceKindComedy, 1),
	ConservationStatuses: statusCounts(domain.ConservationStatusUnassessed, 4, domain.ConservationStatusLowRisk, 1,
		domain.ConservationStatusEndangered, 3, domain.ConservationStatusCriticallyEndangered, 1,
		domain.ConservationStatusRestricted, 1),
	Letters: letters("0", 1, "A", 1, "E", 1, "I", 1, "O", 1, "P", 1, "S", 2, "T", 1, "W", 1),
}

// byTitle are the search fixture's public pieces by sort title, then ID.
var byTitle = []string{armadaSlug, ashGroveSlug, eireAbuSlug, ironCrownSlug, oiPolloiSlug, pikeMarchSlug,
	saltRoadSlug, saltRoad2Slug, masksSlug, wildHeronSlug}

func TestRepository_ListPublicPieces(t *testing.T) {
	t.Parallel()

	db, _ := searches.Get(t)
	repo := mysql.New(db)
	page := domain.Page{Limit: domain.DefaultPageLimit}
	stub, notStub := true, false
	letterZero, letterW := domain.LetterOther, domain.Letter("W")
	salt := mustSearch(t, "salt")

	cases := []struct {
		name  string
		query datasources.PieceQuery
		want  domain.PieceList
	}{
		{
			name:  "every_piece",
			query: datasources.PieceQuery{},
			want:  domain.PieceList{Pieces: pieceSummaries(byTitle...), Total: 10, Facets: everyPieceFacets},
		},
		{
			// (war or sea), (warlike or mournful) and (a song or a story): the salt road and the wild heron. The
			// iron crown is a mournful war poem, so only its kind keeps it out.
			name: "or_within_and_across_facets",
			query: datasources.PieceQuery{TermSlugs: []string{warSlug, seaSlug, warlikeSlug, mournfulSlug},
				Kinds: []domain.PieceKind{domain.PieceKindSong, domain.PieceKindStory}},
			want: domain.PieceList{
				Pieces: pieceSummaries(saltRoadSlug, wildHeronSlug),
				Total:  2,
				Facets: domain.PieceFacets{
					// Topics and moods are counted among the pieces the other facets let through.
					Terms: termCounts(seaSlug, 1, swordsSlug, 1, warSlug, 1, warlikeSlug, 2),
					// Kinds ignore the kind selection, so the iron crown counts as a poem.
					Kinds: kindCounts(domain.PieceKindSong, 2, domain.PieceKindPoem, 1, domain.PieceKindStory, 1),
					ConservationStatuses: statusCounts(domain.ConservationStatusEndangered, 1,
						domain.ConservationStatusCriticallyEndangered, 1),
					Letters: letters("S", 1, "W", 1),
				},
			},
		},
		{
			// (sea or rapiers), warlike and (a song or a poem): only the salt road. The wild heron fails only the
			// topic (swords are not rapiers) and the iron crown only the mood, so each is counted in its own facet.
			name: "facet_counts_ignore_own_selection",
			query: datasources.PieceQuery{TermSlugs: []string{seaSlug, rapiersSlug, warlikeSlug},
				Kinds: []domain.PieceKind{domain.PieceKindSong, domain.PieceKindPoem}},
			want: domain.PieceList{
				Pieces: pieceSummaries(saltRoadSlug),
				Total:  1,
				Facets: domain.PieceFacets{
					Terms:                termCounts(mournfulSlug, 1, seaSlug, 1, swordsSlug, 1, warSlug, 1, warlikeSlug, 1),
					Kinds:                kindCounts(domain.PieceKindSong, 1, domain.PieceKindStory, 1),
					ConservationStatuses: statusCounts(domain.ConservationStatusCriticallyEndangered, 1),
					Letters:              letters("S", 1),
				},
			},
		},
		{
			// the grandchild rapiers matches war, the child swords matches war.
			name:  "closure_grandchild",
			query: datasources.PieceQuery{TermSlugs: []string{warSlug}, Letter: &letterW},
			want: domain.PieceList{
				Pieces: pieceSummaries(wildHeronSlug),
				Total:  1,
				Facets: domain.PieceFacets{
					Terms:                termCounts(swordsSlug, 1, warSlug, 1, warlikeSlug, 1),
					Kinds:                kindCounts(domain.PieceKindSong, 1),
					ConservationStatuses: statusCounts(domain.ConservationStatusEndangered, 1),
					// Letters ignore the letter: armada, ash grove, iron crown and wild heron are war.
					Letters: letters("0", 1, "A", 1, "I", 1, "W", 1),
				},
			},
		},
		{
			name:  "closure_child",
			query: datasources.PieceQuery{TermSlugs: []string{swordsSlug}},
			want: domain.PieceList{
				Pieces: pieceSummaries(ashGroveSlug, ironCrownSlug, wildHeronSlug),
				Total:  3,
				Facets: domain.PieceFacets{
					// the topic counts ignore the topic selection, so they are every piece's.
					Terms: termCounts(mournfulSlug, 1, rapiersSlug, 1, seaSlug, 2, swordsSlug, 3, warSlug, 4,
						warlikeSlug, 1),
					Kinds: kindCounts(domain.PieceKindSong, 2, domain.PieceKindPoem, 2),
					ConservationStatuses: statusCounts(domain.ConservationStatusLowRisk, 1,
						domain.ConservationStatusEndangered, 2),
					Letters: letters("A", 1, "I", 1, "W", 1),
				},
			},
		},
		{
			// a poem as its primary or a secondary kind.
			name:  "kind_primary_or_secondary",
			query: datasources.PieceQuery{Kinds: []domain.PieceKind{domain.PieceKindPoem}},
			want: domain.PieceList{
				Pieces: pieceSummaries(ashGroveSlug, ironCrownSlug, saltRoad2Slug),
				Total:  3,
				Facets: domain.PieceFacets{
					Terms: termCounts(mournfulSlug, 1, rapiersSlug, 1, swordsSlug, 2, warSlug, 2),
					Kinds: everyPieceFacets.Kinds,
					ConservationStatuses: statusCounts(domain.ConservationStatusUnassessed, 1,
						domain.ConservationStatusLowRisk, 1, domain.ConservationStatusEndangered, 1),
					Letters: letters("A", 1, "I", 1, "S", 1),
				},
			},
		},
		{
			name: "kinds_or",
			query: datasources.PieceQuery{Kinds: []domain.PieceKind{domain.PieceKindStory, domain.PieceKindComedy},
				Letter: &letterZero},
			want: domain.PieceList{
				Pieces: []domain.PieceSummary{},
				Total:  0,
				Facets: domain.PieceFacets{
					Terms: termCounts(), ConservationStatuses: statusCounts(),
					// armada is the only piece under 0, a song.
					Kinds:   kindCounts(domain.PieceKindSong, 1),
					Letters: letters("O", 1, "S", 1),
				},
			},
		},
		{
			// conservation status and stub.
			name: "status_and_stub",
			query: datasources.PieceQuery{Stub: &stub,
				ConservationStatuses: []domain.ConservationStatus{domain.ConservationStatusEndangered}},
			want: domain.PieceList{
				Pieces: pieceSummaries(ironCrownSlug),
				Total:  1,
				Facets: domain.PieceFacets{
					Terms: termCounts(mournfulSlug, 1, rapiersSlug, 1, swordsSlug, 1, warSlug, 1),
					Kinds: kindCounts(domain.PieceKindPoem, 1),
					// Statuses ignore the status selection, not the stub filter.
					ConservationStatuses: statusCounts(domain.ConservationStatusEndangered, 1,
						domain.ConservationStatusRestricted, 1),
					Letters: letters("I", 1),
				},
			},
		},
		{
			name: "statuses_or_not_stub",
			query: datasources.PieceQuery{Stub: &notStub, ConservationStatuses: []domain.ConservationStatus{
				domain.ConservationStatusLowRisk, domain.ConservationStatusCriticallyEndangered,
			}},
			want: domain.PieceList{
				Pieces: pieceSummaries(ashGroveSlug, saltRoadSlug),
				Total:  2,
				Facets: domain.PieceFacets{
					Terms: termCounts(seaSlug, 1, swordsSlug, 1, warSlug, 1, warlikeSlug, 1),
					Kinds: kindCounts(domain.PieceKindSong, 2, domain.PieceKindPoem, 1, domain.PieceKindStory, 1),
					ConservationStatuses: statusCounts(domain.ConservationStatusUnassessed, 4,
						domain.ConservationStatusLowRisk, 1, domain.ConservationStatusEndangered, 2,
						domain.ConservationStatusCriticallyEndangered, 1),
					Letters: letters("A", 1, "S", 1),
				},
			},
		},
		{
			// equal scores and equal sort titles fall back to the ID.
			name:  "relevance_ties_by_id",
			query: datasources.PieceQuery{Search: &salt, Sort: domain.PieceSortRelevance},
			want: domain.PieceList{
				Pieces: pieceSummaries(saltRoadSlug, saltRoad2Slug),
				Total:  2,
				Facets: domain.PieceFacets{
					Terms: termCounts(seaSlug, 1, warlikeSlug, 1),
					Kinds: kindCounts(domain.PieceKindSong, 1, domain.PieceKindPoem, 1, domain.PieceKindStory, 1),
					ConservationStatuses: statusCounts(domain.ConservationStatusUnassessed, 1,
						domain.ConservationStatusCriticallyEndangered, 1),
					Letters: letters("S", 2),
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			query := tc.query
			if query.Sort == "" {
				query.Sort = domain.PieceSortTitle
			}
			query.Page = page
			got, err := repo.ListPublicPieces(t.Context(), query)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func mustSearch(t *testing.T, q string) domain.SearchQuery {
	t.Helper()

	search, err := domain.ParseSearchQuery(q)
	require.NoError(t, err)
	return search
}

func TestRepository_ListPublicPieces_Pages(t *testing.T) {
	t.Parallel()

	db, _ := searches.Get(t)
	repo := mysql.New(db)

	// pages of three concatenate to the whole list, without duplicates, each with the full total.
	var slugs []string
	for offset := int32(0); offset < 12; offset += 3 {
		got, err := repo.ListPublicPieces(t.Context(), datasources.PieceQuery{Sort: domain.PieceSortTitle,
			Page: domain.Page{Limit: 3, Offset: offset}})
		require.NoError(t, err)
		assert.Equal(t, int64(10), got.Total)
		assert.Equal(t, everyPieceFacets, got.Facets)
		for _, piece := range got.Pieces {
			slugs = append(slugs, piece.Slug)
		}
	}
	assert.Equal(t, byTitle, slugs)

	// Past the end: no pieces, the same counts.
	got, err := repo.ListPublicPieces(t.Context(), datasources.PieceQuery{Sort: domain.PieceSortTitle,
		Page: domain.Page{Limit: 100, Offset: 10000}})
	require.NoError(t, err)
	assert.Equal(t, domain.PieceList{Pieces: []domain.PieceSummary{}, Total: 10, Facets: everyPieceFacets}, got)
}

func TestRepository_ListPublicPieces_UnknownTerm(t *testing.T) {
	t.Parallel()

	db, _ := searches.Get(t)
	repo := mysql.New(db)

	// unknown, unapproved, under an unapproved parent and affiliation terms are all unknown, and
	// injection-shaped slugs are just unknown.
	for _, slug := range []string{"nope", "spears", "pikes", northmarchSlug, "war'; DROP TABLE pieces; --", "%"} {
		t.Run(slug, func(t *testing.T) {
			t.Parallel()

			_, err := repo.ListPublicPieces(t.Context(), datasources.PieceQuery{
				TermSlugs: []string{warSlug, slug}, Sort: domain.PieceSortTitle, Page: domain.Page{Limit: 1},
			})
			var unknown *datasources.UnknownTermError
			require.ErrorAs(t, err, &unknown)
			assert.Equal(t, slug, unknown.Slug)
		})
	}
}

func TestRepository_ListPublicPieces_InjectionShapedSearches(t *testing.T) {
	t.Parallel()

	db, _ := searches.Get(t)
	repo := mysql.New(db)

	// they run normally and match nothing. "S_lt" and "S%" would match Salt Road if the prefix fallback did
	// not escape LIKE wildcards.
	for _, q := range []string{
		sqlShaped, `"; DROP TABLE pieces; --`, "*/ SELECT 1 /*", `+(>salt <road) ~"wild" -heron @3`, "%", "_",
		"!", `\`, "?", "%alt", "S_lt", "S%", "\x00",
	} {
		t.Run(q, func(t *testing.T) {
			t.Parallel()

			search := mustSearch(t, q)
			got, err := repo.ListPublicPieces(t.Context(), datasources.PieceQuery{Search: &search,
				Sort: domain.PieceSortRelevance, Page: domain.Page{Limit: 10}})
			require.NoError(t, err)
			assert.Empty(t, got.Pieces)
			assert.Zero(t, got.Total)
		})
	}
}

func TestRepository_ListPublicPersons(t *testing.T) {
	t.Parallel()

	db, _ := searches.Get(t)
	repo := mysql.New(db)
	page := domain.Page{Limit: domain.DefaultPageLimit}
	northmarch, ravenmoor := northmarchSlug, ravenmoorSlug
	letterW := domain.Letter("W")
	everyLetter := letters("0", 1, "A", 1, "H", 1, "L", 1, "W", 1)
	none := domain.PersonList{Persons: []domain.PersonSummary{}, Letters: letters()}

	cases := []struct {
		name  string
		query datasources.PersonQuery
		q     string
		want  domain.PersonList
	}{
		{
			// The no-page person is listed, as a stub.
			name: "every_person",
			want: domain.PersonList{
				Persons: personSummaries(nineRavensSlug, ashTallowSlug, hollisFennSlug, lanternBandSlug, wrenSlug),
				Total:   5, Letters: everyLetter,
			},
		},
		{
			name:  "kinds",
			query: datasources.PersonQuery{Kinds: []domain.PersonKind{domain.PersonKindGroup, domain.PersonKindHistorical}},
			want: domain.PersonList{Persons: personSummaries(nineRavensSlug, ashTallowSlug, lanternBandSlug), Total: 3,
				Letters: letters("0", 1, "A", 1, "L", 1)},
		},
		{
			// Not the no-page or the unapproved singer.
			name:  "role",
			query: datasources.PersonQuery{Roles: []domain.PersonRole{domain.PersonRoleSinger}},
			want:  domain.PersonList{Persons: personSummaries(wrenSlug), Total: 1, Letters: letters("W", 1)},
		},
		{
			name: "roles_or",
			query: datasources.PersonQuery{Roles: []domain.PersonRole{domain.PersonRoleSinger,
				domain.PersonRoleSongwriter}},
			want: domain.PersonList{Persons: personSummaries(ashTallowSlug, wrenSlug), Total: 2,
				Letters: letters("A", 1, "W", 1)},
		},
		{
			// Wren through ravenmoor, under northmarch; not the no-page or the unapproved person.
			name:  "affiliation_with_descendants",
			query: datasources.PersonQuery{AffiliationSlug: &northmarch},
			want: domain.PersonList{Persons: personSummaries(ashTallowSlug, wrenSlug), Total: 2,
				Letters: letters("A", 1, "W", 1)},
		},
		{
			name:  "affiliation",
			query: datasources.PersonQuery{AffiliationSlug: &ravenmoor},
			want:  domain.PersonList{Persons: personSummaries(wrenSlug), Total: 1, Letters: letters("W", 1)},
		},
		{
			name:  "letter",
			query: datasources.PersonQuery{Letter: &letterW},
			want:  domain.PersonList{Persons: personSummaries(wrenSlug), Total: 1, Letters: everyLetter},
		},
		{name: "name", q: "wren", want: domain.PersonList{Persons: personSummaries(wrenSlug), Total: 1,
			Letters: letters("W", 1)}},
		{name: "alias", q: "linnet", want: domain.PersonList{Persons: personSummaries(wrenSlug), Total: 1,
			Letters: letters("W", 1)}},
		{name: "alias_prefix", q: "Li", want: domain.PersonList{Persons: personSummaries(wrenSlug), Total: 1,
			Letters: letters("W", 1)}},
		// A no-page person matches by name, and not by a public alias.
		{name: "no_page_name", q: "hollis", want: domain.PersonList{Persons: personSummaries(hollisFennSlug),
			Total: 1, Letters: letters("H", 1)}},
		{name: "no_page_alias", q: "owl", want: none},
		{name: "no_page_alias_prefix", q: "Ow", want: none},
		// Inputs shaped like LIKE wildcards, a FULLTEXT word with an underscore and SQL: matched literally.
		{name: "wildcard_percent", q: "%", want: none},
		{name: "wildcard_underscore", q: "_", want: none},
		{name: "wildcard_inside_word", q: "W_en", want: none},
		{name: "wildcard_prefix", q: "W%", want: none},
		{name: "sql_shaped", q: sqlShaped, want: none},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			query := tc.query
			query.Sort = domain.PersonSortName
			if tc.q != "" {
				search := mustSearch(t, tc.q)
				query.Search = &search
				query.Sort = domain.PersonSortRelevance
			}
			query.Page = page
			got, err := repo.ListPublicPersons(t.Context(), query)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRepository_ListPublicPersons_Pages(t *testing.T) {
	t.Parallel()

	db, _ := searches.Get(t)
	repo := mysql.New(db)

	var slugs []string
	for offset := int32(0); offset < 6; offset += 2 {
		got, err := repo.ListPublicPersons(t.Context(), datasources.PersonQuery{Sort: domain.PersonSortName,
			Page: domain.Page{Limit: 2, Offset: offset}})
		require.NoError(t, err)
		assert.Equal(t, int64(5), got.Total)
		for _, person := range got.Persons {
			slugs = append(slugs, person.Slug)
		}
	}
	assert.Equal(t, []string{nineRavensSlug, ashTallowSlug, hollisFennSlug, lanternBandSlug, wrenSlug}, slugs)
}

func TestRepository_ListPublicPersons_UnknownAffiliation(t *testing.T) {
	t.Parallel()

	db, _ := searches.Get(t)
	repo := mysql.New(db)

	// Unapproved, unknown, piece and SQL-shaped terms are all unknown affiliations.
	for _, slug := range []string{"southmarch", "nope", warSlug, sqlShaped, "%", `"; DROP TABLE persons; --`} {
		t.Run(slug, func(t *testing.T) {
			t.Parallel()

			_, err := repo.ListPublicPersons(t.Context(), datasources.PersonQuery{AffiliationSlug: &slug,
				Sort: domain.PersonSortName, Page: domain.Page{Limit: 1}})
			var unknown *datasources.UnknownTermError
			require.ErrorAs(t, err, &unknown)
			assert.Equal(t, slug, unknown.Slug)
		})
	}
}

func TestRepository_ListPublicCollections(t *testing.T) {
	t.Parallel()

	db, fx := searches.Get(t)
	repo := mysql.New(db)
	letterB := domain.Letter("B")
	everyLetter := letters("0", 1, "B", 3, "N", 1)

	summary := func(ref catalogtest.Ref, name string, kind domain.CollectionKind) domain.CollectionSummary {
		return domain.CollectionSummary{ID: ref.ID, Name: name, Kind: kind}
	}
	folio := summary(fx.folio, "2nd Folio", domain.CollectionKindArchivedSongbook)
	ballads := summary(fx.balladIndex, "The Ballad Index", domain.CollectionKindIndexOrLinkList)
	wiki := summary(fx.bardicWiki, bardicWikiName, domain.CollectionKindWiki)
	blog := summary(fx.bardicBlog, bardicWikiName, domain.CollectionKindBlog)
	book := summary(fx.northmarchBook, "The Northmarch Songbook", domain.CollectionKindSongbookPDF)

	cases := []struct {
		name  string
		query datasources.CollectionQuery
		want  domain.CollectionList
	}{
		{
			// by sort name, equal sort names by ID; the unapproved collection is not listed.
			name:  "every_collection",
			query: datasources.CollectionQuery{Page: domain.Page{Limit: 50}},
			want: domain.CollectionList{Collections: []domain.CollectionSummary{folio, ballads, wiki, blog, book},
				Total: 5, Letters: everyLetter},
		},
		{
			name: "kinds",
			query: datasources.CollectionQuery{Kinds: []domain.CollectionKind{domain.CollectionKindWiki,
				domain.CollectionKindSongbookPDF}, Page: domain.Page{Limit: 50}},
			want: domain.CollectionList{Collections: []domain.CollectionSummary{wiki, book}, Total: 2,
				Letters: letters("B", 1, "N", 1)},
		},
		{
			name:  "letter_page",
			query: datasources.CollectionQuery{Letter: &letterB, Page: domain.Page{Limit: 2, Offset: 1}},
			want: domain.CollectionList{Collections: []domain.CollectionSummary{wiki, blog}, Total: 3,
				Letters: everyLetter},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := repo.ListPublicCollections(t.Context(), tc.query)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestSearchStopwords_MatchInnoDB checks that the tokeniser drops every word of the server's default
// FULLTEXT stopword list, which indexes leave out, and keeps words that are not on it.
func TestSearchStopwords_MatchInnoDB(t *testing.T) {
	t.Parallel()

	db, _ := searches.Get(t)
	rows, err := db.QueryContext(t.Context(), "SELECT value FROM INFORMATION_SCHEMA.INNODB_FT_DEFAULT_STOPWORD")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var server []string
	for rows.Next() {
		var word string
		require.NoError(t, rows.Scan(&word))
		server = append(server, strings.ToLower(word))
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, server)
	for _, word := range server {
		q, err := domain.ParseSearchQuery(word + " lantern")
		require.NoError(t, err)
		assert.Equal(t, []string{"lantern"}, q.Tokens, "stopword [%s]", word)
	}
	// Words that are not stopwords are kept, the shortest included.
	for _, word := range []string{"abc", "theme", "about_us", "wilder"} {
		q, err := domain.ParseSearchQuery(word)
		require.NoError(t, err)
		assert.Equal(t, []string{word}, q.Tokens, "word [%s]", word)
	}
}
