package domain

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transitionRow is a row of the state-transition table (plan §6.1): an actor's action on a revision with status
// from (nil for none yet), and the status it leaves or the error it is refused with. The actors are C (a
// contributor author), C2 (another contributor), M (a moderator author, interactive), M2 (another moderator,
// interactive) and MT (a moderator's API token, which acts as a contributor). Cases that CheckTransition allows
// but another layer decides (middleware, caps, placeholders) are in decidedElsewhereRows, not here; note says
// what else a row relies on.
type transitionRow struct {
	id     string
	from   *RevisionStatus
	action RevisionAction
	actor  string
	want   RevisionStatus
	err    error
	note   string
}

// actorInput is the role and authorship of each actor of the table.
var actorInput = map[string]TransitionInput{
	"C": {Role: RoleContributor, IsAuthor: true}, "C2": {Role: RoleContributor},
	"M": {Role: RoleModerator, IsAuthor: true}, "M2": {Role: RoleModerator},
	"MT": {Role: RoleContributor, IsAuthor: true},
}

func status(s RevisionStatus) *RevisionStatus { return &s }

var (
	draft     = status(RevisionStatusDraft)
	pending   = status(RevisionStatusPending)
	approved  = status(RevisionStatusApproved)
	rejected  = status(RevisionStatusRejected)
	withdrawn = status(RevisionStatusWithdrawn)
)

