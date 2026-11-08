package controller

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// editorDocumentJSON is the editor format of a valid piece or person document as a map, with edit applied:
// fields set, or removed when set to nil.
func editorDocumentJSON(t *testing.T, entityType domain.EntityType, edit map[string]any) json.RawMessage {
	t.Helper()
	var doc any = domain.ToEditorPiece(domain.PieceDocument{Title: testPieceTitle, Kind: domain.PieceKindSong,
		LicensingStatus: domain.LicensingStatusUnknown,
		Conservation:    domain.DocConservation{Status: domain.ConservationStatusUnassessed}}, domain.SlugMap{})
	if entityType == domain.EntityTypePerson {
		doc = domain.ToEditorPerson(domain.PersonDocument{Name: "Morwen", Kind: domain.PersonKindCommunityBard,
			Status: domain.PersonStatusActive, LicensingStatus: domain.LicensingStatusUnknown}, domain.SlugMap{})
	}
	data, err := json.Marshal(doc)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	for k, v := range edit {
		if v == nil {
			delete(fields, k)
			continue
		}
		fields[k] = v
	}
	out, err := json.Marshal(fields)
	require.NoError(t, err)

	return out
}

// withoutPreferences is what a document that carried everything but its preferences sent.
var withoutPreferences = domain.EditorFieldsSent{LicenceVersion: true}

// The JSON of the decoding tests' preferences.
const (
	keyPreferences     = "preferences"
	keyLicenceVersion  = "licence_version"
	keyLicence         = "licence"
	keyLicensingStatus = "licensing_status"
	testPrefsSource    = "https://x.org"
)

// preferenceJSON is a recording preference with value and the fields of extra.
func preferenceJSON(value any, extra map[string]any) map[string]any {
	p := map[string]any{"dimension": "recording", "source_url": testPrefsSource, "value": value}
	maps.Copy(p, extra)

	return p
}

// valueJSON is a preference value with choices and notes, and the fields of extra.
func valueJSON(choices, notes any, extra map[string]any) map[string]any {
	v := map[string]any{"choices": choices, "notes": notes}
	maps.Copy(v, extra)

	return v
}

// jsonNull sets a field to JSON null in editorDocumentJSON.
var jsonNull = json.RawMessage("null")

func TestDecodeEditorDocument_FieldsSent(t *testing.T) {
	t.Parallel()

	preferences := []any{preferenceJSON(valueJSON([]string{"ask_before_recording"}, nil, nil), nil)}
	tests := []struct {
		name       string
		entityType domain.EntityType
		edit       map[string]any
		want       domain.EditorFieldsSent
		wantPrefs  int
	}{
		{name: "piece with preferences", entityType: domain.EntityTypePiece,
			edit: map[string]any{keyPreferences: preferences}, want: domain.AllEditorFieldsSent, wantPrefs: 1},
		{name: "piece with no preferences", entityType: domain.EntityTypePiece,
			edit: map[string]any{keyPreferences: []any{}}, want: domain.AllEditorFieldsSent},
		{name: "piece without the field", entityType: domain.EntityTypePiece,
			edit: map[string]any{keyPreferences: nil}, want: withoutPreferences},
		{name: "piece with null", entityType: domain.EntityTypePiece,
			edit: map[string]any{keyPreferences: jsonNull}, want: withoutPreferences},
		{name: "piece without a licence version", entityType: domain.EntityTypePiece,
			edit: map[string]any{keyLicenceVersion: nil}, want: domain.EditorFieldsSent{Preferences: true}},
		{name: "piece with a licence version", entityType: domain.EntityTypePiece,
			edit: map[string]any{keyLicenceVersion: "4.0", keyLicence: "cc_by", keyLicensingStatus: "known"},
			want: domain.AllEditorFieldsSent},
		{name: "piece with a null licence version", entityType: domain.EntityTypePiece,
			edit: map[string]any{keyLicenceVersion: jsonNull}, want: domain.AllEditorFieldsSent},
		{name: "person with preferences", entityType: domain.EntityTypePerson,
			edit: map[string]any{keyPreferences: preferences}, want: domain.AllEditorFieldsSent, wantPrefs: 1},
		{name: "person without the field", entityType: domain.EntityTypePerson,
			edit: map[string]any{keyPreferences: nil}, want: withoutPreferences},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			doc, sent, ok := editorDocumentToDomain(t.Context(), rec, tt.entityType,
				editorDocumentJSON(t, tt.entityType, tt.edit))
			require.True(t, ok, rec.Body.String())
			assert.Equal(t, tt.want, sent)
			var got []domain.DocPreference
			switch d := doc.(type) {
			case domain.EditorPieceDocument:
				got = d.Preferences
			case domain.EditorPersonDocument:
				got = d.Preferences
			}
			assert.Len(t, got, tt.wantPrefs)
		})
	}
	t.Run("a document type without the fields sends them all", func(t *testing.T) {
		t.Parallel()
		_, sent, ok := editorDocumentToDomain(t.Context(), httptest.NewRecorder(), domain.EntityTypeEvent,
			json.RawMessage(testEditorEventJSON))
		require.True(t, ok)
		assert.Equal(t, domain.AllEditorFieldsSent, sent)
	})
	t.Run("an unknown field is still 400", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		_, _, ok := editorDocumentToDomain(t.Context(), rec, domain.EntityTypePiece,
			editorDocumentJSON(t, domain.EntityTypePiece, map[string]any{keyPreferences: nil, "licence_url": "x"}))
		assert.False(t, ok)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.JSONEq(t, `{"code":"invalid_request","message":"field document has an unknown field"}`,
			rec.Body.String())
	})
}

