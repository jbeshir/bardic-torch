package openapitest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpec_ResponseObjectsAreClosed requires every object schema reachable from a response to set
// additionalProperties: false, so that contract tests reject undocumented response fields.
func TestSpec_ResponseObjectsAreClosed(t *testing.T) {
	t.Parallel()

	assert.Empty(t, openResponseObjects(New(t).doc))
}

// openResponseObjects returns the location of every object schema reachable from a response that allows
// properties beyond those it declares. Objects whose additionalProperties is a schema are dictionaries and are
// allowed.
func openResponseObjects(doc *openapi3.T) []string {
	var open []string
	visited := map[*openapi3.Schema]bool{}

	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			for status, response := range op.Responses.Map() {
				for mediaType, media := range response.Value.Content {
					location := fmt.Sprintf("%s %s %s %s", method, path, status, mediaType)
					open = append(open, openObjects(location, media.Schema, visited)...)
				}
			}
		}
	}

	slices.Sort(open)
	return open
}

func openObjects(location string, ref *openapi3.SchemaRef, visited map[*openapi3.Schema]bool) []string {
	if ref == nil || ref.Value == nil || visited[ref.Value] {
		return nil
	}
	visited[ref.Value] = true

	var open []string
	if isOpenObject(ref.Value) {
		open = append(open, location)
	}
	for subLocation, sub := range subSchemas(location, ref.Value) {
		open = append(open, openObjects(subLocation, sub, visited)...)
	}

	return open
}

func isOpenObject(schema *openapi3.Schema) bool {
	isObject := (schema.Type != nil && schema.Type.Includes(openapi3.TypeObject)) || len(schema.Properties) > 0
	additional := schema.AdditionalProperties
	return isObject && additional.Schema == nil && (additional.Has == nil || *additional.Has)
}

// subSchemas returns the schemas directly nested in schema, keyed by their location.
func subSchemas(location string, schema *openapi3.Schema) map[string]*openapi3.SchemaRef {
	subs := map[string]*openapi3.SchemaRef{
		location + "[]":                    schema.Items,
		location + ".additionalProperties": schema.AdditionalProperties.Schema,
	}
	for name, property := range schema.Properties {
		subs[location+"."+name] = property
	}
	for kind, refs := range map[string]openapi3.SchemaRefs{
		"allOf": schema.AllOf, "anyOf": schema.AnyOf, "oneOf": schema.OneOf,
	} {
		for i, sub := range refs {
			subs[fmt.Sprintf("%s.%s[%d]", location, kind, i)] = sub
		}
	}

	return subs
}

// revisionBody is a piece revision as GET /v1/revisions/{revision_id} returns it, with entity_type set to
// entityType.
func revisionBody(entityType string) string {
	return `{"entity_type": "` + entityType + `", "id": 42,
		"entity": {"slug": "the-copper-kettle", "title": "The Copper Kettle", "kind": "song", "secondary_kinds": [],
			"conservation_status": "endangered", "is_stub": false, "credit_names": []},
		"base_revision_id": null, "source": "import", "summary": "", "author": null, "contributors": ["Archivist"],
		"created_at": "2026-01-01T00:00:00Z",
		"snapshot": {"title": "The Copper Kettle", "kind": "song", "secondary_kinds": [],
			"conservation_status": "endangered", "conservation_source_url": null, "is_stub": false, "notes_md": null,
			"version_notes_md": null, "licensing_status": "unknown", "licensing_notes_md": null, "licence": null, "licence_version": null,
			"historical_source_md": null, "period_start": null, "period_end": null, "aliases": [], "terms": [],
			"credits": [], "relations": [], "links": []},
		"base_snapshot": null, "changed_fields": ["conservation_status", "kind", "title"]}`
}

// TestSpec_RevisionShapeFollowsEntityType checks that the spec's Revision is a closed union chosen by
// entity_type: a piece revision conforms as a piece's, and the same body labelled as a person's does not.
func TestSpec_RevisionShapeFollowsEntityType(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		entityType  string
		wantFailure bool
	}{
		{entityType: "piece"},
		{entityType: "person", wantFailure: true},
		{entityType: "event", wantFailure: true},
		{entityType: "recording", wantFailure: true},
	} {
		t.Run(tc.entityType, func(t *testing.T) {
			t.Parallel()

			recorder := &recordingTB{TB: t}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/revisions/42", nil)
			New(t).Serve(recorder, jsonResponse(http.StatusOK, revisionBody(tc.entityType)), req)

			if !tc.wantFailure {
				assert.Empty(t, recorder.failures)
				return
			}
			require.Len(t, recorder.failures, 1)
			assert.Contains(t, recorder.failures[0], "does not conform to the OpenAPI spec")
		})
	}
}

// recordingTB records failures reported through Errorf instead of failing the test, so that tests can assert
// the Validator reports them. Every other method goes to the wrapped real test.
type recordingTB struct {
	testing.TB
	failures []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

// jsonResponse is a handler that answers every request with status and the JSON body.
func jsonResponse(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}
