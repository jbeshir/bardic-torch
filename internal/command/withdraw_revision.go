package command

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[RevisionRequest, domain.MyRevision] = (*WithdrawRevision)(nil)

// WithdrawRevision withdraws one of the principal's pending revisions.
type WithdrawRevision struct {
	Withdrawer datasources.RevisionWithdrawer
	Revisions  datasources.MyRevisionGetter
}

// Execute withdraws the principal's pending revision req.ID and returns it, as they see it. It fails with
// datasources.ErrNotFound unless the revision is theirs, and with domain.ErrNotPending if it is not pending.
func (c *WithdrawRevision) Execute(ctx context.Context, req RevisionRequest) (domain.MyRevision, error) {
	actor := req.Principal.Actor()
	if err := c.Withdrawer.WithdrawRevision(ctx, actor, req.ID); err != nil {
		return domain.MyRevision{}, fmt.Errorf("withdrawing revision: %w", err)
	}

	result, err := submitted(ctx, c.Revisions, actor, datasources.SubmitResult{RevisionID: req.ID})
	if err != nil {
		return domain.MyRevision{}, err
	}

	return result.Revision, nil
}
