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
// moderator's acknowledgements of their submission's flags; a contributor's are ignored, since
// contributors never see flags.
type SubmitRevisionRequest struct {
	Principal             domain.Principal
	DraftID               int64
	AcknowledgeDuplicates bool
	AcknowledgedFlags     []domain.FlagCode
}

// PossibleDuplicateError is a contributor's submission of a new entity whose names match Duplicates, which they
// have not acknowledged.
type PossibleDuplicateError struct {
	Duplicates []domain.DuplicateMatch
}

func (e *PossibleDuplicateError) Error() string {
	return fmt.Sprintf("the new entity may duplicate %d others", len(e.Duplicates))
}

// SubmitRevision submits a draft: a contributor's (or any API token's) becomes pending, an interactive
// moderator's is approved at once.
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
// *PossibleDuplicateError unless req.AcknowledgeDuplicates. The duplicates are found outside the submission's
// transaction, which refuses a draft changed since (datasources.ErrDocumentChanged): the check is then made again,
// up to maxDuplicateRechecks times, and after that the submission fails with datasources.ErrUnavailable. An
// interactive moderator's submission is approved at once, so a term whose name another term of its facet has
// taken fails with ErrDuplicateName. Otherwise it fails with the errors of datasources.RevisionSubmitter.
func (c *SubmitRevision) Execute(ctx context.Context, req SubmitRevisionRequest) (SubmittedRevision, error) {
	actor := req.Principal.Actor()
	var result datasources.SubmitResult
	err := withDuplicateRechecks(func() error {
		duplicates, err := c.duplicates(ctx, actor, req)
		if err != nil {
			return err
		}
		result, err = c.Submitter.SubmitDraft(ctx, datasources.SubmitRequest{
			Actor: actor, DraftID: req.DraftID, Duplicates: duplicates,
			AcknowledgedFlags: moderatorAcknowledgements(actor, req.AcknowledgedFlags), Now: c.Now(),
		})
		if actor.Role == domain.RoleModerator && isNameDuplicate(err) {
			return ErrDuplicateName
		}
		if err != nil {
			return fmt.Errorf("submitting draft: %w", err)
		}
		return nil
	})
	if err != nil {
		return SubmittedRevision{}, err
	}

	return submitted(ctx, c.Revisions, actor, result)
}

// maxDuplicateRechecks is how many times a write refused with datasources.ErrDocumentChanged is tried again with
// a new duplicate check.
const maxDuplicateRechecks = 2

// withDuplicateRechecks runs write, which checks a document for duplicates and then writes it, and runs it again
// while the write is refused with datasources.ErrDocumentChanged, up to maxDuplicateRechecks more times. After that
// the error is also datasources.ErrUnavailable: the document kept changing, and trying again later may work.
func withDuplicateRechecks(write func() error) error {
	for attempt := 0; ; attempt++ {
		err := write()
		if !errors.Is(err, datasources.ErrDocumentChanged) {
			return err
		}
		if attempt == maxDuplicateRechecks {
			return fmt.Errorf("%w: %w", datasources.ErrUnavailable, err)
		}
	}
}

// duplicates returns the duplicate check of the draft's new entity, made before the submission's transaction
// (docs/PLAN.md §5.1 "Duplicate check"), and refuses the submission as Execute describes. A draft that is no
// longer a draft has none: its submission is a replay, or refused.
func (c *SubmitRevision) duplicates(
	ctx context.Context, actor domain.Actor, req SubmitRevisionRequest,
) (datasources.DuplicateCheck, error) {
	draft, err := c.Drafts.GetDraft(ctx, actor, req.DraftID)
	if errors.Is(err, domain.ErrNotDraft) {
		return datasources.DuplicateCheck{}, nil
	}
	if err != nil {
		return datasources.DuplicateCheck{}, fmt.Errorf("getting draft: %w", err)
	}
	if draft.Entity != nil {
		return datasources.DuplicateCheck{}, nil
	}

	result, err := draftResult(ctx, c.Duplicates, actor, draft)
	if err != nil {
		return datasources.DuplicateCheck{}, err
	}
	matches := result.Duplicates
	if slices.ContainsFunc(matches, func(m domain.DuplicateMatch) bool { return m.Blocked }) {
		return datasources.DuplicateCheck{}, domain.ErrDuplicateBlocked
	}
	if len(matches) > 0 && actor.Role != domain.RoleModerator && !req.AcknowledgeDuplicates {
		return datasources.DuplicateCheck{}, &PossibleDuplicateError{Duplicates: matches}
	}

	return datasources.DuplicateCheck{Candidate: domain.NewDuplicateCandidate(draft.Document), Matches: matches}, nil
}
