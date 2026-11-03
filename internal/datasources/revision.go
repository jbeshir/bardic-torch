package datasources

import (
	"context"
	"errors"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The errors the revision writes refuse with, besides ErrNotFound (an entity, draft or revision the caller may
// not see or act on, which is not revealed to exist), ErrUnavailable and the domain's transition and planning
// errors (domain.ErrNotDraft, domain.ErrNotPending, *domain.DocumentError, ...).
var (
	// ErrDraftLimitReached is a new draft of a user holding domain.MaxDrafts drafts.
	ErrDraftLimitReached = errors.New("draft limit reached")
	// ErrPendingLimitReached is a submission of a user with domain.MaxOpenPendingRevisions pending revisions.
	ErrPendingLimitReached = errors.New("pending revision limit reached")
	// ErrEntityGone is a write whose entity row, or an entity it references, no longer exists: a pending new
	// entity's placeholder was deleted.
	ErrEntityGone = errors.New("entity gone")
)

// EditorRefResolver converts references between the editor format and documents (D5) for a caller: a piece,
// person, term, collection or event they may reference is public, or one of their own pending new terms or
// persons (by placeholder slug); an interactive moderator may reference every entity.
type EditorRefResolver interface {
	// ResolveEditorRefs returns the IDs of the references in refs the actor may make. A reference that does not
	// exist and one the actor may not make are both absent, so the answer reveals nothing.
	ResolveEditorRefs(ctx context.Context, actor domain.Actor, refs domain.EditorRefs) (domain.RefMap, error)
	// EditorReferences returns the entities refs references that the actor may see, each once, with its slug and
	// label (domain.EditorReference); domain.NewSlugMap gives the slugs a document's editor format needs.
	EditorReferences(
		ctx context.Context, actor domain.Actor, refs domain.SnapshotReferences,
	) ([]domain.EditorReference, error)
}

// EditableDocumentGetter returns the document an actor starts editing an entity from (D5), returning ErrNotFound
// if the entity is neither public nor the actor's own pending new entity.
type EditableDocumentGetter interface {
	GetEditable(ctx context.Context, actor domain.Actor, key domain.EntityKey) (domain.Editable, error)
}

// SaveDraftRequest is a draft to save: of the entity Entity (nil for a new entity of EntityType), on the base
// BaseRevisionID (nil for the entity's current revision), holding Document as its author sent it.
type SaveDraftRequest struct {
	Actor          domain.Actor
	EntityType     domain.EntityType
	Entity         *domain.EntityKey
	BaseRevisionID *int64
	Document       domain.AnyDocument
	Summary        string
	Now            time.Time
}

// SavedDraft is a saved draft, and whether saving created it rather than replacing the author's draft of the
// entity.
type SavedDraft struct {
	Draft   domain.Draft
	Created bool
}

// DraftSaver saves a draft (D1). An author holds one draft per existing entity: saving another replaces its
// document and summary, keeping its ID and base. It returns ErrNotFound if the entity is not one the actor may
// edit, a *domain.DocumentError (unknown_reference at /base_revision_id) if the base is not an approved revision of
// the entity, and ErrDraftLimitReached when a new draft would exceed domain.MaxDrafts.
type DraftSaver interface {
	SaveDraft(ctx context.Context, req SaveDraftRequest) (SavedDraft, error)
}

// UpdateDraftRequest is a new document and summary for the draft ID.
type UpdateDraftRequest struct {
	Actor    domain.Actor
	ID       int64
	Document domain.AnyDocument
	Summary  string
	Now      time.Time
}

// DraftUpdater replaces a draft's document and summary, keeping its base. It returns ErrNotFound unless the draft
// is the actor's, and domain.ErrNotDraft if it is no longer a draft.
type DraftUpdater interface {
	UpdateDraft(ctx context.Context, req UpdateDraftRequest) (domain.Draft, error)
}

// DraftGetter returns one of the actor's drafts, returning ErrNotFound unless it is theirs and domain.ErrNotDraft
// if it is no longer a draft.
type DraftGetter interface {
	GetDraft(ctx context.Context, actor domain.Actor, id int64) (domain.Draft, error)
}

// DraftDiscarder deletes one of the actor's drafts, returning ErrNotFound unless it is theirs and
// domain.ErrNotDraft if it is no longer a draft.
type DraftDiscarder interface {
	DiscardDraft(ctx context.Context, actor domain.Actor, id int64) error
}

// SubmitRequest is a submission of the draft DraftID. Duplicates are the entities a draft of a new entity may
// duplicate, as DuplicateFinder found them (D13): any match is stored as a possible_duplicate flag, and for an
// interactive moderator, whose submission is approved at once, they are the approval's (ApproveRequest).
// AcknowledgedFlags are an interactive moderator's acknowledgements of their submission's flags (D3).
type SubmitRequest struct {
	Actor             domain.Actor
	DraftID           int64
	Duplicates        []domain.DuplicateMatch
	AcknowledgedFlags []domain.FlagCode
	Now               time.Time
}

// SubmitResult is the revision a submission leaves: Replayed is set when the draft had already been submitted, and
// the revision is returned as it is (D2).
type SubmitResult struct {
	RevisionID int64
	Status     domain.RevisionStatus
	Replayed   bool
}

// RevisionSubmitter submits a draft (D2, D5): it restores what was withheld from its author onto the draft's own
// base, never merging it with the current document, validates it in two phases (domain.PlanSubmit) and stores it
// as pending, superseding the author's pending revision of the entity. A new entity gets its placeholder row
// (D14). It returns ErrNotFound unless the draft is the actor's, domain.ErrNotDraft for a rejected or withdrawn
// revision, a *domain.DocumentError, ErrPendingLimitReached and ErrEntityGone. An interactive moderator's
// submission is approved at once, inline (D3): a stale one is merged, and the approval's errors are its errors
// (RevisionApprover), the draft staying a draft.
type RevisionSubmitter interface {
	SubmitDraft(ctx context.Context, req SubmitRequest) (SubmitResult, error)
}

// RevisionWithdrawer withdraws one of the actor's pending revisions, deleting its new entity's placeholder if no
// other pending revision has it (D14). It returns ErrNotFound unless the revision is the actor's and
// domain.ErrNotPending if it is not pending.
type RevisionWithdrawer interface {
	WithdrawRevision(ctx context.Context, actor domain.Actor, id int64) error
}

// MyRevisionQuery filters a user's own revisions. Every filter that is set must match.
type MyRevisionQuery struct {
	Status     *domain.RevisionStatus
	EntityType *domain.EntityType
	Page       domain.Page
}

// MyRevisionLister lists one page of the actor's own revisions, drafts included, newest first by when they were
// submitted, else last saved, else created.
type MyRevisionLister interface {
	ListMyRevisions(ctx context.Context, actor domain.Actor, query MyRevisionQuery) (domain.MyRevisionList, error)
}

// MyRevisionGetter returns one of the actor's own revisions with its views (D6), returning ErrNotFound unless it
// is theirs.
type MyRevisionGetter interface {
	GetMyRevision(ctx context.Context, actor domain.Actor, id int64) (domain.MyRevision, error)
}

// PendingVersionGetter returns the actor's pending revision of an entity with its views (D6), returning
// ErrNotFound if they have none, or the entity is neither public nor their own pending new entity.
type PendingVersionGetter interface {
	GetPendingVersion(ctx context.Context, actor domain.Actor, key domain.EntityKey) (domain.MyRevision, error)
}

// DuplicateFinder returns the existing entities a new one may duplicate (D13), comparing the candidate's names with
// every public entity of its type: pieces' titles and aliases, persons' names and public aliases (persons who
// prefer no page included, as blocked matches), the terms of its facet, collections' names and URLs, events'
// names. With includeHidden, for moderator views only, it also compares hidden aliases and the pending new
// entities of the type.
type DuplicateFinder interface {
	FindDuplicates(
		ctx context.Context, candidate domain.DuplicateCandidate, includeHidden bool,
	) ([]domain.DuplicateMatch, error)
}

// HiddenAliasLister lists the names of every hidden alias, which moderator flags look for in revisions' text
// (D19).
type HiddenAliasLister interface {
	ListHiddenAliasNames(ctx context.Context) ([]string, error)
}
