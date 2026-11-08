package domain_test

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/domaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

// withNotes is p with notes.
func withNotes(p domain.DocPreference, notes string) domain.DocPreference {
	p.Value = &domain.PreferenceValue{Choices: p.Value.Choices, Notes: &notes}

	return p
}

func fullPieceDocument() domain.PieceDocument {
	return domain.PieceDocument{
		SchemaVersion:  domain.DocumentSchemaVersion,
		Title:          "The Copper Kettle",
		Kind:           domain.PieceKindSong,
		SecondaryKinds: []domain.PieceKind{domain.PieceKindComedy},
		Conservation: domain.DocConservation{
			Status:    domain.ConservationStatusEndangered,
			SourceURL: ptr("https://example.org/kettle-survey"),
		},
		IsStub:             true,
		NotesMD:            ptr("Sung *loudly*."),
		VersionNotesMD:     ptr("Two endings."),
		LicensingStatus:    domain.LicensingStatusKnown,
		LicensingNotesMD:   ptr("Free to sing."),
		Licence:            ptr(domain.LicenceCCBY),
		LicenceVersion:     ptr(domain.LicenceVersion40),
		HistoricalSourceMD: ptr("A tavern book."),
		PeriodStart:        ptr[int16](1500),
		PeriodEnd:          ptr[int16](1550),
		Aliases:            []domain.DocPieceAlias{{Text: "Kettle Song", Kind: domain.PieceAliasKindNickname}},
		Terms: []domain.DocPieceTerm{
			{TermID: 3, IsPrimary: true, Part: ptr(domain.PiecePartTune)},
			{TermID: 4},
		},
		Relations: []domain.DocPieceRelation{{ToPieceID: 9, Type: domain.PieceRelationTypeVariantOf}},
		Credits: []domain.DocCredit{
			{PersonID: ptr[int64](5), Role: domain.CreditRoleLyrics, Attribution: domain.AttributionNamed,
				Certainty: domain.CertaintyProbable, Part: ptr(domain.PiecePartVerses), Note: ptr("Verse 3 only")},
			{Role: domain.CreditRoleTune, Attribution: domain.AttributionTraditional, Certainty: domain.CertaintyCertain},
		},
		Links: []domain.DocPieceLink{{
			DocLink: domain.DocLink{
				URL: "https://example.org/kettle", Title: ptr("Kettle"), Description: ptr("A recording"),
				Platform: ptr("youtube"), PlaceType: ptr(domain.LinkPlaceTypeRecordingPlatform),
				CollectionID: ptr[int64](2), Locator: ptr("p. 12"),
				Roles: []domain.LinkRole{domain.LinkRolePerformance, domain.LinkRoleLyrics},
			},
			Performances: []domain.DocPerformance{{
				EventID: ptr[int64](7), Year: ptr[int16](2019), Uploader: ptr("fireside-archive"),
				DescriptionVerbatim: "Sung at the revel", PerformerIDs: []int64{5, 6},
			}},
		}},
		Preferences: []domain.DocPreference{withNotes(domaintest.Preference(domain.PreferenceDimensionRecording,
			"https://example.org/prefs", domain.PreferenceChoiceOralOnly, domain.PreferenceChoiceNoRecording),
			"Ask at the event.")},
	}
}

func fullPersonDocument() domain.PersonDocument {
	return domain.PersonDocument{
		SchemaVersion:    domain.DocumentSchemaVersion,
		Name:             "Wren of the Vale",
		Kind:             domain.PersonKindCommunityBard,
		Status:           domain.PersonStatusDeceased,
		StatusDate:       &domain.Date{Time: time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)},
		StatusSourceURL:  ptr("https://example.org/wren"),
		Pronouns:         ptr("they/them"),
		NotesMD:          ptr("Sang at every revel."),
		LicensingStatus:  domain.LicensingStatusKnown,
		LicensingNotesMD: ptr("All works CC BY."),
		IsStub:           true,
		Aliases: []domain.DocPersonAlias{
			{Name: "Wren", Kind: domain.PersonAliasKindNickname, IsPublic: true},
			{Name: "Jo Example", Kind: domain.PersonAliasKindLegal},
		},
		Roles:          []domain.PersonRole{domain.PersonRoleSinger, domain.PersonRolePoet},
		AffiliationIDs: []int64{11},
		Relations:      []domain.DocPersonRelation{{ToPersonID: 6, Type: domain.PersonRelationTypeMemberOf}},
		Links: []domain.DocPersonLink{{
			DocLink: domain.DocLink{URL: "https://example.org/wren-site", Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
			Performances: []domain.DocPersonPerformance{{
				PieceID:        ptr[int64](1),
				DocPerformance: domain.DocPerformance{DescriptionVerbatim: "Live set", PerformerIDs: []int64{5}},
			}},
		}},
		Preferences: []domain.DocPreference{domaintest.Preference(domain.PreferenceDimensionAttribution,
			"https://example.org/wren-prefs", domain.PreferenceChoiceSCAName)},
	}
}

