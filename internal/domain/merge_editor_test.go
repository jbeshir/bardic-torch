package domain

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The slugs of the editor-conflict fixtures. Piece 9 has no slug, as a reference the response cannot name.
const (
	drinkingSongs    = "drinking-songs"
	copperKettle     = "the-copper-kettle"
	seaShanties      = "sea-shanties"
	harbourSongs     = "harbour-songs"
	conflictTermID   = int64(12)
	conflictPieceID  = int64(34)
	unnamedPieceID   = int64(9)
	conflictPersonID = int64(7)
	editorPersonTwo  = "rowan-ashby"
)

func editorConflictSlugs() SlugMap {
	return SlugMap{
		Pieces:  map[int64]string{conflictPieceID: copperKettle},
		Persons: map[int64]string{conflictPersonID: editorPersonTwo},
		Terms:   map[int64]string{conflictTermID: drinkingSongs, 5: seaShanties, 6: harbourSongs},
		Events:  map[int64]string{}, Collections: map[int64]bool{},
	}
}

// assertNoStoredFormat fails if a conflict's path or values name an internal ID field.
func assertNoStoredFormat(t *testing.T, c RebaseConflict) {
	t.Helper()
	for _, v := range []json.RawMessage{c.Base, c.Revision, c.Current} {
		assert.NotContains(t, string(v), `_id"`, "value of %s", c.Path)
		assert.NotContains(t, string(v), `_ids"`, "value of %s", c.Path)
	}
	assert.NotContains(t, string(c.Path), "_id", "path")
}

func TestEditorConflicts(t *testing.T) {
	t.Parallel()

	t.Run("terms", func(t *testing.T) {
		t.Parallel()
		base := validPiece()
		base.Terms = []DocPieceTerm{{TermID: conflictTermID}}
		ours, theirs := base, base
		ours.Terms = []DocPieceTerm{{TermID: conflictTermID, Part: new(PiecePartTune)}}
		theirs.Terms = []DocPieceTerm{{TermID: conflictTermID, IsPrimary: true}}
		_, conflicts := MergeDocuments(base, ours, theirs)
		require.Len(t, conflicts, 1)
		require.Equal(t, FieldPath("terms[12]"), conflicts[0].Path, "stored format inside")

		editor, err := EditorConflicts(EntityTypePiece, conflicts, editorConflictSlugs())
		require.NoError(t, err)
		require.Len(t, editor, 1)
		assert.Equal(t, FieldPath("terms["+drinkingSongs+"]"), editor[0].Path)
		assertNoStoredFormat(t, editor[0])
		for _, v := range []json.RawMessage{editor[0].Base, editor[0].Revision, editor[0].Current} {
			var term EditorPieceTerm
			require.NoError(t, json.Unmarshal(v, &term))
			assert.Equal(t, drinkingSongs, term.Term)
		}
		var current EditorPieceTerm
		require.NoError(t, json.Unmarshal(editor[0].Current, &current))
		assert.True(t, current.IsPrimary)
	})

	t.Run("relations_order_with_an_unnamed_piece", func(t *testing.T) {
		t.Parallel()
		base := validPiece()
		kettle := DocPieceRelation{ToPieceID: conflictPieceID, Type: PieceRelationTypeVariantOf}
		unnamed := DocPieceRelation{ToPieceID: unnamedPieceID, Type: PieceRelationTypeSettingOf}
		other := DocPieceRelation{ToPieceID: 8, Type: PieceRelationTypeSettingOf}
		base.Relations = []DocPieceRelation{kettle, unnamed, other}
		ours, theirs := base, base
		ours.Relations = []DocPieceRelation{unnamed, kettle, other}
		theirs.Relations = []DocPieceRelation{kettle, other, unnamed}
		_, conflicts := MergeDocuments(base, ours, theirs)
		require.Len(t, conflicts, 1)

		editor, err := EditorConflicts(EntityTypePiece, conflicts, editorConflictSlugs())
		require.NoError(t, err)
		require.Len(t, editor, 1)
		assert.Equal(t, FieldPath("relations#order"), editor[0].Path)
		assert.JSONEq(t, `["pending-piece-9:setting_of","`+copperKettle+`:variant_of","pending-piece-8:setting_of"]`,
			string(editor[0].Revision))
		assert.JSONEq(t, `["`+copperKettle+`:variant_of","pending-piece-9:setting_of","pending-piece-8:setting_of"]`,
			string(editor[0].Base))
	})

	t.Run("credits", func(t *testing.T) {
		t.Parallel()
		base := validPiece()
		credit := DocCredit{PersonID: new(conflictPersonID), Role: CreditRoleLyrics, Attribution: AttributionNamed,
			Certainty: CertaintyCertain}
		ours, theirs := base, base
		ours.Credits = []DocCredit{credit}
		theirs.Credits = []DocCredit{{Role: CreditRoleTune, Attribution: AttributionTraditional,
			Certainty: CertaintyCertain}}
		_, conflicts := MergeDocuments(base, ours, theirs)
		require.Len(t, conflicts, 1)

		editor, err := EditorConflicts(EntityTypePiece, conflicts, editorConflictSlugs())
		require.NoError(t, err)
		assert.Equal(t, FieldPath("credits"), editor[0].Path)
		assertNoStoredFormat(t, editor[0])
		var credits []EditorCredit
		require.NoError(t, json.Unmarshal(editor[0].Revision, &credits))
		require.Len(t, credits, 1)
		assert.Equal(t, new(editorPersonTwo), credits[0].Person)
	})

	t.Run("term_parent", func(t *testing.T) {
		t.Parallel()
		base := TermDocument{SchemaVersion: DocumentSchemaVersion, Facet: FacetTopic, Name: "Shanties",
			DisplayName: "Shanties"}
		ours, theirs := base, base
		ours.ParentID, theirs.ParentID = new(int64(5)), new(int64(6))
		_, conflicts := MergeDocuments(base, ours, theirs)
		require.Len(t, conflicts, 1)
		require.Equal(t, FieldPath("parent_id"), conflicts[0].Path, "stored format inside")

		editor, err := EditorConflicts(EntityTypeTerm, conflicts, editorConflictSlugs())
		require.NoError(t, err)
		assert.Equal(t, []RebaseConflict{{Path: "parent", Base: json.RawMessage("null"),
			Revision: json.RawMessage(`"` + seaShanties + `"`), Current: json.RawMessage(`"` + harbourSongs + `"`)}},
			editor)
	})

	t.Run("licensing_group", func(t *testing.T) {
		t.Parallel()
		base := validPiece()
		ours, theirs := base, base
		ours.LicensingNotesMD, theirs.LicensingNotesMD = new("Ours."), new("Theirs.")
		_, conflicts := MergeDocuments(base, ours, theirs)
		require.Len(t, conflicts, 1)

		editor, err := EditorConflicts(EntityTypePiece, conflicts, editorConflictSlugs())
		require.NoError(t, err)
		assert.Equal(t, FieldPath("licensing"), editor[0].Path)
		assert.JSONEq(t, string(conflicts[0].Revision), string(editor[0].Revision), "no reference to convert")
	})

	t.Run("preferences_conflicts", func(t *testing.T) {
		t.Parallel()
		base := validPiece()
		ours, theirs := base, base
		ours.Preferences = []DocPreference{
			preference(PreferenceDimensionRecording, preferenceSource, PreferenceChoiceAskBeforeRecording),
			preference(PreferenceDimensionAdaptations, preferenceSource, PreferenceChoiceAskFirst),
		}
		theirs.Preferences = []DocPreference{
			preference(PreferenceDimensionRecording, preferenceSource, PreferenceChoiceNoRecording),
			preference(PreferenceDimensionAdaptations, preferenceSource, PreferenceChoiceNotAllowed),
		}
		_, conflicts := MergeDocuments(base, ours, theirs)
		require.Len(t, conflicts, 2)

		editor, err := EditorConflicts(EntityTypePiece, conflicts, editorConflictSlugs())
		require.NoError(t, err)
		null := json.RawMessage("null")
		assert.Equal(t, []RebaseConflict{{Path: preferencesPath, Base: null, Revision: null, Current: null}}, editor,
			"one conflict, with no values: the editor format has no preferences")
	})

	t.Run("unknown_type", func(t *testing.T) {
		t.Parallel()
		_, err := EditorConflicts("song", nil, editorConflictSlugs())
		require.ErrorIs(t, err, ErrUnknownDocumentType)
	})

	t.Run("unknown_path", func(t *testing.T) {
		t.Parallel()
		_, err := EditorConflicts(EntityTypeEvent, []RebaseConflict{{Path: pathName}}, editorConflictSlugs())
		require.ErrorIs(t, err, ErrUnknownFieldPath)
	})
}

