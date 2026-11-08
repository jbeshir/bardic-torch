package command

import (
	"context"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[RevertRequest, SubmittedRevision] = (*RevertTo)(nil)

// RevertRequest is Principal's revert of an entity to its approved revision RevisionID, with Summary, which
// may be empty. AcknowledgedFlags are an interactive moderator's, as in SubmitRevisionRequest.
type RevertRequest struct {
	Principal         domain.Principal
	RevisionID        int64
	Summary           string
	AcknowledgedFlags []domain.FlagCode
}

// RevertTo reverts an entity to one of its approved revisions with a new revision: pending for a contributor (or
// any API token), approved at once for an interactive moderator.
type RevertTo struct {
	Reverter  datasources.RevisionReverter
	Revisions datasources.MyRevisionGetter
	Now       func() time.Time
}

// Execute reverts and returns the revision the revert leaves, as the principal sees it; a revert identical to one
// already made is a replay. An interactive moderator's revert is approved at once, so a term whose name another
// term of its facet has taken since fails with ErrDuplicateName; a moderator no longer one once their users row is
// locked reverts as a contributor (asLockedRole). Otherwise it fails with the errors of datasources.RevisionReverter.
func (c *RevertTo) Execute(ctx context.Context, req RevertRequest) (SubmittedRevision, error) {
	return asLockedRole(req.Principal.Actor(), func(actor domain.Actor) (SubmittedRevision, error) {
		return c.revert(ctx, actor, req)
	})
}

// revert is Execute as actor.
func (c *RevertTo) revert(ctx context.Context, actor domain.Actor, req RevertRequest) (SubmittedRevision, error) {
	result, err := c.Reverter.RevertTo(ctx, datasources.RevertRequest{
		Actor: actor, RevisionID: req.RevisionID, Summary: req.Summary,
		AcknowledgedFlags: moderatorAcknowledgements(actor, req.AcknowledgedFlags), Now: c.Now(),
	})
	if actor.Role == domain.RoleModerator && isNameDuplicate(err) {
		return SubmittedRevision{}, ErrDuplicateName
	}
	if err != nil {
		return SubmittedRevision{}, fmt.Errorf("reverting to revision: %w", err)
	}

	return submitted(ctx, c.Revisions, actor, result)
}