func fullTermDocument() domain.TermDocument {
	return domain.TermDocument{
		SchemaVersion:        domain.DocumentSchemaVersion,
		Facet:                domain.FacetTopic,
		Name:                 "Swords (Topic)",
		DisplayName:          "Swords",
		ParentID:             ptr[int64](1),
		RelatedAffiliationID: ptr[int64](2),
		DescriptionMD:        ptr("Songs about swords."),
		Position:             3,
		Aliases:              []string{"Blades"},
	}
}

func fullCollectionDocument() domain.CollectionDocument {
	return domain.CollectionDocument{
		SchemaVersion:    domain.DocumentSchemaVersion,
		Name:             "Fireside Songbook",
		URL:              ptr("https://example.org/fireside.pdf"),
		Kind:             domain.CollectionKindSongbookPDF,
		DescriptionMD:    ptr("A songbook."),
		RegionTermID:     ptr[int64](11),
		ParentID:         ptr[int64](1),
		IsBardicResource: true,
		Enumerable:       domain.CollectionEnumerablePDF,
	}
}

func fullEventDocument() domain.EventDocument {
	return domain.EventDocument{
		SchemaVersion: domain.DocumentSchemaVersion,
		Name:          "Harvest Revel",
		Kind:          domain.EventKindEvent,
		DescriptionMD: ptr("A yearly revel."),
	}
}

// requireRoundTrip encodes doc, checks the encoding has exactly the expected top-level fields, and decodes it
// back to an equal document.
func requireRoundTrip[D domain.Document](t *testing.T, doc D, fields ...string) {
	t.Helper()

	data, err := json.Marshal(doc)
	require.NoError(t, err)

	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &top))
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, fields, keys)
	assert.JSONEq(t, "1", string(top["schema_version"]))

	got, err := domain.DecodeDocument[D](data)
	require.NoError(t, err)
	assert.Equal(t, doc, got)
}

func TestDecodeDocument_RoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("piece", func(t *testing.T) {
		t.Parallel()
		requireRoundTrip(t, fullPieceDocument(), "schema_version", "title", "kind", "secondary_kinds",
			"conservation", "is_stub", "notes_md", "version_notes_md", "licensing_status", "licensing_notes_md",
			"licence", "licence_version", "historical_source_md", "period_start", "period_end", "aliases", "terms",
			"relations",
			"credits", "links", "preferences")
	})
	t.Run("person", func(t *testing.T) {
		t.Parallel()
		requireRoundTrip(t, fullPersonDocument(), "schema_version", "name", "kind", "status", "status_date",
			"status_source_url", "pronouns", "notes_md", "licensing_status", "licensing_notes_md", "is_stub",
			"aliases", "roles", "affiliation_ids", "relations", "links", "preferences")
	})
	t.Run("term", func(t *testing.T) {
		t.Parallel()
		requireRoundTrip(t, fullTermDocument(), "schema_version", "facet", "name", "display_name", "parent_id",
			"related_affiliation_id", "description_md", "position", "aliases")
	})
	t.Run("collection", func(t *testing.T) {
		t.Parallel()
		requireRoundTrip(t, fullCollectionDocument(), "schema_version", "name", "url", "kind", "description_md",
			"region_term_id", "parent_id", "is_bardic_resource", "enumerable")
	})
	t.Run("event", func(t *testing.T) {
		t.Parallel()
		requireRoundTrip(t, fullEventDocument(), "schema_version", "name", "kind", "description_md")
	})
}