func TestDecodeEditorDocument_PreferenceShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		pref   map[string]any
		wantOK bool
	}{
		{name: "closed value", wantOK: true, pref: preferenceJSON(valueJSON([]string{}, "x", nil), nil)},
		{name: "null value decodes, for validation to refuse", wantOK: true, pref: preferenceJSON(nil, nil)},
		{name: "null choices decode, for validation to refuse", wantOK: true,
			pref: preferenceJSON(valueJSON(nil, nil, nil), nil)},
		{name: "unknown field in value",
			pref: preferenceJSON(valueJSON([]string{}, nil, map[string]any{"extra": 1}), nil)},
		{name: "unknown field in the preference",
			pref: preferenceJSON(valueJSON([]string{}, nil, nil), map[string]any{"note": "x"})},
		{name: "choices not a list", pref: preferenceJSON(valueJSON("given", nil, nil), nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			_, _, ok := editorDocumentToDomain(t.Context(), rec, domain.EntityTypePerson,
				editorDocumentJSON(t, domain.EntityTypePerson, map[string]any{keyPreferences: []any{tt.pref}}))
			assert.Equal(t, tt.wantOK, ok, rec.Body.String())
			if !tt.wantOK {
				assert.Equal(t, http.StatusBadRequest, rec.Code)
				assert.Contains(t, rec.Body.String(), `"code":"invalid_request"`)
				assert.NotContains(t, rec.Body.String(), testPrefsSource, "a value is never echoed")
			}
		})
	}
}

// TestDraftCreate_DocumentWithoutPreferences checks, against the spec, that a piece document without preferences
// (from a client built before they existed) is accepted and reaches the command as not having sent them.
func TestDraftCreate_DocumentWithoutPreferences(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		edit map[string]any
		want domain.EditorFieldsSent
	}{
		{name: "without", edit: map[string]any{keyPreferences: nil}, want: withoutPreferences},
		{name: "with", edit: map[string]any{}, want: domain.AllEditorFieldsSent},
	} {
		body := `{"entity_type":"piece","entity":null,"base_revision_id":null,"document":` +
			string(editorDocumentJSON(t, domain.EntityTypePiece, tc.edit)) + `,"summary":""}`
		runRevisionCases(t, []revisionCase{{name: tc.name, method: http.MethodPost, target: draftsTarget, body: body,
			wantStatus: http.StatusCreated, wantBody: testDraftBody}},
			func(t *testing.T, _ revisionCase) http.Handler {
				return DraftCreate{Command: commandFunc[command.SaveDraftRequest, command.SavedDraft](
					func(_ context.Context, req command.SaveDraftRequest) (command.SavedDraft, error) {
						assert.Equal(t, tc.want, req.Sent)
						return command.SavedDraft{DraftResult: command.DraftResult{Draft: testDraft}, Created: true}, nil
					})}
			})
	}
}

// TestDecodeEditorDocument_AbsentLicenceVersionKeepsBase checks that a piece document from a client built before
// licence versions existed decodes, reports the version as not sent, and so keeps the base's (domain.RestoreDocument),
// while a null version is sent and clears it.
func TestDecodeEditorDocument_AbsentLicenceVersionKeepsBase(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		edit     map[string]any
		wantSent bool
	}{
		{name: "absent", edit: map[string]any{keyLicenceVersion: nil}},
		{name: "null", edit: map[string]any{keyLicenceVersion: jsonNull}, wantSent: true},
		{name: "set", edit: map[string]any{keyLicenceVersion: "3.0", keyLicence: "cc_by", keyLicensingStatus: "known"},
			wantSent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			doc, sent, ok := editorDocumentToDomain(t.Context(), rec, domain.EntityTypePiece,
				editorDocumentJSON(t, domain.EntityTypePiece, tc.edit))
			require.True(t, ok, rec.Body.String())
			assert.Equal(t, tc.wantSent, sent.LicenceVersion)
			assert.True(t, sent.Preferences)
			piece, isPiece := doc.(domain.EditorPieceDocument)
			require.True(t, isPiece)
			if tc.name == "set" {
				assert.Equal(t, new(domain.LicenceVersion30), piece.LicenceVersion)
			} else {
				assert.Nil(t, piece.LicenceVersion)
			}
		})
	}
}
