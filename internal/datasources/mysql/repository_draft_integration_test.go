//go:build integration

package mysql_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepository_SaveDraft_OnePerEntity(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := pieceKey(f.song.Slug)
	doc := f.editable(t, actorWren, *key).Document.(domain.PieceDocument)

	first := f.saveDraft(t, actorWren, key, doc)
	assert.True(t, first.Created)
	assert.Equal(t, &f.song.RevisionID, first.Draft.BaseRevisionID, "the base defaults to the current revision")
	assert.Equal(t, doc, first.Draft.Document)
	assert.Equal(t, &domain.RevisionEntity{Type: domain.EntityTypePiece, ID: f.song.ID, Slug: f.song.Slug},
		first.Draft.Entity)

	// The current revision moves on; a second draft of the entity replaces the first, keeping its ID and base.
	moved := f.c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: f.song.ID,
		Snapshot: songDocument(f), BaseRevisionID: f.song.RevisionID})
	f.c.Exec("UPDATE pieces SET current_revision_id = ? WHERE id = ?", moved, f.song.ID)
	doc.NotesMD = new(editedNotes)
	second := f.saveDraft(t, actorWren, key, doc)
	assert.False(t, second.Created)
	assert.Equal(t, first.Draft.ID, second.Draft.ID)
	assert.Equal(t, &f.song.RevisionID, second.Draft.BaseRevisionID)
	assert.Equal(t, editedNotes, *second.Draft.Document.(domain.PieceDocument).NotesMD)
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM revisions WHERE status = 'draft'"))

	editable := f.editable(t, actorWren, *key)
	require.NotNil(t, editable.Draft)
	assert.Equal(t, first.Draft.ID, editable.Draft.ID)

	// Another user's draft of the same entity is their own.
	other := f.saveDraft(t, actorAsh, key, doc)
	assert.True(t, other.Created)
	assert.NotEqual(t, first.Draft.ID, other.Draft.ID)
}

func TestRepository_SaveDraft_Concurrent(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := pieceKey(f.song.Slug)
	doc := f.editable(t, actorWren, *key).Document

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = f.repo.SaveDraft(t.Context(), datasources.SaveDraftRequest{Actor: actorWren,
				EntityType: domain.EntityTypePiece, Entity: key, Document: doc, Now: f.now})
		})
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM revisions WHERE status = 'draft' AND entity_id = ?",
		f.song.ID))
}

func TestRepository_SaveDraft_Limit(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	for i := range domain.MaxDrafts {
		f.saveDraft(t, actorWren, nil, newPieceDocument(fmt.Sprintf("Draft %d", i)))
	}

	_, err := f.repo.SaveDraft(t.Context(), datasources.SaveDraftRequest{Actor: actorWren,
		EntityType: domain.EntityTypePiece, Document: newPieceDocument("One too many"), Now: f.tick()})
	require.ErrorIs(t, err, datasources.ErrDraftLimitReached)

	// Replacing a draft is not a new one, and another user has their own limit.
	key := pieceKey(f.song.Slug)
	f.c.Exec("DELETE FROM revisions WHERE id = (SELECT id FROM (SELECT MIN(id) AS id FROM revisions " +
		"WHERE status = 'draft') d)")
	doc := f.editable(t, actorWren, *key).Document
	f.saveDraft(t, actorWren, key, doc)
	f.saveDraft(t, actorWren, key, doc)
	f.saveDraft(t, actorAsh, nil, newPieceDocument("Ash's draft"))
}

