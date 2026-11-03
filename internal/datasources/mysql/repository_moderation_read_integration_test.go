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

// queue lists the moderation queue for query, failing the test on an error, and returns the listed revisions' IDs
// with the queue.
func (f *editingFixture) queue(t *testing.T, query datasources.ModerationQueueQuery) ([]int64, domain.ModerationQueue) {
	t.Helper()
	if query.Page.Limit == 0 {
		query.Page.Limit = 10
	}
	queue, err := f.repo.ListModerationQueue(t.Context(), query)
	require.NoError(t, err)
	ids := []int64{}
	for _, item := range queue.Items {
		ids = append(ids, item.ID)
	}

	return ids, queue
}

func (f *editingFixture) moderationRevision(t *testing.T, id int64) domain.ModerationRevision {
	t.Helper()
	revision, err := f.repo.GetModerationRevision(t.Context(), id)
	require.NoError(t, err)

	return revision
}

func TestRepository_ModerationQueue_Filters(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	song := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.NotesMD = new(editedNotes)
	}).RevisionID
	morwen := editAndSubmit(t, f, actorAsh, *personKey(f.morwen.Slug), func(d *domain.PersonDocument) {
		d.NotesMD = new("A singer of wren songs.")
	}).RevisionID
	term := f.newPending(t, actorWren, newTermDocument("Sea Songs"))

	ids, queue := f.queue(t, datasources.ModerationQueueQuery{Now: f.now})
	assert.Equal(t, []int64{song, morwen, term.ID}, ids, "oldest first")
	assert.Equal(t, int64(3), queue.Total)
	first := queue.Items[0]
	assert.Equal(t, domain.EntityTypePiece, first.EntityType)
	assert.Equal(t, &domain.RevisionEntity{Type: domain.EntityTypePiece, ID: f.song.ID, Slug: f.song.Slug}, first.Entity)
	assert.Equal(t, songTitle, first.Title)
	assert.Equal(t, draftSummary, first.Summary)
	assert.Equal(t, domain.ModerationAuthor{ID: wrenUserID, DisplayName: wrenDisplay, Role: domain.RoleContributor},
		first.Author)
	assert.False(t, first.Stale)
	assert.False(t, first.IsNew)
	assert.NotNil(t, first.Flags)
	last := queue.Items[2]
	assert.True(t, last.IsNew)
	assert.False(t, last.Stale, "a new entity's revision has no base, and its entity no current revision")
	assert.Equal(t, "Sea Songs", last.Title)
	assert.Equal(t, term.Entity, last.Entity)

	persons := domain.EntityTypePerson
	ids, queue = f.queue(t, datasources.ModerationQueueQuery{EntityType: &persons, Now: f.now})
	assert.Equal(t, []int64{morwen}, ids)
	assert.Equal(t, int64(1), queue.Total)
	ash := ashUserID
	ids, _ = f.queue(t, datasources.ModerationQueueQuery{AuthorUserID: &ash, Now: f.now})
	assert.Equal(t, []int64{morwen}, ids)
	ids, queue = f.queue(t, datasources.ModerationQueueQuery{Page: domain.Page{Limit: 1, Offset: 1}, Now: f.now})
	assert.Equal(t, []int64{morwen}, ids)
	assert.Equal(t, int64(3), queue.Total)

	// Ages, from submission, at an hour after the second submission: the bounds are inclusive.
	at := queue.Items[0].SubmittedAt.Add(time.Hour)
	hour := int32(1)
	ids, queue = f.queue(t, datasources.ModerationQueueQuery{MinAgeHours: &hour, Now: at})
	assert.Equal(t, []int64{song, morwen}, ids)
	assert.Equal(t, int64(2), queue.Total)
	ids, _ = f.queue(t, datasources.ModerationQueueQuery{MaxAgeHours: &hour, Now: at})
	assert.Equal(t, []int64{morwen, term.ID}, ids)
	ids, _ = f.queue(t, datasources.ModerationQueueQuery{MinAgeHours: &hour, MaxAgeHours: &hour, Now: at})
	assert.Equal(t, []int64{morwen}, ids)

	// Another revision of the song approved: Wren's is stale.
	f.mustApprove(t, editAndSubmit(t, f, actorAsh, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.VersionNotesMD = new("As sung at the fair.")
	}).RevisionID)
	_, queue = f.queue(t, datasources.ModerationQueueQuery{Now: f.now})
	require.Equal(t, song, queue.Items[0].ID)
	assert.True(t, queue.Items[0].Stale)

	f.c.Exec("UPDATE users SET blocked_at = ? WHERE id = ?", f.now, ashUserID)
	ids, queue = f.queue(t, datasources.ModerationQueueQuery{Now: f.now})
	assert.Equal(t, []int64{song, term.ID}, ids, "blocked authors' revisions are left out")
	assert.Equal(t, int64(2), queue.Total)
	ids, queue = f.queue(t, datasources.ModerationQueueQuery{IncludeBlocked: true, Now: f.now})
	assert.Equal(t, []int64{song, morwen, term.ID}, ids)
	assert.Equal(t, int64(3), queue.Total)
	assert.True(t, queue.Items[1].Author.Blocked)
}

