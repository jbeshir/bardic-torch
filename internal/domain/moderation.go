package domain

import "time"

// ModerationAuthor is a user as moderator views show them (D8): with their user ID, so that a moderator can block
// them, their role and whether they are blocked. User IDs appear only in moderator views.
type ModerationAuthor struct {
	ID          string
	DisplayName string
	Role        Role
	Blocked     bool
}

// QueueItem is a pending revision as the moderation queue lists it.
type QueueItem struct {
	ID         int64
	EntityType EntityType
	// Entity is nil once a new entity's placeholder is gone.
	Entity *RevisionEntity
	// Title is the title or name the revision's document gives the entity.
	Title       string
	Summary     string
	Author      ModerationAuthor
	SubmittedAt time.Time
	// Stale is set when the revision's base is not its entity's current revision.
	Stale bool
	// IsNew is set when the entity has no current revision: the revision creates it.
	IsNew bool
	// Flags are the codes of the flags stored on the revision, each once.
	Flags []FlagCode
}

// ModerationQueue is one page of the pending revisions, oldest first, with how many there are in all.
type ModerationQueue struct {
	Items []QueueItem
	Total int64
}

// RebaseStatus is what approving a pending revision would do with its document (D9).
type RebaseStatus string

// The rebase statuses.
const (
	// RebaseStatusNotNeeded is a revision that is not stale, or cannot be merged: approving it takes it as it is.
	RebaseStatusNotNeeded RebaseStatus = "not_needed"
	// RebaseStatusClean is a stale revision that merges with the current document without conflicts: approving it
	// rebases it automatically.
	RebaseStatusClean RebaseStatus = "clean"
	// RebaseStatusConflicts is a stale revision whose merge conflicts: the moderator must rebase it first.
	RebaseStatusConflicts RebaseStatus = "conflicts"
)

// RebasePreview is the merge of a stale pending revision with its entity's current document, as approval would
// make it: the merged document if it is clean, else the conflicts.
type RebasePreview struct {
	Status         RebaseStatus
	MergedDocument AnyDocument
	Conflicts      []RebaseConflict
}

// ModerationPendingReference is a pending new term or person of a revision's author that the revision's document
// references, which approving the revision approves alongside it unless it carries a flag of its own (D15).
type ModerationPendingReference struct {
	RevisionID int64
	EntityType EntityType
	Entity     RevisionEntity
	Document   AnyDocument
	// Flags are the flags stored on the pending reference's revision.
	Flags []Flag
}

// ModerationRevision is a revision as a moderator sees it (D8): every document in full, hidden aliases and hidden
// links included, with what approving it involves. Its documents may hold hidden data, so it is served to
// interactive moderators only.
type ModerationRevision struct {
	ID         int64
	Status     RevisionStatus
	EntityType EntityType
	// Entity is nil once a new entity's placeholder is gone.
	Entity *RevisionEntity
	Source RevisionSource
	// Author is nil for an imported revision, whose wiki contributors are Contributors.
	Author       *ModerationAuthor
	Contributors []string
	// Reviewer is the moderator who approved or rejected the revision, if any.
	Reviewer *ModerationAuthor
	// RebasedBy is the moderator who last rebased the revision, if any (with RebasedAt).
	RebasedBy   *ModerationAuthor
	Summary     string
	ReviewNote  *string
	CreatedAt   time.Time
	SubmittedAt *time.Time
	ReviewedAt  *time.Time
	RebasedAt   *time.Time
	// BaseRevisionID is the base of Document; SubmittedBaseRevisionID the base its author edited from, which a
	// rebase never moves.
	BaseRevisionID          *int64
	SubmittedBaseRevisionID *int64
	// CurrentRevisionID is the entity's current revision, nil for a new entity.
	CurrentRevisionID *int64
	// Stale is set when the revision is pending and its base is not the entity's current revision.
	Stale bool
	// Document is the revision's full document; SubmittedDocument what its author submitted, nil for an imported
	// revision.
	Document          AnyDocument
	SubmittedDocument AnyDocument
	// BaseDocument and CurrentDocument are the documents of the base and current revisions, nil for none.
	BaseDocument    AnyDocument
	CurrentDocument AnyDocument
	// References are the entities the view's documents reference, as a moderator sees them.
	References []EditorReference
	// LinkStates are the states of Document's links.
	LinkStates []LinkState
	Rebase     RebasePreview
	// PendingReferences are the pending new terms and persons of the revision's author that Document references,
	// each after those its own document references.
	PendingReferences []ModerationPendingReference
	// Flags are the flags approving the revision gates on, for a pending revision; else the flags stored on it.
	Flags []Flag
	// Duplicates are the entities a pending new entity may duplicate, hidden aliases and other pending new entities
	// compared (D13); none otherwise.
	Duplicates []DuplicateMatch
	// ChangedPaths are what Document changes from BaseDocument (from the default document for a new entity).
	ChangedPaths []FieldPath
}

// ModerationLinkAttachment is an entity a link is attached to, as a moderator sees it: by slug, with its title or
// name, whether it is public, and the link's roles on it in attachment order.
type ModerationLinkAttachment struct {
	EntityType EntityType
	Slug       string
	Title      string
	Public     bool
	Roles      []LinkRole
}

// ModerationLink is a link as a moderator sees it: hidden or not, with every entity it is attached to and how many
// reports of it are open.
type ModerationLink struct {
	LinkID          int64
	URL             string
	Title           *string
	Description     *string
	Platform        *string
	PlaceType       *LinkPlaceType
	Collection      *CollectionRef
	Locator         *string
	Liveness        Liveness
	IsHidden        bool
	Attachments     []ModerationLinkAttachment
	OpenReportCount int64
}