func TestRepository_SaveDraft_Base(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := pieceKey(f.song.Slug)
	doc := f.editable(t, actorWren, *key).Document
	pending := f.c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: f.song.ID,
		Snapshot: songDocument(f), Status: domain.RevisionStatusPending, Source: domain.RevisionSourceUser})
	baseProblem := &domain.DocumentError{Problems: []domain.DocumentProblem{
		{Path: "/base_revision_id", Code: domain.ProblemUnknownReference},
	}}

	for name, base := range map[string]int64{
		"not approved": pending, "of another entity": f.morwen.RevisionID, "no such revision": 999999,
	} {
		_, err := f.repo.SaveDraft(t.Context(), datasources.SaveDraftRequest{Actor: actorWren,
			EntityType: domain.EntityTypePiece, Entity: key, BaseRevisionID: &base, Document: doc, Now: f.tick()})
		var docErr *domain.DocumentError
		require.ErrorAs(t, err, &docErr, name)
		assert.Equal(t, baseProblem, docErr, name)
	}

	_, err := f.repo.SaveDraft(t.Context(), datasources.SaveDraftRequest{Actor: actorWren,
		EntityType: domain.EntityTypePiece, BaseRevisionID: &f.song.RevisionID, Document: newPieceDocument("New"),
		Now: f.tick()})
	var docErr *domain.DocumentError
	require.ErrorAs(t, err, &docErr, "a new entity has no base")

	_, err = f.repo.SaveDraft(t.Context(), datasources.SaveDraftRequest{Actor: actorWren,
		EntityType: domain.EntityTypePiece, Entity: pieceKey(unknownPieceSlug), Document: doc, Now: f.tick()})
	require.ErrorIs(t, err, datasources.ErrNotFound)
}

func TestRepository_SaveDraft_ReceivedKeys(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := personKey(f.morwen.Slug)
	saved := f.saveDraft(t, actorWren, key, f.editable(t, actorWren, *key).Document)

	keys := f.stored(t, saved.Draft.ID).receivedKeys
	assert.Contains(t, keys, strings.ToLower(morwenStage), "the received public alias")
	assert.Contains(t, keys, "example.org/morwen", "the received link")
	for _, withheld := range []string{strings.ToLower(morwenLegal), "morwen/hidden"} {
		assert.NotContains(t, keys, withheld, "no key of a withheld element")
	}

	songKey := pieceKey(f.song.Slug)
	songDraft := f.saveDraft(t, actorWren, songKey, f.editable(t, actorWren, *songKey).Document)
	var songKeys map[string]bool
	require.NoError(t, json.Unmarshal([]byte(f.stored(t, songDraft.Draft.ID).receivedKeys), &songKeys))
	assert.NotEmpty(t, songKeys)
	for k := range songKeys {
		assert.NotContains(t, k, "wren/hidden")
		assert.NotContains(t, k, fmt.Sprintf("\"%d\"|\"tune\"", f.quiet.ID), "the withheld credit")
		assert.False(t, strings.HasPrefix(k, `"performer"`) && strings.HasSuffix(k, fmt.Sprintf(`"%d"|`, f.quiet.ID)),
			"the withheld performer")
	}
}

func TestRepository_Drafts_NotTheCallers(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	draft := f.saveDraft(t, actorWren, nil, newPieceDocument(unsungTitle))
	submitted := f.saveDraft(t, actorWren, nil, newPieceDocument("Sung"))
	f.submit(t, actorWren, submitted.Draft.ID)
	doc := newPieceDocument("Overwritten")

	for _, tt := range []struct {
		name  string
		actor domain.Actor
		id    int64
		want  error
	}{
		{"another user's draft", actorAsh, draft.Draft.ID, datasources.ErrNotFound},
		{"a moderator and another user's draft", actorRowan, draft.Draft.ID, datasources.ErrNotFound},
		{"no such draft", actorWren, 999999, datasources.ErrNotFound},
		{"the caller's submitted revision", actorWren, submitted.Draft.ID, domain.ErrNotDraft},
		{"another user's submitted revision", actorAsh, submitted.Draft.ID, datasources.ErrNotFound},
	} {
		_, err := f.repo.GetDraft(t.Context(), tt.actor, tt.id)
		require.ErrorIs(t, err, tt.want, "get: "+tt.name)
		_, err = f.repo.UpdateDraft(t.Context(), datasources.UpdateDraftRequest{Actor: tt.actor, ID: tt.id,
			Document: doc, Now: f.tick()})
		require.ErrorIs(t, err, tt.want, "update: "+tt.name)
		require.ErrorIs(t, f.repo.DiscardDraft(t.Context(), tt.actor, tt.id), tt.want, "discard: "+tt.name)
	}

	got, err := f.repo.GetDraft(t.Context(), actorWren, draft.Draft.ID)
	require.NoError(t, err)
	assert.Equal(t, newPieceDocument(unsungTitle), got.Document, "nothing was overwritten")
}

