//go:build integration

package mysql_test

import (
	"encoding/json"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepository_ResolveEditorRefs(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	ownTerm := f.newPending(t, actorWren, newTermDocument("Sea Songs"))
	ownPerson := f.newPending(t, actorWren, newPersonDocument("Rowan Redlink"))
	ownEvent := f.newPending(t, actorWren, newEventDocument("Pending Revel"))
	othersTerm := f.newPending(t, actorAsh, newTermDocument("Ash Songs"))
	unapproved := f.c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: unsungTitle}, Unapproved: true})

	refs := domain.EditorRefs{
		PieceSlugs:    []string{f.song.Slug, unapproved.Slug, unknownPieceSlug},
		PersonSlugs:   []string{f.morwen.Slug, f.quiet.Slug, ownPerson.Entity.Slug},
		TermSlugs:     []string{f.ballads.Slug, ownTerm.Entity.Slug, othersTerm.Entity.Slug, unknownTermSlug},
		EventSlugs:    []string{f.fair.Slug, ownEvent.Entity.Slug},
		CollectionIDs: []int64{f.archive.ID, 999999},
	}
	resolved, err := f.repo.ResolveEditorRefs(t.Context(), actorWren, refs)
	require.NoError(t, err)

	assert.Equal(t, map[string]int64{f.song.Slug: f.song.ID}, resolved.Pieces, "an unapproved piece is unknown")
	assert.Equal(t, map[string]int64{f.morwen.Slug: f.morwen.ID, f.quiet.Slug: f.quiet.ID,
		ownPerson.Entity.Slug: ownPerson.Entity.ID}, resolved.Persons, "the own pending person resolves")
	assert.Equal(t, map[string]int64{f.ballads.Slug: f.ballads.ID, ownTerm.Entity.Slug: ownTerm.Entity.ID},
		resolved.Terms, "another user's placeholder is answered as an unknown slug")
	assert.Equal(t, map[string]int64{f.fair.Slug: f.fair.ID}, resolved.Events,
		"an own pending event is not a reference anyone may make")
	assert.Equal(t, map[int64]bool{f.archive.ID: true}, resolved.Collections)

	// The same unknown_reference answer for a slug that does not exist and another user's placeholder.
	editorPiece := domain.EditorPieceDocument{Terms: []domain.EditorPieceTerm{
		{Term: othersTerm.Entity.Slug}, {Term: unknownTermSlug},
	}}
	_, problems := domain.FromEditorPiece(editorPiece, resolved)
	assert.Equal(t, []domain.DocumentProblem{
		{Path: "/terms/0/term", Code: domain.ProblemUnknownReference},
		{Path: "/terms/1/term", Code: domain.ProblemUnknownReference},
	}, problems)

	asAsh, err := f.repo.ResolveEditorRefs(t.Context(), actorAsh, refs)
	require.NoError(t, err)
	assert.Equal(t, map[string]int64{f.ballads.Slug: f.ballads.ID, othersTerm.Entity.Slug: othersTerm.Entity.ID},
		asAsh.Terms)

	moderator, err := f.repo.ResolveEditorRefs(t.Context(), actorRowan, refs)
	require.NoError(t, err)
	assert.Equal(t, othersTerm.Entity.ID, moderator.Terms[othersTerm.Entity.Slug],
		"an interactive moderator resolves every placeholder")
	assert.Equal(t, unapproved.ID, moderator.Pieces[unapproved.Slug])
	token, err := f.repo.ResolveEditorRefs(t.Context(), actorRowanToken, refs)
	require.NoError(t, err)
	assert.NotContains(t, token.Terms, othersTerm.Entity.Slug, "an API token acts as a contributor")
}

