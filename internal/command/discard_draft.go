package command

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
)

var _ Command[RevisionRequest, Empty] = (*DiscardDraft)(nil)

// DiscardDraft deletes one of the principal's drafts.
type DiscardDraft struct {
	Discarder datasources.DraftDiscarder
}

// Execute deletes the principal's draft req.ID, failing with datasources.ErrNotFound unless it is theirs and with
// domain.ErrNotDraft if it is no longer a draft.
func (c *DiscardDraft) Execute(ctx context.Context, req RevisionRequest) (Empty, error) {
	if err := c.Discarder.DiscardDraft(ctx, req.Principal.Actor(), req.ID); err != nil {
		return Empty{}, fmt.Errorf("discarding draft: %w", err)
	}

	return Empty{}, nil
}