func TestRepository_Drafts_UpdateGetDiscard(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	draft := f.saveDraft(t, actorWren, nil, newPieceDocument(unsungTitle))
	assert.Nil(t, draft.Draft.Entity)
	assert.Nil(t, draft.Draft.BaseRevisionID)

	updated, err := f.repo.UpdateDraft(t.Context(), datasources.UpdateDraftRequest{Actor: actorWren,
		ID: draft.Draft.ID, Document: newPieceDocument("Sung at last"), Summary: "Retitled", Now: f.tick()})
	require.NoError(t, err)
	assert.Equal(t, "Retitled", updated.Summary)
	assert.Equal(t, draft.Draft.CreatedAt, updated.CreatedAt)
	assert.True(t, updated.UpdatedAt.After(draft.Draft.UpdatedAt))

	got, err := f.repo.GetDraft(t.Context(), actorWren, draft.Draft.ID)
	require.NoError(t, err)
	assert.Equal(t, updated, got)
	preview, ok := got.Preview.(domain.PieceSnapshot)
	require.True(t, ok)
	assert.Equal(t, "Sung at last", preview.Title)

	_, err = f.repo.UpdateDraft(t.Context(), datasources.UpdateDraftRequest{Actor: actorWren,
		ID: draft.Draft.ID, Document: newEventDocument("Not a piece"), Now: f.tick()})
	require.Error(t, err, "a document of another type")

	require.NoError(t, f.repo.DiscardDraft(t.Context(), actorWren, draft.Draft.ID))
	_, err = f.repo.GetDraft(t.Context(), actorWren, draft.Draft.ID)
	require.ErrorIs(t, err, datasources.ErrNotFound)
}

// TestRepository_SaveDraft_DefaultBaseIsPendingStart checks that a draft saved without a base, from the editor's
// pending starting point, is based on the pending revision's submitted base, not on current: an alias someone else
// added meanwhile was never shown to the author, so it is neither received nor removed by their edit.
func TestRepository_SaveDraft_DefaultBaseIsPendingStart(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := personKey(f.morwen.Slug)
	const added = "Morwen the Bold"
	editAndSubmit(t, f, actorWren, *key, func(d *domain.PersonDocument) { d.NotesMD = new("Wren's notes.") })
	theirs := editAndSubmit(t, f, actorAsh, *key, func(d *domain.PersonDocument) {
		d.Aliases = append(slices.Clone(d.Aliases), domain.DocPersonAlias{Name: added, Kind: domain.PersonAliasKindStage,
			IsPublic: true})
	})
	f.mustApprove(t, theirs.RevisionID)

	editable := f.editable(t, actorWren, *key)
	require.Equal(t, domain.StartingPointPending, editable.StartingPoint)
	draft := f.saveDraft(t, actorWren, key, editable.Document)
	stored := f.stored(t, draft.Draft.ID)
	assert.Equal(t, f.morwen.RevisionID, stored.base.Int64, "the pending start's submitted base")
	assert.NotContains(t, stored.receivedKeys, strings.ToLower(added), "the added alias was never received")

	f.mustApprove(t, f.submit(t, actorWren, draft.Draft.ID).RevisionID)
	current := f.editable(t, actorRowan, *key).Document.(domain.PersonDocument)
	assert.True(t, slices.ContainsFunc(current.Aliases, func(a domain.DocPersonAlias) bool { return a.Name == added }),
		"Ash's alias survives Wren's edit")
	assert.Equal(t, "Wren's notes.", *current.NotesMD)
}
