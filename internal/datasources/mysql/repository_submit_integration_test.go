//go:build integration

package mysql_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepository_SubmitDraft_Pending(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	result := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	assert.Equal(t, domain.RevisionStatusPending, result.Status)
	assert.False(t, result.Replayed)

	stored := f.stored(t, result.RevisionID)
	assert.Equal(t, "pending", stored.status)
	full, err := domain.DecodeDocument[domain.PieceDocument]([]byte(stored.snapshot))
	require.NoError(t, err)
	want := songDocument(f)
	want.NotesMD = new(editedNotes)
	assert.Equal(t, want, full, "the full document: the withheld credit, performer, link and preferences restored")

	submitted, err := domain.DecodeDocument[domain.PieceDocument]([]byte(stored.submitted))
	require.NoError(t, err)
	wantSubmitted := withheldSong(f)
	wantSubmitted.NotesMD = new(editedNotes)
	assert.Equal(t, wantSubmitted, submitted, "the author's document as submitted")
	assert.Equal(t, f.song.RevisionID, stored.base.Int64)
	assert.Equal(t, f.song.RevisionID, stored.submittedBase.Int64)
	assert.JSONEq(t, `[]`, stored.flags)
}

// TestRepository_SubmitDraft_ImportedHiddenAlias checks that a contributor's submission of an imported person
// keeps the hidden alias in the stored document, and that the author's document as submitted never holds it.
func TestRepository_SubmitDraft_ImportedHiddenAlias(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	result := editAndSubmit(t, f, actorWren, *personKey(f.morwen.Slug), func(d *domain.PersonDocument) {
		d.NotesMD = new("A singer of wren songs.")
	})

	stored := f.stored(t, result.RevisionID)
	assert.Contains(t, stored.snapshot, morwenLegal)
	assert.Contains(t, stored.snapshot, morwenHidden)
	assert.NotContains(t, stored.submitted, morwenLegal)
	assert.NotContains(t, stored.submitted, morwenHidden)
	assert.NotContains(t, stored.flags, morwenLegal)
}

func TestRepository_SubmitDraft_Replay(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	draft := f.saveDraft(t, actorWren, nil, newPieceDocument(unsungTitle))
	first := f.submit(t, actorWren, draft.Draft.ID)
	before := f.stored(t, first.RevisionID)

	again := f.submit(t, actorWren, draft.Draft.ID)

	assert.Equal(t, datasources.SubmitResult{RevisionID: first.RevisionID, Status: domain.RevisionStatusPending,
		Replayed: true}, again)
	assert.Equal(t, before, f.stored(t, first.RevisionID), "the revision is unchanged")
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM pieces WHERE current_revision_id IS NULL"),
		"one placeholder")

	_, err := f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Actor: actorAsh, DraftID: draft.Draft.ID,
		Now: f.tick()})
	require.ErrorIs(t, err, datasources.ErrNotFound, "another user's revision")
	require.NoError(t, f.repo.WithdrawRevision(t.Context(), actorWren, first.RevisionID))
	_, err = f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Actor: actorWren, DraftID: draft.Draft.ID,
		Now: f.tick()})
	require.ErrorIs(t, err, domain.ErrNotDraft, "a withdrawn revision")
}

func TestRepository_SubmitDraft_Supersede(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	first := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	second := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) {
		d.NotesMD = new("Edited again.")
	})

	assert.Equal(t, "withdrawn", f.stored(t, first.RevisionID).status)
	assert.Equal(t, "pending", f.stored(t, second.RevisionID).status)
	assert.Equal(t, f.song.RevisionID, f.stored(t, second.RevisionID).submittedBase.Int64,
		"the pending starting point kept its submitted base")

	// Another user's pending revision of the entity is not superseded.
	other := editAndSubmit(t, f, actorAsh, key, func(d *domain.PieceDocument) { d.IsStub = true })
	assert.Equal(t, "pending", f.stored(t, second.RevisionID).status)
	assert.Equal(t, "pending", f.stored(t, other.RevisionID).status)
}

