package command

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
)

var _ Command[RevisionRequest, DraftResult] = (*GetDraft)(nil)

// GetDraft returns one of the principal's drafts. Drafts are private to their author: moderators do not see them.
type GetDraft struct {
	Getter     datasources.DraftGetter
	Duplicates datasources.DuplicateFinder
}

// Execute returns the principal's draft req.ID, failing with datasources.ErrNotFound unless it is theirs and with
// domain.ErrNotDraft if it is no longer a draft.
func (c *GetDraft) Execute(ctx context.Context, req RevisionRequest) (DraftResult, error) {
	actor := req.Principal.Actor()
	draft, err := c.Getter.GetDraft(ctx, actor, req.ID)
	if err != nil {
		return DraftResult{}, fmt.Errorf("getting draft: %w", err)
	}

	return draftResult(ctx, c.Duplicates, actor, draft)
}