func TestCheckTransition(t *testing.T) {
	t.Parallel()

	rows := []transitionRow{
		{id: "new_entity_save_draft_c_draft", action: RevisionActionSaveDraft, actor: "C", want: RevisionStatusDraft},
		{id: "new_entity_save_draft_c_draft_existing_draft",
			action: RevisionActionSaveDraft, actor: "C", want: RevisionStatusDraft,
			note: "the repository updates C's existing draft"},
		{id: "draft_save_draft_c_draft",
			from: draft, action: RevisionActionSaveDraft, actor: "C", want: RevisionStatusDraft},
		{id: "draft_save_draft_c2_not_revision_author",
			from: draft, action: RevisionActionSaveDraft, actor: "C2", err: ErrNotRevisionAuthor},
		{id: "draft_discard_draft_c_discarded", from: draft, action: RevisionActionDiscardDraft, actor: "C", want: ""},
		{id: "draft_discard_draft_c2_not_revision_author",
			from: draft, action: RevisionActionDiscardDraft, actor: "C2", err: ErrNotRevisionAuthor},
		{id: "draft_discard_draft_m2_not_revision_author",
			from: draft, action: RevisionActionDiscardDraft, actor: "M2", err: ErrNotRevisionAuthor},
		{id: "draft_submit_c_pending",
			from: draft, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending},
		{id: "draft_submit_m_approved",
			from: draft, action: RevisionActionSubmit, actor: "M", want: RevisionStatusApproved},
		{id: "draft_submit_c2_not_revision_author",
			from: draft, action: RevisionActionSubmit, actor: "C2", err: ErrNotRevisionAuthor},
		{id: "draft_approve_m2_not_pending",
			from: draft, action: RevisionActionApprove, actor: "M2", err: ErrNotPending},
		{id: "draft_reject_m2_not_pending", from: draft, action: RevisionActionReject, actor: "M2", err: ErrNotPending},
		{id: "draft_withdraw_c_not_pending",
			from: draft, action: RevisionActionWithdraw, actor: "C", err: ErrNotPending},
		{id: "pending_approve_m2_approved",
			from: pending, action: RevisionActionApprove, actor: "M2", want: RevisionStatusApproved},
		{id: "pending_approve_c_moderator_required",
			from: pending, action: RevisionActionApprove, actor: "C", err: ErrModeratorRequired},
		{id: "pending_approve_c2_moderator_required",
			from: pending, action: RevisionActionApprove, actor: "C2", err: ErrModeratorRequired},
		{id: "pending_reject_m2_rejected",
			from: pending, action: RevisionActionReject, actor: "M2", want: RevisionStatusRejected},
		{id: "pending_reject_c_moderator_required",
			from: pending, action: RevisionActionReject, actor: "C", err: ErrModeratorRequired},
		{id: "pending_withdraw_c_withdrawn",
			from: pending, action: RevisionActionWithdraw, actor: "C", want: RevisionStatusWithdrawn},
		{id: "pending_withdraw_c2_not_revision_author",
			from: pending, action: RevisionActionWithdraw, actor: "C2", err: ErrNotRevisionAuthor},
		{id: "pending_withdraw_m2_not_revision_author",
			from: pending, action: RevisionActionWithdraw, actor: "M2", err: ErrNotRevisionAuthor},
		{id: "pending_rebase_m2_pending",
			from: pending, action: RevisionActionRebase, actor: "M2", want: RevisionStatusPending},
		{id: "pending_rebase_c_moderator_required",
			from: pending, action: RevisionActionRebase, actor: "C", err: ErrModeratorRequired},
		{id: "draft_submit_c_pending_supersedes_pending",
			from: draft, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending,
			note: "the repository withdraws the superseded revision in the same transaction"},
		{id: "pending_save_draft_c_not_draft",
			from: pending, action: RevisionActionSaveDraft, actor: "C", err: ErrNotDraft},
		{id: "pending_discard_draft_c_not_draft",
			from: pending, action: RevisionActionDiscardDraft, actor: "C", err: ErrNotDraft},
		{id: "pending_submit_c_pending",
			from: pending, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending},
		{id: "pending_save_draft_c2_not_revision_author",
			from: pending, action: RevisionActionSaveDraft, actor: "C2", err: ErrNotRevisionAuthor,
			note: "getting a draft is a read, refused by the repository the same way"},
		{id: "pending_discard_draft_c2_not_revision_author",
			from: pending, action: RevisionActionDiscardDraft, actor: "C2", err: ErrNotRevisionAuthor},
		{id: "approved_approve_m2_not_pending",
			from: approved, action: RevisionActionApprove, actor: "M2", err: ErrNotPending},
		{id: "approved_reject_m2_not_pending",
			from: approved, action: RevisionActionReject, actor: "M2", err: ErrNotPending},
		{id: "approved_withdraw_c_not_pending",
			from: approved, action: RevisionActionWithdraw, actor: "C", err: ErrNotPending},
		{id: "approved_rebase_m2_not_pending",
			from: approved, action: RevisionActionRebase, actor: "M2", err: ErrNotPending},
		{id: "rejected_approve_m2_not_pending",
			from: rejected, action: RevisionActionApprove, actor: "M2", err: ErrNotPending},
		{id: "rejected_withdraw_c_not_pending",
			from: rejected, action: RevisionActionWithdraw, actor: "C", err: ErrNotPending},
		{id: "rejected_rebase_m2_not_pending",
			from: rejected, action: RevisionActionRebase, actor: "M2", err: ErrNotPending},
		{id: "rejected_submit_c_not_draft", from: rejected, action: RevisionActionSubmit, actor: "C", err: ErrNotDraft},
		{id: "withdrawn_approve_m2_not_pending",
			from: withdrawn, action: RevisionActionApprove, actor: "M2", err: ErrNotPending},
		{id: "withdrawn_withdraw_c_not_pending",
			from: withdrawn, action: RevisionActionWithdraw, actor: "C", err: ErrNotPending},
		{id: "approved_revert_c2_pending",
			from: approved, action: RevisionActionRevert, actor: "C2", want: RevisionStatusPending},
		{id: "approved_revert_m2_approved",
			from: approved, action: RevisionActionRevert, actor: "M2", want: RevisionStatusApproved},
		{id: "pending_revert_c_illegal",
			from: pending, action: RevisionActionRevert, actor: "C", err: ErrIllegalTransition},
		{id: "rejected_revert_m2_illegal",
			from: rejected, action: RevisionActionRevert, actor: "M2", err: ErrIllegalTransition},
		{id: "draft_revert_m_illegal", from: draft, action: RevisionActionRevert, actor: "M", err: ErrIllegalTransition},
		{id: "draft_rebase_m2_not_pending", from: draft, action: RevisionActionRebase, actor: "M2", err: ErrNotPending},
		{id: "pending_withdraw_m_withdrawn",
			from: pending, action: RevisionActionWithdraw, actor: "M", want: RevisionStatusWithdrawn},
		{id: "draft_submit_mt_pending_api_token",
			from: draft, action: RevisionActionSubmit, actor: "MT", want: RevisionStatusPending},
		{id: "pending_approve_mt_moderator_required",
			from: pending, action: RevisionActionApprove, actor: "MT", err: ErrModeratorRequired,
			note: "api_token_not_allowed is the middleware's answer"},
		{id: "pending_reject_mt_moderator_required",
			from: pending, action: RevisionActionReject, actor: "MT", err: ErrModeratorRequired},
		{id: "pending_rebase_mt_moderator_required",
			from: pending, action: RevisionActionRebase, actor: "MT", err: ErrModeratorRequired},
		{id: "approved_revert_mt_pending_api_token",
			from: approved, action: RevisionActionRevert, actor: "MT", want: RevisionStatusPending},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			in := actorInput[row.actor]
			in.From, in.Action = row.from, row.action
			got, err := CheckTransition(in)
			if row.err != nil {
				require.ErrorIs(t, err, row.err)
				assert.Empty(t, got)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, row.want, got)
		})
	}
}