// TestRepository_GetModerationRevision_RebaseConflicts checks the moderation view of stale revisions whose base is
// the imported revision: one conflicting with the current document on the title, one merging cleanly.
func TestRepository_GetModerationRevision_RebaseConflicts(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	conflicting := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.Title = "The Wren's Lament"
	}).RevisionID
	current := editAndSubmit(t, f, actorAsh, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.Title = "The Wren's Air"
	}).RevisionID
	f.mustApprove(t, current)

	revision := f.moderationRevision(t, conflicting)
	assert.Equal(t, domain.RevisionStatusPending, revision.Status)
	assert.Equal(t, domain.RevisionSourceUser, revision.Source)
	assert.Equal(t, &domain.ModerationAuthor{ID: wrenUserID, DisplayName: wrenDisplay, Role: domain.RoleContributor},
		revision.Author)
	assert.Equal(t, []string{}, revision.Contributors)
	assert.Nil(t, revision.Reviewer)
	assert.True(t, revision.Stale)
	assert.Equal(t, &f.song.RevisionID, revision.BaseRevisionID)
	assert.Equal(t, &f.song.RevisionID, revision.SubmittedBaseRevisionID)
	assert.Equal(t, &current, revision.CurrentRevisionID)
	assert.Equal(t, songTitle, revision.BaseDocument.(domain.PieceDocument).Title)
	assert.Equal(t, "The Wren's Air", revision.CurrentDocument.(domain.PieceDocument).Title)
	assert.Equal(t, "The Wren's Lament", revision.Document.(domain.PieceDocument).Title)
	assert.NotNil(t, revision.SubmittedDocument)
	assert.Equal(t, domain.RebaseStatusConflicts, revision.Rebase.Status)
	assert.Nil(t, revision.Rebase.MergedDocument)
	require.Len(t, revision.Rebase.Conflicts, 1)
	assert.Equal(t, domain.FieldPath("title"), revision.Rebase.Conflicts[0].Path)
	assert.JSONEq(t, `"The Wren's Lament"`, string(revision.Rebase.Conflicts[0].Revision))
	assert.JSONEq(t, `"The Wren's Air"`, string(revision.Rebase.Conflicts[0].Current))
	assert.Equal(t, []string{"title"}, revision.ChangedPaths)
	assert.Equal(t, []domain.DuplicateMatch{}, revision.Duplicates, "an existing entity has no duplicates")

	// Moderators see everything: the hidden link, with its state, the preferences and who prefers no page.
	doc := revision.Document.(domain.PieceDocument)
	require.Len(t, doc.Links, 2)
	assert.Equal(t, songHiddenURL, doc.Links[1].URL)
	assert.Len(t, doc.Preferences, 1)
	require.Len(t, revision.LinkStates, 2)
	assert.Equal(t, songHiddenURL, revision.LinkStates[1].URL)
	assert.True(t, revision.LinkStates[1].IsHidden)
	assert.NotNil(t, revision.LinkStates[1].LinkID)
	assert.False(t, revision.LinkStates[0].IsHidden)
	labels := map[string]bool{}
	for _, ref := range revision.References {
		labels[ref.Label] = true
	}
	assert.True(t, labels[quietHarper] && labels[morwenName] && labels[balladsTerm] && labels[fairEvent], labels)

	approved := f.moderationRevision(t, current)
	assert.Equal(t, domain.RevisionStatusApproved, approved.Status)
	assert.Equal(t, &domain.ModerationAuthor{ID: rowanUserID, DisplayName: "Rowan", Role: domain.RoleModerator},
		approved.Reviewer)
	assert.False(t, approved.Stale, "only a pending revision is stale")
	assert.Equal(t, domain.RebaseStatusNotNeeded, approved.Rebase.Status)

	clean := editAndSubmit(t, f, actorWren, *personKey(f.morwen.Slug), func(d *domain.PersonDocument) {
		d.NotesMD = new("A singer of wren songs.")
	}).RevisionID
	f.mustApprove(t, editAndSubmit(t, f, actorAsh, *personKey(f.morwen.Slug), func(d *domain.PersonDocument) {
		d.Pronouns = new("she/her")
	}).RevisionID)
	merged := f.moderationRevision(t, clean)
	assert.True(t, merged.Stale)
	assert.Equal(t, domain.RebaseStatusClean, merged.Rebase.Status)
	assert.Empty(t, merged.Rebase.Conflicts)
	mergedDoc := merged.Rebase.MergedDocument.(domain.PersonDocument)
	assert.Equal(t, "A singer of wren songs.", *mergedDoc.NotesMD)
	assert.Equal(t, "she/her", *mergedDoc.Pronouns)
	assert.Contains(t, merged.Document.(domain.PersonDocument).Aliases,
		domain.DocPersonAlias{Name: morwenLegal, Kind: domain.PersonAliasKindLegal}, "the hidden alias is shown")
	assert.Equal(t, []string{"notes_md"}, merged.ChangedPaths)
}

