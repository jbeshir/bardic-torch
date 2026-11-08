//go:build integration

package mysql_test

import (
	"context"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/domaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The users-row tests change a user while a write by them waits for their users row: a holder locks the row and
// changes it, the write starts and is seen waiting, and the holder commits. The request was authenticated before
// the change, so the write is refused only if it checks the row again under its lock, and a write that does not
// take the users row first never waits, failing the test.

// changedMidWrite runs write while a holder that locked the users row of userID and ran update on it (its one
// argument the user's ID) commits, and returns write's error.
func changedMidWrite(t *testing.T, f *editingFixture, userID, update string, write func(ctx context.Context) error) error {
	t.Helper()
	holder := holdRows(t, f.db, "SELECT id FROM users WHERE id = ? FOR UPDATE", userID)
	_, err := holder.tx.ExecContext(t.Context(), update, userID)
	require.NoError(t, err)
	done := started(t.Context(), write)
	waitUntilBlocked(t, f.db, 1)
	holder.commit()

	return <-done
}

const (
	blockUser   = "UPDATE users SET blocked_at = '2026-03-01 12:00:00' WHERE id = ?"
	demoteUser  = "UPDATE users SET role = 'contributor' WHERE id = ?"
	countWrens  = "SELECT COUNT(*) FROM revisions WHERE author_user_id = ?"
	openReports = "SELECT COUNT(*) FROM link_reports WHERE status = 'open'"
)

// TestRepository_UserLockedWrites_BlockedMidRequest requires every write that locks its author's users row to
// refuse an author blocked after their request was authenticated with datasources.ErrUserBlocked, writing nothing.
func TestRepository_UserLockedWrites_BlockedMidRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// prepare sets up the write before the block and returns it.
		prepare func(t *testing.T, f *editingFixture) func(ctx context.Context) error
		// unchanged checks the write wrote nothing.
		unchanged func(t *testing.T, f *editingFixture)
	}{
		{
			name: "save_draft",
			prepare: func(_ *testing.T, f *editingFixture) func(ctx context.Context) error {
				doc := newPieceDocument(unsungTitle)
				req := datasources.SaveDraftRequest{Actor: actorWren, EntityType: doc.EntityType(), Document: doc,
					Sent: domain.AllEditorFieldsSent, Summary: draftSummary, Now: f.tick()}
				return func(ctx context.Context) error { _, err := f.repo.SaveDraft(ctx, req); return err }
			},
			unchanged: func(t *testing.T, f *editingFixture) {
				assert.Zero(t, f.count(t, countWrens, wrenUserID))
			},
		},
		{
			name: "submit",
			prepare: func(t *testing.T, f *editingFixture) func(ctx context.Context) error {
				draft := f.saveDraft(t, actorWren, nil, newPieceDocument(unsungTitle))
				req := f.submitRequest(t, actorWren, draft.Draft.ID)
				return func(ctx context.Context) error { _, err := f.repo.SubmitDraft(ctx, req); return err }
			},
			unchanged: func(t *testing.T, f *editingFixture) {
				assert.Zero(t, f.count(t, countWrens+" AND status <> 'draft'", wrenUserID))
			},
		},
		{
			name: "revert",
			prepare: func(t *testing.T, f *editingFixture) func(ctx context.Context) error {
				editAndSubmit(t, f, actorRowan, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
					d.NotesMD = new(editedNotes)
				})
				req := datasources.RevertRequest{Actor: actorWren, RevisionID: f.song.RevisionID, Now: f.tick()}
				return func(ctx context.Context) error { _, err := f.repo.RevertTo(ctx, req); return err }
			},
			unchanged: func(t *testing.T, f *editingFixture) {
				assert.Zero(t, f.count(t, countWrens, wrenUserID))
			},
		},
		{
			name: "link_report",
			prepare: func(_ *testing.T, f *editingFixture) func(ctx context.Context) error {
				report := datasources.NewLinkReport{ReporterUserID: wrenUserID, URL: songLyricsURL,
					Reason: domain.LinkReportReasonBroken, Now: f.now}
				return func(ctx context.Context) error { _, _, err := f.repo.InsertLinkReport(ctx, report); return err }
			},
			unchanged: func(t *testing.T, f *editingFixture) {
				assert.Zero(t, f.count(t, openReports))
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newEditingFixture(t)
			write := tc.prepare(t, f)

			require.ErrorIs(t, changedMidWrite(t, f, wrenUserID, blockUser, write), datasources.ErrUserBlocked)
			tc.unchanged(t, f)
		})
	}
}