func TestEditorConflicts_DoesNotChangeTheSlugs(t *testing.T) {
	t.Parallel()
	base := validPiece()
	ours, theirs := base, base
	ours.Credits = []DocCredit{{PersonID: new(int64(99)), Role: CreditRoleLyrics, Attribution: AttributionNamed,
		Certainty: CertaintyCertain}}
	theirs.Credits = []DocCredit{}
	_, conflicts := MergeDocuments(base, ours, theirs)
	slugs := editorConflictSlugs()

	editor, err := EditorConflicts(EntityTypePiece, conflicts, slugs)
	require.NoError(t, err)
	assert.Contains(t, string(editor[0].Revision), `"person":"pending-person-99"`)
	assert.Equal(t, editorConflictSlugs(), slugs)
}

func TestEditorFieldPaths(t *testing.T) {
	t.Parallel()
	base := DefaultPersonDocument()
	base.Name = "Wren"
	doc := base
	doc.AffiliationIDs = []int64{conflictTermID}
	doc.Relations = []DocPersonRelation{{ToPersonID: conflictPersonID, Type: PersonRelationTypeMemberOf}}
	doc.Preferences = []DocPreference{
		preference(PreferenceDimensionRecording, preferenceSource, PreferenceChoiceAskBeforeRecording),
		preference(PreferenceDimensionAdaptations, preferenceSource, PreferenceChoiceAskFirst),
	}
	stored := DiffDocuments(base, doc)
	require.Contains(t, stored, FieldPath("affiliation_ids[12]"), "stored format inside")

	paths, err := EditorFieldPaths(EntityTypePerson, stored, editorConflictSlugs())
	require.NoError(t, err)
	assert.Equal(t, []FieldPath{"affiliations[" + drinkingSongs + "]",
		"relations[" + editorPersonTwo + ":member_of]", preferencesPath}, paths)
	assert.False(t, slices.ContainsFunc(paths, func(p FieldPath) bool { return strings.Contains(string(p), "_id") }))
}
