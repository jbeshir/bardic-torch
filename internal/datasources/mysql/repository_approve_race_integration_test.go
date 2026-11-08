//go:build integration

package mysql_test

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The concurrency tests force each ordering of two contending writes with inOrder: the first commits while the
// second waits for a row it holds, and each test asserts what the second then sees. Each ordering runs once, the
// same every time.

// approveRequest is Rowan's approval of the revision id, with the duplicate checks made now.
func (f *editingFixture) approveRequest(t *testing.T, id int64) datasources.ApproveRequest {
	t.Helper()

	return datasources.ApproveRequest{Actor: actorRowan, RevisionID: id, Duplicates: f.approvalChecks(t, id),
		Now: f.tick()}
}

// approveFirst is inOrder's first write: the approval req in the holder's transaction.
func (f *editingFixture) approveFirst(req datasources.ApproveRequest) func(ctx context.Context, tx *sql.Tx) error {
	return func(ctx context.Context, tx *sql.Tx) error { return f.repo.ApproveRevisionInTx(ctx, tx, req) }
}

// approveSecond is inOrder's second write: the approval req on its own.
func (f *editingFixture) approveSecond(req datasources.ApproveRequest) func(ctx context.Context) error {
	return func(ctx context.Context) error { return f.repo.ApproveRevision(ctx, req) }
}

// submitFirst is inOrder's first write: the submission req in the holder's transaction, its result stored in
// result.
func (f *editingFixture) submitFirst(
	req datasources.SubmitRequest, result *datasources.SubmitResult,
) func(ctx context.Context, tx *sql.Tx) error {
	return func(ctx context.Context, tx *sql.Tx) error {
		var err error
		*result, err = f.repo.SubmitDraftInTx(ctx, tx, req)
		return err
	}
}

// submitSecond is inOrder's second write: the submission req on its own, its result stored in result.
func (f *editingFixture) submitSecond(
	req datasources.SubmitRequest, result *datasources.SubmitResult,
) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		var err error
		*result, err = f.repo.SubmitDraft(ctx, req)
		return err
	}
}

// TestRepository_ApproveRevision_ConcurrentDoubleApprove forces a second approval of a revision to wait while the
// first commits: it then sees the revision approved, and is refused.
func TestRepository_ApproveRevision_ConcurrentDoubleApprove(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	result := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.NotesMD = new(editedNotes)
	})

	err := inOrder(t, f.db, lockRevision, []any{result.RevisionID},
		f.approveFirst(f.approveRequest(t, result.RevisionID)), f.approveSecond(f.approveRequest(t, result.RevisionID)))
	require.ErrorIs(t, err, domain.ErrNotPending)
	assert.Equal(t, result.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
	assert.Equal(t, statusApproved, f.stored(t, result.RevisionID).status)
}

// TestRepository_ApproveRevision_ConcurrentSameEntity forces an approval of a revision to wait for the entity row
// while the approval of another revision on the same base commits. It then sees that revision current: a clean
// merge keeps both edits (no lost update), and a conflict is refused with rebase_required.
func TestRepository_ApproveRevision_ConcurrentSameEntity(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)

	t.Run("clean_merge_keeps_both", func(t *testing.T) {
		ours := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
		theirs := editAndSubmit(t, f, actorAsh, key, func(d *domain.PieceDocument) { d.IsStub = !d.IsStub })
		stub := f.storedPiece(t, theirs.RevisionID).IsStub

		require.NoError(t, inOrder(t, f.db, lockPiece, []any{f.song.ID},
			f.approveFirst(f.approveRequest(t, ours.RevisionID)),
			f.approveSecond(f.approveRequest(t, theirs.RevisionID))))
		got := f.publicPiece(t, f.song.Slug)
		assert.Equal(t, new(editedNotes), got.NotesMD, "no lost update")
		assert.Equal(t, stub, got.IsStub, "no lost update")
		assert.Equal(t, theirs.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
	})
	t.Run("conflict_refused", func(t *testing.T) {
		ours := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.Title = "Ours" })
		theirs := editAndSubmit(t, f, actorAsh, key, func(d *domain.PieceDocument) { d.Title = "Theirs" })

		err := inOrder(t, f.db, lockPiece, []any{f.song.ID}, f.approveFirst(f.approveRequest(t, ours.RevisionID)),
			f.approveSecond(f.approveRequest(t, theirs.RevisionID)))
		require.ErrorIs(t, err, domain.ErrRebaseRequired)
		assert.Equal(t, ours.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
		assert.Equal(t, statusPending, f.stored(t, theirs.RevisionID).status)
	})
}

