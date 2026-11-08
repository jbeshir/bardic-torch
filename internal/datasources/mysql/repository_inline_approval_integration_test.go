//go:build integration

package mysql_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixture strings of the inline approval tests: invented names and example.org URLs only.
const (
	ashExample      = "Ash Example"
	elmExample      = "Elm Example"
	laterPersonLink = "https://example.org/zz-person"
)

// creditPending returns a change crediting the pending person id with the lyrics of a piece.
func creditPending(id int64) func(d *domain.PieceDocument) {
	return func(d *domain.PieceDocument) {
		d.Credits = append(d.Credits, domain.DocCredit{PersonID: &id, Role: domain.CreditRoleLyrics,
			Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain})
	}
}

// moderatorDraftCrediting saves Rowan's draft of the song crediting the pending person id, as an interactive
// moderator, whose submission is approved at once.
func (f *editingFixture) moderatorDraftCrediting(t *testing.T, id int64) int64 {
	t.Helper()
	key := *pieceKey(f.song.Slug)
	doc := f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
	creditPending(id)(&doc)

	return f.saveDraft(t, actorRowan, &key, doc).Draft.ID
}

// TestRepository_SubmitApproved_PendingReferenceDuplicate requires an interactive moderator's submission approved at
// once to check the pending references it approves alongside for duplicates as any approval does: the write first
// names the pending person it has no check of, and with the check made, a pending person matching a public person
// published since its submission refuses the approval with pending_reference_flagged (possible_duplicate), the draft
// staying a draft; one matching nobody is approved alongside.
func TestRepository_SubmitApproved_PendingReferenceDuplicate(t *testing.T) {
	t.Parallel()

	t.Run("Duplicate", func(t *testing.T) {
		t.Parallel()
		f := newEditingFixture(t)
		pending := f.newPending(t, actorRowanToken, newPersonDocument(ashExample))
		f.c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: ashExample}})
		draftID := f.moderatorDraftCrediting(t, pending.Entity.ID)

		req := f.submitRequest(t, actorRowan, draftID)
		_, err := f.repo.SubmitDraft(t.Context(), req)
		var unchecked *datasources.UncheckedPendingReferencesError
		require.ErrorAs(t, err, &unchecked)
		require.ErrorIs(t, err, datasources.ErrDocumentChanged)
		require.Contains(t, unchecked.References, pending.ID)
		assert.Equal(t, pending.Entity.ID, unchecked.References[pending.ID].Entity.ID)

		_, err = f.submitChecked(t, req)
		var flagged *domain.PendingReferenceFlaggedError
		require.ErrorAs(t, err, &flagged)
		assert.Equal(t, pending.ID, flagged.RevisionID)
		assert.Contains(t, flagged.Flags, domain.Flag{Code: domain.FlagPossibleDuplicate})
		assert.Equal(t, statusDraft, f.stored(t, draftID).status)
		assert.Equal(t, statusPending, f.stored(t, pending.ID).status)
	})

	t.Run("NoDuplicate", func(t *testing.T) {
		t.Parallel()
		f := newEditingFixture(t)
		pending := f.newPending(t, actorRowanToken, newPersonDocument(elmExample))
		draftID := f.moderatorDraftCrediting(t, pending.Entity.ID)

		result := f.submit(t, actorRowan, draftID)
		assert.Equal(t, domain.RevisionStatusApproved, result.Status)
		assert.Equal(t, statusApproved, f.stored(t, pending.ID).status)
	})
}

