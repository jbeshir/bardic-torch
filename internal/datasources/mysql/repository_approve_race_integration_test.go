//go:build integration

package mysql_test

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The concurrency tests run their racing writes on goroutines released together, and check only outcomes the lock
// order makes certain, whichever write wins: they are deterministic, and pass under -race.

// race runs each of fns on its own goroutine, released at once, and returns their errors in order.
func race(fns ...func() error) []error {
	errs := make([]error, len(fns))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Go(func() {
			<-start
			errs[i] = fn()
		})
	}
	close(start)
	wg.Wait()

	return errs
}

// approveAt returns an approval of the revision id by Rowan at now, for race.
func (f *editingFixture) approveAt(ctx context.Context, id int64, now time.Time) func() error {
	return func() error {
		return f.repo.ApproveRevision(ctx, datasources.ApproveRequest{Actor: actorRowan, RevisionID: id, Now: now})
	}
}

// oneOf asserts that exactly one of errs is nil, and that the other is want.
func oneOf(t *testing.T, errs []error, want error) int {
	t.Helper()
	require.Len(t, errs, 2)
	if errs[0] == nil {
		require.ErrorIs(t, errs[1], want)
		return 0
	}
	require.ErrorIs(t, errs[0], want)
	require.NoError(t, errs[1])

	return 1
}

// TestRepository_ApproveRevision_ConcurrentDoubleApprove covers R23: of two approvals of one revision, exactly one
// succeeds; the other waits for the revision's lock and sees it approved.
func TestRepository_ApproveRevision_ConcurrentDoubleApprove(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	for i := range 10 {
		result := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
			d.NotesMD = new(fmt.Sprintf("Edit %d.", i))
		})
		errs := race(f.approveAt(t.Context(), result.RevisionID, f.tick()),
			f.approveAt(t.Context(), result.RevisionID, f.tick()))
		oneOf(t, errs, domain.ErrNotPending)
		assert.Equal(t, result.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
	}
}

// TestRepository_ApproveRevision_ConcurrentSameEntity covers R25: two approvals of two revisions of one entity, on
// one base. The second waits for the entity row and sees the first's revision current: a clean merge keeps both
// edits (no lost update), a conflict is refused.
func TestRepository_ApproveRevision_ConcurrentSameEntity(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	for i := range 5 {
		notes := fmt.Sprintf("Edit %d.", i)
		ours := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(notes) })
		theirs := editAndSubmit(t, f, actorAsh, key, func(d *domain.PieceDocument) { d.IsStub = !d.IsStub })
		stub := f.storedPiece(t, theirs.RevisionID).IsStub
		errs := race(f.approveAt(t.Context(), ours.RevisionID, f.tick()),
			f.approveAt(t.Context(), theirs.RevisionID, f.tick()))
		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		got := f.publicPiece(t, f.song.Slug)
		assert.Equal(t, new(notes), got.NotesMD, "no lost update")
		assert.Equal(t, stub, got.IsStub, "no lost update")
	}
	for i := range 5 {
		ours := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) {
			d.Title = fmt.Sprintf("Ours %d", i)
		})
		theirs := editAndSubmit(t, f, actorAsh, key, func(d *domain.PieceDocument) {
			d.Title = fmt.Sprintf("Theirs %d", i)
		})
		errs := race(f.approveAt(t.Context(), ours.RevisionID, f.tick()),
			f.approveAt(t.Context(), theirs.RevisionID, f.tick()))
		winner := []datasources.SubmitResult{ours, theirs}[oneOf(t, errs, domain.ErrRebaseRequired)]
		assert.Equal(t, winner.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
		loser := ours
		if winner == ours {
			loser = theirs
		}
		assert.Equal(t, statusPending, f.stored(t, loser.RevisionID).status)
		require.NoError(t, f.reject(t, loser.RevisionID, ""))
	}
}

