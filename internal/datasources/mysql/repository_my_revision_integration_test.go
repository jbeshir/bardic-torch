//go:build integration

package mysql_test

import (
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepository_ListMyRevisions(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	approved := f.c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: f.song.ID,
		Snapshot: songDocument(f), Source: domain.RevisionSourceUser, AuthorUserID: wrenUserID,
		BaseRevisionID: f.song.RevisionID, CreatedAt: f.now.Add(-24 * time.Hour)})
	pending := editAndSubmit(t, f, actorWren, *personKey(f.morwen.Slug), func(d *domain.PersonDocument) {
		d.NotesMD = new("A singer of wren songs.")
	})
	draft := f.saveDraft(t, actorWren, nil, newPieceDocument("The Unsung Wren"))
	f.saveDraft(t, actorAsh, nil, newPieceDocument("Ash's Song"))

	list, err := f.repo.ListMyRevisions(t.Context(), actorWren, datasources.MyRevisionQuery{
		Page: domain.Page{Limit: 10}})
	require.NoError(t, err)
	assert.Equal(t, int64(3), list.Total)
	require.Len(t, list.Revisions, 3)
	assert.Equal(t, []int64{draft.Draft.ID, pending.RevisionID, approved},
		[]int64{list.Revisions[0].ID, list.Revisions[1].ID, list.Revisions[2].ID},
		"newest first: by submit time, else last save, else creation")
	assert.Equal(t, "The Unsung Wren", list.Revisions[0].Title)
	assert.Nil(t, list.Revisions[0].Entity)
	assert.Equal(t, domain.RevisionStatusDraft, list.Revisions[0].Status)
	assert.Equal(t, morwenName, list.Revisions[1].Title)
	assert.Equal(t, &domain.RevisionEntity{Type: domain.EntityTypePerson, ID: f.morwen.ID, Slug: f.morwen.Slug},
		list.Revisions[1].Entity)
	assert.Equal(t, domain.AuthorRef{DisplayName: wrenDisplay, Role: domain.RoleContributor}, list.Revisions[1].Author)
	assert.NotNil(t, list.Revisions[1].SubmittedAt)
	assert.Equal(t, songTitle, list.Revisions[2].Title)

	status := domain.RevisionStatusPending
	byStatus, err := f.repo.ListMyRevisions(t.Context(), actorWren, datasources.MyRevisionQuery{Status: &status,
		Page: domain.Page{Limit: 10}})
	require.NoError(t, err)
	assert.Equal(t, int64(1), byStatus.Total)
	assert.Equal(t, pending.RevisionID, byStatus.Revisions[0].ID)

	pieces := domain.EntityTypePiece
	byType, err := f.repo.ListMyRevisions(t.Context(), actorWren, datasources.MyRevisionQuery{EntityType: &pieces,
		Page: domain.Page{Limit: 1, Offset: 1}})
	require.NoError(t, err)
	assert.Equal(t, int64(2), byType.Total)
	require.Len(t, byType.Revisions, 1)
	assert.Equal(t, approved, byType.Revisions[0].ID)
}

