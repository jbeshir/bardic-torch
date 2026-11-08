//go:build integration

package mysql_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The per-user caps are counted under the user's users row lock, so that two requests of one user cannot both pass
// a count taken before either wrote. Each test below forces the race: one request short of the cap commits its row
// while a second waits for the users row, and the second then counts that row and is refused.

// TestRepository_SubmitDraft_PendingLimitRace forces the submission of a user's last allowed pending revision to
// commit while another of their submissions waits for their users row: the second is refused at the cap.
func TestRepository_SubmitDraft_PendingLimitRace(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	for i := 1; i < domain.MaxOpenPendingRevisions; i++ {
		f.submit(t, actorWren, f.saveDraft(t, actorWren, nil, newEventDocument(fmt.Sprintf("Revel %d", i))).Draft.ID)
	}
	last := f.submitRequest(t, actorWren, f.saveDraft(t, actorWren, nil, newEventDocument("Last revel")).Draft.ID)
	extra := f.saveDraft(t, actorWren, nil, newEventDocument("One revel too many")).Draft.ID
	extraReq := f.submitRequest(t, actorWren, extra)

	var submitted datasources.SubmitResult
	err := inOrder(t, f.db, lockUser, []any{wrenUserID}, f.submitFirst(last, &submitted),
		func(ctx context.Context) error {
			_, err := f.repo.SubmitDraft(ctx, extraReq)
			return err
		})
	require.ErrorIs(t, err, datasources.ErrPendingLimitReached)
	assert.Equal(t, statusPending, f.stored(t, submitted.RevisionID).status)
	assert.Equal(t, "draft", f.stored(t, extra).status, "nothing was written")
	assert.Equal(t, domain.MaxOpenPendingRevisions, f.count(t,
		"SELECT COUNT(*) FROM revisions WHERE author_user_id = ? AND status = 'pending'", wrenUserID))
}

// TestRepository_SaveDraft_LimitRace forces the save of a user's last allowed draft to commit while another new
// draft of theirs waits for their users row: the second is refused at the cap.
func TestRepository_SaveDraft_LimitRace(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	for i := 1; i < domain.MaxDrafts; i++ {
		f.saveDraft(t, actorWren, nil, newPieceDocument(fmt.Sprintf("Draft %d", i)))
	}
	draftOf := func(title string) datasources.SaveDraftRequest {
		return datasources.SaveDraftRequest{Actor: actorWren, EntityType: domain.EntityTypePiece,
			Document: newPieceDocument(title), Summary: draftSummary, Now: f.tick()}
	}
	last, extra := draftOf("Last draft"), draftOf("One draft too many")

	err := inOrder(t, f.db, lockUser, []any{wrenUserID}, func(ctx context.Context, tx *sql.Tx) error {
		_, err := f.repo.SaveDraftInTx(ctx, tx, last)
		return err
	}, func(ctx context.Context) error {
		_, err := f.repo.SaveDraft(ctx, extra)
		return err
	})
	require.ErrorIs(t, err, datasources.ErrDraftLimitReached)
	assert.Equal(t, domain.MaxDrafts, f.count(t,
		"SELECT COUNT(*) FROM revisions WHERE author_user_id = ? AND status = 'draft'", wrenUserID))
}

// TestRepository_InsertLinkReport_LimitRace forces a user's last allowed open report to commit, written as
// InsertLinkReport writes it under the users row lock, while another report of theirs waits for that row: the
// second is refused at the cap.
func TestRepository_InsertLinkReport_LimitRace(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	lyrics := f.linkID(t, songLyricsURL)
	for range domain.MaxOpenLinkReports - 1 {
		f.insertOpenReport(t, lyrics, wrenUserID)
	}

	err := inOrder(t, f.db, lockUser, []any{wrenUserID}, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO link_reports (link_id, reporter_user_id, reason, created_at) "+
			"VALUES (?, ?, 'other', ?)", lyrics, wrenUserID, f.now)
		return err
	}, func(context.Context) error {
		_, _, err := f.report(t, wrenUserID, morwenSiteURL)
		return err
	})
	require.ErrorIs(t, err, datasources.ErrReportLimitReached)
	assert.Equal(t, domain.MaxOpenLinkReports, f.count(t,
		"SELECT COUNT(*) FROM link_reports WHERE reporter_user_id = ? AND status = 'open'", wrenUserID))
}

// TestRepository_SaveDraft_ConcurrentForced forces a save of a user's draft of an entity to wait for their users
// row while their first save of a draft of that entity commits: the second then finds the first's draft and replaces
// it, so the user has one draft of the entity.
func TestRepository_SaveDraft_ConcurrentForced(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := pieceKey(f.song.Slug)
	doc := f.editable(t, actorWren, *key).Document
	req := datasources.SaveDraftRequest{Actor: actorWren, EntityType: domain.EntityTypePiece, Entity: key,
		Document: doc, Summary: draftSummary, Now: f.now}

	var first, second datasources.SavedDraft
	require.NoError(t, inOrder(t, f.db, lockUser, []any{wrenUserID}, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		first, err = f.repo.SaveDraftInTx(ctx, tx, req)
		return err
	}, func(ctx context.Context) error {
		var err error
		second, err = f.repo.SaveDraft(ctx, req)
		return err
	}))
	assert.True(t, first.Created)
	assert.False(t, second.Created, "the second save replaces the first's draft")
	assert.Equal(t, first.Draft.ID, second.Draft.ID)
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM revisions WHERE status = 'draft' AND entity_id = ?",
		f.song.ID))
}
