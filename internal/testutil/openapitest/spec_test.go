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

func TestOpenResponseObjects_FindsOpenObjects(t *testing.T) {
	t.Parallel()

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData([]byte(`
openapi: 3.1.0
info: {title: open objects, version: "1"}
paths:
  /things:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                additionalProperties: false
                properties:
                  closed:
                    type: object
                    additionalProperties: false
                    properties:
                      name: {type: string}
                  open:
                    type: object
                    properties:
                      name: {type: string}
                  explicitly_open:
                    type: object
                    additionalProperties: true
                  dictionary:
                    type: object
                    additionalProperties: {type: string}
                  list:
                    type: array
                    items:
                      properties:
                        name: {type: string}
                  nullable:
                    type: [object, "null"]
                  choice:
                    oneOf:
                      - type: object
                        additionalProperties: false
                      - type: object
`))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"GET /things 200 application/json.choice.oneOf[1]",
		"GET /things 200 application/json.explicitly_open",
		"GET /things 200 application/json.list[]",
		"GET /things 200 application/json.nullable",
		"GET /things 200 application/json.open",
	}, openResponseObjects(doc))
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
			"conservation_status": "endangered", "is_stub": false},
		"base_revision_id": null, "source": "import", "summary": "", "author": null, "contributors": ["Archivist"],
		"created_at": "2026-01-01T00:00:00Z",
		"snapshot": {"title": "The Copper Kettle", "kind": "song", "secondary_kinds": [],
			"conservation_status": "endangered", "conservation_source_url": null, "is_stub": false, "notes_md": null,
			"version_notes_md": null, "licensing_status": "unknown", "licensing_notes_md": null, "licence": null,
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

// noDatabaseOperations are the operations that read no database, directly or through the auth middleware, and so
// cannot answer 503 unavailable. Every other operation does.
var noDatabaseOperations = []Operation{{Method: http.MethodGet, Path: "/healthz"}}

// TestSpec_DatabaseOperationsDeclare503 requires every operation that reads or writes the database (every one but
// noDatabaseOperations) to document 503: a database that cannot be reached is 503 unavailable on every operation,
// and writes are also 503 when they cannot finish in time.
func TestSpec_DatabaseOperationsDeclare503(t *testing.T) {
	t.Parallel()

	v := New(t)
	for _, op := range v.Operations() {
		operation := v.doc.Paths.Value(op.Path).GetOperation(op.Method)
		declared := operation.Responses.Status(http.StatusServiceUnavailable) != nil
		if slices.Contains(noDatabaseOperations, op) {
			assert.False(t, declared, "%s %s reads no database but declares 503", op.Method, op.Path)
			continue
		}
		assert.True(t, declared, "%s %s reads the database but does not declare 503", op.Method, op.Path)
	}
}

// maxSafeInteger is 2^53-1, the largest integer a JSON number holds exactly in every client (an IEEE double).
const maxSafeInteger = 1<<53 - 1

// TestSpec_Int64IDsHaveSafeMaximum requires every int64 schema of the spec, in parameters, request and response
// bodies and components, to have maximum 2^53-1, so that no client loses precision on an ID. Every int64 is an ID
// or a count of rows, so none is excepted: a count is far below the bound too.
func TestSpec_Int64IDsHaveSafeMaximum(t *testing.T) {
	t.Parallel()

	int64s := int64Schemas(New(t).doc)
	assert.NotEmpty(t, int64s, "the walk finds the int64 schemas")
	var unbounded []string
	for location, schema := range int64s {
		if schema.Max == nil || *schema.Max != maxSafeInteger {
			unbounded = append(unbounded, location)
		}
	}
	slices.Sort(unbounded)
	assert.Empty(t, unbounded, "int64 schemas without maximum: 9007199254740991")
}

// int64Schemas returns every int64 schema reachable in doc, by its first location: components, parameters,
// request bodies and responses, and every schema nested in them.
func int64Schemas(doc *openapi3.T) map[string]*openapi3.Schema {
	found := map[string]*openapi3.Schema{}
	visited := map[*openapi3.Schema]bool{}
	var visit func(location string, ref *openapi3.SchemaRef)
	visit = func(location string, ref *openapi3.SchemaRef) {
		if ref == nil || ref.Value == nil || visited[ref.Value] {
			return
		}
		visited[ref.Value] = true
		if ref.Value.Format == "int64" {
			found[location] = ref.Value
		}
		for subLocation, sub := range subSchemas(location, ref.Value) {
			visit(subLocation, sub)
		}
	}

	for name, schema := range doc.Components.Schemas {
		visit(name, schema)
	}
	for name, p := range doc.Components.Parameters {
		visit("parameter "+name, p.Value.Schema)
	}
	for path, item := range doc.Paths.Map() {
		for _, p := range item.Parameters {
			visit(path+" "+p.Value.Name, p.Value.Schema)
		}
		for method, op := range item.Operations() {
			for location, ref := range operationSchemas(method+" "+path, op) {
				visit(location, ref)
			}
		}
	}

	return found
}

// operationSchemas returns the schemas of op's parameters, request body and responses, by location.
func operationSchemas(location string, op *openapi3.Operation) map[string]*openapi3.SchemaRef {
	schemas := map[string]*openapi3.SchemaRef{}
	for _, p := range op.Parameters {
		schemas[location+" "+p.Value.Name] = p.Value.Schema
	}
	if op.RequestBody != nil {
		for mediaType, media := range op.RequestBody.Value.Content {
			schemas[location+" request "+mediaType] = media.Schema
		}
	}
	for status, response := range op.Responses.Map() {
		for mediaType, media := range response.Value.Content {
			schemas[location+" "+status+" "+mediaType] = media.Schema
		}
	}

	return schemas
}