// TestRepository_ApproveRevision_WithdrawRace forces, in each order, an approval and its author's withdrawal of one
// revision: the one that waits for the revision row sees it no longer pending, and is refused.
func TestRepository_ApproveRevision_WithdrawRace(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	submitted := func() int64 {
		return editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
			d.NotesMD = new(editedNotes)
		}).RevisionID
	}
	withdraw := func(id int64) func(ctx context.Context) error {
		return func(ctx context.Context) error { return f.repo.WithdrawRevision(ctx, actorWren, id) }
	}

	t.Run("approve_first", func(t *testing.T) {
		id := submitted()
		err := inOrder(t, f.db, lockRevision, []any{id}, f.approveFirst(f.approveRequest(t, id)), withdraw(id))
		require.ErrorIs(t, err, domain.ErrNotPending)
		assert.Equal(t, statusApproved, f.stored(t, id).status)
	})
	t.Run("withdraw_first", func(t *testing.T) {
		id := submitted()
		err := inOrder(t, f.db, lockRevision, []any{id}, func(ctx context.Context, tx *sql.Tx) error {
			return f.repo.WithdrawRevisionInTx(ctx, tx, actorWren, id)
		}, f.approveSecond(f.approveRequest(t, id)))
		require.ErrorIs(t, err, domain.ErrNotPending)
		assert.Equal(t, statusWithdrawn, f.stored(t, id).status)
	})
	require.ErrorIs(t, f.repo.WithdrawRevision(t.Context(), actorWren, 999999), datasources.ErrNotFound)
}

// TestRepository_ApproveRevision_SupersedeRace forces, in each order, a superseding submit and an approval of the
// revision it supersedes. Approved first, the old revision stays approved and the submit leaves the new revision
// pending on its stale base; superseded first, the old revision is withdrawn and the approval is refused. Either way
// the author has one pending revision.
func TestRepository_ApproveRevision_SupersedeRace(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	// Each edit adds an alias, which merges cleanly with the aliases the earlier approvals added.
	addAlias := func(name string) func(d *domain.PieceDocument) {
		return func(d *domain.PieceDocument) {
			d.Aliases = append(slices.Clone(d.Aliases), domain.DocPieceAlias{Text: name,
				Kind: domain.PieceAliasKindAltTitle})
		}
	}
	superseding := func(name string) datasources.SubmitRequest {
		doc := f.editable(t, actorWren, key).Document.(domain.PieceDocument)
		addAlias(name)(&doc)
		return f.submitRequest(t, actorWren, f.saveDraft(t, actorWren, &key, doc).Draft.ID)
	}
	onePending := func(t *testing.T) {
		t.Helper()
		assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM revisions WHERE author_user_id = ? "+
			"AND status = 'pending'", wrenUserID), "one pending revision")
	}

	t.Run("approve_first", func(t *testing.T) {
		old := editAndSubmit(t, f, actorWren, key, addAlias("Wren Song Old"))
		var submitted datasources.SubmitResult
		require.NoError(t, inOrder(t, f.db, lockRevision, []any{old.RevisionID},
			f.approveFirst(f.approveRequest(t, old.RevisionID)),
			f.submitSecond(superseding("Wren Song New"), &submitted)))
		assert.Equal(t, statusApproved, f.stored(t, old.RevisionID).status)
		assert.Equal(t, statusPending, f.stored(t, submitted.RevisionID).status)
		assert.Equal(t, old.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
		onePending(t)
		require.NoError(t, f.repo.WithdrawRevision(t.Context(), actorWren, submitted.RevisionID))
	})
	t.Run("supersede_first", func(t *testing.T) {
		old := editAndSubmit(t, f, actorWren, key, addAlias("Wren Song Older"))
		var submitted datasources.SubmitResult
		err := inOrder(t, f.db, lockRevision, []any{old.RevisionID},
			f.submitFirst(superseding("Wren Song Newer"), &submitted),
			f.approveSecond(f.approveRequest(t, old.RevisionID)))
		require.ErrorIs(t, err, domain.ErrNotPending)
		assert.Equal(t, statusWithdrawn, f.stored(t, old.RevisionID).status, "superseded first")
		assert.Equal(t, statusPending, f.stored(t, submitted.RevisionID).status)
		onePending(t)
	})
}