func TestRepository_EditorReferences(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	ownPerson := f.newPending(t, actorWren, newPersonDocument("Rowan Redlink"))
	othersTerm := f.newPending(t, actorAsh, newTermDocument("Ash Songs"))
	unapproved := f.c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: unsungTitle}, Unapproved: true})
	refs := domain.SnapshotReferences{
		PieceIDs:      []int64{f.song.ID, unapproved.ID},
		PersonIDs:     []int64{ownPerson.Entity.ID, f.morwen.ID, f.morwen.ID},
		TermIDs:       []int64{f.ballads.ID, othersTerm.Entity.ID},
		CollectionIDs: []int64{f.archive.ID},
		EventIDs:      []int64{f.fair.ID},
	}

	labelled, err := f.repo.EditorReferences(t.Context(), actorWren, refs)
	require.NoError(t, err)
	assert.ElementsMatch(t, []domain.EditorReference{
		{EntityType: domain.EntityTypePiece, ID: f.song.ID, Slug: f.song.Slug, Label: songTitle},
		{EntityType: domain.EntityTypePerson, ID: f.morwen.ID, Slug: f.morwen.Slug, Label: morwenName},
		{EntityType: domain.EntityTypePerson, ID: ownPerson.Entity.ID, Slug: ownPerson.Entity.Slug,
			Label: "Rowan Redlink", IsPending: true},
		{EntityType: domain.EntityTypeTerm, ID: f.ballads.ID, Slug: f.ballads.Slug, Label: balladsTerm},
		{EntityType: domain.EntityTypeCollection, ID: f.archive.ID, Label: wrenArchive},
		{EntityType: domain.EntityTypeEvent, ID: f.fair.ID, Slug: f.fair.Slug, Label: fairEvent},
	}, labelled, "another user's placeholder and an unapproved piece have no label")

	moderator, err := f.repo.EditorReferences(t.Context(), actorRowan, refs)
	require.NoError(t, err)
	assert.Contains(t, moderator, domain.EditorReference{EntityType: domain.EntityTypeTerm, ID: othersTerm.Entity.ID,
		Slug: othersTerm.Entity.Slug, Label: "Ash Songs", IsPending: true})
}

// withheldSong is the song's document without what a contributor may not see: the credit and performer of the
// person who prefers no page, the hidden link and the preferences.
func withheldSong(f *editingFixture) domain.PieceDocument {
	doc := songDocument(f)
	doc.Credits = doc.Credits[:1]
	doc.Links = doc.Links[:1]
	doc.Links[0].Performances[0].PerformerIDs = []int64{f.morwen.ID}

	return doc
}

func TestRepository_GetEditable_Contributor(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)

	song := f.editable(t, actorWren, *pieceKey(f.song.Slug))
	assert.Equal(t, withheldSong(f), withoutKeys(t, song.Document),
		"the import snapshot minus the withheld elements")
	assert.Equal(t, domain.StartingPointCurrent, song.StartingPoint)
	assert.Equal(t, &f.song.RevisionID, song.BaseRevisionID)
	assert.Equal(t, domain.RevisionEntity{Type: domain.EntityTypePiece, ID: f.song.ID, Slug: f.song.Slug}, song.Entity)
	assert.Empty(t, song.LinkStates)
	assert.Nil(t, song.Draft)
	assert.Nil(t, song.Pending)
	for _, ref := range song.References {
		assert.False(t, ref.EntityType == domain.EntityTypePerson && ref.ID == f.quiet.ID,
			"a withheld reference has no label")
	}
	assert.Contains(t, song.References, domain.EditorReference{EntityType: domain.EntityTypePerson, ID: f.morwen.ID,
		Slug: f.morwen.Slug, Label: morwenName})

	morwen := f.editable(t, actorWren, *personKey(f.morwen.Slug)).Document.(domain.PersonDocument)
	assert.Equal(t, []domain.DocPersonAlias{{Name: morwenStage, Kind: domain.PersonAliasKindStage, IsPublic: true}},
		morwen.Aliases)
	require.Len(t, morwen.Links, 1)
	assert.Equal(t, morwenSiteURL, morwen.Links[0].URL)
	encoded, err := json.Marshal(morwen)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), morwenLegal)
	assert.NotContains(t, string(encoded), morwenHidden)

	quiet := f.editable(t, actorWren, *personKey(f.quiet.Slug)).Document.(domain.PersonDocument)
	assert.Nil(t, quiet.NotesMD, "a person who prefers no page is identity only")
	assert.Equal(t, quietHarper, quiet.Name)
}

