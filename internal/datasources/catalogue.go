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
