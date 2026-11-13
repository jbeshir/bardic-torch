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

var testWren = domain.PersonSummary{Slug: testPersonSlug, Name: testPersonName, Kind: domain.PersonKindCommunityBard}

// testPieceSnapshot is a piece snapshot with every field set, and every list one item long.
func testPieceSnapshot() domain.PieceSnapshot {
	return domain.PieceSnapshot{
		Title:              testPieceTitle,
		Kind:               domain.PieceKindSong,
		ConservationStatus: domain.ConservationStatusEndangered,
		IsStub:             true,
		NotesMD:            new("Notes"),
		VersionNotesMD:     new("Versions"),
		LicensingStatus:    domain.LicensingStatusKnown,
		LicensingNotesMD:   new("Free to sing."),
		Aliases:            []domain.PieceAlias{{Text: "Kettle Song", Kind: domain.PieceAliasKindSubtitle}},
		Terms:              []domain.TermSummary{{Slug: "war", Facet: domain.FacetTopic, DisplayName: testTermName}},
		Credits: []domain.PieceCredit{{
			Role: domain.CreditRoleLyrics, Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain,
			Person: &testWren,
		}},
		Links: []domain.SnapshotPieceLink{{
			SnapshotLink: domain.SnapshotLink{
				URL: testVideoURL, Title: new("At the revel"), Description: new("A recording."),
				Platform: new("video.example.org"), PlaceType: new(domain.LinkPlaceTypeRecordingPlatform),
				Collection: &domain.CollectionRef{ID: 12, Name: testCollectionName}, Locator: new("track 3"),
				Roles: []domain.LinkRole{domain.LinkRolePerformance},
			},
			Performances: []domain.PiecePerformance{{
				Year: new(int16(2025)), Uploader: new("Northmarch Bards"), DescriptionVerbatim: "Sung at the revel",
				Event:      &domain.EventSummary{Slug: testEventSlug, Name: testEventName},
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

	snapshot := eventSnapshotFromDomain(domain.EventSnapshot{Name: testEventName})
	renamed := snapshot
	renamed.Name = "Midsummer Revel"

	piece := pieceSnapshotFromDomain(testPieceSnapshot())
	withoutNotes := piece
	withoutNotes.NotesMD = nil
	moreAliases := piece
	moreAliases.Aliases = append([]PieceAlias{{Text: "Old Kettle", Kind: domain.PieceAliasKindAltTitle}},
		piece.Aliases...)
	retagged := piece
	retagged.Terms = []TermSummary{{Slug: "peace", Facet: domain.FacetTopic, DisplayName: "Peace"}}
	emptyPiece := pieceSnapshotFromDomain(domain.PieceSnapshot{})
	emptyNotes := emptyPiece
	emptyNotes.NotesMD = new("")

	for _, tc := range []struct {
		name string
		got  func() ([]string, error)
		want []string
	}{
		{name: "no_base", got: func() ([]string, error) { return changedFields(snapshot, nil) },
			want: []string{"name"}},
		{name: "no_base_empty_string_unchanged", got: func() ([]string, error) {
			return changedFields(emptyNotes, nil)
		}, want: []string{}},
		{name: "no_base_empty", got: func() ([]string, error) { return changedFields(emptyPiece, nil) },
			want: []string{}},
		{name: "equal", got: func() ([]string, error) { return changedFields(piece, &piece) }, want: []string{}},
		{name: "scalar", got: func() ([]string, error) { return changedFields(renamed, &snapshot) },
			want: []string{"name"}},
		{name: "null_to_value", got: func() ([]string, error) { return changedFields(piece, &withoutNotes) },
			want: []string{"notes_md"}},
		{name: "list_item_added", got: func() ([]string, error) { return changedFields(moreAliases, &piece) },
			want: []string{"aliases"}},
		{name: "list_changed", got: func() ([]string, error) { return changedFields(retagged, &piece) },
			want: []string{"terms"}},
		{name: "everything", got: func() ([]string, error) { return changedFields(piece, &emptyPiece) },
			want: []string{
				"aliases", "conservation_status", "credits", "is_stub", "kind", "licensing_notes_md",
				"licensing_status", "links", "notes_md", "terms", "title", "version_notes_md",
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