func TestRepository_GetEditable_Moderator(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)

	song := f.editable(t, actorRowan, *pieceKey(f.song.Slug))
	assert.Equal(t, songDocument(f), withoutKeys(t, song.Document), "everything")
	require.Len(t, song.LinkStates, 2)
	assert.Equal(t, songLyricsURL, song.LinkStates[0].URL)
	assert.False(t, song.LinkStates[0].IsHidden)
	require.NotNil(t, song.LinkStates[0].LinkID)
	assert.Equal(t, songHiddenURL, song.LinkStates[1].URL)
	assert.True(t, song.LinkStates[1].IsHidden)
	require.NotNil(t, song.LinkStates[1].Liveness)
	assert.Equal(t, domain.LivenessUnknown, *song.LinkStates[1].Liveness)

	morwen := f.editable(t, actorRowan, *personKey(f.morwen.Slug)).Document.(domain.PersonDocument)
	assert.Len(t, morwen.Aliases, 2)

	token := f.editable(t, actorRowanToken, *pieceKey(f.song.Slug))
	assert.Equal(t, withheldSong(f), withoutKeys(t, token.Document), "an API-token moderator gets the contributor projection")
	assert.Empty(t, token.LinkStates)
}

func TestRepository_GetEditable_PlaceholderIsAuthorOnlyEvenForModerators(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	term := f.newPending(t, actorWren, newTermDocument("Sea Songs"))
	key := domain.EntityKey{Type: domain.EntityTypeTerm, Slug: term.Entity.Slug}

	editable := f.editable(t, actorWren, key)
	assert.True(t, editable.Entity.IsNew)
	assert.Equal(t, domain.StartingPointPending, editable.StartingPoint)
	assert.Nil(t, editable.BaseRevisionID)
	assert.Equal(t, newTermDocument("Sea Songs"), editable.Document)
	require.NotNil(t, editable.Pending)
	assert.Equal(t, term.ID, editable.Pending.ID)

	for _, actor := range []domain.Actor{actorAsh, actorRowan} {
		_, err := f.repo.GetEditable(t.Context(), actor, key)
		require.ErrorIs(t, err, datasources.ErrNotFound, "a placeholder is editable by its author only, even by a moderator")
	}
	_, err := f.repo.GetEditable(t.Context(), actorWren, domain.EntityKey{Type: domain.EntityTypeTerm,
		Slug: unknownTermSlug})
	require.ErrorIs(t, err, datasources.ErrNotFound)
	unapproved := f.c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: unsungTitle}, Unapproved: true})
	_, err = f.repo.GetEditable(t.Context(), actorRowan, *pieceKey(unapproved.Slug))
	require.ErrorIs(t, err, datasources.ErrNotFound)
}

func TestRepository_GetEditable_PendingStartingPoint(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	result := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) {
		d.NotesMD = new(editedNotes)
		// A typed URL of a hidden link, in another spelling.
		d.Links = append(d.Links, domain.DocPieceLink{DocLink: domain.DocLink{URL: "https://EXAMPLE.org/wren/hidden",
			Roles: []domain.LinkRole{domain.LinkRolePerformance}}})
	})
	// The current revision moves on: the pending starting point keeps its submitted base.
	moved := f.c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: f.song.ID,
		Snapshot: songDocument(f), BaseRevisionID: f.song.RevisionID})
	f.c.Exec("UPDATE pieces SET current_revision_id = ? WHERE id = ?", moved, f.song.ID)

	editable := f.editable(t, actorWren, key)
	assert.Equal(t, domain.StartingPointPending, editable.StartingPoint)
	assert.Equal(t, &f.song.RevisionID, editable.BaseRevisionID)
	require.NotNil(t, editable.Pending)
	assert.Equal(t, result.RevisionID, editable.Pending.ID)
	doc := editable.Document.(domain.PieceDocument)
	assert.Equal(t, editedNotes, *doc.NotesMD)
	require.Len(t, doc.Links, 2)
	assert.Equal(t, "https://EXAMPLE.org/wren/hidden", doc.Links[1].URL, "the typed URL is kept")
	assert.Len(t, doc.Credits, 1, "what was withheld stays withheld")

	// A link the author received is hidden since: dropped from the starting point.
	f.c.SetLinkState(songLyricsURL, catalogtest.LinkState{IsHidden: true})
	doc = f.editable(t, actorWren, key).Document.(domain.PieceDocument)
	require.Len(t, doc.Links, 1)
	assert.Equal(t, "https://EXAMPLE.org/wren/hidden", doc.Links[0].URL)

	// Another user starts from the current document.
	assert.Equal(t, domain.StartingPointCurrent, f.editable(t, actorAsh, key).StartingPoint)
}
