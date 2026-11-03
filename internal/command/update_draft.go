package command

import (
	"context"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[UpdateDraftRequest, DraftResult] = (*UpdateDraft)(nil)

// UpdateDraftRequest is a new document, in the editor format, and summary for Principal's draft ID.
type UpdateDraftRequest struct {
	Principal domain.Principal
	ID        int64
	Document  domain.AnyEditorDocument
	Summary   string
}

// UpdateDraft replaces one of the principal's drafts' document and summary, keeping its base.
type UpdateDraft struct {
	Resolver   datasources.EditorRefResolver
	Updater    datasources.DraftUpdater
	Duplicates datasources.DuplicateFinder
	Now        func() time.Time
}

// Execute converts req.Document from the editor format and stores it in the draft, failing as SaveDraft does and
// with the errors of datasources.DraftUpdater.
func (c *UpdateDraft) Execute(ctx context.Context, req UpdateDraftRequest) (DraftResult, error) {
	actor := req.Principal.Actor()
	doc, err := toDocument(ctx, c.Resolver, actor, req.Document, req.Summary)
	if err != nil {
		return DraftResult{}, err
	}

	draft, err := c.Updater.UpdateDraft(ctx, datasources.UpdateDraftRequest{
		Actor: actor, ID: req.ID, Document: doc, Summary: req.Summary, Now: c.Now(),
	})
	if err != nil {
		return DraftResult{}, fmt.Errorf("updating draft: %w", err)
	}

	return draftResult(ctx, c.Duplicates, actor, draft)
}
