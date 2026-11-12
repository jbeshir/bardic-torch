package command

import (
	"context"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// DuplicateError is returned for a new entity whose title, name or an alias matches public entities, unless the
// submission acknowledges them.
type DuplicateError struct {
	Matches []domain.DuplicateCandidate
}

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("possible duplicate of %d entities", len(e.Matches))
}

var _ Command[SubmitRevisionRequest, domain.RevisionView] = (*SubmitRevision)(nil)

// SubmitRevisionRequest is the request for the SubmitRevision command.
type SubmitRevisionRequest struct {
	User domain.User
	// BaseRevisionID is the approved revision the edit is based on, or nil for a new entity.
	BaseRevisionID *int64
	Summary        string
	// Document is the editor-format document (domain.EditorPieceDocument …), whose type is the entity type.
	Document              any
	AcknowledgeDuplicates bool
	AcknowledgeStale      bool
}

// SubmitRevision checks and stores a revision: a contributor's is pending, a moderator's approved at once.
type SubmitRevision struct {
	Revisions datasources.Revisions
	Now       func() time.Time
}

// storedDocument is a stored-format document as the command checks it.
type storedDocument interface {
	Validate(ctx domain.ValidationContext) []domain.Problem
	DuplicateKeys() []string
}

// Execute resolves the document's slugs, validates it against its base and checks a new entity for duplicates,
// then submits it and returns the author's view of the stored revision. A document that does not validate
// returns a *datasources.ValidationError, duplicates a *DuplicateError, and an unknown base an error wrapping
// datasources.ErrNotFound; the submission's own errors are as datasources.Revisions.SubmitRevision's.
func (c *SubmitRevision) Execute(ctx context.Context, req SubmitRevisionRequest) (domain.RevisionView, error) {
	moderator := req.User.Role == domain.RoleModerator
	entityType, doc, problems, err := c.fromEditor(ctx, req.Document)
	if err != nil {
		return nil, err
	}

	var base *datasources.RevisionBase
	if req.BaseRevisionID != nil {
		b, err := c.Revisions.GetRevisionBase(ctx, entityType, *req.BaseRevisionID)
		if err != nil {
			return nil, err
		}
		base = &b
	}

	var entityID int64
	if base != nil {
		entityID = base.EntityID
	}
	vctx, err := c.Revisions.ValidationContext(ctx, entityID)
	if err != nil {
		return nil, err
	}
	problems = append(problems, doc.Validate(vctx)...)
	problems = append(problems, domain.ValidateSummary(req.Summary)...)
	problems = append(problems, sourceProblems(doc, base, req.Summary)...)
	if len(problems) > 0 {
		return nil, &datasources.ValidationError{Problems: problems}
	}

	if base == nil {
		if err := c.checkDuplicates(ctx, entityType, doc, req.AcknowledgeDuplicates); err != nil {
			return nil, err
		}
	}

	id, err := c.Revisions.SubmitRevision(ctx, datasources.NewRevision{
		BaseRevisionID: req.BaseRevisionID, AuthorUserID: req.User.ID, Summary: req.Summary, Document: doc,
		ByModerator: moderator, AcknowledgeStale: req.AcknowledgeStale, At: c.Now(),
	})
	if err != nil {
		return nil, err
	}

	return c.Revisions.GetMyRevision(ctx, req.User.ID, id)
}

// fromEditor returns the entity type and stored-format document of an editor document, with the slugs it
// references that resolve to no public entity as problems.
func (c *SubmitRevision) fromEditor(
	ctx context.Context, editor any,
) (domain.EntityType, storedDocument, []domain.Problem, error) {
	switch e := editor.(type) {
	case domain.EditorPieceDocument:
		return convert[domain.EditorPieceDocument, domain.PieceDocument](ctx, c, domain.EntityTypePiece, e)
	case domain.EditorPersonDocument:
		return convert[domain.EditorPersonDocument, domain.PersonDocument](ctx, c, domain.EntityTypePerson, e)
	case domain.EditorTermDocument:
		return convert[domain.EditorTermDocument, domain.TermDocument](ctx, c, domain.EntityTypeTerm, e)
	case domain.EditorCollectionDocument:
		return convert[domain.EditorCollectionDocument, domain.CollectionDocument](ctx, c,
			domain.EntityTypeCollection, e)
	case domain.EditorEventDocument:
		return convert[domain.EditorEventDocument, domain.EventDocument](ctx, c, domain.EntityTypeEvent, e)
	default:
		return "", nil, nil, fmt.Errorf("unknown editor document type %T", editor)
	}
}

// editorDocument is an editor-format document converting to D.
type editorDocument[D storedDocument] interface {
	References() domain.SlugReferences
	FromEditor(ids domain.IDs) (D, []domain.Problem)
}

func convert[E editorDocument[D], D storedDocument](
	ctx context.Context, c *SubmitRevision, entityType domain.EntityType, editor E,
) (domain.EntityType, storedDocument, []domain.Problem, error) {
	ids, err := c.Revisions.ResolveReferences(ctx, editor.References())
	if err != nil {
		return "", nil, nil, err
	}
	doc, problems := editor.FromEditor(ids)

	return entityType, doc, problems, nil
}

// sourceProblems returns the sourced-field problems of a piece or person document, a piece's compared with its
// base's document.
func sourceProblems(doc storedDocument, base *datasources.RevisionBase, summary string) []domain.Problem {
	switch d := doc.(type) {
	case domain.PieceDocument:
		var old *domain.PieceDocument
		if base != nil {
			if o, ok := base.Document.(domain.PieceDocument); ok {
				old = &o
			}
		}
		return domain.ValidatePieceSources(old, d, summary)
	case domain.PersonDocument:
		return domain.ValidatePersonSources(d)
	default:
		return nil
	}
}

// checkDuplicates returns a *DuplicateError if a new entity's document matches public entities of its type
// (terms: of its facet) and the matches are not acknowledged, and a *datasources.ValidationError if a new person
// matches a person who prefers no page, which no acknowledgement passes.
func (c *SubmitRevision) checkDuplicates(
	ctx context.Context, entityType domain.EntityType, doc storedDocument, acknowledged bool,
) error {
	var facet domain.Facet
	if term, ok := doc.(domain.TermDocument); ok {
		facet = term.Facet
	}
	candidates, err := c.Revisions.DuplicateCandidates(ctx, entityType, facet)
	if err != nil {
		return err
	}
	matches := domain.MatchDuplicates(doc.DuplicateKeys(), candidates)
	for _, m := range matches {
		if m.PrefersNoPage {
			return &datasources.ValidationError{Problems: []domain.Problem{
				{Path: "name", Code: domain.ProblemPrefersNoPage},
			}}
		}
	}
	if len(matches) > 0 && !acknowledged {
		return &DuplicateError{Matches: matches}
	}

	return nil
}