// decidedElsewhere is a case of the state-transition table that CheckTransition does not decide: it allows the
// action, and another layer refuses it or adds a rule. decidedBy names the test that pins the real outcome (a test
// function of the repository, so that a rename or removal fails TestDecidedElsewhere_NamesExistingTests).
type decidedElsewhere struct {
	id        string
	rule      string
	decidedBy string
}

var decidedElsewhereRows = []decidedElsewhere{
	{id: "revert_to_current_revision", decidedBy: "TestRepository_RevertTo",
		rule: "reverting to the entity's current revision is already_current, read under the entity lock"},
	{id: "blocked_user_writes", decidedBy: "TestRouter_AuthMatrix",
		rule: "a blocked user is refused before the command runs; CheckTransition has no blocked input"},
	{id: "pending_cap", decidedBy: "TestRepository_SubmitDraft_PendingLimit",
		rule: "the pending cap is counted by the repository under the user lock"},
	{id: "draft_cap", decidedBy: "TestRepository_SaveDraft_Limit",
		rule: "the draft cap is counted by the repository under the user lock"},
	{id: "other_users_placeholder", decidedBy: "TestRepository_SaveDraft_OthersPlaceholder",
		rule: "another user's placeholder does not resolve, so saving a draft on it is not found"},
	{id: "approval_after_author_promoted", decidedBy: "TestRepository_ApproveRevision_AfterAuthorPromoted",
		rule: "the author's role is not an input: approving a contributor's revision after they became a moderator " +
			"is the same approval"},
}

// TestDecidedElsewhere_NamesExistingTests checks that every case CheckTransition does not decide names a test that
// exists under internal, so that the table cannot point at a test that was renamed or never written.
func TestDecidedElsewhere_NamesExistingTests(t *testing.T) {
	t.Parallel()

	declared := map[string]bool{}
	internal := os.DirFS("..")
	err := fs.WalkDir(internal, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := fs.ReadFile(internal, path)
		if err != nil {
			return err
		}
		for line := range strings.SplitSeq(string(src), "\n") {
			if name, ok := strings.CutPrefix(line, "func "); ok {
				name, _, _ = strings.Cut(name, "(")
				declared[name] = true
			}
		}

		return nil
	})
	require.NoError(t, err)

	for _, row := range decidedElsewhereRows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			assert.NotEmpty(t, row.rule)
			assert.True(t, declared[row.decidedBy], "%s names no test: %s", row.id, row.decidedBy)
		})
	}
}

