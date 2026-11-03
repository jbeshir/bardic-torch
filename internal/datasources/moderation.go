package datasources

import (
	"context"
	"errors"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The errors the moderation writes refuse with, besides those of the revision writes.
var (
	// ErrStale is a rebase resolved against a current revision that is no longer the entity's: the entity moved
	// again, and the moderator must resolve against the new current document.
	ErrStale = errors.New("the entity's current revision has changed")
	// ErrAlreadyCurrent is a revert to the entity's current revision.
	ErrAlreadyCurrent = errors.New("the revision is already current")
)

// ApproveRequest is a moderator's approval of the pending revision RevisionID. Duplicates are the entities a new
// entity may duplicate, as DuplicateFinder found them with includeHidden (the revision's own pending entity left
// out): any match is a possible_duplicate flag, a blocked one refuses the approval (D13).
type ApproveRequest struct {
	Actor      domain.Actor
	RevisionID int64
	Options    domain.ApprovalOptions
	Duplicates []domain.DuplicateMatch
	Now        time.Time
}

// RevisionApprover approves a pending revision (D9, D15): under the global lock order it merges a stale revision
// with the current document, validates it (domain.PlanApproval), approves the pending new terms and persons it
// references alongside it, and writes the entity's rows. It returns ErrNotFound for no such revision,
// domain.ErrNotPending, ErrEntityGone, domain.ErrRebaseRequired, domain.ErrHiddenDataConflict,
// domain.ErrDuplicateBlocked, a *domain.DocumentError, *domain.UnacknowledgedFlagsError and
// *domain.PendingReferenceFlaggedError.
type RevisionApprover interface {
	ApproveRevision(ctx context.Context, req ApproveRequest) error
}

// RejectRequest is a moderator's rejection of the pending revision RevisionID, with a review note for its author.
// AcknowledgedFlags may acknowledge hidden_alias_in_review_note.
type RejectRequest struct {
	Actor             domain.Actor
	RevisionID        int64
	ReviewNote        string
	AcknowledgedFlags []domain.FlagCode
	Now               time.Time
}

// RevisionRejecter rejects a pending revision, deleting its new entity's placeholder if no other pending revision
// has it (D14). It returns ErrNotFound for no such revision, domain.ErrNotPending, and a
// *domain.UnacknowledgedFlagsError for a review note holding a hidden alias that is not acknowledged (D19).
type RevisionRejecter interface {
	RejectRevision(ctx context.Context, req RejectRequest) error
}

// RebaseRequest is a moderator's rebase of the pending revision RevisionID onto CurrentRevisionID, the entity's
// current revision the moderator resolved against (nil for a new entity), with Document, the resolved document
// (the moderator's editable view of it: the preferences withheld). Summary, if not nil, replaces the revision's
// summary.
type RebaseRequest struct {
	Actor             domain.Actor
	RevisionID        int64
	CurrentRevisionID *int64
	Document          domain.AnyDocument
	Summary           *string
	Now               time.Time
}

// RevisionRebaser rebases a pending revision (D9): it stores the resolved document, validated against the current
// document, as the revision's document on the current revision, keeping it pending and keeping what its author
// submitted. It returns ErrNotFound for no such revision, domain.ErrNotPending, ErrStale, ErrEntityGone and a
// *domain.DocumentError.
type RevisionRebaser interface {
	RebaseRevision(ctx context.Context, req RebaseRequest) error
}

// RevertRequest is a revert of an entity to its approved revision RevisionID by the actor (D10). The revision's
// summary is Summary, or "Revert to revision <id>." if it is empty; validation sees the summary as given, so a
// conservation status change needs one as its reason. AcknowledgedFlags are an interactive moderator's.
type RevertRequest struct {
	Actor             domain.Actor
	RevisionID        int64
	Summary           string
	AcknowledgedFlags []domain.FlagCode
	Now               time.Time
}

// RevisionReverter reverts an entity to one of its approved revisions (D10), creating a new revision based on the
// current one: pending for a contributor (or an API token), superseding their pending revision of the entity;
// approved at once for an interactive moderator. A revert identical to the caller's pending revision of the entity
// (or for a moderator, to the current revision, which they authored) is a replay, returned as it is. It returns
// ErrNotFound unless the revision is an approved revision of a public entity, ErrAlreadyCurrent,
// ErrPendingLimitReached, a *domain.DocumentError and the approval's errors.
type RevisionReverter interface {
	RevertTo(ctx context.Context, req RevertRequest) (SubmitResult, error)
}
