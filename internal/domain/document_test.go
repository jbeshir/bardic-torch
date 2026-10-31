package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

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
		Preferences: []domain.DocPreference{{
			Dimension: domain.PreferenceDimensionRecording, Value: json.RawMessage(`{"allowed":true}`),
			SourceURL: "https://example.org/prefs",
		}},
	}
}

func fullPersonDocument() domain.PersonDocument {
	return domain.PersonDocument{
		SchemaVersion:    domain.DocumentSchemaVersion,
		Name:             "Wren of the Vale",
		Kind:             domain.PersonKindCommunityBard,
		Status:           domain.PersonStatusDeceased,
		StatusDate:       ptr("2024-05-01"),
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
		Preferences: []domain.DocPreference{{
			Dimension: domain.PreferenceDimensionAttribution, Value: json.RawMessage(`"name me"`),
			SourceURL: "https://example.org/wren-prefs",
		}},
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
			"licence", "historical_source_md", "period_start", "period_end", "aliases", "terms", "relations",
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
		"unknown field":   `{"schema_version": 1, "name": "Harvest Revel", "kind": "event", "slug": "harvest-revel"}`,
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