func TestRepository_SubmitDraft_PendingLimit(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	for i := 1; i < domain.MaxOpenPendingRevisions; i++ {
		f.submit(t, actorWren, f.saveDraft(t, actorWren, nil, newEventDocument(fmt.Sprintf("Revel %d", i))).Draft.ID)
	}
	extra := f.saveDraft(t, actorWren, nil, newEventDocument("One revel too many"))

	_, err := f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Actor: actorWren, DraftID: extra.Draft.ID,
		Now: f.tick()})
	require.ErrorIs(t, err, datasources.ErrPendingLimitReached)
	assert.Equal(t, "draft", f.stored(t, extra.Draft.ID).status, "nothing was written")
	assert.Equal(t, domain.MaxOpenPendingRevisions, f.count(t,
		"SELECT COUNT(*) FROM events WHERE current_revision_id IS NULL")+1, "no placeholder for the refused one")

	// Superseding does not count: the 31st submission of an entity with a pending revision replaces it.
	result := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new("Again.") })
	assert.Equal(t, domain.RevisionStatusPending, result.Status)
	assert.Equal(t, domain.MaxOpenPendingRevisions, f.count(t,
		"SELECT COUNT(*) FROM revisions WHERE author_user_id = ? AND status = 'pending'", wrenUserID))

	// Another user's count is their own.
	f.submit(t, actorAsh, f.saveDraft(t, actorAsh, nil, newEventDocument("Ash's revel")).Draft.ID)
}

// TestRepository_SubmitDraft_StaleBase checks R48: a contributor's submission on a base that is no longer current
// is stored on that base, merged with nothing.
func TestRepository_SubmitDraft_StaleBase(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	doc := f.editable(t, actorWren, key).Document.(domain.PieceDocument)
	doc.NotesMD = new(editedNotes)
	draft := f.saveDraft(t, actorWren, &key, doc)
	current := songDocument(f)
	current.Title = "The Wren's Song Retitled"
	moved := f.c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: f.song.ID,
		Snapshot: current, BaseRevisionID: f.song.RevisionID})
	f.c.Exec("UPDATE pieces SET current_revision_id = ? WHERE id = ?", moved, f.song.ID)

	result := f.submit(t, actorWren, draft.Draft.ID)

	stored := f.stored(t, result.RevisionID)
	assert.Equal(t, f.song.RevisionID, stored.base.Int64)
	assert.Equal(t, f.song.RevisionID, stored.submittedBase.Int64)
	full, err := domain.DecodeDocument[domain.PieceDocument]([]byte(stored.snapshot))
	require.NoError(t, err)
	assert.Equal(t, songTitle, full.Title, "nothing merged from the current document")
	assert.Equal(t, editedNotes, *full.NotesMD)
}

// TestRepository_SubmitDraft_BaseNotApproved checks R19: bases are approved revisions, so a draft whose base is not
// one is a broken invariant, an internal error, never a caller's error.
func TestRepository_SubmitDraft_BaseNotApproved(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := pieceKey(f.song.Slug)
	draft := f.saveDraft(t, actorWren, key, f.editable(t, actorWren, *key).Document)
	pending := f.c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: f.song.ID,
		Snapshot: songDocument(f), Status: domain.RevisionStatusRejected, Source: domain.RevisionSourceUser})
	f.c.Exec("UPDATE revisions SET base_revision_id = ? WHERE id = ?", pending, draft.Draft.ID)

	_, err := f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Actor: actorWren, DraftID: draft.Draft.ID,
		Now: f.tick()})

	require.Error(t, err)
	var docErr *domain.DocumentError
	assert.NotErrorAs(t, err, &docErr)
	for _, typed := range []error{datasources.ErrNotFound, datasources.ErrEntityGone, domain.ErrIllegalTransition,
		datasources.ErrUnavailable} {
		require.NotErrorIs(t, err, typed)
	}
	assert.Equal(t, "draft", f.stored(t, draft.Draft.ID).status)
}

