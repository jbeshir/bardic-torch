package command

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[RevisionRequest, domain.MyRevision] = (*GetMyRevision)(nil)

// GetMyRevision returns one of the principal's own revisions, drafts included, with its views.
type GetMyRevision struct {
	Getter datasources.MyRevisionGetter
}

// Execute returns the principal's revision req.ID, failing with datasources.ErrNotFound unless it is theirs.
func (c *GetMyRevision) Execute(ctx context.Context, req RevisionRequest) (domain.MyRevision, error) {
	revision, err := c.Getter.GetMyRevision(ctx, req.Principal.Actor(), req.ID)
	if err != nil {
		return domain.MyRevision{}, fmt.Errorf("getting revision: %w", err)
	}

	return revision, nil
}
