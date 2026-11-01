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

// MaxPieceQueryTerms is the most distinct terms a piece search may filter on.
const MaxPieceQueryTerms = 20

// PieceQuery is a search of the public pieces. Every filter that is set must match.
type PieceQuery struct {
	// TermSlugs are the distinct slugs of the public piece terms to filter on, at most MaxPieceQueryTerms. A
	// piece matches a term if it is tagged with it or any of its descendants. Terms of one facet match if any
	// does; every facet with terms must match.
	TermSlugs []string
	// Kinds, if any, match a piece of one of them, as its primary or a secondary kind.
	Kinds []domain.PieceKind
	// ConservationStatuses, if any, match a piece with one of them.
	ConservationStatuses []domain.ConservationStatus
	// Stub, if set, matches pieces that are, or are not, stubs.
	Stub *bool
	// Search, if set, matches pieces whose title, notes or an alias contain it.
	Search *domain.SearchQuery
	// Letter, if set, matches pieces whose sort title sorts under it.
	Letter *domain.Letter
	// Sort is the order of the pieces; PieceSortRelevance needs a Search.
	Sort domain.PieceSort
	Page domain.Page
}

// PublicPieceLister searches the public pieces, returning one page of them with the total and facet counts. A
// term slug that is unknown, not public, or of the affiliation facet gives an UnknownTermError.
type PublicPieceLister interface {
	ListPublicPieces(ctx context.Context, query PieceQuery) (domain.PieceList, error)
}

// PersonQuery is a search of the public persons. Every filter that is set must match.
type PersonQuery struct {
	// Kinds, if any, match a person of one of them.
	Kinds []domain.PersonKind
	// Roles, if any, match a person with one of them. Persons who prefer no page match no role.
	Roles []domain.PersonRole
	// AffiliationSlug, if set, is the slug of a public affiliation term matching persons affiliated with it or
	// any of its descendants. Persons who prefer no page match no affiliation.
	AffiliationSlug *string
	// Search, if set, matches persons whose name or a public alias contain it. Persons who prefer no page match
	// by name only.
	Search *domain.SearchQuery
	// Letter, if set, matches persons whose sort name sorts under it.
	Letter *domain.Letter
	// Sort is the order of the persons; PersonSortRelevance needs a Search.
	Sort domain.PersonSort
	Page domain.Page
}

// PublicPersonLister searches the public persons, returning one page of them with the total and letter counts.
// An affiliation slug that is unknown, not public, or not of the affiliation facet gives an UnknownTermError.
type PublicPersonLister interface {
	ListPublicPersons(ctx context.Context, query PersonQuery) (domain.PersonList, error)
}

// CollectionQuery is a filter of the public collections. Every filter that is set must match.
type CollectionQuery struct {
	// Kinds, if any, match a collection of one of them.
	Kinds []domain.CollectionKind
	// Letter, if set, matches collections whose sort name sorts under it.
	Letter *domain.Letter
	Page   domain.Page
}

// PublicCollectionLister lists one page of the public collections matching a filter, by sort name, with the
// total and letter counts.
type PublicCollectionLister interface {
	ListPublicCollections(ctx context.Context, query CollectionQuery) (domain.CollectionList, error)
}
