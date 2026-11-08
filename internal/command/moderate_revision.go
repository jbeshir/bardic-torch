package command

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The moderator commands act only for an interactive moderator: a principal whose effective role
// (domain.Principal.Actor) is moderator, which an API token never has (D25). The router lets no one else reach
// them; each command also refuses anyone else with domain.ErrModeratorRequired.

// ErrDuplicateName is an approval of a term whose name another term of its facet has taken since it was
// submitted (R45).
var ErrDuplicateName = errors.New("the term's name is taken in its facet")

var (
	_ Command[ApproveRevisionRequest, domain.ModerationRevision] = (*ApproveRevision)(nil)
	_ Command[RejectRevisionRequest, domain.ModerationRevision]  = (*RejectRevision)(nil)
	_ Command[RebaseRevisionRequest, domain.ModerationRevision]  = (*RebaseRevision)(nil)
	_ Command[RevisionRequest, domain.ModerationRevision]        = (*GetModerationRevision)(nil)
	_ Command[ModerationQueueRequest, domain.ModerationQueue]    = (*ListModerationQueue)(nil)
)

// moderatorActor returns the actor p acts as if it is an interactive moderator, and domain.ErrModeratorRequired
// otherwise.
func moderatorActor(p domain.Principal) (domain.Actor, error) {
	actor := p.Actor()
	if actor.Role != domain.RoleModerator {
		return domain.Actor{}, domain.ErrModeratorRequired
	}

	return actor, nil
}

// moderationRevision returns the revision id as moderators see it.
func moderationRevision(
	ctx context.Context, getter datasources.ModerationRevisionGetter, id int64,
) (domain.ModerationRevision, error) {
	revision, err := getter.GetModerationRevision(ctx, id)
	if err != nil {
		return domain.ModerationRevision{}, fmt.Errorf("getting moderation revision [%d]: %w", id, err)
	}

	return revision, nil
}

// GetModerationRevision returns a revision of any status but draft as moderators see it (D8).
type GetModerationRevision struct {
	Getter datasources.ModerationRevisionGetter
}

// Execute returns the revision req.ID as moderators see it, failing with datasources.ErrNotFound for no such
// revision and for a draft.
func (c *GetModerationRevision) Execute(ctx context.Context, req RevisionRequest) (domain.ModerationRevision, error) {
	if _, err := moderatorActor(req.Principal); err != nil {
		return domain.ModerationRevision{}, err
	}

	return moderationRevision(ctx, c.Getter, req.ID)
}

// ModerationQueueRequest is Principal's request for a page of the moderation queue; its Now is set by the command.
type ModerationQueueRequest struct {
	Principal domain.Principal
	Query     datasources.ModerationQueueQuery
}

// ListModerationQueue lists the pending revisions for moderators, oldest first.
type ListModerationQueue struct {
	Lister datasources.ModerationQueueLister
	Now    func() time.Time
}

// Execute returns the page of the moderation queue req.Query asks for, its ages measured to now.
func (c *ListModerationQueue) Execute(ctx context.Context, req ModerationQueueRequest) (domain.ModerationQueue, error) {
	if _, err := moderatorActor(req.Principal); err != nil {
		return domain.ModerationQueue{}, err
	}
	query := req.Query
	query.Now = c.Now()
	queue, err := c.Lister.ListModerationQueue(ctx, query)
	if err != nil {
		return domain.ModerationQueue{}, fmt.Errorf("listing the moderation queue: %w", err)
	}

	return queue, nil
}

// ApproveRevisionRequest is Principal's approval of the pending revision RevisionID with Options.
type ApproveRevisionRequest struct {
	Principal  domain.Principal
	RevisionID int64
	Options    domain.ApprovalOptions
}

// ApproveRevision approves a pending revision (D9, D15) and returns it as moderators then see it.
type ApproveRevision struct {
	Revisions  datasources.ModerationRevisionGetter
	Duplicates datasources.DuplicateFinder
	Approver   datasources.RevisionApprover
	Now        func() time.Time
}

// Execute approves the pending revision req.RevisionID. Every new entity it would approve, its own and each pending
// reference approved alongside, is checked for duplicates as moderators check them (docs/PLAN.md §5.1 "Duplicate
// check": hidden aliases and other pending new entities compared, the entity's own placeholder left out), before
// the approval's transaction, which refuses a document changed since: the checks are then made again, up to
// maxDuplicateRechecks times, and after that the approval fails with datasources.ErrUnavailable. A term whose name
// was taken in its facet since fails with ErrDuplicateName; otherwise it fails with the errors of
// datasources.RevisionApprover.
func (c *ApproveRevision) Execute(ctx context.Context, req ApproveRevisionRequest) (domain.ModerationRevision, error) {
	actor, err := moderatorActor(req.Principal)
	if err != nil {
		return domain.ModerationRevision{}, err
	}
	err = withDuplicateRechecks(func() error {
		before, err := moderationRevision(ctx, c.Revisions, req.RevisionID)
		if err != nil {
			return err
		}
		duplicates, err := c.duplicates(ctx, before)
		if err != nil {
			return err
		}
		err = c.Approver.ApproveRevision(ctx, datasources.ApproveRequest{
			Actor: actor, RevisionID: req.RevisionID, Options: req.Options, Duplicates: duplicates, Now: c.Now(),
		})
		if before.EntityType == domain.EntityTypeTerm && isNameDuplicate(err) {
			return ErrDuplicateName
		}
		if err != nil {
			return fmt.Errorf("approving revision [%d]: %w", req.RevisionID, err)
		}
		return nil
	})
	if err != nil {
		return domain.ModerationRevision{}, err
	}

	return moderationRevision(ctx, c.Revisions, req.RevisionID)
}

