package controller

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const revisionIDVar = "revision_id"

var (
	testRevisionCreatedAt = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	testKettle            = domain.PieceSummary{
		Slug: testPieceSlug, Title: testPieceTitle, Kind: domain.PieceKindSong,
		ConservationStatus: domain.ConservationStatusEndangered,
	}
	testWren = domain.PersonSummary{Slug: testPersonSlug, Name: testPersonName, Kind: domain.PersonKindCommunityBard}
)

// testPieceSnapshot is a piece snapshot with every field set, and every list one item long.
func testPieceSnapshot() domain.PieceSnapshot {
	return domain.PieceSnapshot{
		Title:                 testPieceTitle,
		Kind:                  domain.PieceKindSong,
		SecondaryKinds:        []domain.PieceKind{domain.PieceKindDanceMusic},
		ConservationStatus:    domain.ConservationStatusEndangered,
		ConservationSourceURL: new("https://example.org/assessment"),
		IsStub:                true,
		NotesMD:               new("Notes"),
		VersionNotesMD:        new("Versions"),
		LicensingStatus:       domain.LicensingStatusKnown,
		LicensingNotesMD:      new("Free to sing."),
		Licence:               new(domain.LicenceCCBY),
		HistoricalSourceMD:    new("A broadside."),
		PeriodStart:           new(int16(1580)),
		PeriodEnd:             new(int16(1600)),
		Aliases:               []domain.PieceAlias{{Text: "Kettle Song", Kind: domain.PieceAliasKindNickname}},
		Terms: []domain.PieceTerm{{
			Term: domain.TermSummary{Slug: "war", Facet: domain.FacetTopic, DisplayName: testTermName}, IsPrimary: true,
		}},
		Credits: []domain.PieceCredit{{
			Role: domain.CreditRoleLyrics, Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain,
			Person: &testWren,
		}},
		Relations: []domain.SnapshotPieceRelation{{Type: domain.PieceRelationTypeVariantOf, Piece: testKettle}},
		Links: []domain.SnapshotPieceLink{{
			SnapshotLink: domain.SnapshotLink{
				URL: testVideoURL, Title: new("At the revel"), Description: new("A recording."),
				Platform: new("video.example.org"), PlaceType: new(domain.LinkPlaceTypeRecordingPlatform),
				Collection: &domain.CollectionRef{ID: 12, Name: testCollectionName}, Locator: new("track 3"),
				Roles: []domain.LinkRole{domain.LinkRolePerformance},
			},
			Performances: []domain.PiecePerformance{{
				Year: new(int16(2025)), Uploader: new("Northmarch Bards"), DescriptionVerbatim: "Sung at the revel",
				Event:      &domain.EventSummary{Slug: testEventSlug, Name: testEventName, Kind: domain.EventKindEvent},
				Performers: []domain.PersonSummary{testWren},
			}},
		}},
	}
}

// testNamedLyricistJSON is the encoding of testPieceSnapshot's credits.
const testNamedLyricistJSON = `[{"role": "lyrics", "attribution": "named", "certainty": "certain", "part": null, "note": null,
	"person": {"slug": "wren-of-the-vale", "name": "Wren of the Vale", "kind": "community_bard", "is_stub": false}}]`

// testPieceSnapshotJSON is testPieceSnapshot's encoding, with title and the encoded credits set.
func testPieceSnapshotJSON(title, credits string) string {
	return fmt.Sprintf(`{"title": %q, "kind": "song", "secondary_kinds": ["dance_music"],
		"conservation_status": "endangered", "conservation_source_url": "https://example.org/assessment",
		"is_stub": true, "notes_md": "Notes", "version_notes_md": "Versions", "licensing_status": "known",
		"licensing_notes_md": "Free to sing.", "licence": "cc_by", "historical_source_md": "A broadside.",
		"period_start": 1580, "period_end": 1600,
		"aliases": [{"text": "Kettle Song", "kind": "nickname"}],
		"terms": [{"term": {"slug": "war", "facet": "topic", "display_name": "War"}, "is_primary": true, "part": null}],
		"credits": %s,
		"relations": [{"type": "variant_of", "piece": {"slug": "the-copper-kettle", "title": "The Copper Kettle",
			"kind": "song", "secondary_kinds": [], "conservation_status": "endangered", "is_stub": false}}],
		"links": [{"url": "https://video.example.org/watch/kettle", "title": "At the revel",
			"description": "A recording.", "platform": "video.example.org", "place_type": "recording_platform",
			"collection": {"id": 12, "name": "The Northmarch Songbook"}, "locator": "track 3",
			"roles": ["performance"],
			"performances": [{"year": 2025, "uploader": "Northmarch Bards", "description_verbatim": "Sung at the revel",
				"event": {"slug": "midwinter-revel", "name": "Midwinter Revel", "kind": "event"},
				"performers": [{"slug": "wren-of-the-vale", "name": "Wren of the Vale", "kind": "community_bard",
					"is_stub": false}]}]}]}`, title, credits)
}

func TestRevisionGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	const rapiers = "Rapiers"

	base := testPieceSnapshot()
	base.Title = "The Kettle"
	base.Credits = nil
	baseID := int64(41)

	personSnapshot := domain.PersonSnapshot{
		Name: testPersonName, Kind: domain.PersonKindCommunityBard, Status: domain.PersonStatusActive,
		Profile: &domain.PersonSnapshotProfile{
			Pronouns: new("she/her"), LicensingStatus: domain.LicensingStatusUnknown,
			Aliases:      []domain.PersonAlias{{Name: "Wren", Kind: domain.PersonAliasKindStage}},
			Roles:        []domain.PersonRole{domain.PersonRoleSinger},
			Affiliations: []domain.TermSummary{{Slug: testAffiliationSlug, Facet: domain.FacetAffiliation, DisplayName: testAffiliationName}},
			Relations:    []domain.SnapshotPersonRelation{{Type: domain.PersonRelationTypeMemberOf, Person: testWren}},
			Links: []domain.SnapshotPersonLink{{
				SnapshotLink: domain.SnapshotLink{URL: "https://example.org/wren"},
				Performances: []domain.SnapshotPersonPerformance{
					{Piece: &testKettle, PiecePerformance: domain.PiecePerformance{DescriptionVerbatim: "The Kettle"}},
					{PiecePerformance: domain.PiecePerformance{DescriptionVerbatim: "A tale"}},
				},
			}},
		},
	}
	statusDate := domain.Date{Time: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}

	cases := []struct {
		name       string
		revision   domain.Revision
		getErr     error
		wantStatus int
		wantBody   string
	}{
		{
			name: "piece_with_base",
			revision: domain.PieceRevision{
				RevisionSummary: domain.RevisionSummary{
					ID: 42, Source: domain.RevisionSourceUser, Summary: "Credit the words.",
					Author: &domain.RevisionAuthor{DisplayName: testDisplayName}, CreatedAt: testRevisionCreatedAt,
				},
				Entity:   testKettle,
				Snapshot: testPieceSnapshot(),
				Base:     &domain.RevisionBase[domain.PieceSnapshot]{ID: baseID, Snapshot: base},
			},
			wantStatus: http.StatusOK,
			wantBody: `{"entity_type": "piece", "id": 42, "source": "user", "summary": "Credit the words.",
				"author": {"display_name": "Morwen"}, "contributors": [], "created_at": "2026-10-01T09:00:00Z",
				"entity": {"slug": "the-copper-kettle", "title": "The Copper Kettle", "kind": "song",
					"secondary_kinds": [], "conservation_status": "endangered", "is_stub": false},
				"base_revision_id": 41,
				"snapshot": ` + testPieceSnapshotJSON(testPieceTitle, testNamedLyricistJSON) + `,
				"base_snapshot": ` + testPieceSnapshotJSON("The Kettle", "[]") + `,
				"changed_fields": ["credits", "title"]}`,
		},
		{
			name: "person_import",
			revision: domain.PersonRevision{
				RevisionSummary: domain.RevisionSummary{
					ID: 43, Source: domain.RevisionSourceImport, Contributors: []string{"Archivist", "Lorekeeper"},
					CreatedAt: testRevisionCreatedAt,
				},
				Entity:   testWren,
				Snapshot: personSnapshot,
			},
			wantStatus: http.StatusOK,
			wantBody: `{"entity_type": "person", "id": 43, "source": "import", "summary": "", "author": null,
				"contributors": ["Archivist", "Lorekeeper"], "created_at": "2026-10-01T09:00:00Z",
				"entity": {"slug": "wren-of-the-vale", "name": "Wren of the Vale", "kind": "community_bard",
					"is_stub": false},
				"base_revision_id": null,
				"snapshot": {"name": "Wren of the Vale", "kind": "community_bard", "status": "active",
					"status_date": null, "status_source_url": null, "is_stub": false,
					"profile": {"pronouns": "she/her", "notes_md": null, "licensing_status": "unknown",
						"licensing_notes_md": null, "aliases": [{"name": "Wren", "kind": "stage"}],
						"roles": ["singer"],
						"affiliations": [{"slug": "northmarch", "facet": "affiliation", "display_name": "Northmarch"}],
						"relations": [{"type": "member_of", "person": {"slug": "wren-of-the-vale",
							"name": "Wren of the Vale", "kind": "community_bard", "is_stub": false}}],
						"links": [{"url": "https://example.org/wren", "title": null, "description": null,
							"platform": null, "place_type": null, "collection": null, "locator": null, "roles": [],
							"performances": [
								{"piece": {"slug": "the-copper-kettle", "title": "The Copper Kettle", "kind": "song",
									"secondary_kinds": [], "conservation_status": "endangered", "is_stub": false},
									"year": null, "uploader": null, "description_verbatim": "The Kettle",
									"event": null, "performers": []},
								{"piece": null, "year": null, "uploader": null, "description_verbatim": "A tale",
									"event": null, "performers": []}]}]}},
				"base_snapshot": null,
				"changed_fields": ["kind", "name", "profile", "status"]}`,
		},
		{
			name: "person_no_page",
			revision: domain.PersonRevision{
				RevisionSummary: domain.RevisionSummary{ID: 44, Source: domain.RevisionSourceImport,
					CreatedAt: testRevisionCreatedAt},
				Entity: domain.PersonSummary{Slug: testPersonSlug, Name: testPersonName,
					Kind: domain.PersonKindCommunityBard, IsStub: true},
				Snapshot: domain.PersonSnapshot{Name: testPersonName, Kind: domain.PersonKindCommunityBard,
					Status: domain.PersonStatusPrefersNoPage, StatusDate: &statusDate,
					StatusSourceURL: new("https://example.org/requests/12"), IsStub: true},
			},
			wantStatus: http.StatusOK,
			wantBody: `{"entity_type": "person", "id": 44, "source": "import", "summary": "", "author": null,
				"contributors": [], "created_at": "2026-10-01T09:00:00Z",
				"entity": {"slug": "wren-of-the-vale", "name": "Wren of the Vale", "kind": "community_bard",
					"is_stub": true},
				"base_revision_id": null,
				"snapshot": {"name": "Wren of the Vale", "kind": "community_bard", "status": "prefers_no_page",
					"status_date": "2026-03-01", "status_source_url": "https://example.org/requests/12",
					"is_stub": true, "profile": null},
				"base_snapshot": null,
				"changed_fields": ["is_stub", "kind", "name", "status", "status_date", "status_source_url"]}`,
		},
		{
			name: "term",
			revision: domain.TermRevision{
				RevisionSummary: domain.RevisionSummary{ID: 45, Source: domain.RevisionSourceUser,
					Author: &domain.RevisionAuthor{DisplayName: testDisplayName}, CreatedAt: testRevisionCreatedAt},
				Entity: domain.TermSummary{Slug: "rapiers", Facet: domain.FacetTopic, DisplayName: rapiers},
				Snapshot: domain.TermSnapshot{
					Facet: domain.FacetTopic, Name: rapiers, DisplayName: rapiers,
					Parent:             &domain.TermSummary{Slug: "war", Facet: domain.FacetTopic, DisplayName: testTermName},
					RelatedAffiliation: &domain.TermSummary{Slug: testAffiliationSlug, Facet: domain.FacetAffiliation, DisplayName: testAffiliationName},
					DescriptionMD:      new("Blades."), Position: 2, Aliases: []string{"Blades"},
				},
			},
			wantStatus: http.StatusOK,
			wantBody: `{"entity_type": "term", "id": 45, "source": "user", "summary": "",
				"author": {"display_name": "Morwen"}, "contributors": [], "created_at": "2026-10-01T09:00:00Z",
				"entity": {"slug": "rapiers", "facet": "topic", "display_name": "Rapiers"},
				"base_revision_id": null,
				"snapshot": {"facet": "topic", "name": "Rapiers", "display_name": "Rapiers",
					"parent": {"slug": "war", "facet": "topic", "display_name": "War"},
					"related_affiliation": {"slug": "northmarch", "facet": "affiliation", "display_name": "Northmarch"},
					"description_md": "Blades.", "position": 2, "aliases": ["Blades"]},
				"base_snapshot": null,
				"changed_fields": ["aliases", "description_md", "display_name", "facet", "name", "parent", "position",
					"related_affiliation"]}`,
		},
		{
			name: "collection",
			revision: domain.CollectionRevision{
				RevisionSummary: domain.RevisionSummary{ID: 46, Source: domain.RevisionSourceImport,
					CreatedAt: testRevisionCreatedAt},
				Entity: domain.CollectionRef{ID: 12, Name: "Volume Two"},
				Snapshot: domain.CollectionSnapshot{
					Name: "Volume Two", URL: new("https://example.org/v2.pdf"), Kind: domain.CollectionKindSongbookPDF,
					DescriptionMD: new("The second volume."),
					Region:        &domain.TermSummary{Slug: testAffiliationSlug, Facet: domain.FacetAffiliation, DisplayName: testAffiliationName},
					Parent: &domain.CollectionSummary{ID: 11, Name: testCollectionName,
						Kind: domain.CollectionKindSongbookWebsite},
					Enumerable: domain.CollectionEnumerableUnknown,
				},
			},
			wantStatus: http.StatusOK,
			wantBody: `{"entity_type": "collection", "id": 46, "source": "import", "summary": "", "author": null,
				"contributors": [], "created_at": "2026-10-01T09:00:00Z",
				"entity": {"id": 12, "name": "Volume Two"},
				"base_revision_id": null,
				"snapshot": {"name": "Volume Two", "url": "https://example.org/v2.pdf", "kind": "songbook_pdf",
					"description_md": "The second volume.",
					"region": {"slug": "northmarch", "facet": "affiliation", "display_name": "Northmarch"},
					"parent": {"id": 11, "name": "The Northmarch Songbook", "kind": "songbook_website", "url": null,
						"is_bardic_resource": false},
					"is_bardic_resource": false, "enumerable": "unknown"},
				"base_snapshot": null,
				"changed_fields": ["description_md", "enumerable", "kind", "name", "parent", "region", "url"]}`,
		},
		{
			name: "event",
			revision: domain.EventRevision{
				RevisionSummary: domain.RevisionSummary{ID: 47, Source: domain.RevisionSourceImport,
					CreatedAt: testRevisionCreatedAt},
				Entity:   domain.EventSummary{Slug: testEventSlug, Name: testEventName, Kind: domain.EventKindEvent},
				Snapshot: domain.EventSnapshot{Name: testEventName, Kind: domain.EventKindEvent},
			},
			wantStatus: http.StatusOK,
			wantBody: `{"entity_type": "event", "id": 47, "source": "import", "summary": "", "author": null,
				"contributors": [], "created_at": "2026-10-01T09:00:00Z",
				"entity": {"slug": "midwinter-revel", "name": "Midwinter Revel", "kind": "event"},
				"base_revision_id": null,
				"snapshot": {"name": "Midwinter Revel", "kind": "event", "description_md": null},
				"base_snapshot": null,
				"changed_fields": ["kind", "name"]}`,
		},
		{
			name:       notFoundCase,
			getErr:     datasources.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"code":"not_found","message":"revision not found"}`,
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

			getter := mocks.NewPublicRevisionGetter(t)
			getter.EXPECT().GetPublicRevision(mock.Anything, int64(42)).Return(tc.revision, tc.getErr)

			rec := serveGet(t, RevisionGet{Getter: getter}, "/v1/revisions/42",
				map[string]string{revisionIDVar: "42"}, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidIDCases(revisionIDVar) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveGet(t, RevisionGet{Getter: mocks.NewPublicRevisionGetter(t)}, "/v1/revisions/1",
				map[string]string{revisionIDVar: tc.pathID}, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}

// TestRevisionGet_UndecodableSnapshotLogged checks that a revision whose snapshot the datasource cannot decode
// gives 500, with the cause logged and kept out of the response.
func TestRevisionGet_UndecodableSnapshotLogged(t *testing.T) {
	t.Parallel()

	getter := mocks.NewPublicRevisionGetter(t)
	getter.EXPECT().GetPublicRevision(mock.Anything, int64(42)).
		Return(nil, fmt.Errorf("snapshot: %w [2]", domain.ErrUnsupportedSchemaVersion))

	var logs bytes.Buffer
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/revisions/42", nil)
	req.SetPathValue(revisionIDVar, "42")
	req = req.WithContext(domain.ContextWithLogger(req.Context(), slog.New(slog.NewTextHandler(&logs, nil))))
	rec := openapitest.New(t).Serve(t, RevisionGet{Getter: getter}, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, internalErrorBody, rec.Body.String())
	assert.Contains(t, logs.String(), "level=ERROR")
	assert.Contains(t, logs.String(), "unsupported document schema version [2]")
}

// TestRevisionFromDomain_UnknownType checks that a revision of a type the API does not show is an error, not a
// panic.
func TestRevisionFromDomain_UnknownType(t *testing.T) {
	t.Parallel()

	_, err := revisionFromDomain(nil)
	require.ErrorIs(t, err, errUnknownRevision)
}

func TestChangedFields(t *testing.T) {
	t.Parallel()

	snapshot := eventSnapshotFromDomain(domain.EventSnapshot{Name: testEventName, Kind: domain.EventKindEvent})
	renamed := snapshot
	renamed.Name = "Midsummer Revel"
	described := snapshot
	described.DescriptionMD = new("A gathering.")
	emptyDescription := snapshot
	emptyDescription.DescriptionMD = new("")

	eventFields := []string{"kind", "name"}
	piece := pieceSnapshotFromDomain(testPieceSnapshot())
	moreAliases := piece
	moreAliases.Aliases = append([]PieceAlias{{Text: "Old Kettle", Kind: domain.PieceAliasKindAltTitle}},
		piece.Aliases...)
	reordered := pieceSnapshotFromDomain(testPieceSnapshot())
	reordered.SecondaryKinds = []domain.PieceKind{domain.PieceKindPoem, domain.PieceKindDanceMusic}
	emptyPiece := pieceSnapshotFromDomain(domain.PieceSnapshot{})

	for _, tc := range []struct {
		name string
		got  func() ([]string, error)
		want []string
	}{
		{name: "no_base", got: func() ([]string, error) { return changedFields(snapshot, nil) },
			want: eventFields},
		{name: "no_base_empty_string_unchanged", got: func() ([]string, error) {
			return changedFields(emptyDescription, nil)
		}, want: eventFields},
		{name: "no_base_empty", got: func() ([]string, error) { return changedFields(emptyPiece, nil) },
			want: []string{}},
		{name: "equal", got: func() ([]string, error) { return changedFields(piece, &piece) }, want: []string{}},
		{name: "scalar", got: func() ([]string, error) { return changedFields(renamed, &snapshot) },
			want: []string{"name"}},
		{name: "null_to_value", got: func() ([]string, error) { return changedFields(described, &snapshot) },
			want: []string{"description_md"}},
		{name: "list_item_added", got: func() ([]string, error) { return changedFields(moreAliases, &piece) },
			want: []string{"aliases"}},
		{name: "list_changed", got: func() ([]string, error) { return changedFields(reordered, &piece) },
			want: []string{"secondary_kinds"}},
		{name: "everything", got: func() ([]string, error) { return changedFields(piece, &emptyPiece) },
			want: []string{
				"aliases", "conservation_source_url", "conservation_status", "credits", "historical_source_md",
				"is_stub", "kind", "licence", "licensing_notes_md", "licensing_status", "links", "notes_md",
				"period_end", "period_start", "relations", "secondary_kinds", "terms", "title", "version_notes_md",
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := tc.got()
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
