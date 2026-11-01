package controller

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const piecesPath = "/v1/pieces"

// defaultPieceQuery is what a request with no query parameters searches for.
var defaultPieceQuery = datasources.PieceQuery{Sort: domain.PieceSortTitle, Page: domain.Page{Limit: domain.DefaultPageLimit}}

// errTimeout is a search the database stopped for running too long.
var errTimeout = fmt.Errorf("listing: %w", datasources.ErrQueryTimeout)

func TestPieceListGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	stub := false
	letterW := domain.Letter("W")
	wild := domain.SearchQuery{Text: "the wild", Tokens: []string{"wild"}}
	theOnly := domain.SearchQuery{Text: "the"}
	twenty := make([]string, 0, datasources.MaxPieceQueryTerms)
	for i := range datasources.MaxPieceQueryTerms {
		twenty = append(twenty, fmt.Sprintf("term-%d", i))
	}

	cases := []struct {
		name       string
		query      string
		want       datasources.PieceQuery
		list       domain.PieceList
		listErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name: "pieces_and_facets",
			want: defaultPieceQuery,
			list: domain.PieceList{
				Pieces: []domain.PieceSummary{{Slug: "the-wild-rover", Title: "The Wild Rover", Kind: domain.PieceKindSong,
					SecondaryKinds: []domain.PieceKind{domain.PieceKindPoem}, ConservationStatus: domain.ConservationStatusEndangered}},
				Total: 61,
				Facets: domain.PieceFacets{
					Terms: []domain.TermCount{{Slug: testTermSlug, Count: 4}},
					Kinds: []domain.PieceKindCount{{Kind: domain.PieceKindSong, Count: 5}, {Kind: domain.PieceKindPoem, Count: 2}},
					ConservationStatuses: []domain.ConservationStatusCount{
						{ConservationStatus: domain.ConservationStatusEndangered, Count: 3},
					},
					Letters: []domain.LetterCount{{Letter: domain.LetterOther, Count: 1}, {Letter: "W", Count: 60}},
				},
			},
			wantStatus: http.StatusOK,
			wantBody: `{"data": [{"slug": "the-wild-rover", "title": "The Wild Rover", "kind": "song",
					"secondary_kinds": ["poem"], "conservation_status": "endangered", "is_stub": false}],
				"total": 61,
				"facets": {
					"terms": [{"slug": "war", "count": 4}],
					"kinds": [{"kind": "song", "count": 5}, {"kind": "poem", "count": 2}],
					"conservation_statuses": [{"conservation_status": "endangered", "count": 3}],
					"letters": [{"letter": "0", "count": 1}, {"letter": "W", "count": 60}]
				}}`,
		},
		{
			name:       emptyListsCase,
			want:       defaultPieceQuery,
			wantStatus: http.StatusOK,
			wantBody: `{"data": [], "total": 0,
				"facets": {"terms": [], "kinds": [], "conservation_statuses": [], "letters": []}}`,
		},
		{
			// Repeated terms count once, in the order first given; a search defaults to relevance.
			name: everyParameterCase,
			query: "?term=war&term=warlike&term=war&kind=song&kind=poem&conservation_status=endangered" +
				"&stub=false&q=the+wild&letter=W&limit=100&offset=10000",
			want: datasources.PieceQuery{
				TermSlugs:            []string{testTermSlug, "warlike"},
				Kinds:                []domain.PieceKind{domain.PieceKindSong, domain.PieceKindPoem},
				ConservationStatuses: []domain.ConservationStatus{domain.ConservationStatusEndangered},
				Stub:                 &stub, Search: &wild, Letter: &letterW,
				Sort: domain.PieceSortRelevance, Page: widestPage,
			},
			wantStatus: http.StatusOK,
			wantBody:   emptyPieceListBody,
		},
		{
			name:       "title_order_with_search",
			query:      "?q=the&sort=title&letter=0",
			want:       datasources.PieceQuery{Search: &theOnly, Letter: new(domain.LetterOther), Sort: domain.PieceSortTitle, Page: defaultPieceQuery.Page},
			wantStatus: http.StatusOK,
			wantBody:   emptyPieceListBody,
		},
		{
			name:       "twenty_terms",
			query:      "?term=" + strings.Join(twenty, "&term="),
			want:       datasources.PieceQuery{TermSlugs: twenty, Sort: domain.PieceSortTitle, Page: defaultPieceQuery.Page},
			wantStatus: http.StatusOK,
			wantBody:   emptyPieceListBody,
		},
		{
			name:       "longest_search",
			query:      "?q=" + strings.Repeat("%C3%A9", domain.MaxSearchLength),
			want:       datasources.PieceQuery{Search: new(mustParseSearch(t, strings.Repeat("é", domain.MaxSearchLength))), Sort: domain.PieceSortRelevance, Page: defaultPieceQuery.Page},
			wantStatus: http.StatusOK,
			wantBody:   emptyPieceListBody,
		},
		{
			// Unknown and unapproved terms give the same body, echoing only the slug as given.
			name:       "unknown_term",
			query:      "?term=war&term=spears",
			want:       datasources.PieceQuery{TermSlugs: []string{testTermSlug, "spears"}, Sort: domain.PieceSortTitle, Page: defaultPieceQuery.Page},
			listErr:    fmt.Errorf("listing: %w", &datasources.UnknownTermError{Slug: "spears"}),
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code": "invalid_request", "message": "unknown term: spears"}`,
		},
		{
			name:       listFailureCase,
			want:       defaultPieceQuery,
			listErr:    errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
		{
			name:       "timeout",
			want:       defaultPieceQuery,
			listErr:    errTimeout,
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lister := mocks.NewPublicPieceLister(t)
			lister.EXPECT().ListPublicPieces(mock.Anything, tc.want).Return(tc.list, tc.listErr)

			rec := serveGet(t, PieceListGet{Lister: lister}, piecesPath+tc.query, nil, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	twentyOne := "term=" + strings.Join(append(twenty, "one-more"), "&term=")
	invalidTermBody := `{"code":"invalid_request","message":"invalid term: must be lowercase ASCII letters and ` +
		`digits, in runs joined by single hyphens, at most 191 characters"}`
	invalid := append([]invalidQueryCase{
		{name: "too_many_terms", query: twentyOne,
			wantBody: `{"code":"invalid_request","message":"query parameter term must be given at most 20 times"}`},
		{name: "term_not_slug", query: "term=War", wantBody: invalidTermBody},
		{name: "term_injection", query: "term=war%27%3B+DROP+TABLE+pieces", wantBody: invalidTermBody},
		{name: "term_empty", query: "term=", wantBody: invalidTermBody},
		{name: unknownKindCase, query: "kind=opera", wantBody: invalidKindBody},
		{name: "empty_kind", query: "kind=", wantBody: invalidKindBody},
		{name: duplicateKindsCase, query: "kind=song&kind=poem&kind=song",
			wantBody: duplicateKindBody},
		{name: "unknown_status", query: "conservation_status=extinct",
			wantBody: `{"code":"invalid_request","message":"invalid conservation_status: not one of its allowed values"}`},
		{name: "duplicate_statuses", query: "conservation_status=restricted&conservation_status=restricted",
			wantBody: `{"code":"invalid_request","message":"query parameter conservation_status must not repeat a value"}`},
		{name: "stub_not_boolean", query: "stub=yes",
			wantBody: `{"code":"invalid_request","message":"invalid stub: must be true or false"}`},
		{name: "stub_repeated", query: "stub=true&stub=true",
			wantBody: `{"code":"invalid_request","message":"query parameter stub must be given at most once"}`},
		{name: "relevance_without_search", query: "sort=relevance",
			wantBody: `{"code":"invalid_request","message":"invalid sort: relevance needs a search query q"}`},
		{name: "unknown_sort", query: "sort=name&q=wild",
			wantBody: `{"code":"invalid_request","message":"invalid sort: not one of its allowed values"}`},
	}, append(invalidSearchCases, invalidPageCases...)...)
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveGet(t, PieceListGet{Lister: mocks.NewPublicPieceLister(t)}, piecesPath+"?"+tc.query, nil, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}

const emptyPieceListBody = `{"data": [], "total": 0,
	"facets": {"terms": [], "kinds": [], "conservation_statuses": [], "letters": []}}`

// invalidSearchCases are the invalid q and letter parameters every search controller rejects.
var invalidSearchCases = []invalidQueryCase{
	{name: "search_too_long", query: "q=" + strings.Repeat("a", domain.MaxSearchLength+1),
		wantBody: `{"code":"invalid_request","message":"invalid q: must be at most 200 characters"}`},
	{name: "search_too_long_code_points", query: "q=" + strings.Repeat("%C3%A9", domain.MaxSearchLength+1),
		wantBody: `{"code":"invalid_request","message":"invalid q: must be at most 200 characters"}`},
	{name: "search_empty", query: "q=", wantBody: blankSearchBody},
	{name: "search_blank", query: "q=+%09+", wantBody: blankSearchBody},
	{name: "search_invalid_utf8", query: "q=wild%FF",
		wantBody: `{"code":"invalid_request","message":"invalid q: must be valid UTF-8"}`},
	{name: "search_repeated", query: "q=wild&q=rover",
		wantBody: `{"code":"invalid_request","message":"query parameter q must be given at most once"}`},
	{name: "letter_lower_case", query: "letter=w", wantBody: invalidLetterBody},
	{name: "letter_two", query: "letter=AB", wantBody: invalidLetterBody},
	{name: "letter_digit", query: "letter=1", wantBody: invalidLetterBody},
}

const (
	blankSearchBody   = `{"code":"invalid_request","message":"invalid q: must contain a character other than spaces"}`
	invalidLetterBody = `{"code":"invalid_request","message":"invalid letter: not one of its allowed values"}`
)

func mustParseSearch(t *testing.T, q string) domain.SearchQuery {
	t.Helper()

	search, err := domain.ParseSearchQuery(q)
	require.NoError(t, err)
	return search
}

// TestSearchControllers_TimeoutLoggedAsWarning checks that a search the database stopped for running too long is
// answered with 500 and logged as a warning, and any other failure as an error.
func TestSearchControllers_TimeoutLoggedAsWarning(t *testing.T) {
	t.Parallel()

	handlers := map[string]func(t *testing.T, err error) (http.Handler, string){
		"pieces": func(t *testing.T, err error) (http.Handler, string) {
			lister := mocks.NewPublicPieceLister(t)
			lister.EXPECT().ListPublicPieces(mock.Anything, mock.Anything).Return(domain.PieceList{}, err)
			return PieceListGet{Lister: lister}, piecesPath
		},
		"persons": func(t *testing.T, err error) (http.Handler, string) {
			lister := mocks.NewPublicPersonLister(t)
			lister.EXPECT().ListPublicPersons(mock.Anything, mock.Anything).Return(domain.PersonList{}, err)
			return PersonListGet{Lister: lister}, personsPath
		},
		"collections": func(t *testing.T, err error) (http.Handler, string) {
			lister := mocks.NewPublicCollectionLister(t)
			lister.EXPECT().ListPublicCollections(mock.Anything, mock.Anything).Return(domain.CollectionList{}, err)
			return CollectionListGet{Lister: lister}, collectionsPath
		},
	}
	for name, handler := range handlers {
		for _, tc := range []struct {
			err       error
			wantLevel string
		}{{errTimeout, "WARN"}, {errors.New("connection refused"), "ERROR"}} {
			t.Run(name+"_"+tc.wantLevel, func(t *testing.T) {
				t.Parallel()

				var logs bytes.Buffer
				h, path := handler(t, tc.err)
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
				req = req.WithContext(domain.ContextWithLogger(req.Context(), slog.New(slog.NewTextHandler(&logs, nil))))

				rec := openapitest.New(t).Serve(t, h, req)

				assert.Equal(t, http.StatusInternalServerError, rec.Code)
				assert.JSONEq(t, internalErrorBody, rec.Body.String())
				assert.Contains(t, logs.String(), "level="+tc.wantLevel)
			})
		}
	}
}
