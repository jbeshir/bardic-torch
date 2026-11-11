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
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
)

const (
	piecesPath      = "/v1/pieces"
	personsPath     = "/v1/persons"
	collectionsPath = "/v1/collections"
	needsWorkPath   = "/v1/needs-work"
)

// errTimeout is a search the database stopped for running too long.
var errTimeout = fmt.Errorf("listing: %w", datasources.ErrQueryTimeout)

// TestListControllers_RejectInvalidQueries checks the rules the controllers apply to query parameters before they
// read the datasource, which the spec forbids sending: one case for each. The fake finds nothing, so a request
// that got past the rule would be answered 200.
func TestListControllers_RejectInvalidQueries(t *testing.T) {
	t.Parallel()

	rule := func(message string) string {
		return `{"code":"invalid_request","message":"` + message + `"}`
	}
	invalidSlug := rule("invalid %s: must be lowercase ASCII letters and digits, in runs joined by single hyphens, " +
		"at most 191 characters")
	invalidLetter := rule("invalid letter: not one of its allowed values")
	invalidKind := rule("invalid kind: not one of its allowed values")
	invalidLimit := rule("invalid limit: must be an integer from 1 to 100")
	relevanceNeedsSearch := rule("invalid sort: relevance needs a search query q")
	once := func(name string) string { return rule("query parameter " + name + " must be given at most once") }

	cases := []struct {
		name     string
		path     string
		query    string
		wantBody string
	}{
		{"term_not_slug", piecesPath, "term=War", fmt.Sprintf(invalidSlug, "term")},
		{"too_many_terms", piecesPath, "term=" + twentyOneTerms(),
			rule("query parameter term must be given at most 20 times")},
		{"unknown_kind", piecesPath, "kind=opera", invalidKind},
		{"duplicate_kinds", piecesPath, "kind=song&kind=poem&kind=song",
			rule("query parameter kind must not repeat a value")},
		{"unknown_status", piecesPath, "conservation_status=extinct",
			rule("invalid conservation_status: not one of its allowed values")},
		{"stub_not_boolean", piecesPath, "stub=yes", rule("invalid stub: must be true or false")},
		{"relevance_without_search", piecesPath, "sort=relevance", relevanceNeedsSearch},
		{"unknown_sort", piecesPath, "sort=name&q=wild", rule("invalid sort: not one of its allowed values")},
		{"search_too_long", piecesPath, "q=" + strings.Repeat("%C3%A9", domain.MaxSearchLength+1),
			rule("invalid q: must be at most 200 characters")},
		{"search_blank", piecesPath, "q=+%09+",
			rule("invalid q: must contain a character other than spaces")},
		{"search_repeated", piecesPath, "q=wild&q=heron", once("q")},
		{"letter_lower_case", piecesPath, "letter=w", invalidLetter},
		{"limit_zero", piecesPath, "limit=0", invalidLimit},
		{"limit_too_large", piecesPath, "limit=101", invalidLimit},
		{"limit_repeated", piecesPath, "limit=5&limit=6", once("limit")},
		{"offset_negative", piecesPath, "offset=-1", rule("invalid offset: must be an integer from 0 to 10000")},
		{"offset_too_large", piecesPath, "offset=10001", rule("invalid offset: must be an integer from 0 to 10000")},
		{"bad_encoding", piecesPath, "offset=%zz", rule("invalid encoding of query string")},
		{"unknown_role", personsPath, "role=juggler", rule("invalid role: not one of its allowed values")},
		{"affiliation_not_slug", personsPath, "affiliation=North+March", fmt.Sprintf(invalidSlug, "affiliation")},
		{"affiliation_repeated", personsPath, "affiliation=a&affiliation=b", once("affiliation")},
		{"person_relevance_without_search", personsPath, "sort=relevance", relevanceNeedsSearch},
		{"collection_unknown_kind", collectionsPath, "kind=library", invalidKind},
		{"collection_letter_repeated", collectionsPath, "letter=N&letter=N", once("letter")},
		{"needs_work_unknown_reason", needsWorkPath, "reason=lyrics",
			rule("invalid reason: not one of its allowed values")},
	}
	handlers := map[string]http.Handler{
		piecesPath:      PieceListGet{Catalogue: &fakerepo.Repo{}},
		personsPath:     PersonListGet{Catalogue: &fakerepo.Repo{}},
		collectionsPath: CollectionListGet{Catalogue: &fakerepo.Repo{}},
		needsWorkPath:   NeedsWorkListGet{Catalogue: &fakerepo.Repo{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path+"?"+tc.query, nil)
			rec := openapitest.New(t).ServeResponseOnly(t, handlers[tc.path], req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}

// TestSearchControllers_TimeoutLoggedAsWarning checks that a search the database stopped for running too long is
// answered with 500 and logged as a warning, and any other failure as an error.
func TestSearchControllers_TimeoutLoggedAsWarning(t *testing.T) {
	t.Parallel()

	handlers := map[string]func(c datasources.Catalogue) http.Handler{
		piecesPath:      func(c datasources.Catalogue) http.Handler { return PieceListGet{Catalogue: c} },
		personsPath:     func(c datasources.Catalogue) http.Handler { return PersonListGet{Catalogue: c} },
		collectionsPath: func(c datasources.Catalogue) http.Handler { return CollectionListGet{Catalogue: c} },
		needsWorkPath:   func(c datasources.Catalogue) http.Handler { return NeedsWorkListGet{Catalogue: c} },
	}
	for path, handler := range handlers {
		for _, tc := range []struct {
			err       error
			wantLevel string
		}{{errTimeout, "WARN"}, {errors.New("connection refused"), "ERROR"}} {
			t.Run(strings.TrimPrefix(path, "/v1/")+"_"+tc.wantLevel, func(t *testing.T) {
				t.Parallel()

				var logs bytes.Buffer
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
				req = req.WithContext(domain.ContextWithLogger(req.Context(), slog.New(slog.NewTextHandler(&logs, nil))))

				rec := openapitest.New(t).Serve(t, handler(&fakerepo.Repo{Err: tc.err}), req)

				assert.Equal(t, http.StatusInternalServerError, rec.Code)
				assert.JSONEq(t, internalErrorBody, rec.Body.String())
				assert.Contains(t, logs.String(), "level="+tc.wantLevel)
			})
		}
	}
}

// twentyOneTerms is a query of one more valid term than a piece search may filter on.
func twentyOneTerms() string {
	terms := make([]string, 0, datasources.MaxPieceQueryTerms+1)
	for i := range datasources.MaxPieceQueryTerms + 1 {
		terms = append(terms, fmt.Sprintf("term-%d", i))
	}
	return strings.Join(terms, "&term=")
}
