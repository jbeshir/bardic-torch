//go:build integration

package mysql_test

import (
	"encoding/json"
	"slices"
	"strconv"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/domaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepository_SubmitDraft_ModeratorApproved covers an interactive moderator's submission approved at once,
// with the moderator its reviewer, and keeps what the moderator could not see (the preferences).
func TestRepository_SubmitDraft_ModeratorApproved(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	result := editAndSubmit(t, f, actorRowan, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.NotesMD = new(editedNotes)
	})

	assert.Equal(t, domain.RevisionStatusApproved, result.Status)
	stored := f.stored(t, result.RevisionID)
	assert.Equal(t, statusApproved, stored.status)
	assert.Equal(t, rowanUserID, f.reviewed(t, result.RevisionID).reviewer, "the reviewer is the author")
	assert.Equal(t, result.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
	want := songDocument(f)
	want.NotesMD = new(editedNotes)
	assert.Equal(t, want, f.storedPiece(t, result.RevisionID), "preferences and hidden data kept")
	assert.Equal(t, new(editedNotes), f.publicPiece(t, f.song.Slug).NotesMD)

	again := f.submit(t, actorRowan, result.RevisionID)
	assert.Equal(t, datasources.SubmitResult{RevisionID: result.RevisionID, Status: domain.RevisionStatusApproved,
		Replayed: true}, again, "a replay")

	token := editAndSubmit(t, f, actorRowanToken, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.IsStub = true
	})
	assert.Equal(t, domain.RevisionStatusPending, token.Status, "an API token's submission is pending")
}

// TestRepository_SubmitDraft_ModeratorNewEntity checks that a moderator's new entity is approved at once, its
// placeholder filled in and given its slug.
func TestRepository_SubmitDraft_ModeratorNewEntity(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	draft := f.saveDraft(t, actorRowan, nil, newEventDocument("Winter Revel"))
	result := f.submit(t, actorRowan, draft.Draft.ID)
	assert.Equal(t, domain.RevisionStatusApproved, result.Status)
	event, err := f.repo.GetPublicEvent(t.Context(), "winter-revel")
	require.NoError(t, err)
	assert.Equal(t, "Winter Revel", event.Name)
}

// TestRepository_SubmitDraft_ModeratorStale covers a stale moderator submission merged cleanly and a
// conflicting one refused, the draft still a draft.
func TestRepository_SubmitDraft_ModeratorStale(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	cleanDoc := f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
	cleanDoc.NotesMD = new(editedNotes)
	clean := f.saveDraft(t, actorRowan, &key, cleanDoc)
	other := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.IsStub = true })
	f.mustApprove(t, other.RevisionID)

	result := f.submit(t, actorRowan, clean.Draft.ID)
	assert.Equal(t, domain.RevisionStatusApproved, result.Status)
	stored := f.stored(t, result.RevisionID)
	assert.Equal(t, other.RevisionID, stored.base.Int64, "rebased")
	merged := f.storedPiece(t, result.RevisionID)
	assert.True(t, merged.IsStub)
	assert.Equal(t, new(editedNotes), merged.NotesMD)

	conflictDoc := f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
	conflictDoc.Title = resolvedTitle
	conflict := f.saveDraft(t, actorRowan, &key, conflictDoc)
	theirs := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.Title = "Another Title" })
	f.mustApprove(t, theirs.RevisionID)
	_, err := f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Keys: domaintest.Keys(), Actor: actorRowan,
		DraftID: conflict.Draft.ID, Now: f.tick()})
	require.ErrorIs(t, err, domain.ErrRebaseRequired)
	assert.Equal(t, statusDraft, f.stored(t, conflict.Draft.ID).status, "the draft is still a draft")
}

// TestRepository_SubmitDraft_ModeratorSupersedes covers that a moderator's submission supersedes their own pending
// revision of the entity, made with an API token.
func TestRepository_SubmitDraft_ModeratorSupersedes(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	old := editAndSubmit(t, f, actorRowanToken, key, func(d *domain.PieceDocument) { d.IsStub = true })
	result := editAndSubmit(t, f, actorRowan, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })

	assert.Equal(t, statusWithdrawn, f.stored(t, old.RevisionID).status)
	assert.Equal(t, statusApproved, f.stored(t, result.RevisionID).status)
	assert.True(t, f.storedPiece(t, result.RevisionID).IsStub, "the pending starting point's edit is kept")
}