// TestRepository_SubmitDraft_RenameToHiddenAlias checks W27: renaming a person to one of their own hidden aliases is
// pending with a hidden_data_conflict flag only moderators see, and everything the author is answered is as for a
// rename to a fresh name.
func TestRepository_SubmitDraft_RenameToHiddenAlias(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	person := func(name, hidden string) catalogtest.Ref {
		doc := newPersonDocument(name)
		doc.Aliases = []domain.DocPersonAlias{{Name: hidden, Kind: domain.PersonAliasKindLegal}}
		return f.c.Person(catalogtest.Person{Doc: doc})
	}
	alder := person("Alder Placeholder", "Alder Hidden")
	birch := person("Birch Placeholder", "Birch Hidden")
	rename := func(ref catalogtest.Ref, name string) (datasources.SubmitResult, domain.MyRevision) {
		result := editAndSubmit(t, f, actorWren, *personKey(ref.Slug), func(d *domain.PersonDocument) { d.Name = name })
		revision, err := f.repo.GetMyRevision(t.Context(), actorWren, result.RevisionID)
		require.NoError(t, err)
		return result, revision
	}

	hiddenResult, hiddenView := rename(alder, "Alder Hidden")
	freshResult, freshView := rename(birch, "Birch Fresh")

	assert.Contains(t, f.stored(t, hiddenResult.RevisionID).flags, string(domain.FlagHiddenDataConflict))
	assert.NotContains(t, f.stored(t, freshResult.RevisionID).flags, string(domain.FlagHiddenDataConflict))
	mask := func(v any, replace ...string) string {
		encoded, err := json.Marshal(v)
		require.NoError(t, err)
		return strings.NewReplacer(replace...).Replace(string(encoded))
	}
	hiddenResult.RevisionID, freshResult.RevisionID = 0, 0
	assert.Equal(t, mask(freshResult), mask(hiddenResult))
	for _, view := range []*domain.MyRevision{&hiddenView, &freshView} {
		view.ID, view.Entity.ID, view.CreatedAt, view.SubmittedAt = 0, 0, f.now, nil
		view.SubmittedBaseRevisionID, view.BaseRevisionID = nil, nil
	}
	assert.Equal(t,
		mask(freshView, "Birch Fresh", "NAME", "Birch Placeholder", "OLD", birch.Slug, "SLUG"),
		mask(hiddenView, "Alder Hidden", "NAME", "Alder Placeholder", "OLD", alder.Slug, "SLUG"))
}

func TestRepository_SubmitDraft_Placeholders(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	term := f.newPending(t, actorWren, newTermDocument("Sea Songs"))
	piece := f.newPending(t, actorWren, newPieceDocument("The Unsung Wren"))
	f.newPending(t, actorWren, newPersonDocument("Rowan Redlink"))
	f.newPending(t, actorWren, newEventDocument("Pending Revel"))
	collection := domain.CollectionDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: "Pending Archive",
		Kind: domain.CollectionKindOther, Enumerable: domain.CollectionEnumerableUnknown}
	f.newPending(t, actorWren, collection)

	assert.True(t, strings.HasPrefix(term.Entity.Slug, "pending-term-"))
	assert.Len(t, strings.TrimPrefix(piece.Entity.Slug, "pending-piece-"), 32)
	assert.True(t, piece.Entity.IsNew)
	for _, query := range []string{
		"SELECT COUNT(*) FROM pieces WHERE current_revision_id IS NULL AND title = '' AND sort_title = ''",
		"SELECT COUNT(*) FROM persons WHERE current_revision_id IS NULL AND name = '' AND sort_name = ''",
		"SELECT COUNT(*) FROM collections WHERE current_revision_id IS NULL AND name = ''",
		"SELECT COUNT(*) FROM events WHERE current_revision_id IS NULL AND name = ''",
	} {
		assert.Equal(t, 1, f.count(t, query), "a placeholder holds no content: %s", query)
	}
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM terms WHERE id = ? AND name = slug AND display_name = ''",
		term.Entity.ID))
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM term_ancestors WHERE ancestor_id = ? AND descendant_id = ? "+
		"AND depth = 0", term.Entity.ID, term.Entity.ID), "the term placeholder's self closure row")

	terms, err := f.repo.ListPublicTerms(t.Context(), nil)
	require.NoError(t, err)
	for _, listed := range terms {
		assert.NotEqual(t, term.Entity.Slug, listed.Slug)
	}
	_, err = f.repo.GetPublicTerm(t.Context(), term.Entity.Slug, domain.Page{Limit: 10})
	require.ErrorIs(t, err, datasources.ErrNotFound)
	pieces, err := f.repo.ListPublicPieces(t.Context(), datasources.PieceQuery{Page: domain.Page{Limit: 100}})
	require.NoError(t, err)
	assert.Equal(t, int64(1), pieces.Total, "the song only")
	_, err = f.repo.GetPublicPiece(t.Context(), piece.Entity.Slug)
	require.ErrorIs(t, err, datasources.ErrNotFound)
}

