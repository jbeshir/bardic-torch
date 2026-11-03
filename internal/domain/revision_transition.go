package domain

import (
	"errors"
	"fmt"
)

// The errors CheckTransition refuses an action with.
var (
	// ErrIllegalTransition is an action the revision's status does not allow. ErrNotDraft and ErrNotPending are
	// the cases a caller tells apart; the bare error is an action on a revision that does not exist yet other than
	// creating a draft.
	ErrIllegalTransition = errors.New("illegal revision transition")
	// ErrNotDraft is a draft action on a revision of the caller's that is no longer a draft.
	ErrNotDraft = fmt.Errorf("%w: the revision is not a draft", ErrIllegalTransition)
	// ErrNotPending is a moderation action, or a withdrawal, of a revision that is not pending.
	ErrNotPending = fmt.Errorf("%w: the revision is not pending", ErrIllegalTransition)
	// ErrNotApproved is a revert to a revision that is not approved, which is answered as if it did not exist.
	ErrNotApproved = errors.New("the revision is not approved")
	// ErrNotRevisionAuthor is an author's action by someone else, which is answered as if the revision did not
	// exist.
	ErrNotRevisionAuthor = errors.New("not the revision's author")
	// ErrModeratorRequired is a moderation action by a contributor.
	ErrModeratorRequired = errors.New("moderator required")
)

// TransitionInput is what decides whether an action on a revision is allowed (D2). Blocking is not an input: a
// blocked user's requests are refused before they reach a revision.
type TransitionInput struct {
	// Role is the actor's effective role: a moderator using an API token acts as a contributor (D25).
	Role Role
	// IsAuthor is whether the actor is the revision's author.
	IsAuthor bool
	// From is the revision's status, read under its row lock, or nil for a revision that does not exist yet.
	From *RevisionStatus
	// Action is what the actor does.
	Action RevisionAction
}

// moderatorActions are the actions only a moderator may take.
var moderatorActions = map[RevisionAction]bool{
	RevisionActionApprove: true, RevisionActionReject: true, RevisionActionRebase: true,
}

// authorActions are the actions only the revision's author may take.
var authorActions = map[RevisionAction]bool{
	RevisionActionSaveDraft: true, RevisionActionDiscardDraft: true, RevisionActionSubmit: true,
	RevisionActionWithdraw: true,
}

// pendingActions are the actions on a pending revision, and the status each leaves it in.
var pendingActions = map[RevisionAction]RevisionStatus{
	RevisionActionApprove:  RevisionStatusApproved,
	RevisionActionReject:   RevisionStatusRejected,
	RevisionActionWithdraw: RevisionStatusWithdrawn,
	RevisionActionRebase:   RevisionStatusPending,
}

// CheckTransition decides an action on a revision (D2): it returns the status the action leaves the revision in,
// or why the action is refused. Discarding a draft deletes it, and returns the empty status. Submitting a revision
// its author already submitted is a replay, which leaves its status unchanged. Reverting to an approved revision
// returns the status of the revision the revert creates: pending, or approved for a moderator.
func CheckTransition(t TransitionInput) (RevisionStatus, error) {
	if moderatorActions[t.Action] && t.Role != RoleModerator {
		return "", ErrModeratorRequired
	}
	if t.From == nil {
		if t.Action == RevisionActionSaveDraft {
			return RevisionStatusDraft, nil
		}

		return "", ErrIllegalTransition
	}
	if authorActions[t.Action] && !t.IsAuthor {
		return "", ErrNotRevisionAuthor
	}
	from := *t.From
	if to, ok := pendingActions[t.Action]; ok {
		if from != RevisionStatusPending {
			return "", ErrNotPending
		}

		return to, nil
	}

	return draftTransition(t.Role, from, t.Action)
}

// submittedStatus is the status of a revision an actor of role submits: a moderator's is approved at once (D3).
func submittedStatus(role Role) RevisionStatus {
	if role == RoleModerator {
		return RevisionStatusApproved
	}

	return RevisionStatusPending
}

// draftTransition decides the actions on drafts, and reverts, of a revision with status from.
func draftTransition(role Role, from RevisionStatus, action RevisionAction) (RevisionStatus, error) {
	if action == RevisionActionRevert {
		if from != RevisionStatusApproved {
			return "", ErrNotApproved
		}

		return submittedStatus(role), nil
	}
	switch {
	case action == RevisionActionSubmit:
		return submitTransition(role, from)
	case action == RevisionActionSaveDraft && from == RevisionStatusDraft:
		return RevisionStatusDraft, nil
	case action == RevisionActionDiscardDraft && from == RevisionStatusDraft:
		return "", nil
	case authorActions[action]:
		return "", ErrNotDraft
	}

	return "", ErrIllegalTransition
}

// submitTransition decides an author's submission of a revision with status from: a draft is submitted, and a
// revision already submitted is a replay, which leaves it unchanged.
func submitTransition(role Role, from RevisionStatus) (RevisionStatus, error) {
	switch from {
	case RevisionStatusDraft:
		return submittedStatus(role), nil
	case RevisionStatusPending, RevisionStatusApproved:
		return from, nil
	case RevisionStatusRejected, RevisionStatusWithdrawn:
	}

	return "", ErrNotDraft
}