// TestRepository_SubmitDraft_ModeratorRules covers that sourced fields bind moderators too and the moderator's
// own flags, which need acknowledging.
func TestRepository_SubmitDraft_ModeratorRules(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *personKey(f.ash.Slug)
	doc := f.editable(t, actorRowan, key).Document.(domain.PersonDocument)
	doc.Status = domain.PersonStatusDeceased
	draft := f.saveDraft(t, actorRowan, &key, doc)
	_, err := f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Keys: domaintest.Keys(), Actor: actorRowan, DraftID: draft.Draft.ID,
		Now: f.tick()})
	var problems *domain.DocumentError
	require.ErrorAs(t, err, &problems)
	assert.True(t, slices.ContainsFunc(problems.Problems, func(p domain.DocumentProblem) bool {
		return p.Code == domain.ProblemSourceRequired
	}), "%v", problems.Problems)

	doc = f.editable(t, actorRowan, key).Document.(domain.PersonDocument)
	doc.NotesMD = new("Plays with " + morwenLegal + ".")
	draft = f.saveDraft(t, actorRowan, &key, doc)
	_, err = f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Keys: domaintest.Keys(), Actor: actorRowan, DraftID: draft.Draft.ID,
		Now: f.tick()})
	var unacknowledged *domain.UnacknowledgedFlagsError
	require.ErrorAs(t, err, &unacknowledged)
	assert.Equal(t, domain.FlagHiddenAliasInText, unacknowledged.Flags[0].Code)
	assert.Equal(t, statusDraft, f.stored(t, draft.Draft.ID).status)

	result, err := f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Keys: domaintest.Keys(), Actor: actorRowan,
		DraftID: draft.Draft.ID, AcknowledgedFlags: []domain.FlagCode{domain.FlagHiddenAliasInText}, Now: f.tick()})
	require.NoError(t, err)
	assert.Equal(t, domain.RevisionStatusApproved, result.Status)
}

// TestRepository_SubmitDraft_ModeratorOthersPendingTerm covers, at approval: a document referencing another
// user's pending new term is refused (unknown_reference), even a moderator's, who may see it.
func TestRepository_SubmitDraft_ModeratorOthersPendingTerm(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	term := f.newPending(t, actorAsh, newTermDocument("Laments"))
	key := *pieceKey(f.song.Slug)
	doc := f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
	doc.Terms = append(slices.Clone(doc.Terms), domain.DocPieceTerm{TermID: term.Entity.ID})
	draft := f.saveDraft(t, actorRowan, &key, doc)

	_, err := f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Keys: domaintest.Keys(), Actor: actorRowan, DraftID: draft.Draft.ID,
		Now: f.tick()})
	var problems *domain.DocumentError
	require.ErrorAs(t, err, &problems)
	assert.Contains(t, problems.Problems, domain.DocumentProblem{Path: pathSecondTerm,
		Code: domain.ProblemUnknownReference})
	assert.Equal(t, statusPending, f.stored(t, term.ID).status)
}

func (f *editingFixture) reject(t *testing.T, id int64, note string, ack ...domain.FlagCode) error {
	t.Helper()

	return f.repo.RejectRevision(t.Context(), datasources.RejectRequest{Actor: actorRowan, RevisionID: id,
		ReviewNote: note, AcknowledgedFlags: ack, Now: f.tick()})
}

func TestRepository_RejectRevision(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	result := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.NotesMD = new(editedNotes)
	})

	var unacknowledged *domain.UnacknowledgedFlagsError
	require.ErrorAs(t, f.reject(t, result.RevisionID, "Ask "+morwenLegal), &unacknowledged,
		"a review note holding a hidden alias")
	assert.Equal(t, statusPending, f.stored(t, result.RevisionID).status)

	require.NoError(t, f.reject(t, result.RevisionID, "Not sourced."))
	assert.Equal(t, statusRejected, f.stored(t, result.RevisionID).status)
	r := f.reviewed(t, result.RevisionID)
	assert.Equal(t, []string{rowanUserID, "Not sourced."}, []string{r.reviewer, r.note})
	mine, err := f.repo.GetMyRevision(t.Context(), actorWren, result.RevisionID)
	require.NoError(t, err, "the author still reads it")
	assert.Equal(t, domain.RevisionStatusRejected, mine.Status)
	assert.Equal(t, f.song.RevisionID, f.currentRevision(t, "pieces", f.song.ID))

	require.ErrorIs(t, f.reject(t, result.RevisionID, ""), domain.ErrNotPending)
	acknowledged := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.IsStub = true
	})
	require.NoError(t, f.reject(t, acknowledged.RevisionID, "Ask "+morwenLegal,
		domain.FlagHiddenAliasInReviewNote))
}

