package domain

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reasonVocabulary is the reason a list of vocabulary values is never withheld.
const reasonVocabulary = "vocabulary values, never withheld"

// notInTables are the list and reference fields of the documents and their links that no withheld-fields table
// names, each with the reason. A field added to a document must be classified here or in its type's table.
var notInTables = map[reflect.Type]map[string]string{
	reflect.TypeFor[PieceDocument](): {
		"secondary_kinds": reasonVocabulary,
		"aliases":         "piece aliases are always public",
	},
	reflect.TypeFor[PersonDocument](): {
		"roles": reasonVocabulary,
	},
	reflect.TypeFor[TermDocument](): {
		"aliases": "term aliases are always public",
	},
	reflect.TypeFor[CollectionDocument](): {},
	reflect.TypeFor[EventDocument]():      {},
	reflect.TypeFor[DocLink](): {
		"collection_id": "withheld and restored by the links field's parts (pieceLink, personLink)",
		"roles":         reasonVocabulary,
	},
	reflect.TypeFor[DocPieceLink](): {
		"performances": "shown whole; their parts are withheld by the links field's parts",
	},
	reflect.TypeFor[DocPersonLink](): {
		"performances": "withheld by piece and restored by the links field's parts (personLink, restorePersonLink)",
	},
	reflect.TypeFor[DocPerformance](): {
		"event_id":      "withheld and restored by the links field's parts (performance, restorePerformance)",
		"performer_ids": "withheld and restored by the links field's parts (performance, restorePerformance)",
	},
	reflect.TypeFor[DocPersonPerformance](): {
		"piece_id": "part of the performance's key; a performance of a piece that is not public is withheld whole",
	},
}

// tableNames are the JSON names each document type's table names.
var tableNames = map[reflect.Type][]string{
	reflect.TypeFor[PieceDocument]():      pieceWithheld.names(),
	reflect.TypeFor[PersonDocument]():     personWithheld.names(),
	reflect.TypeFor[TermDocument]():       termWithheld.names(),
	reflect.TypeFor[CollectionDocument](): collectionWithheld.names(),
	reflect.TypeFor[EventDocument]():      eventWithheld.names(),
}

func TestWithheldFields_CoverEveryField(t *testing.T) {
	t.Parallel()

	for typ, unlisted := range notInTables {
		t.Run(typ.Name(), func(t *testing.T) {
			t.Parallel()
			listed := tableNames[typ]
			var fields []string
			for i := range typ.NumField() {
				f := typ.Field(i)
				if f.Anonymous {
					continue
				}
				name := jsonFieldName(f)
				fields = append(fields, name)
				if f.Type.Kind() != reflect.Slice && f.Type != reflect.TypeFor[*int64]() {
					continue
				}
				_, reason := unlisted[name]
				assert.NotEqual(t, reason, slices.Contains(listed, name),
					"%s.%s must be named either in its withheld-fields table or in notInTables, not both", typ.Name(), name)
			}
			for _, name := range listed {
				assert.Contains(t, fields, name, "the table names a field %s does not have", typ.Name())
			}
			for name := range unlisted {
				assert.Contains(t, fields, name, "notInTables names a field %s does not have", typ.Name())
			}
		})
	}
}

func TestWithheldFields_TableKeys(t *testing.T) {
	t.Parallel()

	// The element keys are stored as drafts' received keys, so they must not change.
	keys := map[string]string{}
	for _, f := range termWithheld.refs {
		keys[f.name] = f.elementKey(7)
	}
	for _, f := range collectionWithheld.refs {
		keys["collection."+f.name] = f.elementKey(7)
	}
	for _, f := range pieceWithheld.sources {
		keys["piece."+f.path] = f.elementKey()
	}
	for _, f := range personWithheld.sources {
		keys["person."+f.path] = f.elementKey()
	}
	assert.Equal(t, map[string]string{
		"parent_id": elementKey("parent", 7), "related_affiliation_id": elementKey("related_affiliation", 7),
		"collection.region_term_id": elementKey("region", 7), "collection.parent_id": elementKey("collection_parent", 7),
		"piece./conservation/source_url": elementKey("source", "conservation"),
		"person./status_source_url":      elementKey("source", "status"),
	}, keys)
}

func TestIdentityFields_MatchWithIdentity(t *testing.T) {
	t.Parallel()

	var d PersonDocument
	fill(reflect.ValueOf(&d).Elem())
	profile := DefaultPersonDocument()
	got := withIdentity(profile, d)
	gv, pv, dv := reflect.ValueOf(got), reflect.ValueOf(profile), reflect.ValueOf(d)
	var copied []string
	for i := range gv.NumField() {
		name := jsonFieldName(gv.Type().Field(i))
		switch {
		case reflect.DeepEqual(gv.Field(i).Interface(), dv.Field(i).Interface()):
			copied = append(copied, name)
		default:
			assert.Equal(t, pv.Field(i).Interface(), gv.Field(i).Interface(), "%s is the profile's", name)
		}
	}
	assert.ElementsMatch(t, identityFields, copied, "withIdentity copies exactly the identity fields")
	assert.ElementsMatch(t, []string{"schema_version", "name", "kind", "status", "status_date", "status_source_url",
		"is_stub"}, identityFields, "a person's identity: name, kind, status with its date and source, and stub")
	data, err := json.Marshal(identityOnly(d))
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"notes_md":"x"`, "a no-page person's projection holds no profile field")
}

// fill sets every field of v, recursively, to a non-zero value that no default document holds.
func fill(v reflect.Value) {
	switch k := v.Kind(); {
	case v.Type() == reflect.TypeFor[time.Time]():
		v.Set(reflect.ValueOf(time.Date(2001, 2, 3, 0, 0, 0, 0, time.UTC)))
	case v.Type() == reflect.TypeFor[json.RawMessage]():
		v.SetBytes([]byte(`"x"`))
	case k == reflect.String:
		v.SetString("x")
	case k == reflect.Bool:
		v.SetBool(true)
	case v.CanInt():
		v.SetInt(7)
	case v.CanUint():
		v.SetUint(7)
	case k == reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(v.Elem())
	case k == reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fill(v.Index(0))
	case k == reflect.Struct:
		for i := range v.NumField() {
			fill(v.Field(i))
		}
	default:
		panic("fill: unhandled kind " + k.String())
	}
}