func TestRepository_GetModerationRevision_ImportedRevision(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	imported := f.c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: f.song.ID,
		Snapshot: songDocument(f), Summary: "Wiki import", Contributors: []string{"Wiki Editor B", "Wiki Editor A"}})

	revision := f.moderationRevision(t, imported)
	assert.Nil(t, revision.Author, "an imported revision has no author")
	assert.Equal(t, []string{"Wiki Editor B", "Wiki Editor A"}, revision.Contributors)
	assert.Equal(t, domain.RevisionSourceImport, revision.Source)
	assert.Equal(t, domain.RevisionStatusApproved, revision.Status)
	assert.Nil(t, revision.SubmittedDocument)
	assert.Nil(t, revision.BaseDocument)
	assert.Equal(t, &f.song.RevisionID, revision.CurrentRevisionID)
	assert.Equal(t, domain.RebaseStatusNotNeeded, revision.Rebase.Status)
	assert.Equal(t, []domain.ModerationPendingReference{}, revision.PendingReferences)
	assert.Equal(t, []domain.Flag{}, revision.Flags)
	assert.Contains(t, revision.ChangedPaths, "title", "compared with the default document")
}

func TestRepository_GetModerationRevision_PendingReferences(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	term := f.newPending(t, actorWren, newTermDocument("Sea Songs"))
	person := f.newPending(t, actorWren, newPersonDocument("Rowan Redlink"))
	doc := newPieceDocument(songTitle)
	doc.Terms = []domain.DocPieceTerm{{TermID: term.Entity.ID, IsPrimary: true}}
	doc.Credits = []domain.DocCredit{{PersonID: &person.Entity.ID, Role: domain.CreditRoleLyrics,
		Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain}}
	piece := f.submit(t, actorWren, f.saveDraft(t, actorWren, nil, doc).Draft.ID).RevisionID

	revision := f.moderationRevision(t, piece)
	require.NotNil(t, revision.Entity)
	assert.True(t, revision.Entity.IsNew)
	assert.Nil(t, revision.CurrentRevisionID)
	assert.Nil(t, revision.CurrentDocument)
	assert.False(t, revision.Stale)
	require.Len(t, revision.PendingReferences, 2)
	first := revision.PendingReferences[0]
	assert.Equal(t, term.ID, first.RevisionID, "the term first: references are visited terms first")
	assert.Equal(t, domain.EntityTypeTerm, first.EntityType)
	assert.Equal(t, *term.Entity, first.Entity)
	assert.Equal(t, "Sea Songs", first.Document.(domain.TermDocument).Name)
	assert.Equal(t, []domain.Flag{}, first.Flags)
	assert.Equal(t, person.ID, revision.PendingReferences[1].RevisionID)
	assert.Equal(t, *person.Entity, revision.PendingReferences[1].Entity)
	codes := map[domain.FlagCode]bool{}
	for _, flag := range revision.Flags {
		codes[flag.Code] = true
	}
	assert.True(t, codes[domain.FlagReferencesPending], revision.Flags)
	assert.True(t, codes[domain.FlagPossibleDuplicate], revision.Flags)
	require.Len(t, revision.Duplicates, 1, "the song, and not the revision's own pending piece")
	assert.Equal(t, f.song.Slug, revision.Duplicates[0].Slug)
	assert.Contains(t, revision.References, domain.EditorReference{EntityType: domain.EntityTypeTerm,
		ID: term.Entity.ID, Slug: term.Entity.Slug, Label: "Sea Songs", IsPending: true})
	assert.Contains(t, revision.ChangedPaths, "title")
}

