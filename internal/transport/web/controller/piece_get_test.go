package controller

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const testPieceSlug = "the-copper-kettle"

// testPiece has every field and list of a piece set, including each nullable reference both set and null.
func testPiece() domain.Piece {
	checkedAt := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	tune := domain.PiecePartTune
	licence := domain.LicenceCCBYSA
	licenceVersion := domain.LicenceVersion40
	wren := domain.PersonSummary{
		Slug: "wren-of-the-vale", Name: testPersonName, Kind: domain.PersonKindCommunityBard,
	}

	return domain.Piece{
		PieceSummary: domain.PieceSummary{
			Slug:               testPieceSlug,
			Title:              testPieceTitle,
			Kind:               domain.PieceKindSong,
			SecondaryKinds:     []domain.PieceKind{domain.PieceKindDanceMusic},
			ConservationStatus: domain.ConservationStatusEndangered,
			CreditNames:        []string{testPersonName},
		},
		UpdatedAt:          testRevisionCreatedAt,
		NotesMD:            new("A drinking song."),
		VersionNotesMD:     new("Two verses."),
		LicensingStatus:    domain.LicensingStatusKnown,
		LicensingNotesMD:   new("Free with credit."),
		Licence:            &licence,
		LicenceVersion:     &licenceVersion,
		HistoricalSourceMD: new("A broadside."),
		PeriodStart:        new(int16(1580)),
		PeriodEnd:          new(int16(1600)),
		Aliases:            []domain.PieceAlias{{Text: "Kettle Song", Kind: domain.PieceAliasKindNickname}},
		Terms: []domain.PieceTerm{
			{Term: domain.TermSummary{Slug: "drinking", Facet: domain.FacetTopic, DisplayName: "Drinking"}, IsPrimary: true},
			{
				Term: domain.TermSummary{Slug: "16th-century", Facet: domain.FacetOrigin, DisplayName: "16th Century"},
				Part: &tune,
			},
		},
		Credits: []domain.PieceCredit{
			{
				Role: domain.CreditRoleLyrics, Attribution: domain.AttributionNamed,
				Certainty: domain.CertaintyCertain, Person: &wren,
			},
			{
				Role: domain.CreditRoleTune, Attribution: domain.AttributionTraditional,
				Certainty: domain.CertaintyDisputed, Part: &tune, Note: new("An old reel."),
			},
		},
		Relations: []domain.PieceRelation{{
			Type:      domain.PieceRelationTypeVariantOf,
			Direction: domain.RelationDirectionIncoming,
			Piece: domain.PieceSummary{
				Slug: "the-brass-kettle", Title: "The Brass Kettle", Kind: domain.PieceKindPoem,
				ConservationStatus: domain.ConservationStatusLowRisk, IsStub: true,
			},
		}},
		Links: []domain.PieceLink{
			{
				Link: domain.Link{
					URL:           testVideoURL,
					Title:         new("At the revel"),
					Description:   new("A recording."),
					Platform:      new("video.example.org"),
					PlaceType:     new(domain.LinkPlaceTypeRecordingPlatform),
					Collection:    &domain.CollectionRef{ID: 12, Name: testCollectionName},
					Locator:       new("p. 12"),
					Liveness:      domain.LivenessOK,
					LastCheckedAt: &checkedAt,
					ArchivedURL:   new("https://archive.example.org/kettle"),
					Roles:         []domain.LinkRole{domain.LinkRolePerformance, domain.LinkRoleLyrics},
				},
				Performances: []domain.PiecePerformance{
					{
						Year: new(int16(2025)), Uploader: new("Northmarch Bards"),
						DescriptionVerbatim: "Sung at the revel",
						Event: &domain.EventSummary{
							Slug: "midwinter-revel", Name: testEventName, Kind: domain.EventKindEvent,
						},
						Performers: []domain.PersonSummary{wren},
					},
					{DescriptionVerbatim: "Sung somewhere"},
				},
			},
			{Link: domain.Link{URL: "https://example.org/kettle", Liveness: domain.LivenessUnknown}},
		},
		ConservationHistory: []domain.ConservationChange{{
			Status: domain.ConservationStatusEndangered, Reason: "Few singers know it.",
			SourceURL: new("https://example.org/assessment"), At: testNow,
		}},
		Preferences: []domain.DocPreference{{Dimension: domain.PreferenceDimensionRecording,
			Value: &domain.PreferenceValue{Choices: []domain.PreferenceChoice{domain.PreferenceChoiceOralOnly},
				Notes: new("Ask first.")},
			SourceURL: "https://example.org/preferences"}},
		ConservationSuggestion: &domain.ConservationSuggestion{Status: domain.ConservationStatusEndangered,
			PlaceCount: 1, Places: []domain.LinkPlaceType{domain.LinkPlaceTypeRecordingPlatform}},
	}
}