// TestRepository_RejectRevision_DeletesPlaceholder checks that rejecting a new piece deletes its placeholder, with
// its child rows, and keeps the revision.
func TestRepository_RejectRevision_DeletesPlaceholder(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	pending := f.newPending(t, actorWren, newPieceDocument(unsungTitle))
	f.c.Exec("INSERT INTO piece_aliases (piece_id, text, kind) VALUES (?, 'Planted', 'alt_title')", pending.Entity.ID)

	require.NoError(t, f.reject(t, pending.ID, ""))
	assert.Zero(t, f.count(t, "SELECT COUNT(*) FROM pieces WHERE id = ?", pending.Entity.ID))
	assert.Zero(t, f.count(t, "SELECT COUNT(*) FROM piece_aliases WHERE piece_id = ?", pending.Entity.ID))
	assert.Equal(t, statusRejected, f.stored(t, pending.ID).status)
	_, err := f.repo.GetMyRevision(t.Context(), actorWren, pending.ID)
	require.NoError(t, err)
}

func (f *editingFixture) rebase(
	t *testing.T, id int64, current *int64, doc domain.AnyDocument, summary *string,
) error {
	t.Helper()

	return f.repo.RebaseRevision(t.Context(), datasources.RebaseRequest{Keys: domaintest.Keys(), Actor: actorRowan, RevisionID: id,
		CurrentRevisionID: current, Document: doc, Sent: domain.AllEditorFieldsSent, Summary: summary,
		Now: f.tick()})
}

// TestRepository_RebaseRevision covers a rebase then an approval, and that the rebase keeps what the author
// submitted and the base they edited from.
func TestRepository_RebaseRevision(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	ours := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.Title = resolvedTitle })
	theirs := editAndSubmit(t, f, actorAsh, key, func(d *domain.PieceDocument) { d.Title = "Another Title" })
	f.mustApprove(t, theirs.RevisionID)
	require.ErrorIs(t, f.approve(t, ours.RevisionID, domain.ApprovalOptions{}), domain.ErrRebaseRequired)
	before := f.stored(t, ours.RevisionID)

	resolved := f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
	resolved.Title = resolvedTitle
	require.Equal(t, songDocument(f).Preferences, resolved.Preferences, "a moderator edits the preferences")
	require.NoError(t, f.rebase(t, ours.RevisionID, &theirs.RevisionID, resolved, new("Resolved against Ash's.")))

	after := f.stored(t, ours.RevisionID)
	assert.Equal(t, statusPending, after.status)
	assert.Equal(t, theirs.RevisionID, after.base.Int64)
	assert.Equal(t, before.submittedBase, after.submittedBase, "the submitted base is kept")
	assert.Equal(t, f.song.RevisionID, after.submittedBase.Int64)
	assert.JSONEq(t, before.submitted, after.submitted, "what the author submitted is kept")
	rebased := f.storedPiece(t, ours.RevisionID)
	assert.Equal(t, resolvedTitle, rebased.Title)
	assert.Equal(t, songDocument(f).Preferences, rebased.Preferences, "the resolved document's preferences")
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM revisions WHERE id = ? AND rebased_by_user_id = ? "+
		"AND rebased_at IS NOT NULL", ours.RevisionID, rowanUserID))
	view, err := f.repo.GetModerationRevision(t.Context(), ours.RevisionID)
	require.NoError(t, err)
	require.NotNil(t, view.RebasedBy, "the moderation view names who rebased it")
	assert.Equal(t, rowanUserID, view.RebasedBy.ID)
	assert.Equal(t, domain.RoleModerator, view.RebasedBy.Role)
	assert.NotNil(t, view.RebasedAt)
	mine, err := f.repo.GetMyRevision(t.Context(), actorWren, ours.RevisionID)
	require.NoError(t, err)
	assert.Equal(t, songTitle, mine.BaseSnapshot.(domain.PieceSnapshot).Title,
		"the author's views compare with the base they edited from")

	f.mustApprove(t, ours.RevisionID)
	assert.Equal(t, resolvedTitle, f.publicPiece(t, f.song.Slug).Title)
	assert.Equal(t, "Resolved against Ash's.", f.reviewed(t, ours.RevisionID).summary)
}