func TestRepository_SubmitDraft_ReferencesOwnPending(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	term := f.newPending(t, actorWren, newTermDocument("Sea Songs"))
	person := f.newPending(t, actorWren, newPersonDocument("Rowan Redlink"))
	doc := newPieceDocument("The Sea Wren")
	doc.Terms = []domain.DocPieceTerm{{TermID: term.Entity.ID, IsPrimary: true}}
	doc.Credits = []domain.DocCredit{{PersonID: &person.Entity.ID, Role: domain.CreditRoleLyrics,
		Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain}}

	result := f.submit(t, actorWren, f.saveDraft(t, actorWren, nil, doc).Draft.ID)

	assert.Contains(t, f.stored(t, result.RevisionID).flags, string(domain.FlagReferencesPending))
	revision, err := f.repo.GetMyRevision(t.Context(), actorWren, result.RevisionID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []domain.EditorReference{
		{EntityType: domain.EntityTypePerson, ID: person.Entity.ID, Slug: person.Entity.Slug, Label: "Rowan Redlink",
			IsPending: true},
		{EntityType: domain.EntityTypeTerm, ID: term.Entity.ID, Slug: term.Entity.Slug, Label: "Sea Songs",
			IsPending: true},
	}, revision.PendingReferences)

	// Another user may not reference them: the stored reference is to an entity they may not reference.
	_, err = f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Actor: actorAsh,
		DraftID: f.saveDraft(t, actorAsh, nil, doc).Draft.ID, Now: f.tick()})
	var docErr *domain.DocumentError
	require.ErrorAs(t, err, &docErr)
}

func TestRepository_SubmitDraft_EntityGone(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	piece := f.newPending(t, actorWren, newPieceDocument("The Unsung Wren"))
	draft := f.saveDraft(t, actorWren, pieceKey(piece.Entity.Slug), newPieceDocument("The Unsung Wren, edited"))
	f.c.Exec("UPDATE revisions SET status = 'rejected' WHERE id = ?", piece.ID)
	f.c.Exec("DELETE FROM pieces WHERE id = ?", piece.Entity.ID)

	_, err := f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Actor: actorWren, DraftID: draft.Draft.ID,
		Now: f.tick()})
	require.ErrorIs(t, err, datasources.ErrEntityGone)
}

func TestRepository_WithdrawRevision(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	result := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	draft := f.saveDraft(t, actorWren, nil, newPieceDocument(unsungTitle))

	require.ErrorIs(t, f.repo.WithdrawRevision(t.Context(), actorAsh, result.RevisionID), datasources.ErrNotFound)
	require.ErrorIs(t, f.repo.WithdrawRevision(t.Context(), actorRowan, result.RevisionID), datasources.ErrNotFound,
		"a moderator rejects instead")
	require.ErrorIs(t, f.repo.WithdrawRevision(t.Context(), actorWren, draft.Draft.ID), domain.ErrNotPending)
	require.ErrorIs(t, f.repo.WithdrawRevision(t.Context(), actorWren, 999999), datasources.ErrNotFound)

	require.NoError(t, f.repo.WithdrawRevision(t.Context(), actorWren, result.RevisionID))
	assert.Equal(t, "withdrawn", f.stored(t, result.RevisionID).status)
	require.ErrorIs(t, f.repo.WithdrawRevision(t.Context(), actorWren, result.RevisionID), domain.ErrNotPending)
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM pieces WHERE id = ?", f.song.ID), "a public entity stays")
}

