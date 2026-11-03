package command

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[EntityRequest, domain.MyRevision] = (*GetPendingVersion)(nil)

// GetPendingVersion returns the principal's pending revision of an entity, as its author sees it (D6).
type GetPendingVersion struct {
	Getter datasources.PendingVersionGetter
}

// Execute returns the principal's pending revision of req.Key, failing with datasources.ErrNotFound if they have
// none, or the entity is neither public nor their own pending new entity.
func (c *GetPendingVersion) Execute(ctx context.Context, req EntityRequest) (domain.MyRevision, error) {
	revision, err := c.Getter.GetPendingVersion(ctx, req.Principal.Actor(), req.Key)
	if err != nil {
		return domain.MyRevision{}, fmt.Errorf("getting pending version: %w", err)
	}

	return revision, nil
}