// TestRepository_RebaseRevision_Stale covers: a rebase resolved against a current revision that is no longer
// the entity's.
func TestRepository_RebaseRevision_Stale(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	ours := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.Title = resolvedTitle })
	resolved := f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
	theirs := editAndSubmit(t, f, actorAsh, key, func(d *domain.PieceDocument) { d.IsStub = true })
	f.mustApprove(t, theirs.RevisionID)

	require.ErrorIs(t, f.rebase(t, ours.RevisionID, &f.song.RevisionID, resolved, nil), datasources.ErrStale)
	require.ErrorIs(t, f.rebase(t, ours.RevisionID, nil, resolved, nil), datasources.ErrStale)
	assert.Equal(t, f.song.RevisionID, f.stored(t, ours.RevisionID).base.Int64, "nothing changes")
	require.ErrorIs(t, f.rebase(t, theirs.RevisionID, &theirs.RevisionID, resolved, nil), domain.ErrNotPending)
}

// TestRepository_RebaseRevision_Validated covers that the resolved document is validated against the current one.
func TestRepository_RebaseRevision_Validated(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	ours := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	resolved := f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
	resolved.Licence = new(domain.LicenceCCBY)

	err := f.rebase(t, ours.RevisionID, &f.song.RevisionID, resolved, nil)
	var problems *domain.DocumentError
	require.ErrorAs(t, err, &problems)
	assert.NotEmpty(t, problems.Problems)
	assert.Nil(t, f.storedPiece(t, ours.RevisionID).Licence, "nothing stored")
}

// TestRepository_RebaseRevision_EntityGone checks that a rebase of a revision whose placeholder is gone is
// datasources.ErrEntityGone.
func TestRepository_RebaseRevision_EntityGone(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	pending := f.newPending(t, actorWren, newPieceDocument(unsungTitle))
	f.c.Exec("DELETE FROM pieces WHERE id = ?", pending.Entity.ID)

	require.ErrorIs(t, f.rebase(t, pending.ID, nil, newPieceDocument(unsungTitle), nil), datasources.ErrEntityGone)
}

func (f *editingFixture) revert(t *testing.T, actor domain.Actor, id int64, summary string) (
	datasources.SubmitResult, error,
) {
	t.Helper()

	return f.repo.RevertTo(t.Context(), datasources.RevertRequest{Actor: actor, RevisionID: id, Summary: summary,
		Now: f.tick()})
}

// TestRepository_RevertTo covers reverts to the import revision: a contributor's is pending on the current
// revision, a moderator's approved; a replay returns the revision it created; the current revision is
// datasources.ErrAlreadyCurrent.
func TestRepository_RevertTo(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	edit := editAndSubmit(t, f, actorRowan, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.NotesMD = new(editedNotes)
	})

	_, err := f.revert(t, actorWren, edit.RevisionID, "")
	require.ErrorIs(t, err, datasources.ErrAlreadyCurrent)
	_, err = f.revert(t, actorWren, 999999, "")
	require.ErrorIs(t, err, datasources.ErrNotFound)

	pending, err := f.revert(t, actorWren, f.song.RevisionID, "")
	require.NoError(t, err)
	assert.Equal(t, domain.RevisionStatusPending, pending.Status)
	stored := f.stored(t, pending.RevisionID)
	assert.Equal(t, edit.RevisionID, stored.base.Int64, "based on the current revision")
	assert.Equal(t, edit.RevisionID, stored.submittedBase.Int64)
	assert.Equal(t, songDocument(f), f.storedPiece(t, pending.RevisionID))
	assert.NotContains(t, stored.submitted, songHiddenURL, "the author's document is the projection")
	assert.Equal(t, "Revert to revision "+strconv.FormatInt(f.song.RevisionID, 10)+".", f.reviewed(t, pending.RevisionID).summary)
	again, err := f.revert(t, actorWren, f.song.RevisionID, "")
	require.NoError(t, err)
	assert.Equal(t, datasources.SubmitResult{RevisionID: pending.RevisionID, Status: domain.RevisionStatusPending,
		Replayed: true}, again)

	approved, err := f.revert(t, actorRowan, f.song.RevisionID, "")
	require.NoError(t, err)
	assert.Equal(t, domain.RevisionStatusApproved, approved.Status)
	assert.Equal(t, approved.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
	assert.Equal(t, new(songNotes), f.publicPiece(t, f.song.Slug).NotesMD)
	replayed, err := f.revert(t, actorRowan, f.song.RevisionID, "")
	require.NoError(t, err)
	assert.Equal(t, datasources.SubmitResult{RevisionID: approved.RevisionID, Status: domain.RevisionStatusApproved,
		Replayed: true}, replayed)
}

