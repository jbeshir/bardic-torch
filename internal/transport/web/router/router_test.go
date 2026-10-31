package router

import (
	"fmt"
	"slices"
	"testing"

	"github.com/gorilla/mux"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRouter_RoutesMatchOpenAPI checks that every route the router registers is an operation in the OpenAPI
// spec, and that every operation in the spec is routed.
func TestRouter_RoutesMatchOpenAPI(t *testing.T) {
	t.Parallel()

	var routed []openapitest.Operation
	err := newRoutes(Dependencies{}).Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		// Routes without a handler, such as the /v1 prefix, only hold subrouters.
		if route.GetHandler() == nil {
			return nil
		}

		path, err := route.GetPathTemplate()
		if err != nil {
			return err
		}
		methods, err := route.GetMethods()
		if err != nil {
			return fmt.Errorf("route %s must be registered with Methods: %w", path, err)
		}

		for _, method := range methods {
			routed = append(routed, openapitest.Operation{Method: method, Path: path})
		}
		return nil
	})
	require.NoError(t, err)

	seen := make(map[openapitest.Operation]bool)
	for _, op := range routed {
		assert.False(t, seen[op], "%s %s is routed twice", op.Method, op.Path)
		seen[op] = true
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
