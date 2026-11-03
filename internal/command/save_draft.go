package command

import (
	"context"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[SaveDraftRequest, SavedDraft] = (*SaveDraft)(nil)

// SaveDraftRequest is a draft Principal saves: of the entity Entity (nil for a new entity of Document's type), on
// the base BaseRevisionID (nil for the entity's current revision), holding Document in the editor format.
type SaveDraftRequest struct {
	Principal      domain.Principal
	Entity         *domain.EntityKey
	BaseRevisionID *int64
	Document       domain.AnyEditorDocument
	Summary        string
}

// SavedDraft is a saved draft, and whether saving created it rather than replacing the principal's draft of the
// entity.
type SavedDraft struct {
	DraftResult
	Created bool
}

// SaveDraft saves a draft (D1): one per author and existing entity, replacing the author's draft of the entity if
// they have one.
type SaveDraft struct {
	Resolver   datasources.EditorRefResolver
	Saver      datasources.DraftSaver
	Duplicates datasources.DuplicateFinder
	Now        func() time.Time
}

// Execute converts req.Document from the editor format and saves it. It fails with a *domain.DocumentError for a
// reference the principal may not make and for the problems even a draft may not have, and with the errors of
// datasources.DraftSaver; a draft may otherwise be incomplete, since it is validated in full only when submitted.
func (c *SaveDraft) Execute(ctx context.Context, req SaveDraftRequest) (SavedDraft, error) {
	actor := req.Principal.Actor()
	doc, err := toDocument(ctx, c.Resolver, actor, req.Document, req.Summary)
	if err != nil {
		return SavedDraft{}, err
	}

	saved, err := c.Saver.SaveDraft(ctx, datasources.SaveDraftRequest{
		Actor: actor, EntityType: doc.EntityType(), Entity: req.Entity, BaseRevisionID: req.BaseRevisionID,
		Document: doc, Summary: req.Summary, Now: c.Now(),
	})
	if err != nil {
		return SavedDraft{}, fmt.Errorf("saving draft: %w", err)
	}

	result, err := draftResult(ctx, c.Duplicates, actor, saved.Draft)
	if err != nil {
		return SavedDraft{}, err
	}

	return SavedDraft{DraftResult: result, Created: saved.Created}, nil
}