// TestRepository_GetMyRevision_AuthorView checks the author's view of their pending revision (D6, §6.4 at the
// repository level): what they typed is shown, what was withheld is not, and what they received is redacted by
// the current state.
func TestRepository_GetMyRevision_AuthorView(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	typedURL := "https://example.org/wren/HIDDEN"
	result := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) {
		d.NotesMD = new(editedNotes)
		d.Links = append(d.Links, domain.DocPieceLink{DocLink: domain.DocLink{URL: typedURL,
			Roles: []domain.LinkRole{domain.LinkRolePerformance}}})
	})

	revision, err := f.repo.GetMyRevision(t.Context(), actorWren, result.RevisionID)
	require.NoError(t, err)
	view := revision.Snapshot.(domain.PieceSnapshot)
	assert.Equal(t, editedNotes, *view.NotesMD)
	require.Len(t, view.Links, 2)
	assert.Equal(t, songLyricsURL, view.Links[0].URL)
	assert.Equal(t, typedURL, view.Links[1].URL, "a typed URL of a hidden link, as typed (W10, W32)")
	require.Len(t, view.Credits, 1, "the credit of the person who prefers no page is never shown (W11)")
	assert.Equal(t, morwenName, view.Credits[0].Person.Name)
	base := revision.BaseSnapshot.(domain.PieceSnapshot)
	assert.Equal(t, songNotes, *base.NotesMD)
	assert.Len(t, base.Links, 1, "the base's hidden link is never shown")
	assert.Equal(t, &f.song.RevisionID, revision.SubmittedBaseRevisionID)
	assert.Empty(t, revision.PendingReferences)

	// A link the author received is hidden since: dropped from their view (W34).
	f.c.SetLinkState(songLyricsURL, catalogtest.LinkState{IsHidden: true})
	revision, err = f.repo.GetMyRevision(t.Context(), actorWren, result.RevisionID)
	require.NoError(t, err)
	view = revision.Snapshot.(domain.PieceSnapshot)
	require.Len(t, view.Links, 1)
	assert.Equal(t, typedURL, view.Links[0].URL)

	_, err = f.repo.GetMyRevision(t.Context(), actorAsh, result.RevisionID)
	require.ErrorIs(t, err, datasources.ErrNotFound)
	_, err = f.repo.GetMyRevision(t.Context(), actorRowan, result.RevisionID)
	require.ErrorIs(t, err, datasources.ErrNotFound, "moderators read revisions through the moderation view")
}

// TestRepository_GetMyRevision_TypedAlias checks W33 and W07: a typed public alias equal to a hidden base alias is
// shown to its author, a typed legal alias never is, and the base's hidden alias never is.
func TestRepository_GetMyRevision_TypedAlias(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	result := editAndSubmit(t, f, actorWren, *personKey(f.morwen.Slug), func(d *domain.PersonDocument) {
		d.Aliases = append(d.Aliases,
			domain.DocPersonAlias{Name: "MORWEN LEGALNAME", Kind: domain.PersonAliasKindStage, IsPublic: true},
			domain.DocPersonAlias{Name: "Morwen Typedlegal", Kind: domain.PersonAliasKindLegal})
	})

	revision, err := f.repo.GetMyRevision(t.Context(), actorWren, result.RevisionID)
	require.NoError(t, err)
	profile := revision.Snapshot.(domain.PersonSnapshot).Profile
	require.NotNil(t, profile)
	assert.Equal(t, []domain.PersonAlias{{Name: morwenStage, Kind: domain.PersonAliasKindStage},
		{Name: "MORWEN LEGALNAME", Kind: domain.PersonAliasKindStage}}, profile.Aliases)
	assert.Contains(t, f.stored(t, result.RevisionID).flags, string(domain.FlagHiddenAliasCollision))
}

// TestRepository_GetMyRevision_SubmittedBase checks that the author's views compare with the base they edited
// from, whatever the current revision is since.
func TestRepository_GetMyRevision_SubmittedBase(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	result := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	current := songDocument(f)
	current.Title = "The Wren's Song Retitled"
	moved := f.c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: f.song.ID,
		Snapshot: current, BaseRevisionID: f.song.RevisionID})
	f.c.Exec("UPDATE pieces SET current_revision_id = ? WHERE id = ?", moved, f.song.ID)
	// A rebase moves the base, never the submitted base (R49's author half).
	f.c.Exec("UPDATE revisions SET base_revision_id = ? WHERE id = ?", moved, result.RevisionID)

	revision, err := f.repo.GetMyRevision(t.Context(), actorWren, result.RevisionID)
	require.NoError(t, err)
	assert.Equal(t, songTitle, revision.BaseSnapshot.(domain.PieceSnapshot).Title)
	assert.Equal(t, songTitle, revision.Snapshot.(domain.PieceSnapshot).Title)
	assert.Equal(t, &f.song.RevisionID, revision.SubmittedBaseRevisionID)
	assert.Equal(t, &moved, revision.BaseRevisionID)
}

