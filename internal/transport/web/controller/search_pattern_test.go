package controller

import (
	"regexp"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shorthandClass matches a regular expression shorthand character class, such as \s: their meaning differs
// between ECMA-262, which OpenAPI patterns use (\s includes Unicode spaces), and Go's RE2 (ASCII only).
var shorthandClass = regexp.MustCompile(`\\[sSwWdDbB]`)

// TestSearchPattern_MatchesParseSearchQuery checks that the spec's pattern for q states the controllers' rule
// exactly: it uses no shorthand class, whose meaning would differ for a client validating with ECMA-262, and a
// query of any one character matches it exactly when ParseSearchQuery finds it not blank.
func TestSearchPattern_MatchesParseSearchQuery(t *testing.T) {
	t.Parallel()

	doc, err := openapi3.NewLoader().LoadFromData(openapi.Spec)
	require.NoError(t, err)
	param := doc.Components.Parameters["Search"]
	require.NotNil(t, param)
	pattern := param.Value.Schema.Value.Pattern
	require.NotEmpty(t, pattern)
	assert.NotRegexp(t, shorthandClass, pattern, "the pattern must not use a shorthand class")

	re := regexp.MustCompile(pattern)
	for _, r := range []rune{' ', '\t', '\n', '\f', '\r', '\v', 0x85, 0xa0, 0x1680, 0x2003, 0x2028, 0x3000, 0xfeff,
		'a', '_', '!', 'é'} {
		_, err := domain.ParseSearchQuery(string(r))
		assert.Equal(t, err == nil, re.MatchString(string(r)), "%U", r)
	}
}