// TestDecodeDocument_LinkFieldsAreFlat pins the encoding of an embedded DocLink: its fields sit beside
// performances, not under a nested object.
func TestDecodeDocument_LinkFieldsAreFlat(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(fullPersonDocument().Links[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"url": "https://example.org/wren-site", "title": null, "description": null,
		"platform": null, "place_type": null, "collection_id": null, "locator": null, "roles": ["website"],
		"performances": [{"piece_id": 1, "event_id": null, "year": null, "uploader": null,
			"description_verbatim": "Live set", "performer_ids": [5]}]}`, string(data))
}

func TestDecodeDocument_UnsupportedSchemaVersion(t *testing.T) {
	t.Parallel()

	for name, data := range map[string]string{
		"newer":   `{"schema_version": 2, "name": "Harvest Revel", "kind": "event", "description_md": null}`,
		"zero":    `{"schema_version": 0, "name": "Harvest Revel", "kind": "event", "description_md": null}`,
		"missing": `{"name": "Harvest Revel", "kind": "event", "description_md": null}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.DecodeDocument[domain.EventDocument]([]byte(data))
			require.ErrorIs(t, err, domain.ErrUnsupportedSchemaVersion)
		})
	}
}

func TestDecodeDocument_Malformed(t *testing.T) {
	t.Parallel()

	for name, data := range map[string]string{
		"not JSON":        `{"schema_version": 1,`,
		"not an object":   `[1]`,
		"version string":  `{"schema_version": "1"}`,
		"wrong type":      `{"schema_version": 1, "name": 7}`,
		"trailing object": `{"schema_version": 1, "name": "Harvest Revel"} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.DecodeDocument[domain.EventDocument]([]byte(data))
			require.Error(t, err)
			require.NotErrorIs(t, err, domain.ErrUnsupportedSchemaVersion)
		})
	}
}

func TestDecodeDocument_TrailingWhitespace(t *testing.T) {
	t.Parallel()

	doc, err := domain.DecodeDocument[domain.EventDocument](
		[]byte("{\"schema_version\": 1, \"name\": \"Harvest Revel\", \"kind\": \"venue\"}\n"))
	require.NoError(t, err)
	assert.Equal(t, domain.EventDocument{SchemaVersion: 1, Name: "Harvest Revel", Kind: domain.EventKindVenue}, doc)
}

// TestDecodeDocument_UnknownFields checks that fields the document type does not have, at any depth, are
// ignored, as a later release may add them: the document decodes to the fields this release knows.
func TestDecodeDocument_UnknownFields(t *testing.T) {
	t.Parallel()

	data := withValues(t, fullPersonDocument(), map[string]any{
		"added_field":                    "a later release's",
		"aliases.0.added_field":          true,
		"links.0.added_field":            7,
		"links.0.performances.0.added":   []any{"x"},
		"relations.0.added_field":        nil,
		"links.0.performances.0.nested":  map[string]any{"status": "not an enum here"},
		"preferences.0.added_field_note": "ignored",
	})
	got, err := domain.DecodeDocument[domain.PersonDocument](data)
	require.NoError(t, err)
	assert.Equal(t, fullPersonDocument(), got)
}

// TestDecodeDocument_UnknownEnumValue checks that a vocabulary value this release does not know fails the
// decode with ErrUnknownEnumValue, wherever it is in the document, as one in a column fails a read.
// unknownValue is a value of no vocabulary, and kindField the field most documents hold their kind in.
const (
	unknownValue          = "bogus"
	kindField             = "kind"
	firstPreferenceChoice = "preferences.0.value.choices.0"
)

func TestDecodeDocument_UnknownEnumValue(t *testing.T) {
	t.Parallel()

	pieceFields := []string{
		kindField, "secondary_kinds.0", "conservation.status", "licensing_status", "licence", "aliases.0.kind",
		"terms.0.part", "relations.0.type", "credits.0.role", "credits.0.attribution", "credits.0.certainty",
		"credits.0.part", "links.0.place_type", "links.0.roles.1", "preferences.0.dimension",
		"preferences.0.value.choices.1",
	}
	for _, field := range pieceFields {
		t.Run("piece."+field, func(t *testing.T) {
			t.Parallel()
			_, err := domain.DecodeDocument[domain.PieceDocument](
				withValues(t, fullPieceDocument(), map[string]any{field: unknownValue}))
			require.ErrorIs(t, err, domain.ErrUnknownEnumValue)
		})
	}

	personFields := []string{
		kindField, "status", "licensing_status", "aliases.1.kind", "roles.0", "relations.0.type", "links.0.roles.0",
		"links.0.place_type", "preferences.0.dimension", firstPreferenceChoice,
	}
	for _, field := range personFields {
		t.Run("person."+field, func(t *testing.T) {
			t.Parallel()
			_, err := domain.DecodeDocument[domain.PersonDocument](
				withValues(t, fullPersonDocument(), map[string]any{field: unknownValue}))
			require.ErrorIs(t, err, domain.ErrUnknownEnumValue)
		})
	}

	t.Run("term.facet", func(t *testing.T) {
		t.Parallel()
		_, err := domain.DecodeDocument[domain.TermDocument](
			withValues(t, fullTermDocument(), map[string]any{"facet": unknownValue}))
		require.ErrorIs(t, err, domain.ErrUnknownEnumValue)
	})
	for _, field := range []string{kindField, "enumerable"} {
		t.Run("collection."+field, func(t *testing.T) {
			t.Parallel()
			_, err := domain.DecodeDocument[domain.CollectionDocument](
				withValues(t, fullCollectionDocument(), map[string]any{field: unknownValue}))
			require.ErrorIs(t, err, domain.ErrUnknownEnumValue)
		})
	}
	t.Run("event.kind", func(t *testing.T) {
		t.Parallel()
		_, err := domain.DecodeDocument[domain.EventDocument](
			withValues(t, fullEventDocument(), map[string]any{kindField: unknownValue}))
		require.ErrorIs(t, err, domain.ErrUnknownEnumValue)
	})
	t.Run("missing_required", func(t *testing.T) {
		t.Parallel()
		_, err := domain.DecodeDocument[domain.EventDocument](
			[]byte(`{"schema_version": 1, "name": "Harvest Revel", "description_md": null}`))
		require.ErrorIs(t, err, domain.ErrUnknownEnumValue, "an empty kind is not a kind")
	})
}

// TestDecodeDocument_UnknownPreferenceChoiceIs500 checks that a stored preference holding a choice this release
// does not know fails the decode with ErrUnknownEnumValue, so a read of it is an internal error (500) rather than
// a profile shown without it, while a known choice of another dimension decodes: only validation knows which
// dimension has which choices.
func TestDecodeDocument_UnknownPreferenceChoiceIs500(t *testing.T) {
	t.Parallel()

	for name, choice := range map[string]string{"unknown": "maybe", "blank_code": ""} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.DecodeDocument[domain.PieceDocument](
				withValues(t, fullPieceDocument(), map[string]any{firstPreferenceChoice: choice}))
			require.ErrorIs(t, err, domain.ErrUnknownEnumValue)
		})
	}
	t.Run("other_dimension", func(t *testing.T) {
		t.Parallel()
		got, err := domain.DecodeDocument[domain.PersonDocument](withValues(t, fullPersonDocument(),
			map[string]any{firstPreferenceChoice: string(domain.PreferenceChoiceCCBY)}))
		require.NoError(t, err)
		assert.Equal(t, []domain.PreferenceChoice{domain.PreferenceChoiceCCBY}, got.Preferences[0].Value.Choices)
	})
}

// TestDocuments_EnumFieldsAreValidated checks that every field of a document whose type is one of the domain's
// named string types is a vocabulary with a Valid method, which DecodeDocument checks. It looks through pointers,
// slices, arrays and map keys and values, as DecodeDocument does (TestValidateEnums_MapsAndArrays).
func TestDocuments_EnumFieldsAreValidated(t *testing.T) {
	t.Parallel()

	validator := reflect.TypeFor[interface{ Valid() bool }]()
	domainPkg := reflect.TypeFor[domain.PieceKind]().PkgPath()
	seen := map[reflect.Type]bool{}
	var visit func(typ reflect.Type, path string)
	visit = func(typ reflect.Type, path string) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
			typ = typ.Elem()
		}
		switch {
		case typ.Kind() == reflect.Map:
			visit(typ.Key(), path+"[key]")
			visit(typ.Elem(), path+"[]")
		case typ.Kind() == reflect.String && typ.PkgPath() == domainPkg:
			seen[typ] = true
			assert.True(t, typ.Implements(validator), "%s: %s has no Valid method", path, typ)
		case typ.Kind() == reflect.Struct && typ.PkgPath() == domainPkg && typ != reflect.TypeFor[domain.Date]():
			for i := range typ.NumField() {
				visit(typ.Field(i).Type, path+"."+typ.Field(i).Name)
			}
		}
	}
	for _, doc := range []any{domain.PieceDocument{}, domain.PersonDocument{}, domain.TermDocument{},
		domain.CollectionDocument{}, domain.EventDocument{}} {
		visit(reflect.TypeOf(doc), reflect.TypeOf(doc).Name())
	}
	assert.Len(t, seen, 24, "the vocabularies documents use")
}

func TestDecodeDocument_StatusDate(t *testing.T) {
	t.Parallel()

	got, err := domain.DecodeDocument[domain.PersonDocument](
		withValues(t, fullPersonDocument(), map[string]any{"status_date": nil}))
	require.NoError(t, err)
	assert.Nil(t, got.StatusDate)

	for _, invalid := range []any{"March 2024", "2024-02-30", "2024-05-01T00:00:00Z", "", 20240501} {
		_, err := domain.DecodeDocument[domain.PersonDocument](
			withValues(t, fullPersonDocument(), map[string]any{"status_date": invalid}))
		assert.Error(t, err, "%v", invalid)
	}
}

// withValues returns the encoding of doc with the value at each dotted path (object keys and list indexes) set,
// adding object keys that are missing.
func withValues(t *testing.T, doc any, values map[string]any) []byte {
	t.Helper()

	data, err := json.Marshal(doc)
	require.NoError(t, err)
	var tree any
	require.NoError(t, json.Unmarshal(data, &tree))

	for path, value := range values {
		keys := strings.Split(path, ".")
		node := tree
		for i, key := range keys {
			last := i == len(keys)-1
			switch n := node.(type) {
			case map[string]any:
				if last {
					n[key] = value
				} else {
					node = n[key]
				}
			case []any:
				index, err := strconv.Atoi(key)
				require.NoError(t, err, path)
				require.Less(t, index, len(n), path)
				if last {
					n[index] = value
				} else {
					node = n[index]
				}
			default:
				require.Failf(t, "no such path", "%s", path)
			}
		}
	}

	data, err = json.Marshal(tree)
	require.NoError(t, err)

	return data
}

// TestValidateEnums_MapsAndArrays checks that enum validation also reaches enums in arrays and in map keys and
// values, which no document has yet, so that a document field of such a type is validated when one is added.
func TestValidateEnums_MapsAndArrays(t *testing.T) {
	t.Parallel()

	type holder struct {
		Fixed  [2]domain.PieceKind
		ByKind map[domain.PieceKind]string
		Kinds  map[string]*domain.PieceKind
	}
	valid := func() holder {
		return holder{
			Fixed:  [2]domain.PieceKind{domain.PieceKindSong, domain.PieceKindPoem},
			ByKind: map[domain.PieceKind]string{domain.PieceKindSong: "x"},
			Kinds:  map[string]*domain.PieceKind{"a": ptr(domain.PieceKindPoem), "b": nil},
		}
	}
	require.NoError(t, domain.ValidateEnums(reflect.ValueOf(valid()), "holder"))

	unknown := domain.PieceKind(unknownValue)
	for name, mutate := range map[string]func(*holder){
		"array_element": func(h *holder) { h.Fixed[1] = unknown },
		"map_key":       func(h *holder) { h.ByKind[unknown] = "y" },
		"map_value":     func(h *holder) { h.Kinds["c"] = &unknown },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := valid()
			mutate(&h)
			require.ErrorIs(t, domain.ValidateEnums(reflect.ValueOf(h), "holder"), domain.ErrUnknownEnumValue)
		})
	}
}