// TestRepository_ApproveRevision_WithdrawRace covers R24: an approval and its author's withdrawal of one revision;
// whichever locks the revision first wins, and the other sees it no longer pending.
func TestRepository_ApproveRevision_WithdrawRace(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	for i := range 10 {
		result := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
			d.NotesMD = new(fmt.Sprintf("Edit %d.", i))
		})
		errs := race(f.approveAt(t.Context(), result.RevisionID, f.tick()), func() error {
			return f.repo.WithdrawRevision(t.Context(), actorWren, result.RevisionID)
		})
		status := []string{statusApproved, statusWithdrawn}[oneOf(t, errs, domain.ErrNotPending)]
		assert.Equal(t, status, f.stored(t, result.RevisionID).status)
	}
	require.ErrorIs(t, f.repo.WithdrawRevision(t.Context(), actorWren, 999999), datasources.ErrNotFound)
}

// TestRepository_ApproveRevision_SupersedeRace runs a superseding submit and an approval of the revision it
// supersedes concurrently, 50 times. Each time either the old revision is approved (and the new one is pending on
// its stale base) or it is withdrawn (and the approval sees it no longer pending); no other error escapes.
func TestRepository_ApproveRevision_SupersedeRace(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	addAlias := func(d *domain.PieceDocument, i int) {
		d.Aliases = append(slices.Clone(d.Aliases), domain.DocPieceAlias{Text: fmt.Sprintf("Wren Song %d", i),
			Kind: domain.PieceAliasKindAltTitle})
	}
	// Each edit adds an alias, which merges cleanly with the aliases the earlier approvals added.
	old := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { addAlias(d, 0) })
	for i := 1; i <= 50; i++ {
		doc := f.editable(t, actorWren, key).Document.(domain.PieceDocument)
		addAlias(&doc, i)
		draft := f.saveDraft(t, actorWren, &key, doc)
		submitAt, approveAt := f.tick(), f.tick()
		var submitted datasources.SubmitResult
		errs := race(f.approveAt(t.Context(), old.RevisionID, approveAt), func() error {
			var err error
			submitted, err = f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Actor: actorWren,
				DraftID: draft.Draft.ID, Now: submitAt})
			return err
		})
		require.NoError(t, errs[1], "the submit always succeeds")
		if errs[0] != nil {
			require.ErrorIs(t, errs[0], domain.ErrNotPending)
			assert.Equal(t, statusWithdrawn, f.stored(t, old.RevisionID).status, "superseded first")
		} else {
			assert.Equal(t, statusApproved, f.stored(t, old.RevisionID).status)
		}
		assert.Equal(t, statusPending, f.stored(t, submitted.RevisionID).status)
		assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM revisions WHERE author_user_id = ? AND status = 'pending'",
			wrenUserID), "one pending revision")
		old = submitted
	}
}

// lockDeadlocks is InnoDB's count of deadlocks, server-wide.
func lockDeadlocks(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count FROM information_schema.INNODB_METRICS "+
		"WHERE name = 'lock_deadlocks'").Scan(&n))

	return n
}