// TestRepository_RevertTo_HiddenAliasStaysHidden covers: an alias hidden since the target revision stays
// hidden.
func TestRepository_RevertTo_HiddenAliasStaysHidden(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	hidden := editAndSubmit(t, f, actorRowan, *personKey(f.morwen.Slug), func(d *domain.PersonDocument) {
		d.Aliases[0].IsPublic = false
	})
	require.Equal(t, domain.RevisionStatusApproved, hidden.Status)

	result, err := f.revert(t, actorRowan, f.morwen.RevisionID, "Back to the import.")
	require.NoError(t, err)
	assert.Equal(t, 0, f.count(t, "SELECT COUNT(*) FROM person_aliases WHERE person_id = ? AND name = ? "+
		"AND is_public", f.morwen.ID, morwenStage))
	person, err := f.repo.GetPublicPerson(t.Context(), f.morwen.Slug)
	require.NoError(t, err)
	for _, a := range person.Profile.Aliases {
		assert.NotEqual(t, morwenStage, a.Name)
	}
	assert.Equal(t, result.RevisionID, f.currentRevision(t, "persons", f.morwen.ID))
}

// TestRepository_RevertTo_ReasonRequired covers that reverting a conservation status change needs a summary as its
// reason.
func TestRepository_RevertTo_ReasonRequired(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	doc := f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
	doc.Conservation = domain.DocConservation{Status: domain.ConservationStatusEndangered, SourceURL: new(conservedURL)}
	draft := f.saveDraft(t, actorRowan, &key, doc)
	endangered := f.submit(t, actorRowan, draft.Draft.ID)
	doc = f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
	doc.Conservation = domain.DocConservation{Status: domain.ConservationStatusRestricted,
		SourceURL: new("https://example.org/wren/restricted")}
	draft = f.saveDraft(t, actorRowan, &key, doc)
	require.Equal(t, domain.RevisionStatusApproved, f.submit(t, actorRowan, draft.Draft.ID).Status)
	var err error

	for _, actor := range []domain.Actor{actorWren, actorRowan} {
		_, err = f.revert(t, actor, endangered.RevisionID, "")
		var problems *domain.DocumentError
		require.ErrorAs(t, err, &problems, actor.Role)
		assert.Contains(t, problems.Problems, domain.DocumentProblem{Path: "/summary",
			Code: domain.ProblemReasonRequired}, actor.Role)
	}
	result, err := f.revert(t, actorWren, endangered.RevisionID, "Endangered again.")
	require.NoError(t, err)
	assert.Equal(t, domain.RevisionStatusPending, result.Status)
}

// storedFlags are the flags stored with the revision id.
func (f *editingFixture) storedFlags(t *testing.T, id int64) []domain.Flag {
	t.Helper()
	var flags []domain.Flag
	require.NoError(t, json.Unmarshal([]byte(f.stored(t, id).flags), &flags))

	return flags
}

// TestRepository_RebaseRevision_ClearsRestoreFlags checks that a revision whose submission found hidden data in the
// way (a rename to a hidden alias, a public alias equal to a hidden one) can be approved once a moderator rebases
// it: the rebase clears the submission's restore flags, and the hidden alias stays hidden.
func TestRepository_RebaseRevision_ClearsRestoreFlags(t *testing.T) {
	t.Parallel()
	for name, tt := range map[string]struct {
		edit func(*domain.PersonDocument)
		flag domain.FlagCode
	}{
		"RenameToHiddenAlias": {edit: func(d *domain.PersonDocument) { d.Name = morwenLegal },
			flag: domain.FlagHiddenDataConflict},
		"PublicAliasCollision": {edit: func(d *domain.PersonDocument) {
			d.Aliases = append(slices.Clone(d.Aliases), domain.DocPersonAlias{Name: morwenLegal,
				Kind: domain.PersonAliasKindStage, IsPublic: true})
		}, flag: domain.FlagHiddenAliasCollision},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newEditingFixture(t)
			key := *personKey(f.morwen.Slug)
			result := editAndSubmit(t, f, actorWren, key, tt.edit)
			stored := f.storedFlags(t, result.RevisionID)
			require.True(t, slices.ContainsFunc(stored, func(fl domain.Flag) bool { return fl.Code == tt.flag }))
			require.ErrorIs(t, f.approve(t, result.RevisionID, domain.ApprovalOptions{}), domain.ErrHiddenDataConflict)

			resolved := f.editable(t, actorRowan, key).Document.(domain.PersonDocument)
			resolved.Name = "Morwen Renamed"
			require.NoError(t, f.rebase(t, result.RevisionID, &f.morwen.RevisionID, resolved, nil))
			assert.Equal(t, domain.FlagsAfterRebase(stored), f.storedFlags(t, result.RevisionID),
				"the restore's flags are cleared, the others kept")

			f.mustApprove(t, result.RevisionID)
			person, err := f.repo.GetPublicPerson(t.Context(), f.morwen.Slug)
			require.NoError(t, err)
			assert.Equal(t, "Morwen Renamed", person.Name)
			current := f.editable(t, actorRowan, key).Document.(domain.PersonDocument)
			assert.Equal(t, morwenDocument(f).Aliases, current.Aliases, "the hidden alias is still hidden, once")
		})
	}
}