// duplicates returns the duplicate checks of the new entities approving the revision, as moderators see it, may
// approve, by revision ID: its own entity's, which the moderation view holds, and each pending reference's.
func (c *ApproveRevision) duplicates(
	ctx context.Context, revision domain.ModerationRevision,
) (map[int64]datasources.DuplicateCheck, error) {
	checks := map[int64]datasources.DuplicateCheck{}
	if revision.Entity != nil && revision.Entity.IsNew {
		checks[revision.ID] = datasources.DuplicateCheck{
			Candidate: domain.NewDuplicateCandidate(revision.Document), Matches: revision.Duplicates,
		}
	}
	for _, ref := range revision.PendingReferences {
		candidate := domain.NewDuplicateCandidate(ref.Document)
		matches, err := c.Duplicates.FindDuplicates(ctx, candidate, true)
		if err != nil {
			return nil, fmt.Errorf("finding duplicates of pending reference [%d]: %w", ref.RevisionID, err)
		}
		checks[ref.RevisionID] = datasources.DuplicateCheck{Candidate: candidate,
			Matches: domain.MatchesOtherThan(matches, ref.Entity)}
	}

	return checks, nil
}

// isNameDuplicate reports whether err is a document error whose only problem is a name another entity has taken.
func isNameDuplicate(err error) bool {
	var documentErr *domain.DocumentError
	if !errors.As(err, &documentErr) {
		return false
	}

	return slices.Equal(documentErr.Problems, []domain.DocumentProblem{{Path: "/name", Code: domain.ProblemDuplicate}})
}

// RejectRevisionRequest is Principal's rejection of the pending revision RevisionID with a review note for its
// author; AcknowledgedFlags may acknowledge a hidden alias in the note (D19).
type RejectRevisionRequest struct {
	Principal         domain.Principal
	RevisionID        int64
	ReviewNote        string
	AcknowledgedFlags []domain.FlagCode
}

// RejectRevision rejects a pending revision and returns it as moderators then see it.
type RejectRevision struct {
	Revisions datasources.ModerationRevisionGetter
	Rejecter  datasources.RevisionRejecter
	Now       func() time.Time
}

// Execute rejects the pending revision req.RevisionID, failing with the errors of datasources.RevisionRejecter.
func (c *RejectRevision) Execute(ctx context.Context, req RejectRevisionRequest) (domain.ModerationRevision, error) {
	actor, err := moderatorActor(req.Principal)
	if err != nil {
		return domain.ModerationRevision{}, err
	}
	err = c.Rejecter.RejectRevision(ctx, datasources.RejectRequest{
		Actor: actor, RevisionID: req.RevisionID, ReviewNote: req.ReviewNote,
		AcknowledgedFlags: req.AcknowledgedFlags, Now: c.Now(),
	})
	if err != nil {
		return domain.ModerationRevision{}, fmt.Errorf("rejecting revision [%d]: %w", req.RevisionID, err)
	}

	return moderationRevision(ctx, c.Revisions, req.RevisionID)
}

// RebaseRevisionRequest is Principal's rebase of the pending revision RevisionID onto CurrentRevisionID, the
// entity's current revision they resolved Document against (nil for a new entity). Summary, if not nil, replaces
// the revision's summary.
type RebaseRevisionRequest struct {
	Principal         domain.Principal
	RevisionID        int64
	CurrentRevisionID *int64
	Document          domain.AnyEditorDocument
	Summary           *string
}

// RebaseRevision rebases a pending revision (D9) and returns it as moderators then see it.
type RebaseRevision struct {
	Resolver  datasources.EditorRefResolver
	Revisions datasources.ModerationRevisionGetter
	Rebaser   datasources.RevisionRebaser
	Now       func() time.Time
}

// Execute converts req.Document from the editor format, failing with a *domain.DocumentError for references it
// cannot resolve, and rebases the revision req.RevisionID onto it, failing with the errors of
// datasources.RevisionRebaser.
func (c *RebaseRevision) Execute(ctx context.Context, req RebaseRevisionRequest) (domain.ModerationRevision, error) {
	actor, err := moderatorActor(req.Principal)
	if err != nil {
		return domain.ModerationRevision{}, err
	}
	var summary string
	if req.Summary != nil {
		summary = *req.Summary
	}
	doc, err := toDocument(ctx, c.Resolver, actor, req.Document, summary)
	if err != nil {
		return domain.ModerationRevision{}, err
	}
	err = c.Rebaser.RebaseRevision(ctx, datasources.RebaseRequest{
		Actor: actor, RevisionID: req.RevisionID, CurrentRevisionID: req.CurrentRevisionID, Document: doc,
		Summary: req.Summary, Now: c.Now(),
	})
	if err != nil {
		return domain.ModerationRevision{}, fmt.Errorf("rebasing revision [%d]: %w", req.RevisionID, err)
	}

	return moderationRevision(ctx, c.Revisions, req.RevisionID)
}