// TestRepository_ApproveRevision_CrossingApprovals runs a piece approval crediting a person and an approval of that
// person with a performance of the piece concurrently, 50 times. Both succeed every time: the foreign-key checks'
// shared locks may deadlock them (D15), which withWriteTx retries. The deadlocks are counted, never asserted absent.
func TestRepository_ApproveRevision_CrossingApprovals(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	before := lockDeadlocks(t, f.db)
	for i := range 50 {
		piece := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
			d.Credits = append(slices.Clone(d.Credits[:1]), domain.DocCredit{PersonID: &f.ash.ID,
				Role: domain.CreditRoleTune, Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain,
				Note: new(fmt.Sprintf("Credit %d", i))})
		})
		person := editAndSubmit(t, f, actorAsh, *personKey(f.ash.Slug), func(d *domain.PersonDocument) {
			d.Links = []domain.DocPersonLink{{
				DocLink: domain.DocLink{URL: "https://example.org/ash/fair",
					Roles: []domain.LinkRole{domain.LinkRolePerformance}},
				Performances: []domain.DocPersonPerformance{{PieceID: &f.song.ID,
					DocPerformance: domain.DocPerformance{DescriptionVerbatim: fmt.Sprintf("Performance %d", i),
						PerformerIDs: []int64{f.ash.ID}}}},
			}}
		})
		errs := race(f.approveAt(t.Context(), piece.RevisionID, f.tick()),
			f.approveAt(t.Context(), person.RevisionID, f.tick()))
		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
	}
	t.Logf("deadlocks during the crossing approvals (server-wide, retried): %d", lockDeadlocks(t, f.db)-before)
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM performances WHERE owner_person_id = ?", f.ash.ID))
}

// TestRepository_ApproveRevision_ApproveVersusModeratorEdit races an approval with an interactive moderator's
// submission of the same entity: the entity row serialises them, and both edits survive.
func TestRepository_ApproveRevision_ApproveVersusModeratorEdit(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	for i := range 10 {
		notes := fmt.Sprintf("Edit %d.", i)
		pending := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(notes) })
		doc := f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
		doc.IsStub = !doc.IsStub
		draft := f.saveDraft(t, actorRowan, &key, doc)
		submitAt := f.tick()
		errs := race(f.approveAt(t.Context(), pending.RevisionID, f.tick()), func() error {
			_, err := f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Actor: actorRowan,
				DraftID: draft.Draft.ID, Now: submitAt})
			return err
		})
		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		got := f.publicPiece(t, f.song.Slug)
		assert.Equal(t, new(notes), got.NotesMD)
		assert.Equal(t, doc.IsStub, got.IsStub)
	}
}

// TestRepository_ApproveRevision_ConcurrentOptOut races a piece approval adding a credit to a person with an
// approval turning that person prefers_no_page. The FOR SHARE lock on the person serialises them: either the credit
// is approved before the opt-out (and stays, B2), or the plan sees the opt-out and refuses the new credit.
func TestRepository_ApproveRevision_ConcurrentOptOut(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	for i := range 5 {
		credited := f.c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: fmt.Sprintf("Finch %d", i)}})
		piece := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
			d.Credits = append(slices.Clone(d.Credits[:1]), domain.DocCredit{PersonID: &credited.ID,
				Role: domain.CreditRoleTune, Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain})
		})
		key := *personKey(credited.Slug)
		doc := f.editable(t, actorRowan, key).Document.(domain.PersonDocument)
		doc.Status, doc.StatusSourceURL = domain.PersonStatusPrefersNoPage, new("https://example.org/finch/request")
		optOut := f.saveDraft(t, actorRowan, &key, doc)
		submitAt := f.tick()
		errs := race(f.approveAt(t.Context(), piece.RevisionID, f.tick()), func() error {
			_, err := f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Actor: actorRowan,
				DraftID: optOut.Draft.ID, Now: submitAt})
			return err
		})
		require.NoError(t, errs[1], "the opt-out is approved")
		if errs[0] != nil {
			var problems *domain.DocumentError
			require.ErrorAs(t, errs[0], &problems)
			assert.True(t, slices.ContainsFunc(problems.Problems, func(p domain.DocumentProblem) bool {
				return p.Code == domain.ProblemCreditToNoPagePerson
			}), "the plan sees the opt-out: %v", problems.Problems)
			require.NoError(t, f.reject(t, piece.RevisionID, ""))
		} else {
			assert.Equal(t, piece.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
		}
	}
}

