//go:build integration

package mysql_test

import (
	"context"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repertoireFixture is an editing fixture with two more public pieces, an unapproved piece and a pending new piece
// of Wren's, whose placeholder slug addresses no public piece.
type repertoireFixture struct {
	*editingFixture
	ballad, reel catalogtest.Ref
	unapproved   catalogtest.Ref
	placeholder  string
}

func newRepertoireFixture(t *testing.T) *repertoireFixture {
	t.Helper()
	f := &repertoireFixture{editingFixture: newEditingFixture(t)}
	f.ballad = f.c.Piece(catalogtest.Piece{Doc: newPieceDocument("A Ballad of Ash")})
	f.reel = f.c.Piece(catalogtest.Piece{Doc: newPieceDocument("The Kettle Reel")})
	f.unapproved = f.c.Piece(catalogtest.Piece{Doc: newPieceDocument(unsungTitle), Unapproved: true})
	f.placeholder = f.newPending(t, actorWren, newPieceDocument("A Pending Air")).Entity.Slug

	return f
}

// put writes userID's entry for slug with the cap maxEntries, at the fixture's next minute.
func (f *repertoireFixture) put(
	t *testing.T, userID, slug string, notes *string, visibility domain.RepertoireVisibility, maxEntries int,
) (domain.RepertoireEntry, bool, error) {
	t.Helper()

	return f.repo.PutRepertoireEntry(t.Context(), datasources.RepertoireEntryWrite{
		UserID: userID, PieceSlug: slug, Notes: notes, Visibility: visibility, MaxEntries: maxEntries, Now: f.tick(),
	})
}

// songSummary is the summary of the fixture's song: its credit line names Morwen, never the person who prefers no
// page.
func (f *repertoireFixture) songSummary() domain.PieceSummary {
	return domain.PieceSummary{Slug: f.song.Slug, Title: songTitle, Kind: domain.PieceKindSong,
		ConservationStatus: domain.ConservationStatusUnassessed, CreditNames: []string{morwenName}}
}

// TestRepository_PutRepertoireEntry walks the repertoire writes on one fixture, each step on the state the
// previous left: adding, updating (which keeps added_at), the pieces that are not public, the cap, removing and
// a block committed since the request was authenticated.
func TestRepository_PutRepertoireEntry(t *testing.T) {
	t.Parallel()
	f := newRepertoireFixture(t)
	public, private := domain.RepertoireVisibilityPublic, domain.RepertoireVisibilityPrivate
	const maxEntries = 2

	var added domain.RepertoireEntry
	steps := []struct {
		name        string
		userID      string
		slug        string
		notes       *string
		visibility  domain.RepertoireVisibility
		wantErr     error
		wantCreated bool
		check       func(t *testing.T, entry domain.RepertoireEntry)
	}{
		{
			name: "add", userID: wrenUserID, slug: f.song.Slug, notes: new("Capo 2."), visibility: private,
			wantCreated: true,
			check: func(t *testing.T, entry domain.RepertoireEntry) {
				added = entry
				assert.Equal(t, domain.RepertoireEntry{Piece: f.songSummary(), Notes: new("Capo 2."),
					Visibility: private, AddedAt: f.now, UpdatedAt: f.now}, entry)
			},
		},
		{
			name: "update keeps added_at", userID: wrenUserID, slug: f.song.Slug, visibility: public,
			check: func(t *testing.T, entry domain.RepertoireEntry) {
				assert.Equal(t, domain.RepertoireEntry{Piece: f.songSummary(), Visibility: public,
					AddedAt: added.AddedAt, UpdatedAt: f.now}, entry)
				assert.True(t, entry.UpdatedAt.After(entry.AddedAt))
			},
		},
		{name: "unapproved piece", userID: wrenUserID, slug: f.unapproved.Slug, visibility: public,
			wantErr: datasources.ErrNotFound},
		{name: "own placeholder", userID: wrenUserID, slug: f.placeholder, visibility: public,
			wantErr: datasources.ErrNotFound},
		{name: "unknown piece", userID: wrenUserID, slug: unknownPieceSlug, visibility: public,
			wantErr: datasources.ErrNotFound},
		{name: "second entry", userID: wrenUserID, slug: f.ballad.Slug, visibility: public, wantCreated: true},
		{name: "cap", userID: wrenUserID, slug: f.reel.Slug, visibility: public,
			wantErr: datasources.ErrRepertoireLimitReached},
		{
			name: "an update of a full repertoire", userID: wrenUserID, slug: f.ballad.Slug, notes: new("Slow."),
			visibility: private,
			check: func(t *testing.T, entry domain.RepertoireEntry) {
				assert.Equal(t, new("Slow."), entry.Notes)
			},
		},
		{name: "another user's cap is their own", userID: ashUserID, slug: f.reel.Slug, visibility: public,
			wantCreated: true},
	}
	for _, step := range steps {
		entry, created, err := f.put(t, step.userID, step.slug, step.notes, step.visibility, maxEntries)
		if step.wantErr != nil {
			require.ErrorIs(t, err, step.wantErr, step.name)
			continue
		}
		require.NoError(t, err, step.name)
		assert.Equal(t, step.wantCreated, created, step.name)
		if step.check != nil {
			step.check(t, entry)
		}
	}

	t.Run("delete", func(t *testing.T) {
		ctx := t.Context()
		require.NoError(t, f.repo.DeleteRepertoireEntry(ctx, wrenUserID, f.ballad.Slug))
		require.ErrorIs(t, f.repo.DeleteRepertoireEntry(ctx, wrenUserID, f.ballad.Slug), datasources.ErrNotFound)
		require.ErrorIs(t, f.repo.DeleteRepertoireEntry(ctx, wrenUserID, f.reel.Slug), datasources.ErrNotFound,
			"another user's entry is not the caller's")
		require.ErrorIs(t, f.repo.DeleteRepertoireEntry(ctx, wrenUserID, f.placeholder), datasources.ErrNotFound)
		_, err := f.repo.GetRepertoireEntry(ctx, wrenUserID, f.ballad.Slug)
		require.ErrorIs(t, err, datasources.ErrNotFound)
		_, err = f.repo.GetRepertoireEntry(ctx, ashUserID, f.reel.Slug)
		require.NoError(t, err, "another user's entry is untouched")
	})

	t.Run("blocked at the lock", func(t *testing.T) {
		require.NoError(t, f.repo.BlockUser(t.Context(), rowanUserID, wrenUserID, f.now))
		_, _, err := f.put(t, wrenUserID, f.reel.Slug, nil, public, maxEntries)
		require.ErrorIs(t, err, datasources.ErrUserBlocked)
		require.ErrorIs(t, f.repo.DeleteRepertoireEntry(t.Context(), wrenUserID, f.song.Slug),
			datasources.ErrUserBlocked)
		entry, err := f.repo.GetRepertoireEntry(t.Context(), wrenUserID, f.song.Slug)
		require.NoError(t, err, "a blocked user's entries are kept")
		assert.Equal(t, public, entry.Visibility)
	})
}

// TestRepository_PutRepertoireEntry_ConcurrentCap requires two concurrent new entries of a repertoire one short of
// its cap to be serialised by the users row lock: one is added and the other refused. A holder locks the users row
// until both writes are seen waiting for it, so that both read the repertoire only after it commits, and race.
func TestRepository_PutRepertoireEntry_ConcurrentCap(t *testing.T) {
	t.Parallel()
	f := newRepertoireFixture(t)
	const maxEntries = 2
	_, _, err := f.put(t, wrenUserID, f.song.Slug, nil, domain.RepertoireVisibilityPrivate, maxEntries)
	require.NoError(t, err)

	holder := holdRows(t, f.db, "SELECT id FROM users WHERE id = ? FOR UPDATE", wrenUserID)
	writes := make([]<-chan error, 0, 2)
	for _, slug := range []string{f.ballad.Slug, f.reel.Slug} {
		entry := datasources.RepertoireEntryWrite{UserID: wrenUserID, PieceSlug: slug,
			Visibility: domain.RepertoireVisibilityPublic, MaxEntries: maxEntries, Now: f.now}
		writes = append(writes, started(t.Context(), func(ctx context.Context) error {
			_, _, err := f.repo.PutRepertoireEntry(ctx, entry)
			return err
		}))
	}
	waitUntilBlocked(t, f.db, 2)
	holder.commit()

	var added, refused int
	for _, done := range writes {
		err := <-done
		if err == nil {
			added++
			continue
		}
		require.ErrorIs(t, err, datasources.ErrRepertoireLimitReached)
		refused++
	}
	assert.Equal(t, 1, added)
	assert.Equal(t, 1, refused)

	list, err := f.repo.ListRepertoire(t.Context(), datasources.RepertoireQuery{UserID: wrenUserID,
		Page: domain.Page{Limit: 10}})
	require.NoError(t, err)
	assert.Equal(t, int64(maxEntries), list.Total)
}

// TestRepository_ListRepertoire checks the owner's own list: both visibilities with their notes, newest first, then
// by piece ID, the visibility filter, paging, and an entry of a piece that is no longer public left out.
func TestRepository_ListRepertoire(t *testing.T) {
	t.Parallel()
	f := newRepertoireFixture(t)
	public, private := domain.RepertoireVisibilityPublic, domain.RepertoireVisibilityPrivate
	for _, e := range []struct {
		slug       string
		visibility domain.RepertoireVisibility
	}{{f.song.Slug, public}, {f.ballad.Slug, private}, {f.reel.Slug, public}} {
		_, _, err := f.put(t, wrenUserID, e.slug, new("Notes on "+e.slug), e.visibility, domain.MaxRepertoireEntries)
		require.NoError(t, err)
	}
	// An entry of a piece that is not public, as only a piece taken down after it was added leaves.
	f.c.Exec("INSERT INTO repertoire (user_id, piece_id, notes, visibility, added_at, updated_at) "+
		"VALUES (?, ?, 'Hidden', 'public', ?, ?)", wrenUserID, f.unapproved.ID, f.tick(), f.now)

	list := func(visibility *domain.RepertoireVisibility, page domain.Page) domain.RepertoireList {
		t.Helper()
		got, err := f.repo.ListRepertoire(t.Context(), datasources.RepertoireQuery{
			UserID: wrenUserID, Visibility: visibility, Page: page,
		})
		require.NoError(t, err)
		return got
	}
	slugs := func(l domain.RepertoireList) []string {
		var s []string
		for _, e := range l.Entries {
			s = append(s, e.Piece.Slug)
			assert.Equal(t, new("Notes on "+e.Piece.Slug), e.Notes, "the owner sees their notes")
		}
		return s
	}

	all := list(nil, domain.Page{Limit: 10})
	assert.Equal(t, int64(3), all.Total)
	assert.Equal(t, []string{f.reel.Slug, f.ballad.Slug, f.song.Slug}, slugs(all), "newest first")
	assert.Equal(t, f.songSummary(), all.Entries[2].Piece)
	page := list(nil, domain.Page{Limit: 1, Offset: 1})
	assert.Equal(t, int64(3), page.Total)
	assert.Equal(t, []string{f.ballad.Slug}, slugs(page))
	publicOnly := list(&public, domain.Page{Limit: 10})
	assert.Equal(t, int64(2), publicOnly.Total)
	assert.Equal(t, []string{f.reel.Slug, f.song.Slug}, slugs(publicOnly))
	privateOnly := list(&private, domain.Page{Limit: 10})
	assert.Equal(t, []string{f.ballad.Slug}, slugs(privateOnly))
	_, err := f.repo.GetRepertoireEntry(t.Context(), wrenUserID, f.unapproved.Slug)
	require.ErrorIs(t, err, datasources.ErrNotFound)

	// Entries added in the same second are ordered by piece ID, newest piece first.
	same := f.tick()
	for _, ref := range []catalogtest.Ref{f.song, f.ballad} {
		f.c.Exec("UPDATE repertoire SET added_at = ? WHERE user_id = ? AND piece_id = ?", same, wrenUserID, ref.ID)
	}
	assert.Equal(t, []string{f.ballad.Slug, f.song.Slug, f.reel.Slug}, slugs(list(nil, domain.Page{Limit: 10})))

	empty, err := f.repo.ListRepertoire(t.Context(), datasources.RepertoireQuery{UserID: ashUserID,
		Page: domain.Page{Limit: 10}})
	require.NoError(t, err)
	assert.Equal(t, domain.RepertoireList{Entries: []domain.RepertoireEntry{}}, empty)
}

// TestRepository_ListPublicRepertoire checks the public list: only public entries of public pieces, without notes
// (the entry type has none), newest first; and that an unknown public ID, a blocked user and a user with only
// private entries are each the same not found.
func TestRepository_ListPublicRepertoire(t *testing.T) {
	t.Parallel()
	f := newRepertoireFixture(t)
	ctx := t.Context()
	public, private := domain.RepertoireVisibilityPublic, domain.RepertoireVisibilityPrivate
	const wrenPublicID, ashPublicID, rowanPublicID = "wrenwrenwrenwren", "ashashashashash2", "rowanrowanrowan2"
	for userID, publicID := range map[string]string{wrenUserID: wrenPublicID, ashUserID: ashPublicID,
		rowanUserID: rowanPublicID} {
		require.NoError(t, f.repo.SetUserPublicIDIfMissing(ctx, userID, publicID))
	}
	writes := []struct {
		userID, slug string
		visibility   domain.RepertoireVisibility
	}{
		{wrenUserID, f.song.Slug, public}, {wrenUserID, f.ballad.Slug, private}, {wrenUserID, f.reel.Slug, public},
		{ashUserID, f.song.Slug, private}, {rowanUserID, f.ballad.Slug, public},
	}
	for _, w := range writes {
		_, _, err := f.put(t, w.userID, w.slug, new("leakmark-repertoire-note"), w.visibility, domain.MaxRepertoireEntries)
		require.NoError(t, err)
	}
	f.c.Exec("INSERT INTO repertoire (user_id, piece_id, visibility, added_at, updated_at) VALUES (?, ?, 'public', ?, ?)",
		wrenUserID, f.unapproved.ID, f.tick(), f.now)
	reelAdded, songAdded := f.now.Add(-3*time.Minute), f.now.Add(-5*time.Minute)

	got, err := f.repo.ListPublicRepertoire(ctx, wrenPublicID, domain.Page{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, domain.PublicRepertoire{
		Owner: domain.RepertoireOwner{DisplayName: wrenDisplay, Role: domain.RoleContributor},
		Entries: []domain.PublicRepertoireEntry{
			{Piece: domain.PieceSummary{Slug: f.reel.Slug, Title: "The Kettle Reel", Kind: domain.PieceKindSong,
				ConservationStatus: domain.ConservationStatusUnassessed}, AddedAt: reelAdded},
			{Piece: f.songSummary(), AddedAt: songAdded},
		},
		Total: 2,
	}, got)
	paged, err := f.repo.ListPublicRepertoire(ctx, wrenPublicID, domain.Page{Limit: 1, Offset: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(2), paged.Total)
	assert.Equal(t, []domain.PublicRepertoireEntry{{Piece: f.songSummary(), AddedAt: songAdded}}, paged.Entries)
	past, err := f.repo.ListPublicRepertoire(ctx, wrenPublicID, domain.Page{Limit: 1, Offset: 5})
	require.NoError(t, err)
	assert.Equal(t, domain.PublicRepertoire{Owner: got.Owner, Entries: []domain.PublicRepertoireEntry{}, Total: 2},
		past, "a page past the end of a shown repertoire is empty, with its total")

	moderator, err := f.repo.ListPublicRepertoire(ctx, rowanPublicID, domain.Page{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, domain.RepertoireOwner{DisplayName: contactModeratorName, Role: domain.RoleModerator}, moderator.Owner)

	require.NoError(t, f.repo.BlockUser(ctx, rowanUserID, wrenUserID, f.now))
	for name, publicID := range map[string]string{
		"blocked": wrenPublicID, "only private entries": ashPublicID, "unknown public ID": "unknownunknown22",
	} {
		_, err := f.repo.ListPublicRepertoire(ctx, publicID, domain.Page{Limit: 10})
		require.ErrorIs(t, err, datasources.ErrNotFound, name)
	}
}

// TestRepository_PutRepertoireEntry_ApprovalLockOrder requires a repertoire write and an approval of the same piece,
// both waiting for its row, to wait for one another rather than deadlock: the repertoire write locks its users row
// and then, through the foreign key, the piece row, after the users and revision rows of the approval's order
// (docs/PLAN.md §5.1 "Approval").
func TestRepository_PutRepertoireEntry_ApprovalLockOrder(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	edit := editAndSubmit(t, f, actorAsh, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.NotesMD = new("Sung at the turning of the year.")
	})
	f.c.Exec("INSERT INTO users (id, display_name, role, created_at) VALUES ('google-oauth2|approver', "+
		"'Approver', 'moderator', ?)", f.now)
	approval := datasources.ApproveRequest{Actor: domain.Actor{UserID: "google-oauth2|approver",
		Role: domain.RoleModerator}, RevisionID: edit.RevisionID, Duplicates: f.approvalChecks(t, edit.RevisionID),
		Now: f.tick()}
	entry := datasources.RepertoireEntryWrite{UserID: wrenUserID, PieceSlug: f.song.Slug,
		Visibility: domain.RepertoireVisibilityPublic, MaxEntries: domain.MaxRepertoireEntries, Now: f.now}

	holder := holdRows(t, f.db, "SELECT id FROM pieces WHERE id = ? FOR UPDATE", f.song.ID)
	approved := started(t.Context(), func(ctx context.Context) error { return f.repo.ApproveRevisionOnce(ctx, approval) })
	put := started(t.Context(), func(ctx context.Context) error { return f.repo.PutRepertoireEntryOnce(ctx, entry) })
	waitUntilBlocked(t, f.db, 2)
	holder.commit()

	for name, done := range map[string]<-chan error{"approval": approved, "repertoire write": put} {
		err := <-done
		assert.False(t, isDeadlock(err), "%s deadlocked: %v", name, err)
		require.NoError(t, err, name)
	}
	assert.Equal(t, statusApproved, f.stored(t, edit.RevisionID).status)
	_, err := f.repo.GetRepertoireEntry(t.Context(), wrenUserID, f.song.Slug)
	require.NoError(t, err)
}
