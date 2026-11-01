package datasources

import (
	"context"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// PublicPieceGetter retrieves a public piece by slug with everything its page shows, returning ErrNotFound if no
// public piece has the slug. A piece that exists but is not approved is not found either.
type PublicPieceGetter interface {
	GetPublicPiece(ctx context.Context, slug string) (domain.Piece, error)
}

// PublicPersonGetter retrieves a public person by slug, returning ErrNotFound if no public person has the slug.
// A person who prefers not to have a page is returned with only their identity (a nil Profile).
type PublicPersonGetter interface {
	GetPublicPerson(ctx context.Context, slug string) (domain.Person, error)
}

// PublicTermLister lists the public terms, of one facet or (with a nil facet) every facet, in tree order: by
// facet, each parent before its children, and siblings by position, display name and ID.
type PublicTermLister interface {
	ListPublicTerms(ctx context.Context, facet *domain.Facet) ([]domain.TermListItem, error)
}

// PublicTermGetter retrieves a public term by slug with one page of its members, returning ErrNotFound if no
// public term has the slug. A term that is not approved, or has an ancestor that is not, is not found either.
type PublicTermGetter interface {
	GetPublicTerm(ctx context.Context, slug string, members domain.Page) (domain.Term, error)
}

// PublicCollectionGetter retrieves a public collection by ID, returning ErrNotFound if no public collection has
// the ID.
type PublicCollectionGetter interface {
	GetPublicCollection(ctx context.Context, id int64) (domain.Collection, error)
}

// PublicEventLister lists one page of the public events and venues, of one kind or (with a nil kind) both.
type PublicEventLister interface {
	ListPublicEvents(ctx context.Context, kind *domain.EventKind, page domain.Page) (domain.EventList, error)
}

// PublicEventGetter retrieves a public event or venue by slug, returning ErrNotFound if no public event has the
// slug.
type PublicEventGetter interface {
	GetPublicEvent(ctx context.Context, slug string) (domain.Event, error)
}
