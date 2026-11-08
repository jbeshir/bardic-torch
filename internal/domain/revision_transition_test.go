package domain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transitionRow is a row of the state-transition table (plan §6.1): an actor's action on a revision with status
// from (nil for none yet), and the status it leaves or the error it is refused with. The actors are C (a
// contributor author), C2 (another contributor), M (a moderator author, interactive), M2 (another moderator,
// interactive) and MT (a moderator's API token, which acts as a contributor). Rows decided elsewhere (middleware,
// caps, placeholders) check what CheckTransition contributes to them; note says what decides the rest.
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
		{id: "new_entity_save_draft_c_draft_for_new_entity",
			action: RevisionActionSaveDraft, actor: "C", want: RevisionStatusDraft, note: "new entity"},
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
		{id: "pending_withdraw_c_withdrawn_superseded",
			from: pending, action: RevisionActionWithdraw, actor: "C", want: RevisionStatusWithdrawn},
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
		{id: "approved_revert_c2_pending_already_current",
			from: approved, action: RevisionActionRevert, actor: "C2", want: RevisionStatusPending,
			note: "already_current is the repository's, which reads the current revision under the entity lock"},
		{id: "pending_revert_c_not_approved",
			from: pending, action: RevisionActionRevert, actor: "C", err: ErrNotApproved},
		{id: "rejected_revert_m2_not_approved",
			from: rejected, action: RevisionActionRevert, actor: "M2", err: ErrNotApproved},
		{id: "draft_revert_m_not_approved", from: draft, action: RevisionActionRevert, actor: "M", err: ErrNotApproved},
		{id: "draft_submit_c_pending_blocked_user",
			from: draft, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending,
			note: "a blocked user is refused by requireAuth: CheckTransition has no blocked input"},
		{id: "draft_submit_c_pending_pending_cap",
			from: draft, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending,
			note: "the pending cap is counted by the repository under the user lock"},
		{id: "draft_submit_c_pending_plain",
			from: draft, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending},
		{id: "new_entity_save_draft_c_draft_draft_cap",
			action: RevisionActionSaveDraft, actor: "C", want: RevisionStatusDraft,
			note: "the draft cap is counted by the repository under the user lock"},
		{id: "new_entity_save_draft_c2_draft_foreign_placeholder",
			action: RevisionActionSaveDraft, actor: "C2", want: RevisionStatusDraft,
			note: "another user's placeholder does not resolve (EditorRefResolver), so the entity is not found"},
		{id: "pending_reject_m2_rejected_recheck",
			from: pending, action: RevisionActionReject, actor: "M2", want: RevisionStatusRejected},
		{id: "pending_withdraw_c_withdrawn_recheck",
			from: pending, action: RevisionActionWithdraw, actor: "C", want: RevisionStatusWithdrawn},
		{id: "draft_submit_c_pending_recheck",
			from: draft, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending},
		{id: "pending_approve_m2_approved_author_role_not_input",
			from: pending, action: RevisionActionApprove, actor: "M2", want: RevisionStatusApproved,
			note: "the author's role is not an input"},
		{id: "pending_discard_draft_c_not_draft_recheck",
			from: pending, action: RevisionActionDiscardDraft, actor: "C", err: ErrNotDraft},
		{id: "draft_rebase_m2_not_pending", from: draft, action: RevisionActionRebase, actor: "M2", err: ErrNotPending},
		{id: "rejected_withdraw_c_not_pending_recheck",
			from: rejected, action: RevisionActionWithdraw, actor: "C", err: ErrNotPending},
		{id: "draft_submit_m_approved_moderator_author",
			from: draft, action: RevisionActionSubmit, actor: "M", want: RevisionStatusApproved},
		{id: "pending_approve_m2_approved_recheck",
			from: pending, action: RevisionActionApprove, actor: "M2", want: RevisionStatusApproved},
		{id: "pending_withdraw_m_withdrawn_moderator_author",
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
// rule: a draft action on a non-draft, a pending action on a non-pending, a revert to a non-approved revision.
func refusal(in TransitionInput) error {
	switch {
	case moderatorActions[in.Action] && in.Role != RoleModerator:
		return ErrModeratorRequired
	case in.From == nil:
		return ErrIllegalTransition
	case authorActions[in.Action] && !in.IsAuthor:
		return ErrNotRevisionAuthor
	case in.Action == RevisionActionRevert:
		return ErrNotApproved
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
