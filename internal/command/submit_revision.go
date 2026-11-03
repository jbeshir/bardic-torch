package command

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[SubmitRevisionRequest, SubmittedRevision] = (*SubmitRevision)(nil)

// SubmitRevisionRequest is Principal's submission of their draft DraftID. AcknowledgeDuplicates confirms that a new
// entity is not a duplicate of the entities its draft's names match. AcknowledgedFlags are an interactive
// moderator's acknowledgements of their submission's flags (D3, D19); a contributor's are ignored, since
// contributors never see flags.
type SubmitRevisionRequest struct {
	Principal             domain.Principal
	DraftID               int64
	AcknowledgeDuplicates bool
	AcknowledgedFlags     []domain.FlagCode
}

// PossibleDuplicateError is a contributor's submission of a new entity whose names match Duplicates, which they
// have not acknowledged (D13).
type PossibleDuplicateError struct {
	Duplicates []domain.DuplicateMatch
}

func (e *PossibleDuplicateError) Error() string {
	return fmt.Sprintf("the new entity may duplicate %d others", len(e.Duplicates))
}

// SubmitRevision submits a draft (D2): a contributor's (or any API token's) becomes pending, an interactive
// moderator's is approved at once (D3).
type SubmitRevision struct {
	Drafts     datasources.DraftGetter
	Duplicates datasources.DuplicateFinder
	Submitter  datasources.RevisionSubmitter
	Revisions  datasources.MyRevisionGetter
	Now        func() time.Time
}

// Execute submits the principal's draft req.DraftID and returns the revision it leaves, as they see it; a draft
// already submitted is a replay. A draft of a new entity whose names match a person who prefers no page fails with
// domain.ErrDuplicateBlocked, and one matching other entities fails for a contributor with a
// *PossibleDuplicateError unless req.AcknowledgeDuplicates. Otherwise it fails with the errors of
// datasources.RevisionSubmitter.
func (c *SubmitRevision) Execute(ctx context.Context, req SubmitRevisionRequest) (SubmittedRevision, error) {
	actor := req.Principal.Actor()
	duplicates, err := c.duplicates(ctx, actor, req)
	if err != nil {
		return SubmittedRevision{}, err
	}

	result, err := c.Submitter.SubmitDraft(ctx, datasources.SubmitRequest{
		Actor: actor, DraftID: req.DraftID, Duplicates: duplicates,
		AcknowledgedFlags: moderatorAcknowledgements(actor, req.AcknowledgedFlags), Now: c.Now(),
	})
	if err != nil {
		return SubmittedRevision{}, fmt.Errorf("submitting draft: %w", err)
	}

	return submitted(ctx, c.Revisions, actor, result)
}

// duplicates returns the entities the draft's new entity may duplicate, read before the submission's transaction
// (D13), and refuses the submission as Execute describes. A draft that is no longer a draft has none: its
// submission is a replay, or refused.
func (c *SubmitRevision) duplicates(
	ctx context.Context, actor domain.Actor, req SubmitRevisionRequest,
) ([]domain.DuplicateMatch, error) {
	draft, err := c.Drafts.GetDraft(ctx, actor, req.DraftID)
	if errors.Is(err, domain.ErrNotDraft) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting draft: %w", err)
	}

	result, err := draftResult(ctx, c.Duplicates, actor, draft)
	if err != nil {
		return nil, err
	}
	matches := result.Duplicates
	if slices.ContainsFunc(matches, func(m domain.DuplicateMatch) bool { return m.Blocked }) {
		return nil, domain.ErrDuplicateBlocked
	}
	if len(matches) > 0 && actor.Role != domain.RoleModerator && !req.AcknowledgeDuplicates {
		return nil, &PossibleDuplicateError{Duplicates: matches}
	}

	return matches, nil
}
