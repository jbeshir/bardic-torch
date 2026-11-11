package controller

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	revisionIDVar      = "revision_id"
	testPieceTitle     = "The Copper Kettle"
	testPieceSlug      = "the-copper-kettle"
	testPersonName     = "Wren of the Vale"
	testPersonSlug     = "wren-of-the-vale"
	testTermName       = "War"
	testEventName      = "Midwinter Revel"
	testEventSlug      = "midwinter-revel"
	testCollectionName = "The Northmarch Songbook"
	testVideoURL       = "https://video.example.org/watch/kettle"
)

var (
	testKettle = domain.PieceSummary{
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

// TestRevisionGet_UndecodableSnapshotLogged checks that a revision whose snapshot the datasource cannot decode
// gives 500, with the cause logged and kept out of the response.
func TestRevisionGet_UndecodableSnapshotLogged(t *testing.T) {
	t.Parallel()

	repo := &fakerepo.Repo{Err: fmt.Errorf("snapshot: %w [2]", domain.ErrUnsupportedSchemaVersion)}

	var logs bytes.Buffer
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/revisions/42", nil)
	req.SetPathValue(revisionIDVar, "42")
	req = req.WithContext(domain.ContextWithLogger(req.Context(), slog.New(slog.NewTextHandler(&logs, nil))))
	rec := openapitest.New(t).Serve(t, RevisionGet{Catalogue: repo}, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, internalErrorBody, rec.Body.String())
	assert.Contains(t, logs.String(), "level=ERROR")
	assert.Contains(t, logs.String(), "unsupported document schema version [2]")
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