// TestRepository_ModeratorWrites_DemotedActorRefused requires every moderator-only write to lock the moderator's
// users row first and to refuse a moderator demoted after their request was authenticated with
// domain.ErrModeratorRequired, writing nothing.
func TestRepository_ModeratorWrites_DemotedActorRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prepare   func(t *testing.T, f *editingFixture) func(ctx context.Context) error
		unchanged func(t *testing.T, f *editingFixture)
	}{
		{
			name: "approve",
			prepare: func(t *testing.T, f *editingFixture) func(ctx context.Context) error {
				id := f.editedSong(t)
				req := datasources.ApproveRequest{Actor: actorRowan, RevisionID: id,
					Duplicates: f.approvalChecks(t, id), Now: f.tick()}
				return func(ctx context.Context) error { return f.repo.ApproveRevision(ctx, req) }
			},
			unchanged: func(t *testing.T, f *editingFixture) {
				assert.Equal(t, f.song.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
			},
		},
		{
			name: "reject",
			prepare: func(t *testing.T, f *editingFixture) func(ctx context.Context) error {
				req := datasources.RejectRequest{Actor: actorRowan, RevisionID: f.editedSong(t), Now: f.tick()}
				return func(ctx context.Context) error { return f.repo.RejectRevision(ctx, req) }
			},
			unchanged: func(t *testing.T, f *editingFixture) {
				assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM revisions WHERE status = 'pending'"))
			},
		},
		{
			name: "rebase",
			prepare: func(t *testing.T, f *editingFixture) func(ctx context.Context) error {
				id := f.editedSong(t)
				doc := f.editable(t, actorRowan, *pieceKey(f.song.Slug)).Document
				req := datasources.RebaseRequest{Keys: domaintest.Keys(), Actor: actorRowan, RevisionID: id,
					CurrentRevisionID: &f.song.RevisionID, Document: doc, Sent: domain.AllEditorFieldsSent,
					Now: f.tick()}
				return func(ctx context.Context) error { return f.repo.RebaseRevision(ctx, req) }
			},
			unchanged: func(t *testing.T, f *editingFixture) {
				assert.Zero(t, f.count(t, "SELECT COUNT(*) FROM revisions WHERE rebased_at IS NOT NULL"))
			},
		},
		{
			name: "hide_link",
			prepare: func(t *testing.T, f *editingFixture) func(ctx context.Context) error {
				req := datasources.SetLinkHiddenRequest{ModeratorUserID: rowanUserID,
					LinkID: f.linkID(t, songLyricsURL), Hidden: true, Now: f.now}
				return func(ctx context.Context) error { _, err := f.repo.SetLinkHidden(ctx, req); return err }
			},
			unchanged: func(t *testing.T, f *editingFixture) {
				assert.Zero(t, f.count(t, "SELECT COUNT(*) FROM links WHERE url = ? AND is_hidden", songLyricsURL))
			},
		},
		{
			name:    "resolve_link_report",
			prepare: resolveReport(domain.LinkReportStatusResolved),
			unchanged: func(t *testing.T, f *editingFixture) {
				assert.Equal(t, 1, f.count(t, openReports))
				assert.Zero(t, f.count(t, "SELECT COUNT(*) FROM links WHERE url = ? AND is_hidden", songLyricsURL))
			},
		},
		{
			name:    "dismiss_link_report",
			prepare: resolveReport(domain.LinkReportStatusDismissed),
			unchanged: func(t *testing.T, f *editingFixture) {
				assert.Equal(t, 1, f.count(t, openReports))
			},
		},
		{
			name: "dismiss_contact_request",
			prepare: func(t *testing.T, f *editingFixture) func(ctx context.Context) error {
				id, err := f.repo.CreateContactRequest(t.Context(), datasources.NewContactRequest{
					Kind: domain.ContactRequestKindGeneral, Email: "hazel@example.org", Message: "Hello.", Now: f.now})
				require.NoError(t, err)
				req := datasources.ResolveContactRequestRequest{ModeratorUserID: rowanUserID, RequestID: id,
					Status: domain.ContactRequestStatusDismissed, Now: f.now}
				return func(ctx context.Context) error { _, err := f.repo.ResolveContactRequest(ctx, req); return err }
			},
			unchanged: func(t *testing.T, f *editingFixture) {
				assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM contact_requests WHERE status = 'open'"))
			},
		},
		{
			name: "dismiss_contact_requests",
			prepare: func(t *testing.T, f *editingFixture) func(ctx context.Context) error {
				ids := make([]int64, 0, 2)
				for _, message := range []string{"Buy now.", "Buy again."} {
					id, err := f.repo.CreateContactRequest(t.Context(), datasources.NewContactRequest{
						Kind: domain.ContactRequestKindGeneral, Email: "hazel@example.org", Message: message, Now: f.now})
					require.NoError(t, err)
					ids = append(ids, id)
				}
				req := datasources.DismissContactRequestsRequest{ModeratorUserID: rowanUserID, RequestIDs: ids,
					Now: f.now}
				return func(ctx context.Context) error { _, err := f.repo.DismissContactRequests(ctx, req); return err }
			},
			unchanged: func(t *testing.T, f *editingFixture) {
				assert.Equal(t, 2, f.count(t, "SELECT COUNT(*) FROM contact_requests WHERE status = 'open'"))
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newEditingFixture(t)
			write := tc.prepare(t, f)

			require.ErrorIs(t, changedMidWrite(t, f, rowanUserID, demoteUser, write), domain.ErrModeratorRequired)
			tc.unchanged(t, f)
		})
	}
}