func TestRepository_WithdrawRevision_DeletesPlaceholder(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	term := f.newPending(t, actorWren, newTermDocument("Sea Songs"))
	termAliases := newTermDocument("Capstan Chants")
	termAliases.Aliases = []string{"Capstan Songs"}
	withAlias := f.newPending(t, actorWren, termAliases)
	f.c.Exec("INSERT INTO term_aliases (term_id, name) VALUES (?, ?)", withAlias.Entity.ID, "Capstan Songs")

	require.NoError(t, f.repo.WithdrawRevision(t.Context(), actorWren, term.ID))
	require.NoError(t, f.repo.WithdrawRevision(t.Context(), actorWren, withAlias.ID))

	for _, id := range []int64{term.Entity.ID, withAlias.Entity.ID} {
		assert.Equal(t, 0, f.count(t, "SELECT COUNT(*) FROM terms WHERE id = ?", id))
		assert.Equal(t, 0, f.count(t, "SELECT COUNT(*) FROM term_ancestors WHERE descendant_id = ?", id),
			"the closure row cascades")
	}
	assert.Equal(t, 0, f.count(t, "SELECT COUNT(*) FROM term_aliases WHERE term_id = ?", withAlias.Entity.ID))
	assert.Equal(t, "withdrawn", f.stored(t, term.ID).status, "the revision row stays")

	revision, err := f.repo.GetMyRevision(t.Context(), actorWren, term.ID)
	require.NoError(t, err)
	assert.Nil(t, revision.Entity, "its entity is gone")
	assert.Equal(t, "Sea Songs", revision.Title)
}

// TestRepository_WithdrawRevision_AfterSupersede checks T44 then T43: the revision withdrawn is not the one that
// created the placeholder, which is deleted all the same.
func TestRepository_WithdrawRevision_AfterSupersede(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	piece := f.newPending(t, actorWren, newPieceDocument("The Unsung Wren"))
	key := *pieceKey(piece.Entity.Slug)
	second := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	assert.Equal(t, "withdrawn", f.stored(t, piece.ID).status)
	assert.Equal(t, piece.Entity.ID, f.stored(t, second.RevisionID).entityID.Int64, "the placeholder is reused")
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM pieces WHERE id = ?", piece.Entity.ID))

	require.NoError(t, f.repo.WithdrawRevision(t.Context(), actorWren, second.RevisionID))

	assert.Equal(t, 0, f.count(t, "SELECT COUNT(*) FROM pieces WHERE id = ?", piece.Entity.ID))
}

func TestRepository_WithdrawRevision_PromotedAuthor(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	result := f.submit(t, actorWren, f.saveDraft(t, actorWren, nil, newPieceDocument(unsungTitle)).Draft.ID)
	f.c.Exec("UPDATE users SET role = 'moderator' WHERE id = ?", wrenUserID)

	promoted := domain.Actor{UserID: wrenUserID, Role: domain.RoleModerator}
	require.NoError(t, f.repo.WithdrawRevision(t.Context(), promoted, result.RevisionID))
	assert.Equal(t, "withdrawn", f.stored(t, result.RevisionID).status)
}

// TestRepository_SubmitDraft_EditedPerformanceKeepsHiddenPerformer checks that a contributor's edit of a
// performance's year, which changes the key the restore matches performances by, never silently removes the
// performer withheld from them: the submission is flagged hidden_data_conflict, approval refuses it, and the
// moderator rebases it with the performer kept.
func TestRepository_SubmitDraft_EditedPerformanceKeepsHiddenPerformer(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	performers := func() int {
		return f.count(t, "SELECT COUNT(*) FROM performance_performers WHERE person_id = ?", f.quiet.ID)
	}
	require.Equal(t, 1, performers())
	result := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) {
		require.Equal(t, []int64{f.morwen.ID}, d.Links[0].Performances[0].PerformerIDs, "the performer is withheld")
		d.Links[0].Performances[0].Year = new(int16(2002))
	})
	assert.Contains(t, f.stored(t, result.RevisionID).flags, string(domain.FlagHiddenDataConflict))
	require.ErrorIs(t, f.approve(t, result.RevisionID, domain.ApprovalOptions{}), domain.ErrHiddenDataConflict)

	resolved := f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
	require.Equal(t, []int64{f.morwen.ID, f.quiet.ID}, resolved.Links[0].Performances[0].PerformerIDs)
	resolved.Links[0].Performances[0].Year = new(int16(2002))
	require.NoError(t, f.rebase(t, result.RevisionID, &f.song.RevisionID, resolved, nil))
	f.mustApprove(t, result.RevisionID)

	assert.Equal(t, 1, performers(), "the hidden performer is kept")
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM performances WHERE year = 2002"))
}
