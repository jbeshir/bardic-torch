package domain

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
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

// specEnums maps each named enum schema of openapi/api.yaml to the values of the domain vocabulary it describes,
// in the order the spec lists them (the order of the database ENUM).
func specEnums() map[string][]string {
	letters := []string{string(LetterOther)}
	for r := 'A'; r <= 'Z'; r++ {
		letters = append(letters, string(r))
	}
	return map[string][]string{
		"PieceKind":          strs(pieceKindValues),
		"ConservationStatus": strs(conservationStatusValues),
		"PersonKind":         strs(personKindValues),
		"PersonStatus":       strs(personStatusValues),
		"Facet":              strs(facetValues),
		"LicensingStatus":    strs(licensingStatusValues),
		"PieceAliasKind":     strs(pieceAliasKindValues),
		"PersonAliasKind":    strs(personAliasKindValues),
		"CreditRole":         strs(creditRoleValues),
		"Attribution":        strs(attributionValues),
		"Certainty":          strs(certaintyValues),
		"PersonRole":         strs(personRoleValues),
		"LinkPlaceType":      strs(linkPlaceTypeValues),
		"Liveness":           strs(livenessValues),
		"LinkRole":           strs(linkRoleValues),
		"CollectionKind":     strs(collectionKindValues),
		"RevisionSource":     strs(revisionSourceValues),
		"RevisionStatus":     strs(revisionStatusValues),
		"EntityType":         strs(entityTypeValues),
		"ProblemCode":        strs(problemCodeValues),
		"NeedsWorkReason":    strs(NeedsWorkReasons()),
		"PieceSort":          strs(pieceSortValues),
		"PersonSort":         strs(personSortValues),
		"Letter":             letters,
	}
}

// TestSpec_EnumSchemasMatchDomain checks that every named enum schema in the spec lists exactly the values, in
// the order, of the domain vocabulary it describes, so that a value added in the domain and the database without
// the spec (or the reverse) fails here rather than breaking the contract in production. A new enum schema must be
// added to specEnums. Inline enums (error codes, the changed-field names of each entity type, the entity type
// tags of a response) describe no domain vocabulary and are not compared.
func TestSpec_EnumSchemasMatchDomain(t *testing.T) {
	t.Parallel()

	doc, err := openapi3.NewLoader().LoadFromData(openapi.Spec)
	require.NoError(t, err)

	want := specEnums()
	found := map[string]bool{}
	for name, ref := range doc.Components.Schemas {
		if ref.Value == nil || len(ref.Value.Enum) == 0 {
			continue
		}
		found[name] = true
		domainValues, ok := want[name]
		if !assert.True(t, ok, "enum schema %s is not compared with a domain vocabulary: add it to specEnums", name) {
			continue
		}
		var specValues []string
		for _, v := range ref.Value.Enum {
			s, isString := v.(string)
			require.True(t, isString, "%s has a value that is not a string: %v", name, v)
			specValues = append(specValues, s)
		}
		assert.Equal(t, domainValues, specValues, "enum schema %s", name)
	}
	for name := range want {
		assert.True(t, found[name], "specEnums has %s, which is not an enum schema in the spec", name)
	}
}