// editedSong submits Wren's edit of the song's notes, returning the pending revision's ID.
func (f *editingFixture) editedSong(t *testing.T) int64 {
	t.Helper()

	return editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.NotesMD = new(editedNotes)
	}).RevisionID
}

// resolveReport prepares Rowan's resolving of Wren's open report of the song's lyrics link with status, hiding the
// link.
func resolveReport(status domain.LinkReportStatus) func(t *testing.T, f *editingFixture) func(ctx context.Context) error {
	return func(t *testing.T, f *editingFixture) func(ctx context.Context) error {
		id := f.insertOpenReport(t, f.linkID(t, songLyricsURL), wrenUserID)
		req := datasources.ResolveLinkReportRequest{ModeratorUserID: rowanUserID, ReportID: id, Status: status,
			HideLink: status == domain.LinkReportStatusResolved, Now: f.now}
		return func(ctx context.Context) error { _, err := f.repo.ResolveLinkReport(ctx, req); return err }
	}
}

// TestRepository_SubmitDraft_DemotedModeratorGoesPending requires an interactive moderator's submission, which
// would be approved at once, to be refused with datasources.ErrActorChanged when their users row no longer holds the
// moderator role once locked, leaving the draft a draft; submitted again as a contributor, it is pending.
func TestRepository_SubmitDraft_DemotedModeratorGoesPending(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	// The document as a contributor edits it, so that it is valid submitted as one.
	doc := f.editable(t, actorRowanToken, *pieceKey(f.song.Slug)).Document.(domain.PieceDocument)
	doc.NotesMD = new(editedNotes)
	draft := f.saveDraft(t, actorRowan, pieceKey(f.song.Slug), doc)
	req := f.submitRequest(t, actorRowan, draft.Draft.ID)

	err := changedMidWrite(t, f, rowanUserID, demoteUser, func(ctx context.Context) error {
		_, err := f.repo.SubmitDraft(ctx, req)
		return err
	})
	require.ErrorIs(t, err, datasources.ErrActorChanged)
	assert.Equal(t, statusDraft, f.stored(t, draft.Draft.ID).status)
	assert.Equal(t, f.song.RevisionID, f.currentRevision(t, "pieces", f.song.ID))

	result := f.submit(t, actorRowanToken, draft.Draft.ID)
	assert.Equal(t, domain.RevisionStatusPending, result.Status)
	assert.Equal(t, statusPending, f.stored(t, draft.Draft.ID).status)
	assert.Equal(t, f.song.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
}

// TestRepository_ApproveRevision_LocksModeratorFirst requires an approval to take the moderator's users row before
// the revision's: while it waits for a held users row, the revision's row is free.
func TestRepository_ApproveRevision_LocksModeratorFirst(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	id := f.editedSong(t)
	req := datasources.ApproveRequest{Actor: actorRowan, RevisionID: id, Duplicates: f.approvalChecks(t, id),
		Now: f.tick()}

	holder := holdRows(t, f.db, "SELECT id FROM users WHERE id = ? FOR UPDATE", rowanUserID)
	done := started(t.Context(), func(ctx context.Context) error { return f.repo.ApproveRevision(ctx, req) })
	waitUntilBlocked(t, f.db, 1)
	require.NoError(t, lockNowait(t, f.db, "SELECT id FROM revisions WHERE id = ? FOR UPDATE NOWAIT", id),
		"the approval holds no revision while it waits for the moderator's users row")
	holder.commit()

	require.NoError(t, <-done)
	assert.Equal(t, statusApproved, f.stored(t, id).status)
}