// TestRepository_RevertTo_TargetNotApproved checks that a revert's target must be an approved revision: a revert,
// by a contributor or an interactive moderator, to another user's or their own draft, another user's pending
// revision, a rejected or a withdrawn revision is datasources.ErrNotFound, as for an ID no revision has. It writes
// no revision and leaves the entities' current revisions as they were.
func TestRepository_RevertTo_TargetNotApproved(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	song, morwen := *pieceKey(f.song.Slug), *personKey(f.morwen.Slug)
	withNotes := func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) }

	rejected := editAndSubmit(t, f, actorWren, song, withNotes).RevisionID
	var acknowledged []domain.FlagCode
	for _, flag := range f.storedFlags(t, rejected) {
		acknowledged = append(acknowledged, flag.Code)
	}
	require.NoError(t, f.reject(t, rejected, "Not this time.", acknowledged...))
	withdrawn := editAndSubmit(t, f, actorWren, morwen, func(d *domain.PersonDocument) {
		d.NotesMD = new(editedNotes)
	}).RevisionID
	require.NoError(t, f.repo.WithdrawRevision(t.Context(), actorWren, withdrawn))
	pending := editAndSubmit(t, f, actorAsh, song, withNotes).RevisionID
	draftOf := func(actor domain.Actor, key domain.EntityKey) int64 {
		return f.saveDraft(t, actor, &key, f.editable(t, actor, key).Document).Draft.ID
	}
	ownDraft := map[domain.Role]int64{
		domain.RoleContributor: draftOf(actorWren, song),
		domain.RoleModerator:   draftOf(actorRowan, song),
	}
	othersDraft := draftOf(actorAsh, morwen)
	for id, status := range map[int64]domain.RevisionStatus{
		rejected: domain.RevisionStatusRejected, withdrawn: domain.RevisionStatusWithdrawn,
		pending: domain.RevisionStatusPending, othersDraft: domain.RevisionStatusDraft,
		ownDraft[domain.RoleContributor]: domain.RevisionStatusDraft,
		ownDraft[domain.RoleModerator]:   domain.RevisionStatusDraft,
	} {
		require.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM revisions WHERE id = ? AND status = ?", id, status))
	}

	revisions := f.count(t, "SELECT COUNT(*) FROM revisions")
	currentSong, currentMorwen := f.currentRevision(t, "pieces", f.song.ID), f.currentRevision(t, "persons", f.morwen.ID)
	for _, actor := range []domain.Actor{actorWren, actorRowan} {
		for name, target := range map[string]int64{
			"others_draft": othersDraft, "own_draft": ownDraft[actor.Role], "others_pending": pending,
			"rejected": rejected, "withdrawn": withdrawn,
		} {
			t.Run(string(actor.Role)+"_"+name, func(t *testing.T) {
				_, err := f.revert(t, actor, target, "Back to this one.")
				require.ErrorIs(t, err, datasources.ErrNotFound)
				assert.Equal(t, revisions, f.count(t, "SELECT COUNT(*) FROM revisions"), "a revision was written")
				assert.Equal(t, currentSong, f.currentRevision(t, "pieces", f.song.ID))
				assert.Equal(t, currentMorwen, f.currentRevision(t, "persons", f.morwen.ID))
			})
		}
	}
}
