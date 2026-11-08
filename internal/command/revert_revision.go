package command

import (
	"context"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[RevertRequest, SubmittedRevision] = (*RevertTo)(nil)

// RevertRequest is Principal's revert of an entity to its approved revision RevisionID (D10), with Summary, which
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
// term of its facet has taken since fails with ErrDuplicateName; otherwise it fails with the errors of
// datasources.RevisionReverter.
func (c *RevertTo) Execute(ctx context.Context, req RevertRequest) (SubmittedRevision, error) {
	actor := req.Principal.Actor()
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
