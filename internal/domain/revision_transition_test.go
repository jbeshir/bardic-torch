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
		{id: "T01", action: RevisionActionSaveDraft, actor: "C", want: RevisionStatusDraft},
		{id: "T02", action: RevisionActionSaveDraft, actor: "C", want: RevisionStatusDraft, note: "new entity"},
		{id: "T03", action: RevisionActionSaveDraft, actor: "C", want: RevisionStatusDraft,
			note: "the repository updates C's existing draft"},
		{id: "T04", from: draft, action: RevisionActionSaveDraft, actor: "C", want: RevisionStatusDraft},
		{id: "T05", from: draft, action: RevisionActionSaveDraft, actor: "C2", err: ErrNotRevisionAuthor},
		{id: "T06", from: draft, action: RevisionActionDiscardDraft, actor: "C", want: ""},
		{id: "T07_C2", from: draft, action: RevisionActionDiscardDraft, actor: "C2", err: ErrNotRevisionAuthor},
		{id: "T07_M2", from: draft, action: RevisionActionDiscardDraft, actor: "M2", err: ErrNotRevisionAuthor},
		{id: "T08", from: draft, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending},
		{id: "T09", from: draft, action: RevisionActionSubmit, actor: "M", want: RevisionStatusApproved},
		{id: "T10", from: draft, action: RevisionActionSubmit, actor: "C2", err: ErrNotRevisionAuthor},
		{id: "T11", from: draft, action: RevisionActionApprove, actor: "M2", err: ErrNotPending},
		{id: "T12", from: draft, action: RevisionActionReject, actor: "M2", err: ErrNotPending},
		{id: "T13", from: draft, action: RevisionActionWithdraw, actor: "C", err: ErrNotPending},
		{id: "T14", from: pending, action: RevisionActionApprove, actor: "M2", want: RevisionStatusApproved},
		{id: "T15", from: pending, action: RevisionActionApprove, actor: "C", err: ErrModeratorRequired},
		{id: "T16", from: pending, action: RevisionActionApprove, actor: "C2", err: ErrModeratorRequired},
		{id: "T17", from: pending, action: RevisionActionReject, actor: "M2", want: RevisionStatusRejected},
		{id: "T18", from: pending, action: RevisionActionReject, actor: "C", err: ErrModeratorRequired},
		{id: "T19", from: pending, action: RevisionActionWithdraw, actor: "C", want: RevisionStatusWithdrawn},
		{id: "T20", from: pending, action: RevisionActionWithdraw, actor: "C2", err: ErrNotRevisionAuthor},
		{id: "T21", from: pending, action: RevisionActionWithdraw, actor: "M2", err: ErrNotRevisionAuthor},
		{id: "T22", from: pending, action: RevisionActionRebase, actor: "M2", want: RevisionStatusPending},
		{id: "T23", from: pending, action: RevisionActionRebase, actor: "C", err: ErrModeratorRequired},
		{id: "T24_new", from: draft, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending,
			note: "the repository withdraws the superseded revision in the same transaction"},
		{id: "T24_old", from: pending, action: RevisionActionWithdraw, actor: "C", want: RevisionStatusWithdrawn},
		{id: "T25_save", from: pending, action: RevisionActionSaveDraft, actor: "C", err: ErrNotDraft},
		{id: "T25_discard", from: pending, action: RevisionActionDiscardDraft, actor: "C", err: ErrNotDraft},
		{id: "T25b", from: pending, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending},
		{id: "T25c_save", from: pending, action: RevisionActionSaveDraft, actor: "C2", err: ErrNotRevisionAuthor,
			note: "getting a draft is a read, refused by the repository the same way"},
		{id: "T25c_discard", from: pending, action: RevisionActionDiscardDraft, actor: "C2", err: ErrNotRevisionAuthor},
		{id: "T26", from: approved, action: RevisionActionApprove, actor: "M2", err: ErrNotPending},
		{id: "T27", from: approved, action: RevisionActionReject, actor: "M2", err: ErrNotPending},
		{id: "T28", from: approved, action: RevisionActionWithdraw, actor: "C", err: ErrNotPending},
		{id: "T29", from: approved, action: RevisionActionRebase, actor: "M2", err: ErrNotPending},
		{id: "T30_approve", from: rejected, action: RevisionActionApprove, actor: "M2", err: ErrNotPending},
		{id: "T30_withdraw", from: rejected, action: RevisionActionWithdraw, actor: "C", err: ErrNotPending},
		{id: "T30_rebase", from: rejected, action: RevisionActionRebase, actor: "M2", err: ErrNotPending},
		{id: "T30_submit", from: rejected, action: RevisionActionSubmit, actor: "C", err: ErrNotDraft},
		{id: "T31", from: withdrawn, action: RevisionActionApprove, actor: "M2", err: ErrNotPending},
		{id: "T32", from: withdrawn, action: RevisionActionWithdraw, actor: "C", err: ErrNotPending},
		{id: "T33", from: approved, action: RevisionActionRevert, actor: "C2", want: RevisionStatusPending},
		{id: "T34", from: approved, action: RevisionActionRevert, actor: "M2", want: RevisionStatusApproved},
		{id: "T35", from: approved, action: RevisionActionRevert, actor: "C2", want: RevisionStatusPending,
			note: "already_current is the repository's, which reads the current revision under the entity lock"},
		{id: "T36_pending", from: pending, action: RevisionActionRevert, actor: "C", err: ErrNotApproved},
		{id: "T36_rejected", from: rejected, action: RevisionActionRevert, actor: "M2", err: ErrNotApproved},
		{id: "T36_draft", from: draft, action: RevisionActionRevert, actor: "M", err: ErrNotApproved},
		{id: "T37", from: draft, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending,
			note: "a blocked user is refused by requireAuth: CheckTransition has no blocked input"},
		{id: "T38", from: draft, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending,
			note: "the pending cap is counted by the repository under the user lock"},
		{id: "T39", from: draft, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending},
		{id: "T40", action: RevisionActionSaveDraft, actor: "C", want: RevisionStatusDraft,
			note: "the draft cap is counted by the repository under the user lock"},
		{id: "T41", action: RevisionActionSaveDraft, actor: "C2", want: RevisionStatusDraft,
			note: "another user's placeholder does not resolve (EditorRefResolver), so the entity is not found"},
		{id: "T42", from: pending, action: RevisionActionReject, actor: "M2", want: RevisionStatusRejected},
		{id: "T43", from: pending, action: RevisionActionWithdraw, actor: "C", want: RevisionStatusWithdrawn},
		{id: "T44", from: draft, action: RevisionActionSubmit, actor: "C", want: RevisionStatusPending},
		{id: "T45", from: pending, action: RevisionActionApprove, actor: "M2", want: RevisionStatusApproved,
			note: "the author's role is not an input"},
		{id: "T46", from: pending, action: RevisionActionDiscardDraft, actor: "C", err: ErrNotDraft},
		{id: "T47", from: draft, action: RevisionActionRebase, actor: "M2", err: ErrNotPending},
		{id: "T48", from: rejected, action: RevisionActionWithdraw, actor: "C", err: ErrNotPending},
		{id: "T49", from: draft, action: RevisionActionSubmit, actor: "M", want: RevisionStatusApproved},
		{id: "T50", from: pending, action: RevisionActionApprove, actor: "M2", want: RevisionStatusApproved},
		{id: "T51", from: pending, action: RevisionActionWithdraw, actor: "M", want: RevisionStatusWithdrawn},
		{id: "T52", from: draft, action: RevisionActionSubmit, actor: "MT", want: RevisionStatusPending},
		{id: "T53_approve", from: pending, action: RevisionActionApprove, actor: "MT", err: ErrModeratorRequired,
			note: "api_token_not_allowed is the middleware's answer"},
		{id: "T53_reject", from: pending, action: RevisionActionReject, actor: "MT", err: ErrModeratorRequired},
		{id: "T53_rebase", from: pending, action: RevisionActionRebase, actor: "MT", err: ErrModeratorRequired},
		{id: "T54", from: approved, action: RevisionActionRevert, actor: "MT", want: RevisionStatusPending},
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

// legalTransition returns the status an allowed (role, author, from, action) combination leaves (D2), or false.
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
