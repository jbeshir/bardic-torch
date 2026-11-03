package command

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[ListMyRevisionsRequest, domain.MyRevisionList] = (*ListMyRevisions)(nil)

// ListMyRevisionsRequest asks for one page of Principal's own revisions matching Query.
type ListMyRevisionsRequest struct {
	Principal domain.Principal
	Query     datasources.MyRevisionQuery
}

// ListMyRevisions lists the principal's own revisions, drafts included, newest first.
type ListMyRevisions struct {
	Lister datasources.MyRevisionLister
}

// Execute returns the page of the principal's revisions req.Query asks for.
func (c *ListMyRevisions) Execute(ctx context.Context, req ListMyRevisionsRequest) (domain.MyRevisionList, error) {
	list, err := c.Lister.ListMyRevisions(ctx, req.Principal.Actor(), req.Query)
	if err != nil {
		return domain.MyRevisionList{}, fmt.Errorf("listing revisions: %w", err)
	}

	return list, nil
}
