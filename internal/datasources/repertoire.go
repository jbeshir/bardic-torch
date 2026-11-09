package datasources

import (
	"context"
	"errors"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// ErrRepertoireLimitReached is a new repertoire entry of a user whose repertoire holds the most entries it may.
var ErrRepertoireLimitReached = errors.New("repertoire limit reached")

// RepertoireEntryWrite is a user's entry for the public piece with slug PieceSlug, with their notes and its
// visibility.
type RepertoireEntryWrite struct {
	UserID     string
	PieceSlug  string
	Notes      *string
	Visibility domain.RepertoireVisibility
	// MaxEntries is how many entries the user's repertoire may hold. Production always passes
	// domain.MaxRepertoireEntries; it is a field so that tests can shrink the cap rather than fill thousands of entries.
	MaxEntries int
	Now        time.Time
}

// RepertoireEntryPutter adds a public piece to a user's repertoire, or replaces the notes and visibility of its
// entry there, keeping when it was added, and returns the entry, with created true if it is new. It returns
// ErrNotFound if no public piece has the slug, ErrRepertoireLimitReached for a new entry of a full repertoire, and
// ErrUserBlocked if the user is blocked.
type RepertoireEntryPutter interface {
	PutRepertoireEntry(ctx context.Context, entry RepertoireEntryWrite) (domain.RepertoireEntry, bool, error)
}

// RepertoireEntryDeleter removes the public piece with slug pieceSlug from a user's repertoire. It returns
// ErrNotFound if the piece is not in their repertoire or no public piece has the slug, and ErrUserBlocked if the
// user is blocked.
type RepertoireEntryDeleter interface {
	DeleteRepertoireEntry(ctx context.Context, userID, pieceSlug string) error
}

// RepertoireEntryGetter returns a user's entry for the public piece with slug pieceSlug, returning ErrNotFound if
// the piece is not in their repertoire or no public piece has the slug.
type RepertoireEntryGetter interface {
	GetRepertoireEntry(ctx context.Context, userID, pieceSlug string) (domain.RepertoireEntry, error)
}

// RepertoireQuery is a request for one page of a user's own repertoire, optionally of one visibility.
type RepertoireQuery struct {
	UserID     string
	Visibility *domain.RepertoireVisibility
	Page       domain.Page
}

// RepertoireLister lists one page of a user's own repertoire, entries of public pieces only, newest first.
type RepertoireLister interface {
	ListRepertoire(ctx context.Context, query RepertoireQuery) (domain.RepertoireList, error)
}

// PublicRepertoireLister lists one page of the public entries, of public pieces, of the repertoire of the user with
// public ID publicID, newest first. It returns ErrNotFound, the same for each, if no user has the public ID, the
// user is blocked, or their repertoire has no public entry of a public piece.
type PublicRepertoireLister interface {
	ListPublicRepertoire(ctx context.Context, publicID string, page domain.Page) (domain.PublicRepertoire, error)
}