// TestRepository_ApproveRevision_CrossingApprovalsSerialise forces, in each order, a piece approval crediting a
// person and an approval of that person with a performance of the piece. Each locks the other's entity row, and
// the global lock order makes the later one wait for the earlier, never deadlock: both are approved, and the
// person's performance is stored once.
func TestRepository_ApproveRevision_CrossingApprovalsSerialise(t *testing.T) {
	t.Parallel()
	for name, pieceFirst := range map[string]bool{"piece_first": true, "person_first": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newEditingFixture(t)
			piece := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
				d.Credits = append(slices.Clone(d.Credits[:1]), domain.DocCredit{PersonID: &f.ash.ID,
					Role: domain.CreditRoleTune, Attribution: domain.AttributionNamed,
					Certainty: domain.CertaintyCertain, Note: new("Credit of the crossing approvals")})
			})
			person := editAndSubmit(t, f, actorAsh, *personKey(f.ash.Slug), func(d *domain.PersonDocument) {
				d.Links = []domain.DocPersonLink{{
					DocLink: domain.DocLink{URL: "https://example.org/ash/fair",
						Roles: []domain.LinkRole{domain.LinkRolePerformance}},
					Performances: []domain.DocPersonPerformance{{PieceID: &f.song.ID,
						DocPerformance: domain.DocPerformance{DescriptionVerbatim: "Performance at the fair",
							PerformerIDs: []int64{f.ash.ID}}}},
				}}
			})
			first, second := piece.RevisionID, person.RevisionID
			query, args := lockPiece, []any{f.song.ID}
			if !pieceFirst {
				first, second = second, first
				query, args = lockPerson, []any{f.ash.ID}
			}

			// The second approval runs once, without retries: a deadlock would fail it.
			req := f.approveRequest(t, second)
			require.NoError(t, inOrder(t, f.db, query, args, f.approveFirst(f.approveRequest(t, first)),
				func(ctx context.Context) error { return f.repo.ApproveRevisionOnce(ctx, req) }))
			assert.Equal(t, piece.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
			assert.Equal(t, person.RevisionID, f.currentRevision(t, "persons", f.ash.ID))
			assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM performances WHERE owner_person_id = ?", f.ash.ID))
		})
	}
}

// TestRepository_ApproveRevision_ApproveVersusModeratorEdit forces, in each order, an approval and an interactive
// moderator's submission of the same entity: the entity row serialises them, the later merges onto the earlier, and
// both edits survive.
func TestRepository_ApproveRevision_ApproveVersusModeratorEdit(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	for name, approveFirst := range map[string]bool{"approve_first": true, "moderator_first": false} {
		t.Run(name, func(t *testing.T) {
			notes := "Edit, " + name + "."
			pending := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(notes) })
			doc := f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
			doc.IsStub = !doc.IsStub
			moderator := f.submitRequest(t, actorRowan, f.saveDraft(t, actorRowan, &key, doc).Draft.ID)
			approve := f.approveRequest(t, pending.RevisionID)
			var submitted datasources.SubmitResult

			var err error
			if approveFirst {
				err = inOrder(t, f.db, lockPiece, []any{f.song.ID}, f.approveFirst(approve),
					f.submitSecond(moderator, &submitted))
			} else {
				err = inOrder(t, f.db, lockPiece, []any{f.song.ID}, f.submitFirst(moderator, &submitted),
					f.approveSecond(approve))
			}
			require.NoError(t, err)
			assert.Equal(t, domain.RevisionStatusApproved, submitted.Status)
			got := f.publicPiece(t, f.song.Slug)
			assert.Equal(t, new(notes), got.NotesMD)
			assert.Equal(t, doc.IsStub, got.IsStub)
		})
	}
}

