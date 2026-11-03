package datasources

import (
	"context"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// ModerationQueueQuery filters the moderation queue. Every filter that is set must match. Ages are measured from
// when a revision was submitted to Now.
type ModerationQueueQuery struct {
	EntityType   *domain.EntityType
	AuthorUserID *string
	MinAgeHours  *int32
	MaxAgeHours  *int32
	// IncludeBlocked lists the revisions of blocked authors too, which the queue otherwise leaves out.
	IncludeBlocked bool
	Page           domain.Page
	Now            time.Time
}

// ModerationQueueLister lists one page of the pending revisions for moderators, oldest first by when they were
// submitted, then by ID.
type ModerationQueueLister interface {
	ListModerationQueue(ctx context.Context, query ModerationQueueQuery) (domain.ModerationQueue, error)
}

// ModerationRevisionGetter returns a revision of any status but draft as a moderator sees it (D8), returning
// ErrNotFound for no such revision and for a draft, which is private to its author.
type ModerationRevisionGetter interface {
	GetModerationRevision(ctx context.Context, id int64) (domain.ModerationRevision, error)
}

// ModerationLinkGetter returns the link whose normalized URL is url's as a moderator sees it, returning ErrNotFound
// if there is none or url does not normalize.
type ModerationLinkGetter interface {
	GetModerationLink(ctx context.Context, url string) (domain.ModerationLink, error)
}
