//go:build integration

package mysql_test

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/domaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// basePreferences are the two preferences of the song's base in the absent-preferences tests.
func basePreferences() []domain.DocPreference {
	return []domain.DocPreference{
		domaintest.Preference(domain.PreferenceDimensionPerformancePermission, preferencesURL,
			domain.PreferenceChoiceGiven, domain.PreferenceChoiceInformWelcome),
		domaintest.Preference(domain.PreferenceDimensionRecording, preferencesURL, domain.PreferenceChoiceNoRecording),
	}
}

// receivesPreference is whether the draft id's received keys hold a preference's key.
func (f *editingFixture) receivesPreference(t *testing.T, id int64) bool {
	t.Helper()

	return f.count(t, "SELECT COUNT(*) FROM revisions WHERE id = ? AND JSON_SEARCH(JSON_KEYS(received_keys_json), "+
		"'one', '\"preference\"|%') IS NOT NULL", id) > 0
}

// TestRepository_SubmitDraft_AbsentPreferencesKeepBase covers a client built before preferences existed: its
// draft, saved and then updated without them, never received them, so the approved document keeps both of the
// base's preferences.
func TestRepository_SubmitDraft_AbsentPreferencesKeepBase(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	base := songDocument(f)
	base.Title, base.Preferences = "Preference Ballad", basePreferences()
	piece := f.c.Piece(catalogtest.Piece{Doc: base})
	key := *pieceKey(piece.Slug)

	doc := f.editable(t, actorWren, key).Document.(domain.PieceDocument)
	require.Equal(t, basePreferences(), doc.Preferences, "the editable document shows them")
	doc.Preferences = nil
	doc.NotesMD = new("Saved by an older client.")
	saved, err := f.repo.SaveDraft(t.Context(), datasources.SaveDraftRequest{Actor: actorWren,
		EntityType: domain.EntityTypePiece, Entity: &key, Document: doc, Sent: domain.EditorFieldsSent{},
		Summary: draftSummary, Now: f.tick()})
	require.NoError(t, err)
	assert.False(t, f.receivesPreference(t, saved.Draft.ID), "no preference was received")
	assert.Empty(t, saved.Draft.Document.(domain.PieceDocument).Preferences, "nothing is copied into the draft")

	doc.NotesMD = new("Updated by an older client.")
	_, err = f.repo.UpdateDraft(t.Context(), datasources.UpdateDraftRequest{Actor: actorWren, ID: saved.Draft.ID,
		Document: doc, Sent: domain.EditorFieldsSent{}, Summary: draftSummary, Now: f.tick()})
	require.NoError(t, err)
	result := f.submit(t, actorWren, saved.Draft.ID)
	f.mustApprove(t, result.RevisionID)

	stored := f.storedPiece(t, result.RevisionID)
	assert.Equal(t, basePreferences(), stored.Preferences)
	assert.Equal(t, new("Updated by an older client."), stored.NotesMD)
	assert.Equal(t, 2, f.count(t, "SELECT COUNT(*) FROM piece_preferences WHERE piece_id = ?", piece.ID))
}

// TestRepository_UpdateDraft_ReceivedKeysOnlyShrink checks that a draft which received the preferences gives them
// up when an update leaves them out, and does not receive them again when a later update sends them.
func TestRepository_UpdateDraft_ReceivedKeysOnlyShrink(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	doc := f.editable(t, actorWren, key).Document.(domain.PieceDocument)
	saved := f.saveDraft(t, actorWren, &key, doc)
	require.True(t, f.receivesPreference(t, saved.Draft.ID))
	update := func(sent domain.EditorFieldsSent) {
		_, err := f.repo.UpdateDraft(t.Context(), datasources.UpdateDraftRequest{Actor: actorWren,
			ID: saved.Draft.ID, Document: doc, Sent: sent, Summary: draftSummary, Now: f.tick()})
		require.NoError(t, err)
	}

	update(domain.EditorFieldsSent{})
	assert.False(t, f.receivesPreference(t, saved.Draft.ID), "given up")
	update(domain.AllEditorFieldsSent())
	assert.False(t, f.receivesPreference(t, saved.Draft.ID), "never received again")
	assert.Contains(t, f.stored(t, saved.Draft.ID).receivedKeys, songLyricsURL, "the other keys are kept")
}

