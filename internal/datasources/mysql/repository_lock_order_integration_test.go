//go:build integration

package mysql_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The lock order tests force two approvals to take their first entity locks at the same moment: a holder locks the
// rows both approvals lock first, both are seen waiting, and the holder commits, granting both. On a lock order
// that is not global the two then wait for each other, and InnoDB rolls one back with a deadlock (1213); in the
// global order (docs/PLAN.md §5.1 "Approval") one waits for the other to commit. The approvals run without
// withWriteTx's retries (ApproveRevisionOnce), so a deadlock is seen and not absorbed.

// approveTogether approves the revisions ids, each once and without retries, after the holder of rows has released
// them to both at once, and returns the approvals' errors in order. Each approval is by a moderator of its own: an
// approval first locks its moderator's users row, so two by one moderator would wait there, not on rows.
func (f *editingFixture) approveTogether(t *testing.T, rows *heldRows, ids ...int64) []error {
	t.Helper()
	done := make([]<-chan error, len(ids))
	for i, id := range ids {
		moderator := domain.Actor{UserID: fmt.Sprintf("google-oauth2|moderator-%d", i), Role: domain.RoleModerator}
		f.c.Exec("INSERT INTO users (id, display_name, role, created_at) VALUES (?, ?, 'moderator', ?)",
			moderator.UserID, fmt.Sprintf("Moderator %d", i), f.now)
		req := datasources.ApproveRequest{Actor: moderator, RevisionID: id, Duplicates: f.approvalChecks(t, id),
			Now: f.tick()}
		done[i] = started(t.Context(), func(ctx context.Context) error { return f.repo.ApproveRevisionOnce(ctx, req) })
	}
	waitUntilBlocked(t, f.db, len(ids))
	rows.commit()
	errs := make([]error, len(ids))
	for i, d := range done {
		errs[i] = <-d
	}

	return errs
}

// TestRepository_ApproveRevision_SameFacetTermsNoDeadlock requires two approvals of new terms of one facet, each
// holding its own term's row, to serialise on the facet's terms rather than deadlock: the terms of a rebuilt facet
// are locked first among the term rows, all of them.
func TestRepository_ApproveRevision_SameFacetTermsNoDeadlock(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	laments := f.newPending(t, actorWren, newTermDocument("Laments"))
	reels := f.newPending(t, actorAsh, newTermDocument("Reels"))

	holder := holdRows(t, f.db, "SELECT id FROM terms WHERE id IN (?, ?) ORDER BY id FOR UPDATE",
		laments.Entity.ID, reels.Entity.ID)
	errs := f.approveTogether(t, holder, laments.ID, reels.ID)

	for i, err := range errs {
		assert.False(t, isDeadlock(err), "approval %d deadlocked: %v", i, err)
		require.NoError(t, err)
	}
	assert.Equal(t, statusApproved, f.stored(t, laments.ID).status)
	assert.Equal(t, statusApproved, f.stored(t, reels.ID).status)
}

// TestRepository_ApproveRevision_MutualRelationsNoDeadlock requires two approvals of pieces that each add a
// relation to the other to wait for one another rather than deadlock: a piece's own row and the rows it references
// are locked in one ascending pass.
func TestRepository_ApproveRevision_MutualRelationsNoDeadlock(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	other := f.c.Piece(catalogtest.Piece{Doc: newPieceDocument(unsungTitle)})
	variantOf := func(id int64) func(d *domain.PieceDocument) {
		return func(d *domain.PieceDocument) {
			d.Relations = append(d.Relations, domain.DocPieceRelation{ToPieceID: id,
				Type: domain.PieceRelationTypeVariantOf})
		}
	}
	first := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), variantOf(other.ID))
	second := editAndSubmit(t, f, actorAsh, *pieceKey(other.Slug), variantOf(f.song.ID))

	holder := holdRows(t, f.db, "SELECT id FROM pieces WHERE id IN (?, ?) ORDER BY id FOR UPDATE",
		f.song.ID, other.ID)
	errs := f.approveTogether(t, holder, first.RevisionID, second.RevisionID)

	for i, err := range errs {
		assert.False(t, isDeadlock(err), "approval %d deadlocked: %v", i, err)
		require.NoError(t, err)
	}
	assert.Equal(t, []domain.DocPieceRelation{{ToPieceID: other.ID, Type: domain.PieceRelationTypeVariantOf}},
		f.storedPiece(t, first.RevisionID).Relations)
	assert.Equal(t, []domain.DocPieceRelation{{ToPieceID: f.song.ID, Type: domain.PieceRelationTypeVariantOf}},
		f.storedPiece(t, second.RevisionID).Relations)
}