// TestRepository_ApproveRevision_ForeignKeyParentGone checks that a write whose foreign-key parent is gone (MySQL
// error 1452) is datasources.ErrEntityGone, never an internal error.
func TestRepository_ApproveRevision_ForeignKeyParentGone(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	doc := songDocument(f)
	doc.Credits = []domain.DocCredit{{PersonID: new(int64(999999)), Role: domain.CreditRoleTune,
		Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain}}
	err := f.repo.WithWriteTx(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		return f.repo.WriteApprovedDocument(ctx, tx, f.song.ID, doc)
	})
	require.ErrorIs(t, err, datasources.ErrEntityGone)
	assert.Equal(t, 2, f.count(t, "SELECT COUNT(*) FROM piece_credits WHERE piece_id = ?", f.song.ID),
		"rolled back")
}

// TestRepository_RevertTo_ConcurrentSubmit covers T33 under concurrency, 20 times: a contributor's revert and their
// submission of the same entity. The users row serialises them: one supersedes the other, one pending revision
// remains, and the cap counts it once.
func TestRepository_RevertTo_ConcurrentSubmit(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	edit := editAndSubmit(t, f, actorRowan, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	require.Equal(t, domain.RevisionStatusApproved, edit.Status)
	for i := range 20 {
		doc := f.editable(t, actorWren, key).Document.(domain.PieceDocument)
		doc.NotesMD = new(fmt.Sprintf("Draft %d.", i))
		draft := f.saveDraft(t, actorWren, &key, doc)
		submitAt, revertAt := f.tick(), f.tick()
		var submitted, reverted datasources.SubmitResult
		errs := race(func() error {
			var err error
			submitted, err = f.repo.SubmitDraft(t.Context(), datasources.SubmitRequest{Actor: actorWren,
				DraftID: draft.Draft.ID, Now: submitAt})
			return err
		}, func() error {
			var err error
			reverted, err = f.repo.RevertTo(t.Context(), datasources.RevertRequest{Actor: actorWren,
				RevisionID: f.song.RevisionID, Summary: fmt.Sprintf("Revert %d.", i), Now: revertAt})
			return err
		})
		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		statuses := []string{f.stored(t, submitted.RevisionID).status, f.stored(t, reverted.RevisionID).status}
		slices.Sort(statuses)
		assert.Equal(t, []string{statusPending, statusWithdrawn}, statuses)
		assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM revisions WHERE author_user_id = ? AND status = 'pending'",
			wrenUserID), "one pending revision")
	}
}

// TestRepository_RevertTo_ConcurrentApprove covers T34 under concurrency: a moderator's revert and an approval of
// another revision of the entity. The entity row serialises them; the later one sees the earlier's revision
// current, so the revert is always based on the current revision, and the approval is merged onto it or refused.
func TestRepository_RevertTo_ConcurrentApprove(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	for i := range 10 {
		edit := editAndSubmit(t, f, actorRowan, key, func(d *domain.PieceDocument) {
			d.NotesMD = new(fmt.Sprintf("Edit %d.", i))
		})
		pending := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.IsStub = !d.IsStub })
		revertAt := f.tick()
		var reverted datasources.SubmitResult
		errs := race(f.approveAt(t.Context(), pending.RevisionID, f.tick()), func() error {
			var err error
			reverted, err = f.repo.RevertTo(t.Context(), datasources.RevertRequest{Actor: actorRowan,
				RevisionID: f.song.RevisionID, Summary: "Revert.", Now: revertAt})
			return err
		})
		require.NoError(t, errs[1])
		base := f.stored(t, reverted.RevisionID).base.Int64
		if errs[0] != nil {
			require.ErrorIs(t, errs[0], domain.ErrRebaseRequired)
			assert.Equal(t, edit.RevisionID, base)
			require.NoError(t, f.reject(t, pending.RevisionID, ""))
			continue
		}
		approvedBase := f.stored(t, pending.RevisionID).base.Int64
		assert.True(t, base == pending.RevisionID || approvedBase == reverted.RevisionID,
			"one of them is based on the other: revert base %d, approval base %d", base, approvedBase)
	}
}