// TestRepository_ApproveRevision_ConcurrentOptOut forces, in each order, a piece approval adding a credit to a
// person and a moderator's edit turning that person prefers_no_page, which the person row serialises. Credited
// first, the credit is approved and stays; opted out first, the approval's plan sees the opt-out and refuses the
// new credit.
func TestRepository_ApproveRevision_ConcurrentOptOut(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	setUp := func(name string) (datasources.SubmitResult, datasources.SubmitRequest, catalogtest.Ref) {
		credited := f.c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: name}})
		piece := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
			d.Credits = append(slices.Clone(d.Credits[:1]), domain.DocCredit{PersonID: &credited.ID,
				Role: domain.CreditRoleTune, Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain})
		})
		key := *personKey(credited.Slug)
		doc := f.editable(t, actorRowan, key).Document.(domain.PersonDocument)
		doc.Status, doc.StatusSourceURL = domain.PersonStatusPrefersNoPage, new("https://example.org/finch/request")
		return piece, f.submitRequest(t, actorRowan, f.saveDraft(t, actorRowan, &key, doc).Draft.ID), credited
	}

	t.Run("credit_first", func(t *testing.T) {
		piece, optOut, credited := setUp("Finch Credited First")
		var submitted datasources.SubmitResult
		require.NoError(t, inOrder(t, f.db, lockPerson, []any{credited.ID},
			f.approveFirst(f.approveRequest(t, piece.RevisionID)), f.submitSecond(optOut, &submitted)))
		assert.Equal(t, domain.RevisionStatusApproved, submitted.Status, "the opt-out is approved")
		assert.Equal(t, piece.RevisionID, f.currentRevision(t, "pieces", f.song.ID), "the credit stays")
	})
	t.Run("opt_out_first", func(t *testing.T) {
		piece, optOut, credited := setUp("Finch Opted Out First")
		var submitted datasources.SubmitResult
		err := inOrder(t, f.db, lockPerson, []any{credited.ID}, f.submitFirst(optOut, &submitted),
			f.approveSecond(f.approveRequest(t, piece.RevisionID)))
		var problems *domain.DocumentError
		require.ErrorAs(t, err, &problems)
		assert.True(t, slices.ContainsFunc(problems.Problems, func(p domain.DocumentProblem) bool {
			return p.Code == domain.ProblemCreditToNoPagePerson
		}), "the plan sees the opt-out: %v", problems.Problems)
		assert.Equal(t, domain.RevisionStatusApproved, submitted.Status)
		assert.Equal(t, statusPending, f.stored(t, piece.RevisionID).status)
	})
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

