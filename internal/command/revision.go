package command

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The revision commands act for a principal with their effective role (domain.Principal.Actor): a request made with
// an API token has contributor powers only, whatever its user's role (D25).

// EntityRequest is a request by Principal about the entity Key.
type EntityRequest struct {
	Principal domain.Principal
	Key       domain.EntityKey
}

// RevisionRequest is a request by Principal about their draft or revision ID.
type RevisionRequest struct {
	Principal domain.Principal
	ID        int64
}

// DraftResult is a draft with the entities its document may duplicate: for a draft of a new entity, the matches
// of domain.FindDuplicates (D13); for a draft of an existing entity, none.
type DraftResult struct {
	Draft      domain.Draft
	Duplicates []domain.DuplicateMatch
}

// SubmittedRevision is the revision a submission or a revert leaves, as its author sees it. Replayed is set when
// the request had already been carried out, and the revision is returned as it is (D2, D10).
type SubmittedRevision struct {
	Revision domain.MyRevision
	Replayed bool
}

// toDocument converts doc from the editor format with the references actor may make. It fails with a
// *domain.DocumentError for each reference it cannot resolve, whether it does not exist or the actor may not make
// it, and for the problems even a draft may not have (domain.DraftProblems, with summary).
func toDocument(
	ctx context.Context, resolver datasources.EditorRefResolver, actor domain.Actor, doc domain.AnyEditorDocument,
	summary string,
) (domain.AnyDocument, error) {
	refs, err := resolver.ResolveEditorRefs(ctx, actor, doc.References())
	if err != nil {
		return nil, fmt.Errorf("resolving references: %w", err)
	}
	converted, problems, err := domain.FromEditor(doc, refs)
	if err != nil {
		return nil, err
	}
	if problems = append(problems, domain.DraftProblems(converted, summary)...); len(problems) > 0 {
		return nil, &domain.DocumentError{Problems: problems}
	}

	return converted, nil
}

// draftResult returns draft with the entities it may duplicate: for a draft of a new entity, the public entities
// its names match, and for an interactive moderator also hidden aliases and pending new entities (D13); none for
// a draft of an existing entity.
func draftResult(
	ctx context.Context, finder datasources.DuplicateFinder, actor domain.Actor, draft domain.Draft,
) (DraftResult, error) {
	if draft.Entity != nil {
		return DraftResult{Draft: draft, Duplicates: []domain.DuplicateMatch{}}, nil
	}
	matches, err := finder.FindDuplicates(ctx, domain.NewDuplicateCandidate(draft.Document),
		actor.Role == domain.RoleModerator)
	if err != nil {
		return DraftResult{}, fmt.Errorf("finding duplicates: %w", err)
	}

	return DraftResult{Draft: draft, Duplicates: matches}, nil
}

// moderatorAcknowledgements returns the flags an actor acknowledges: an interactive moderator's acknowledged,
// and a contributor's none, since contributors never see flags.
func moderatorAcknowledgements(actor domain.Actor, acknowledged []domain.FlagCode) []domain.FlagCode {
	if actor.Role != domain.RoleModerator {
		return nil
	}

	return acknowledged
}

// submitted returns the revision result left, as its author sees it.
func submitted(
	ctx context.Context, getter datasources.MyRevisionGetter, actor domain.Actor, result datasources.SubmitResult,
) (SubmittedRevision, error) {
	revision, err := getter.GetMyRevision(ctx, actor, result.RevisionID)
	if err != nil {
		return SubmittedRevision{}, fmt.Errorf("getting revision [%d]: %w", result.RevisionID, err)
	}

	return SubmittedRevision{Revision: revision, Replayed: result.Replayed}, nil
}