const testPieceBody = `{
	"slug": "the-copper-kettle", "title": "The Copper Kettle", "kind": "song", "secondary_kinds": ["dance_music"],
	"conservation_status": "endangered", "is_stub": false, "credit_names": ["Wren of the Vale"],
	"updated_at": "2026-10-01T09:00:00Z", "notes_md": "A drinking song.", "version_notes_md": "Two verses.", "licensing_status": "known",
	"licensing_notes_md": "Free with credit.", "licence": "cc_by_sa", "licence_version": "4.0",
	"historical_source_md": "A broadside.",
	"period_start": 1580, "period_end": 1600,
	"aliases": [{"text": "Kettle Song", "kind": "nickname"}],
	"terms": [
		{"term": {"slug": "drinking", "facet": "topic", "display_name": "Drinking"}, "is_primary": true, "part": null},
		{"term": {"slug": "16th-century", "facet": "origin", "display_name": "16th Century"}, "is_primary": false,
			"part": "tune"}
	],
	"credits": [
		{"role": "lyrics", "attribution": "named", "certainty": "certain", "part": null, "note": null,
			"person": {"slug": "wren-of-the-vale", "name": "Wren of the Vale", "kind": "community_bard",
				"is_stub": false}},
		{"role": "tune", "attribution": "traditional", "certainty": "disputed", "part": "tune",
			"note": "An old reel.", "person": null}
	],
	"relations": [{"type": "variant_of", "direction": "incoming", "piece": {"slug": "the-brass-kettle",
		"title": "The Brass Kettle", "kind": "poem", "secondary_kinds": [], "conservation_status": "low_risk",
		"is_stub": true, "credit_names": []}}],
	"links": [
		{"url": "https://video.example.org/watch/kettle", "title": "At the revel", "description": "A recording.",
			"platform": "video.example.org", "place_type": "recording_platform",
			"collection": {"id": 12, "name": "The Northmarch Songbook"}, "locator": "p. 12", "liveness": "ok",
			"last_checked_at": "2026-10-01T09:00:00Z", "archived_url": "https://archive.example.org/kettle",
			"roles": ["performance", "lyrics"],
			"performances": [
				{"year": 2025, "uploader": "Northmarch Bards", "description_verbatim": "Sung at the revel",
					"event": {"slug": "midwinter-revel", "name": "Midwinter Revel", "kind": "event"},
					"performers": [{"slug": "wren-of-the-vale", "name": "Wren of the Vale",
						"kind": "community_bard", "is_stub": false}]},
				{"year": null, "uploader": null, "description_verbatim": "Sung somewhere", "event": null,
					"performers": []}
			]},
		{"url": "https://example.org/kettle", "title": null, "description": null, "platform": null,
			"place_type": null, "collection": null, "locator": null, "liveness": "unknown",
			"last_checked_at": null, "archived_url": null, "roles": [], "performances": []}
	],
	"conservation_history": [{"conservation_status": "endangered", "reason": "Few singers know it.",
		"source_url": "https://example.org/assessment", "at": "2026-10-31T12:00:00Z"}],
	"preferences": [{"dimension": "recording", "value": {"choices": ["oral_only"], "notes": "Ask first."},
		"source_url": "https://example.org/preferences"}],
	"conservation_suggestion": {"status": "endangered", "place_count": 1, "places": ["recording_platform"]}
}`

// emptyPieceBody is a piece with only its required scalars set: every list is [] and every nullable field null.
const emptyPieceBody = `{
	"slug": "the-copper-kettle", "title": "The Copper Kettle", "kind": "story", "secondary_kinds": [],
	"conservation_status": "unassessed", "is_stub": true, "credit_names": [], "updated_at": "2026-10-01T09:00:00Z",
	"notes_md": null, "version_notes_md": null, "licensing_status": "unknown", "licensing_notes_md": null,
	"licence": null, "licence_version": null, "historical_source_md": null,
	"period_start": null, "period_end": null, "aliases": [], "terms": [], "credits": [], "relations": [],
	"links": [], "conservation_history": [], "preferences": [], "conservation_suggestion": null
}`

func TestPieceGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	emptyPiece := domain.Piece{PieceSummary: domain.PieceSummary{
		Slug: testPieceSlug, Title: testPieceTitle, Kind: domain.PieceKindStory,
		ConservationStatus: domain.ConservationStatusUnassessed, IsStub: true,
	}, UpdatedAt: testRevisionCreatedAt, LicensingStatus: domain.LicensingStatusUnknown}

	cases := []struct {
		name       string
		piece      domain.Piece
		getErr     error
		wantStatus int
		wantBody   string
	}{
		{name: "piece", piece: testPiece(), wantStatus: http.StatusOK, wantBody: testPieceBody},
		{name: emptyListsCase, piece: emptyPiece, wantStatus: http.StatusOK, wantBody: emptyPieceBody},
		{
			name:       notFoundCase,
			getErr:     datasources.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"code":"not_found","message":"piece not found"}`,
		},
		{
			name:       getFailureCase,
			getErr:     errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			getter := mocks.NewPublicPieceGetter(t)
			getter.EXPECT().GetPublicPiece(mock.Anything, testPieceSlug).Return(tc.piece, tc.getErr)

			rec := serveSlug(t, PieceGet{Getter: getter}, "pieces", testPieceSlug, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidSlugCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveSlug(t, PieceGet{Getter: mocks.NewPublicPieceGetter(t)}, "pieces", tc.pathSlug, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
