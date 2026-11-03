package command

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[EntityRequest, domain.Editable] = (*GetEditable)(nil)

// GetEditable returns the document the principal starts an edit of an entity from (D5): what they may see of the
// current document, or of their pending revision of it.
type GetEditable struct {
	Getter datasources.EditableDocumentGetter
}

// Execute returns the editable document of req.Key, failing with datasources.ErrNotFound if the entity is
// neither public nor the principal's own pending new entity.
func (c *GetEditable) Execute(ctx context.Context, req EntityRequest) (domain.Editable, error) {
	editable, err := c.Getter.GetEditable(ctx, req.Principal.Actor(), req.Key)
	if err != nil {
		return domain.Editable{}, fmt.Errorf("getting editable document: %w", err)
	}

	return editable, nil
}