// TestRepository_SubmitApproved_LocksReferencesBeforeOwnEntity requires an interactive moderator's submission
// approved at once to lock the revisions of the pending references it approves alongside before its own entity
// row, in the global lock order: while it waits for a held pending reference's revision, its piece's row is free.
func TestRepository_SubmitApproved_LocksReferencesBeforeOwnEntity(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	pending := f.newPending(t, actorRowanToken, newPersonDocument(elmExample))
	draftID := f.moderatorDraftCrediting(t, pending.Entity.ID)
	req := f.submitRequest(t, actorRowan, draftID)
	_, err := f.repo.SubmitDraft(t.Context(), req)
	var unchecked *datasources.UncheckedPendingReferencesError
	require.ErrorAs(t, err, &unchecked)
	req.PendingReferenceDuplicates = f.pendingReferenceChecks(t, unchecked)

	holder := holdRows(t, f.db, lockRevision, pending.ID)
	var result datasources.SubmitResult
	done := started(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.SubmitDraft(ctx, req)
		return err
	})
	waitUntilBlocked(t, f.db, 1)
	require.NoError(t, lockNowait(t, f.db, lockPiece+" NOWAIT", f.song.ID),
		"the submission holds its piece's row while it waits for a pending reference's revision")
	holder.commit()

	require.NoError(t, <-done)
	assert.Equal(t, domain.RevisionStatusApproved, result.Status)
	assert.Equal(t, statusApproved, f.stored(t, pending.ID).status)
}

// TestRepository_ApproveRevision_LinksUpsertedInOneAscendingPass requires an approval to upsert the links rows of
// every document it writes, its own and its pending references', in one ascending pass by normalized URL: while it
// waits for a held links row of its piece, it holds no links row of the pending person approved alongside, whose
// URL sorts later, though the person is written first.
func TestRepository_ApproveRevision_LinksUpsertedInOneAscendingPass(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	other := newPieceDocument(unsungTitle)
	other.Links = []domain.DocPieceLink{{DocLink: domain.DocLink{URL: laterPersonLink,
		Roles: []domain.LinkRole{domain.LinkRoleLyrics}}}}
	f.c.Piece(catalogtest.Piece{Doc: other})
	person := newPersonDocument(elmExample)
	person.Links = []domain.DocPersonLink{{DocLink: domain.DocLink{URL: laterPersonLink,
		Roles: []domain.LinkRole{domain.LinkRoleWebsite}}}}
	pending := f.newPending(t, actorWren, person)
	revision := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), creditPending(pending.Entity.ID))

	lyrics := f.count(t, "SELECT id FROM links WHERE url = ?", songLyricsURL)
	later := f.count(t, "SELECT id FROM links WHERE url = ?", laterPersonLink)
	holder := holdRows(t, f.db, "SELECT id FROM links WHERE id = ? FOR UPDATE", lyrics)
	req := datasources.ApproveRequest{Actor: actorRowan, RevisionID: revision.RevisionID,
		Duplicates: f.approvalChecks(t, revision.RevisionID), Now: f.tick()}
	done := started(t.Context(), func(ctx context.Context) error { return f.repo.ApproveRevisionOnce(ctx, req) })
	waitUntilBlocked(t, f.db, 1)
	require.NoError(t, lockNowait(t, f.db, "SELECT id FROM links WHERE id = ? FOR UPDATE NOWAIT", later),
		"the approval upserted the pending person's later link before its piece's earlier one")
	holder.commit()

	require.NoError(t, <-done)
	assert.Equal(t, statusApproved, f.stored(t, revision.RevisionID).status)
	assert.Equal(t, statusApproved, f.stored(t, pending.ID).status)
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM links WHERE url = ?", laterPersonLink))
}

// lockNowait runs query, a locking read with NOWAIT, in a transaction of its own, rolled back at once, and returns
// MySQL's ER_LOCK_NOWAIT (3572) if another transaction holds the row it reads, which must exist.
func lockNowait(t *testing.T, db *sql.DB, query string, args ...any) error {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	var id int64
	err = tx.QueryRowContext(t.Context(), query, args...).Scan(&id)
	var mysqlErr *mysqldriver.MySQLError
	if err != nil && !errors.As(err, &mysqlErr) {
		require.NoError(t, err)
	}

	return err
}