// TestRepository_SubmitDraft_PreferenceRoundTrip checks that a contributor's and a moderator's preference edits,
// sent in any order, are stored in canonical order and written to the preference rows once approved.
func TestRepository_SubmitDraft_PreferenceRoundTrip(t *testing.T) {
	t.Parallel()

	for _, actor := range []domain.Actor{actorWren, actorRowan} {
		t.Run(string(actor.Role), func(t *testing.T) {
			t.Parallel()
			f := newEditingFixture(t)
			key := *pieceKey(f.song.Slug)
			doc := f.editable(t, actor, key).Document.(domain.PieceDocument)
			doc.Preferences = []domain.DocPreference{
				domaintest.Preference(domain.PreferenceDimensionRecording, preferencesURL,
					domain.PreferenceChoiceAskBeforeRecording, domain.PreferenceChoiceOralOnly),
				domaintest.Preference(domain.PreferenceDimensionPerformancePermission, preferencesURL,
					domain.PreferenceChoiceInformWelcome, domain.PreferenceChoiceGiven),
			}
			draft := f.saveDraft(t, actor, &key, domain.CanonicalAnyDocument(doc))
			result := f.submit(t, actor, draft.Draft.ID)
			if actor.Role != domain.RoleModerator {
				f.mustApprove(t, result.RevisionID)
			}

			want := []domain.DocPreference{
				domaintest.Preference(domain.PreferenceDimensionPerformancePermission, preferencesURL,
					domain.PreferenceChoiceGiven, domain.PreferenceChoiceInformWelcome),
				domaintest.Preference(domain.PreferenceDimensionRecording, preferencesURL,
					domain.PreferenceChoiceOralOnly, domain.PreferenceChoiceAskBeforeRecording),
			}
			assert.Equal(t, want, f.storedPiece(t, result.RevisionID).Preferences)
			assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM piece_preferences WHERE piece_id = ? AND "+
				"dimension = 'recording' AND JSON_CONTAINS(value_json, '\"oral_only\"', '$.choices')", f.song.ID))
			assert.Equal(t, 2, f.count(t, "SELECT COUNT(*) FROM piece_preferences WHERE piece_id = ?", f.song.ID))
		})
	}
}

// TestRepository_ApproveRevision_PreferenceConflictNeedsRebase checks that two edits of the same preference
// conflict at the dimension's path, so the later approval needs a rebase, while an edit of another dimension
// merges.
func TestRepository_ApproveRevision_PreferenceConflictNeedsRebase(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	recording := func(choice domain.PreferenceChoice) func(*domain.PieceDocument) {
		return func(d *domain.PieceDocument) {
			d.Preferences = []domain.DocPreference{
				domaintest.Preference(domain.PreferenceDimensionRecording, preferencesURL, choice),
			}
		}
	}
	first := editAndSubmit(t, f, actorWren, key, recording(domain.PreferenceChoiceAskBeforeUpload))
	second := editAndSubmit(t, f, actorAsh, key, recording(domain.PreferenceChoiceNotifyAfter))
	other := editAndSubmit(t, f, actorRowanToken, key, func(d *domain.PieceDocument) {
		d.Preferences = append(d.Preferences, domaintest.Preference(domain.PreferenceDimensionAdaptations,
			preferencesURL, domain.PreferenceChoiceAskFirst))
	})
	f.mustApprove(t, first.RevisionID)

	require.ErrorIs(t, f.approve(t, second.RevisionID, domain.ApprovalOptions{}), domain.ErrRebaseRequired)
	var paths []domain.FieldPath
	for _, c := range f.moderationRevision(t, second.RevisionID).Rebase.Conflicts {
		paths = append(paths, c.Path)
	}
	assert.Equal(t, []domain.FieldPath{"preferences[recording]"}, paths)

	f.mustApprove(t, other.RevisionID)
	merged := f.storedPiece(t, other.RevisionID).Preferences
	assert.Equal(t, []domain.DocPreference{
		domaintest.Preference(domain.PreferenceDimensionAdaptations, preferencesURL, domain.PreferenceChoiceAskFirst),
		domaintest.Preference(domain.PreferenceDimensionRecording, preferencesURL,
			domain.PreferenceChoiceAskBeforeUpload),
	}, merged, "the other dimension merges with the first approval's")
}

// TestRepository_GetPublicRevision_HiddenSourcePreferenceDropped checks that a snapshot view leaves out a
// preference whose source is a hidden link, in any spelling, or does not normalize, and shows the others.
func TestRepository_GetPublicRevision_HiddenSourcePreferenceDropped(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	shown := domaintest.Preference(domain.PreferenceDimensionRecording, preferencesURL,
		domain.PreferenceChoiceNoRecording)
	doc := songDocument(f)
	doc.Title = "Hidden Source Ballad"
	doc.Preferences = []domain.DocPreference{
		domaintest.Preference(domain.PreferenceDimensionPerformancePermission, "HTTPS://EXAMPLE.org/wren/hidden",
			domain.PreferenceChoiceGiven),
		domaintest.Preference(domain.PreferenceDimensionAttribution, "https://exa mple.org/prefs",
			domain.PreferenceChoiceSCAName),
		shown,
	}
	piece := f.c.Piece(catalogtest.Piece{Doc: doc})

	snapshot := snapshotOf[domain.PieceRevision](t, f, piece.RevisionID).Snapshot
	assert.Equal(t, []domain.DocPreference{shown}, snapshot.Preferences)
}