func TestRepository_GetMyRevision_Approved(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	doc := morwenDocument(f)
	doc.SchemaVersion = domain.DocumentSchemaVersion
	approved := f.c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePerson, EntityID: f.morwen.ID,
		Snapshot: doc, Source: domain.RevisionSourceUser, AuthorUserID: wrenUserID,
		BaseRevisionID: f.morwen.RevisionID})

	revision, err := f.repo.GetMyRevision(t.Context(), actorWren, approved)
	require.NoError(t, err)
	public, err := f.repo.GetPublicRevision(t.Context(), approved)
	require.NoError(t, err)
	assert.Equal(t, public.(domain.PersonRevision).Snapshot, revision.Snapshot, "the public view")
	assert.Equal(t, public.(domain.PersonRevision).Base.Snapshot, revision.BaseSnapshot)
	profile := revision.Snapshot.(domain.PersonSnapshot).Profile
	require.NotNil(t, profile)
	assert.Len(t, profile.Aliases, 1, "no hidden alias")
	assert.Len(t, profile.Links, 1, "no hidden link")
}

// TestRepository_Draft_PreviewIsTheAuthorView checks W38: a draft's preview shows a typed hidden-link URL, and equals
// the author's view after submit.
func TestRepository_Draft_PreviewIsTheAuthorView(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := pieceKey(f.song.Slug)
	doc := f.editable(t, actorWren, *key).Document.(domain.PieceDocument)
	doc.Links = append(doc.Links, domain.DocPieceLink{DocLink: domain.DocLink{URL: songHiddenURL,
		Roles: []domain.LinkRole{domain.LinkRolePerformance}}})
	draft := f.saveDraft(t, actorWren, key, doc)
	preview := draft.Draft.Preview.(domain.PieceSnapshot)
	require.Len(t, preview.Links, 2)
	assert.Equal(t, songHiddenURL, preview.Links[1].URL)

	result := f.submit(t, actorWren, draft.Draft.ID)
	revision, err := f.repo.GetMyRevision(t.Context(), actorWren, result.RevisionID)
	require.NoError(t, err)
	assert.Equal(t, preview, revision.Snapshot)

	public, err := f.repo.GetPublicPiece(t.Context(), f.song.Slug)
	require.NoError(t, err)
	for _, link := range public.Links {
		assert.NotEqual(t, songHiddenURL, link.URL)
	}

	// The preview of a new person's draft shows them by the name it gives them.
	person := f.saveDraft(t, actorWren, nil, newPersonDocument("Rowan Redlink"))
	assert.Equal(t, "Rowan Redlink", person.Draft.Preview.(domain.PersonSnapshot).Name)
}

func TestRepository_GetPendingVersion(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	_, err := f.repo.GetPendingVersion(t.Context(), actorWren, key)
	require.ErrorIs(t, err, datasources.ErrNotFound, "no pending revision")

	result := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	pending, err := f.repo.GetPendingVersion(t.Context(), actorWren, key)
	require.NoError(t, err)
	assert.Equal(t, result.RevisionID, pending.ID)
	assert.Equal(t, editedNotes, *pending.Snapshot.(domain.PieceSnapshot).NotesMD)

	_, err = f.repo.GetPendingVersion(t.Context(), actorAsh, key)
	require.ErrorIs(t, err, datasources.ErrNotFound, "another user's pending revision")

	person := f.newPending(t, actorWren, newPersonDocument("Rowan Redlink"))
	placeholder := domain.EntityKey{Type: domain.EntityTypePerson, Slug: person.Entity.Slug}
	own, err := f.repo.GetPendingVersion(t.Context(), actorWren, placeholder)
	require.NoError(t, err)
	assert.Equal(t, "Rowan Redlink", own.Snapshot.(domain.PersonSnapshot).Name)
	assert.True(t, own.Entity.IsNew)
	assert.Nil(t, own.BaseSnapshot)
	_, err = f.repo.GetPendingVersion(t.Context(), actorAsh, placeholder)
	require.ErrorIs(t, err, datasources.ErrNotFound)
}
