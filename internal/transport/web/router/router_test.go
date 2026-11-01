package router

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
)

// wildcard matches a path pattern's wildcard segments.
var wildcard = regexp.MustCompile(`\{[^}]*\}`)

// TestRouter_RoutesMatchOpenAPI checks that every route in the route table is an operation in the OpenAPI spec,
// that every operation in the spec is routed, and that the router built from the table routes each route's
// method and path to that route.
func TestRouter_RoutesMatchOpenAPI(t *testing.T) {
	t.Parallel()

	table := routes(Dependencies{})
	router := newRouter(table, authMiddleware(nil, nil))

	var routed []openapitest.Operation
	for _, rt := range table {
		op := openapitest.Operation{Method: rt.method, Path: rt.path}
		assert.False(t, slices.Contains(routed, op), "%s %s is routed twice", op.Method, op.Path)
		routed = append(routed, op)

		// A path with every wildcard filled in must reach this route, not another one that also matches it.
		req := httptest.NewRequestWithContext(t.Context(), rt.method, wildcard.ReplaceAllString(rt.path, "x"), nil)
		template, ok := router.lookup(req)
		assert.True(t, ok, "%s %s is not routed", op.Method, op.Path)
		assert.Equal(t, rt.path, template, "%s %s is routed to another route", op.Method, op.Path)
	}

	documented := openapitest.New(t).Operations()

	var undocumented, unrouted []openapitest.Operation
	for _, op := range routed {
		if !slices.Contains(documented, op) {
			undocumented = append(undocumented, op)
		}
	}
	for _, op := range documented {
		if !slices.Contains(routed, op) {
			unrouted = append(unrouted, op)
		}
	}

	assert.Empty(t, undocumented, "routes missing from openapi/api.yaml")
	assert.Empty(t, unrouted, "operations in openapi/api.yaml with no route")
}

// TestNewRouter_RejectsUnsupportedPaths checks that newRouter refuses paths whose ServeMux matching would
// differ from the router's segment-by-segment rules.
func TestNewRouter_RejectsUnsupportedPaths(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/v1/songs/", "/v1/files/{path...}"} {
		assert.Panics(t, func() {
			newRouter([]route{{method: http.MethodGet, path: path, handler: http.NotFoundHandler()}}, nil)
		}, path)
	}
}
