package openapitest

import (
	"fmt"
	"slices"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
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
