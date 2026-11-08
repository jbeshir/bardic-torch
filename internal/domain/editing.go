package domain

import "time"

// Actor is who acts on revisions: a user, with their effective role. A moderator acting with an API token
// acts as a contributor.
type Actor struct {
	UserID string
	Role   Role
}

// EntityKey addresses an entity as the API does: a piece, person, term or event by slug, a collection by ID.
type EntityKey struct {
	Type EntityType
	// Slug addresses an entity of every type but collection; a placeholder slug a pending new entity.
	Slug string
	// CollectionID addresses a collection.
	CollectionID int64
}

// AnyDocument is a document of one of the Document types, for values whose entity type is known only at run time.
type AnyDocument interface {
	// EntityType is the type of entity the document is of.
	EntityType() EntityType
}

// EntityType is EntityTypePiece.
func (PieceDocument) EntityType() EntityType { return EntityTypePiece }

// EntityType is EntityTypePerson.
func (PersonDocument) EntityType() EntityType { return EntityTypePerson }

// EntityType is EntityTypeTerm.
func (TermDocument) EntityType() EntityType { return EntityTypeTerm }

// EntityType is EntityTypeCollection.
func (CollectionDocument) EntityType() EntityType { return EntityTypeCollection }

// EntityType is EntityTypeEvent.
func (EventDocument) EntityType() EntityType { return EntityTypeEvent }

// Snapshot is a view of one of the document types: PieceSnapshot, PersonSnapshot, TermSnapshot, CollectionSnapshot
// or EventSnapshot.
type Snapshot interface {
	isSnapshot()
}

func (PieceSnapshot) isSnapshot()      {}
func (PersonSnapshot) isSnapshot()     {}
func (TermSnapshot) isSnapshot()       {}
func (CollectionSnapshot) isSnapshot() {}
func (EventSnapshot) isSnapshot()      {}

// RevisionEntity is the entity a revision or draft edits, as its author or a moderator addresses it: by slug, or a
// collection by ID. ID is the internal ID, which is exposed only for collections. IsNew is set for a pending new
// entity, addressed by its placeholder slug.
type RevisionEntity struct {
	Type  EntityType
	ID    int64
	Slug  string
	IsNew bool
}

// StartingPoint is what an editable document starts from.
type StartingPoint string

const (
	// StartingPointCurrent is the entity's current document.
	StartingPointCurrent StartingPoint = "current"
	// StartingPointPending is the caller's pending revision of the entity, as its author sees it.
	StartingPointPending StartingPoint = "pending"
)

// LinkState is the moderator state of a link a document holds, by the document's URL: the existing link's ID,
// hidden state and liveness, or no ID for a URL no link has yet.
type LinkState struct {
	URL      string
	LinkID   *int64
	IsHidden bool
	Liveness *Liveness
}

// DraftRef is the caller's draft of an entity.
type DraftRef struct {
	ID        int64
	UpdatedAt time.Time
}

// PendingRef is the caller's pending revision of an entity.
type PendingRef struct {
	ID          int64
	SubmittedAt time.Time
}

// Editable is the document a caller starts an edit of an entity from: the projection of the current document
// without what the caller may not see, or of the author document of their pending revision of it, with its base.
type Editable struct {
	Entity RevisionEntity
	// BaseRevisionID is the base a draft made from Document has: the current revision, or the submitted base of the
	// pending revision it starts from, or nil for a pending new entity.
	BaseRevisionID *int64
	StartingPoint  StartingPoint
	Document       AnyDocument
	// References are the entities Document references, as the caller sees them.
	References []EditorReference
	// LinkStates are the states of Document's links, for an interactive moderator only.
	LinkStates []LinkState
	// Draft and Pending are the caller's draft and pending revision of the entity, if any.
	Draft   *DraftRef
	Pending *PendingRef
}

// Draft is a draft as its author saved it.
type Draft struct {
	ID         int64
	EntityType EntityType
	// Entity is nil for a draft of a new entity.
	Entity         *RevisionEntity
	BaseRevisionID *int64
	Document       AnyDocument
	Summary        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// References are the entities Document references, as its author sees them.
	References []EditorReference
	// Preview is the draft as it would show once approved, as its author's view of it after submit would, or nil
	// if the document has no view.
	Preview Snapshot
}

// AuthorRef is how a revision's author is shown to them: their display name and role.
type AuthorRef struct {
	DisplayName string
	Role        Role
}

// MyRevisionSummary is one of a caller's own revisions, drafts included, as their list of revisions shows it.
type MyRevisionSummary struct {
	ID         int64
	Status     RevisionStatus
	EntityType EntityType
	// Entity is nil for a new entity's draft, or once a new entity's placeholder is deleted.
	Entity *RevisionEntity
	// Title is the title or name the author's document gives the entity.
	Title                   string
	Summary                 string
	Source                  RevisionSource
	Author                  AuthorRef
	CreatedAt               time.Time
	SubmittedAt             *time.Time
	ReviewedAt              *time.Time
	ReviewNote              *string
	BaseRevisionID          *int64
	SubmittedBaseRevisionID *int64
}

// MyRevision is one of a caller's own revisions with its views: the author's view of what they submitted, and
// the same view of the base they edited from; for an approved revision, the public views.
type MyRevision struct {
	MyRevisionSummary
	Snapshot Snapshot
	// BaseSnapshot is nil for a new entity.
	BaseSnapshot Snapshot
	// PendingReferences are the caller's pending new terms and persons the revision references.
	PendingReferences []EditorReference
}

// MyRevisionList is one page of a caller's revisions, newest first, with how many there are in all.
type MyRevisionList struct {
	Revisions []MyRevisionSummary
	Total     int64
}