// legalTransition returns the status an allowed (role, author, from, action) combination leaves, or false.
func legalTransition(in TransitionInput) (RevisionStatus, bool) {
	moderator := in.Role == RoleModerator
	submitted := RevisionStatusPending
	if moderator {
		submitted = RevisionStatusApproved
	}
	from := RevisionStatus("")
	if in.From != nil {
		from = *in.From
	}
	type key struct {
		from   RevisionStatus
		action RevisionAction
	}
	legal := map[key]RevisionStatus{{"", RevisionActionSaveDraft}: RevisionStatusDraft}
	if in.IsAuthor {
		legal[key{RevisionStatusDraft, RevisionActionSaveDraft}] = RevisionStatusDraft
		legal[key{RevisionStatusDraft, RevisionActionDiscardDraft}] = ""
		legal[key{RevisionStatusDraft, RevisionActionSubmit}] = submitted
		legal[key{RevisionStatusPending, RevisionActionSubmit}] = RevisionStatusPending
		legal[key{RevisionStatusApproved, RevisionActionSubmit}] = RevisionStatusApproved
		legal[key{RevisionStatusPending, RevisionActionWithdraw}] = RevisionStatusWithdrawn
	}
	if moderator {
		legal[key{RevisionStatusPending, RevisionActionApprove}] = RevisionStatusApproved
		legal[key{RevisionStatusPending, RevisionActionReject}] = RevisionStatusRejected
		legal[key{RevisionStatusPending, RevisionActionRebase}] = RevisionStatusPending
	}
	legal[key{RevisionStatusApproved, RevisionActionRevert}] = submitted
	to, ok := legal[key{from, in.Action}]

	return to, ok
}

// refusal is the error an illegal combination is refused with: a contributor's moderation action needs a
// moderator; an action on no revision is illegal; another user's revision is not found; then the action's own
// rule: a draft action on a non-draft, a pending action on a non-pending, and a revert to a non-approved revision is illegal.
func refusal(in TransitionInput) error {
	switch {
	case moderatorActions[in.Action] && in.Role != RoleModerator:
		return ErrModeratorRequired
	case in.From == nil:
		return ErrIllegalTransition
	case authorActions[in.Action] && !in.IsAuthor:
		return ErrNotRevisionAuthor
	case in.Action == RevisionActionRevert:
		return ErrIllegalTransition
	case in.Action == RevisionActionSaveDraft || in.Action == RevisionActionDiscardDraft ||
		in.Action == RevisionActionSubmit:
		return ErrNotDraft
	}

	return ErrNotPending
}

func TestCheckTransition_Exhaustive(t *testing.T) {
	t.Parallel()

	froms := []*RevisionStatus{nil, draft, pending, approved, rejected, withdrawn}
	actions := []RevisionAction{
		RevisionActionSaveDraft, RevisionActionDiscardDraft, RevisionActionSubmit, RevisionActionApprove,
		RevisionActionReject, RevisionActionWithdraw, RevisionActionRebase, RevisionActionRevert,
	}
	for _, role := range []Role{RoleContributor, RoleModerator} {
		for _, author := range []bool{false, true} {
			for _, from := range froms {
				for _, action := range actions {
					in := TransitionInput{Role: role, IsAuthor: author, From: from, Action: action}
					got, err := CheckTransition(in)
					name := fmt.Sprintf("%s/author=%t/%v/%s", role, author, deref(statusString(from)), action)
					if want, ok := legalTransition(in); ok {
						assert.NoError(t, err, name)
						assert.Equal(t, want, got, name)

						continue
					}
					wantErr := refusal(in)
					require.ErrorIs(t, err, wantErr, name)
					if errors.Is(wantErr, ErrIllegalTransition) {
						assert.ErrorIs(t, err, ErrIllegalTransition, name)
					}
				}
			}
		}
	}
}

func statusString(s *RevisionStatus) *string {
	if s == nil {
		return nil
	}
	str := string(*s)

	return &str
}
