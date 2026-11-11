package domain

import (
	"errors"
	"fmt"
	"time"
)

// RevisionSummary is a public revision as an entity's history lists it. Only approved revisions of public
// entities are public.
type RevisionSummary struct {
	ID      int64
	Source  RevisionSource
	Summary string
	// Author is the revision's author, or nil for a revision with none, such as an import.
	Author *RevisionAuthor
	// Contributors are the wiki users who wrote an imported revision, in order.
	Contributors []string
	CreatedAt    time.Time
}

// RevisionAuthor is the public identity of a revision's author: their display name, never their user ID.
type RevisionAuthor struct {
	DisplayName string
}

// RevisionHistory is one page of a public entity's approved revisions, newest first, with how many there are in
// all.
type RevisionHistory struct {
	Revisions []RevisionSummary
	Total     int64
}

// Revision is a public revision with its snapshot views: one of PieceRevision, PersonRevision, TermRevision,
// CollectionRevision and EventRevision.
type Revision interface {
	isRevision()
}

// EntityRevision is a public revision of an entity, shown as Entity, whose snapshot view is of type S.
type EntityRevision[E, S any] struct {
	RevisionSummary
	Entity   E
	Snapshot S
	// Base is the revision this one was based on, if that is an approved revision of the same entity, and nil
	// otherwise.
	Base *RevisionBase[S]
}

// RevisionBase is the revision another was based on, with its snapshot view of type S.
type RevisionBase[S any] struct {
	ID       int64
	Snapshot S
}

func (EntityRevision[E, S]) isRevision() {}

// The revisions of each type of entity.
type (
	PieceRevision      = EntityRevision[PieceSummary, PieceSnapshot]
	PersonRevision     = EntityRevision[PersonSummary, PersonSnapshot]
	TermRevision       = EntityRevision[TermSummary, TermSnapshot]
	CollectionRevision = EntityRevision[CollectionRef, CollectionSnapshot]
	EventRevision      = EntityRevision[EventSummary, EventSnapshot]
)

// NeedsWorkEntry is a public piece or person listed as needing work, with every reason it is listed for, in
// NeedsWorkReason's declaration order.
type NeedsWorkEntry struct {
	// EntityType is EntityTypePiece or EntityTypePerson.
	EntityType EntityType
	Slug       string
	// Title is the piece's title or the person's name.
	Title   string
	Reasons []NeedsWorkReason
}

// NeedsWorkCount is how many entries are listed for a reason.
type NeedsWorkCount struct {
	Reason NeedsWorkReason
	Count  int64
}

// NeedsWorkList is one page of the entries needing work, pieces first, each by sort key, with how many there are
// in all and how many are listed for each reason, whatever reason the list is filtered to.
//
// A public piece is listed for each of: being a stub, an unassessed conservation status, unknown licensing, and
// a visible dead link attached to it; a restricted piece only for being a stub. A public person who does not
// prefer no page is listed for being a stub, for unknown licensing if a community bard, and for a visible dead
// link attached to them.
type NeedsWorkList struct {
	Entries []NeedsWorkEntry
	Total   int64
	// Counts are for every reason, in NeedsWorkReason's declaration order.
	Counts []NeedsWorkCount
}

// ErrRevisionNotPending is returned for withdrawing, approving or rejecting a revision that is not pending.
var ErrRevisionNotPending = errors.New("revision is not pending")

// CheckPending returns ErrRevisionNotPending unless a revision of status may still be withdrawn, approved or
// rejected: only a pending revision moves, and only once.
func CheckPending(status RevisionStatus) error {
	if status != RevisionStatusPending {
		return fmt.Errorf("%w [%s]", ErrRevisionNotPending, status)
	}

	return nil
}

// IsStale reports whether a revision based on baseRevisionID is stale: its entity's current revision is another.
// A new entity's revision (no base) is never stale.
func IsStale(baseRevisionID, currentRevisionID *int64) bool {
	return baseRevisionID != nil && !equalPointers(baseRevisionID, currentRevisionID)
}

// RevisionView is a revision of any status as its author or a moderator sees it: one of PieceRevisionView,
// PersonRevisionView, TermRevisionView, CollectionRevisionView and EventRevisionView.
type RevisionView interface {
	isRevisionView()
}

// EntityRevisionView is a revision as its author or a moderator sees it, with the same snapshot views as a public
// revision.
type EntityRevisionView[E, S any] struct {
	RevisionSummary
	Status     RevisionStatus
	ReviewNote *string
	ReviewedAt *time.Time
	// Entity is the revision's entity, or nil for a new entity's revision that is not approved.
	Entity   *E
	Snapshot S
	// Base is as in EntityRevision.
	Base *RevisionBase[S]
	// AuthorUserID, Current and PossibleDuplicates are in the moderator view only: the author's user ID (nil for
	// none), the entity's current revision when the revision is stale, and for a new entity's pending revision the
	// public entities it may duplicate.
	AuthorUserID       *string
	Current            *RevisionBase[S]
	PossibleDuplicates []DuplicateCandidate
}

func (EntityRevisionView[E, S]) isRevisionView() {}

// Public returns the revision as the public sees it, for a revision whose Entity is set.
func (v EntityRevisionView[E, S]) Public() EntityRevision[E, S] {
	return EntityRevision[E, S]{RevisionSummary: v.RevisionSummary, Entity: *v.Entity, Snapshot: v.Snapshot,
		Base: v.Base}
}

// The revision views of each type of entity.
type (
	PieceRevisionView      = EntityRevisionView[PieceSummary, PieceSnapshot]
	PersonRevisionView     = EntityRevisionView[PersonSummary, PersonSnapshot]
	TermRevisionView       = EntityRevisionView[TermSummary, TermSnapshot]
	CollectionRevisionView = EntityRevisionView[CollectionRef, CollectionSnapshot]
	EventRevisionView      = EntityRevisionView[EventSummary, EventSnapshot]
)

// RevisionListItem is a revision as its author's list and the moderation queue show it.
type RevisionListItem struct {
	ID         int64
	EntityType EntityType
	// Entity is the revision's entity, or nil for a new entity's revision that is not approved.
	Entity     *RevisionEntity
	Status     RevisionStatus
	Summary    string
	CreatedAt  time.Time
	ReviewedAt *time.Time
	// IsStale is IsStale of the revision's base and its entity's current revision.
	IsStale bool
	// Author is set in the moderation queue only.
	Author *RevisionAuthorAccount
}

// RevisionEntity is the entity a listed revision edits: by slug, or for a collection by ID, with its name.
type RevisionEntity struct {
	Slug         *string
	CollectionID *int64
	Name         string
}

// RevisionAuthorAccount is a revision's author as moderators see them.
type RevisionAuthorAccount struct {
	UserID      string
	DisplayName string
}

// RevisionList is one page of revisions with how many there are in all.
type RevisionList struct {
	Revisions []RevisionListItem
	Total     int64
}
