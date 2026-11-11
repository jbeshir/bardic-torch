package datasources

import (
	"context"
	"errors"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// MaxPendingRevisions is how many pending revisions a contributor may have at once.
const MaxPendingRevisions = 30

// ErrPendingLimit is returned, possibly wrapped, by a contributor's submission while they have
// MaxPendingRevisions pending revisions.
var ErrPendingLimit = errors.New("pending revision limit reached")

// ErrStale is returned, possibly wrapped, by approving a stale revision without acknowledging it.
var ErrStale = errors.New("revision is stale")

// ValidationError is returned, possibly wrapped, by approving a revision whose document no longer validates.
type ValidationError struct {
	Problems []domain.Problem
}

func (e *ValidationError) Error() string {
	return "document does not validate"
}

// NewRevision is a revision to submit.
type NewRevision struct {
	// BaseRevisionID is the approved revision of the entity the revision edits, or nil for a new entity.
	BaseRevisionID *int64
	AuthorUserID   string
	Summary        string
	// Document is the stored-format document, validated: a domain.PieceDocument, PersonDocument, TermDocument,
	// CollectionDocument or EventDocument, whose type is the revision's entity type.
	Document any
	// ByModerator submits and approves the revision at once, refusing a stale one unless AcknowledgeStale.
	ByModerator      bool
	AcknowledgeStale bool
	At               time.Time
}

// RevisionBase is the approved revision a submission edits.
type RevisionBase struct {
	EntityID int64
	// Document is the base's stored-format document, of the type of NewRevision.Document.
	Document any
	// ShowsUnlistedPerson is domain.ShowsUnlistedPerson of the document: only a moderator may edit it.
	ShowsUnlistedPerson bool
}

// Editable is what an editor starts from for an existing entity.
type Editable struct {
	// BaseRevisionID is the revision to submit an edit against.
	BaseRevisionID int64
	// PendingRevisionID is the caller's own pending revision of the entity, whose document Document is, if they
	// have one.
	PendingRevisionID *int64
	// Document is the editor-format document (domain.EditorPieceDocument …) without hidden links.
	Document any
	// ShowsUnlistedPerson is domain.ShowsUnlistedPerson of the document: only a moderator may edit it.
	ShowsUnlistedPerson bool
}

// Revisions submits and moderates revisions. Each change is one transaction; approval writes the document into
// the catalogue.
type Revisions interface {
	// SubmitRevision stores rev and returns its ID. A contributor's earlier pending revision of the same entity is
	// withdrawn, and ErrPendingLimit returned if they still have MaxPendingRevisions pending. A moderator's
	// revision is approved at once, as ApproveRevision. A base that is not an approved revision of an entity of the
	// document's type is ErrNotFound.
	SubmitRevision(ctx context.Context, rev NewRevision) (int64, error)
	// GetRevisionBase returns the approved revision baseRevisionID of an entity of entityType, or ErrNotFound.
	GetRevisionBase(ctx context.Context, entityType domain.EntityType, baseRevisionID int64) (RevisionBase, error)
	// WithdrawRevision withdraws the pending revision id of userID: ErrNotFound if it is not theirs,
	// domain.ErrRevisionNotPending if it is not pending.
	WithdrawRevision(ctx context.Context, id int64, userID string) error
	// RejectRevision rejects the pending revision id with note (domain.ErrRevisionNotPending, ErrNotFound).
	RejectRevision(ctx context.Context, id int64, reviewerUserID, note string, at time.Time) error
	// ApproveRevision approves the pending revision id and writes its document into the catalogue: ErrStale for a
	// stale revision unless acknowledgeStale, a *ValidationError if the document no longer validates,
	// domain.ErrRevisionNotPending, ErrNotFound.
	ApproveRevision(ctx context.Context, id int64, reviewerUserID string, acknowledgeStale bool, at time.Time) error

	// GetEditable returns the editable document of the public entity of entityType with key (a slug, or for a
	// collection its ID): userID's own pending revision of it if they have one, and otherwise its current revision.
	// ErrNotFound if there is no such public entity.
	GetEditable(ctx context.Context, entityType domain.EntityType, key, userID string) (Editable, error)
	// ResolveReferences returns the IDs of the public entities with the slugs refs holds; slugs that resolve to
	// none are left out. Persons who prefer no page are included only if includeUnlisted.
	ResolveReferences(ctx context.Context, refs domain.SlugReferences, includeUnlisted bool) (domain.IDs, error)
	// ValidationContext returns the context validating a document of the entity entityID (0 for a new one) needs:
	// every term and every collection's parent.
	ValidationContext(ctx context.Context, entityID int64) (domain.ValidationContext, error)
	// DuplicateCandidates returns the public entities of entityType, for a term only those of facet, with the
	// duplicate keys of their current documents.
	DuplicateCandidates(
		ctx context.Context, entityType domain.EntityType, facet domain.Facet,
	) ([]domain.DuplicateCandidate, error)

	// ListMyRevisions lists one page of userID's revisions, of status if not nil, newest first.
	ListMyRevisions(
		ctx context.Context, userID string, status *domain.RevisionStatus, page domain.Page,
	) (domain.RevisionList, error)
	// GetMyRevision returns userID's revision id, or ErrNotFound if it is not theirs.
	GetMyRevision(ctx context.Context, userID string, id int64) (domain.RevisionView, error)
	// ListModerationQueue lists one page of the pending revisions, oldest first, with their authors.
	ListModerationQueue(ctx context.Context, page domain.Page) (domain.RevisionList, error)
	// GetModerationRevision returns the moderator view of revision id, or ErrNotFound.
	GetModerationRevision(ctx context.Context, id int64) (domain.RevisionView, error)
}