// TestRepository_RevertTo_ConcurrentSubmit forces, in each order, a contributor's revert and their submission of
// the same entity. The users row serialises them: the later supersedes the earlier, so one pending revision
// remains, and the cap counts it once.
func TestRepository_RevertTo_ConcurrentSubmit(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	edit := editAndSubmit(t, f, actorRowan, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	require.Equal(t, domain.RevisionStatusApproved, edit.Status)
	setUp := func(name string) (datasources.SubmitRequest, datasources.RevertRequest) {
		doc := f.editable(t, actorWren, key).Document.(domain.PieceDocument)
		doc.NotesMD = new("Draft, " + name + ".")
		return f.submitRequest(t, actorWren, f.saveDraft(t, actorWren, &key, doc).Draft.ID),
			datasources.RevertRequest{Actor: actorWren, RevisionID: f.song.RevisionID, Summary: "Revert, " + name + ".",
				Now: f.tick()}
	}
	revertFirst := func(req datasources.RevertRequest, result *datasources.SubmitResult) func(context.Context, *sql.Tx) error {
		return func(ctx context.Context, tx *sql.Tx) error {
			var err error
			*result, err = f.repo.RevertToInTx(ctx, tx, req)
			return err
		}
	}
	revertSecond := func(req datasources.RevertRequest, result *datasources.SubmitResult) func(context.Context) error {
		return func(ctx context.Context) error {
			var err error
			*result, err = f.repo.RevertTo(ctx, req)
			return err
		}
	}

	t.Run("submit_first", func(t *testing.T) {
		submit, revert := setUp("submit first")
		var submitted, reverted datasources.SubmitResult
		require.NoError(t, inOrder(t, f.db, lockUser, []any{wrenUserID}, f.submitFirst(submit, &submitted),
			revertSecond(revert, &reverted)))
		assert.Equal(t, statusWithdrawn, f.stored(t, submitted.RevisionID).status, "the revert supersedes it")
		assert.Equal(t, statusPending, f.stored(t, reverted.RevisionID).status)
	})
	t.Run("revert_first", func(t *testing.T) {
		submit, revert := setUp("revert first")
		var submitted, reverted datasources.SubmitResult
		require.NoError(t, inOrder(t, f.db, lockUser, []any{wrenUserID}, revertFirst(revert, &reverted),
			f.submitSecond(submit, &submitted)))
		assert.Equal(t, statusWithdrawn, f.stored(t, reverted.RevisionID).status, "the submit supersedes it")
		assert.Equal(t, statusPending, f.stored(t, submitted.RevisionID).status)
	})
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM revisions WHERE author_user_id = ? AND status = 'pending'",
		wrenUserID), "one pending revision")
}

// TestRepository_RevertTo_ConcurrentApprove forces, in each order, a moderator's revert and an approval of another
// revision of the entity, which the entity row serialises. Approved first, the revert is based on the approved
// revision; reverted first, the approval merges onto the revert.
func TestRepository_RevertTo_ConcurrentApprove(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	setUp := func(name string) (datasources.SubmitResult, datasources.ApproveRequest, datasources.RevertRequest) {
		edit := editAndSubmit(t, f, actorRowan, key, func(d *domain.PieceDocument) {
			d.NotesMD = new("Edit, " + name + ".")
		})
		pending := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.IsStub = !d.IsStub })
		return edit, f.approveRequest(t, pending.RevisionID), datasources.RevertRequest{Actor: actorRowan,
			RevisionID: f.song.RevisionID, Summary: "Revert, " + name + ".", Now: f.tick()}
	}

	t.Run("approve_first", func(t *testing.T) {
		_, approve, revert := setUp("approve first")
		var reverted datasources.SubmitResult
		require.NoError(t, inOrder(t, f.db, lockPiece, []any{f.song.ID}, f.approveFirst(approve),
			func(ctx context.Context) error {
				var err error
				reverted, err = f.repo.RevertTo(ctx, revert)
				return err
			}))
		assert.Equal(t, approve.RevisionID, f.stored(t, reverted.RevisionID).base.Int64,
			"the revert is based on the approved revision")
		assert.Equal(t, reverted.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
	})
	t.Run("revert_first", func(t *testing.T) {
		edit, approve, revert := setUp("revert first")
		var reverted datasources.SubmitResult
		require.NoError(t, inOrder(t, f.db, lockPiece, []any{f.song.ID}, func(ctx context.Context, tx *sql.Tx) error {
			var err error
			reverted, err = f.repo.RevertToInTx(ctx, tx, revert)
			return err
		}, f.approveSecond(approve)))
		assert.Equal(t, edit.RevisionID, f.stored(t, reverted.RevisionID).base.Int64)
		assert.Equal(t, approve.RevisionID, f.currentRevision(t, "pieces", f.song.ID), "merged onto the revert")
		got := f.publicPiece(t, f.song.Slug)
		assert.Equal(t, songDocument(f).NotesMD, got.NotesMD, "the revert's notes stay")
		assert.Equal(t, f.storedPiece(t, approve.RevisionID).IsStub, got.IsStub, "the approved edit is kept")
	})
}