func TestRepository_GetModerationRevision_NotFound(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	draft := f.saveDraft(t, actorWren, nil, newPieceDocument(unsungTitle))

	for _, id := range []int64{draft.Draft.ID, 0, draft.Draft.ID + 1000} {
		_, err := f.repo.GetModerationRevision(t.Context(), id)
		require.ErrorIs(t, err, datasources.ErrNotFound, "revision [%d]: drafts are private to their author", id)
	}
}

func TestRepository_GetModerationLink(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)

	hidden, err := f.repo.GetModerationLink(t.Context(), songHiddenURL)
	require.NoError(t, err)
	assert.True(t, hidden.IsHidden)
	assert.Equal(t, songHiddenURL, hidden.URL)
	assert.Equal(t, domain.LivenessUnknown, hidden.Liveness)
	assert.Equal(t, []domain.ModerationLinkAttachment{{EntityType: domain.EntityTypePiece, Slug: f.song.Slug,
		Title: songTitle, Public: true, Roles: []domain.LinkRole{domain.LinkRolePerformance}}}, hidden.Attachments)
	assert.Equal(t, int64(0), hidden.OpenReportCount)

	for _, status := range []string{"open", "open", "dismissed"} {
		f.c.Exec("INSERT INTO link_reports (link_id, reporter_user_id, reason, status, created_at) "+
			"VALUES (?, ?, 'privacy', ?, ?)", hidden.LinkID, ashUserID, status, f.now)
	}
	reported, err := f.repo.GetModerationLink(t.Context(), songHiddenURL)
	require.NoError(t, err)
	assert.Equal(t, int64(2), reported.OpenReportCount, "open reports only")

	lyrics, err := f.repo.GetModerationLink(t.Context(), songLyricsURL)
	require.NoError(t, err)
	assert.False(t, lyrics.IsHidden)
	assert.Equal(t, &domain.CollectionRef{ID: f.archive.ID, Name: wrenArchive}, lyrics.Collection)
	assert.Equal(t, []domain.ModerationLinkAttachment{{EntityType: domain.EntityTypePiece, Slug: f.song.Slug,
		Title: songTitle, Public: true, Roles: []domain.LinkRole{domain.LinkRoleLyrics}}}, lyrics.Attachments)

	site, err := f.repo.GetModerationLink(t.Context(), morwenHidden)
	require.NoError(t, err)
	assert.True(t, site.IsHidden)
	assert.Equal(t, []domain.ModerationLinkAttachment{{EntityType: domain.EntityTypePerson, Slug: f.morwen.Slug,
		Title: morwenName, Public: true, Roles: []domain.LinkRole{domain.LinkRoleWebsite}}}, site.Attachments)

	for _, url := range []string{"not a url", "https://example.org/nothing-here"} {
		_, err := f.repo.GetModerationLink(t.Context(), url)
		require.ErrorIs(t, err, datasources.ErrNotFound, url)
	}
}
