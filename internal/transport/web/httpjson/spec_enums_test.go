package httpjson

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// strs returns the values of a vocabulary as strings.
func strs[E ~string](values []E) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

// enumOf returns the string values of the inline enum of schema s, failing the test if it has none.
func enumOf(t *testing.T, name string, s *openapi3.SchemaRef) []string {
	t.Helper()

	require.NotNil(t, s, "%s is in the spec", name)
	require.NotEmpty(t, s.Value.Enum, "%s has an enum", name)
	values := make([]string, 0, len(s.Value.Enum))
	for _, v := range s.Value.Enum {
		value, ok := v.(string)
		require.True(t, ok, "%s has a value that is not a string: %v", name, v)
		values = append(values, value)
	}
	return values
}

// TestSpec_InlineEnumsMatchCode checks the inline enums of the spec against the code's vocabularies, as
// TestSpec_EnumSchemasMatchDomain checks the named ones: the error codes of every error body (Error,
// DocumentError, DuplicateError and FlagsError together) are ErrorCodeValues, the problem codes of DocumentError
// are domain.ProblemCodeValues, and User's role is domain.RoleValues.
func TestSpec_InlineEnumsMatchCode(t *testing.T) {
	t.Parallel()

	doc, err := openapi3.NewLoader().LoadFromData(openapi.Spec)
	require.NoError(t, err)
	schemas := doc.Components.Schemas
	property := func(schema, name string) *openapi3.SchemaRef {
		require.Contains(t, schemas, schema)
		return schemas[schema].Value.Properties[name]
	}

	var codes []string
	for _, body := range []string{"Error", "DocumentError", "DuplicateError", "FlagsError"} {
		codes = append(codes, enumOf(t, body+".code", property(body, "code"))...)
	}
	assert.ElementsMatch(t, strs(ErrorCodeValues()), codes, "the error codes of the error bodies, each in one")

	problem := property("DocumentError", "problems").Value.Items.Value.Properties["problem"]
	assert.Equal(t, strs(domain.ProblemCodeValues()), enumOf(t, "DocumentError.problems[].problem", problem))
	assert.Equal(t, strs(domain.RoleValues()), enumOf(t, "User.role", property("User", "role")))
}
