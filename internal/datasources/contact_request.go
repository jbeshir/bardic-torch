package datasources

import (
	"context"
	"errors"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The errors the contact request writes refuse with.
var (
	// ErrContactRequestLimitReached is a new contact request while domain.MaxOpenContactRequests are open.
	ErrContactRequestLimitReached = errors.New("open contact request limit reached")
	// ErrContactRequestNotOpen is the resolution of a contact request that is already resolved or dismissed.
	ErrContactRequestNotOpen = errors.New("the contact request is not open")
)

// NewContactRequest is a message sent through the contact form (D21). Name, Email, Message and URL are personal
// data, which must never be logged or named in an error.
type NewContactRequest struct {
	Kind    domain.ContactRequestKind
	Name    *string
	Email   string
	Message string
	URL     *string
	Now     time.Time
}

// ContactRequestCreator stores an open contact request and returns its ID. It returns
// ErrContactRequestLimitReached if domain.MaxOpenContactRequests are already open.
type ContactRequestCreator interface {
	CreateContactRequest(ctx context.Context, request NewContactRequest) (int64, error)
}

// ContactRequestQuery filters the contact requests moderators list.
type ContactRequestQuery struct {
	Status *domain.ContactRequestStatus
	Kind   *domain.ContactRequestKind
	Page   domain.Page
}

// ContactRequestLister lists one page of contact requests for moderators, newest first, then by ID descending.
type ContactRequestLister interface {
	ListContactRequests(ctx context.Context, query ContactRequestQuery) (domain.ContactRequestList, error)
}

// ResolveContactRequestRequest is a moderator's resolution of the open contact request RequestID: resolved or
// dismissed, with an optional note.
type ResolveContactRequestRequest struct {
	ModeratorUserID string
	RequestID       int64
	Status          domain.ContactRequestStatus
	Note            *string
	Now             time.Time
}

// ContactRequestResolver resolves or dismisses an open contact request and returns it as it is then. It returns
// ErrNotFound for no such request and ErrContactRequestNotOpen for a request that is not open.
type ContactRequestResolver interface {
	ResolveContactRequest(ctx context.Context, req ResolveContactRequestRequest) (domain.ContactRequest, error)
}

// ContactRequestPurger deletes the personal data (name, email, message and URL) of every contact request resolved
// or dismissed more than domain.ContactRetention before now, marking them purged at now, and returns how many it
// purged. Open requests are never purged.
type ContactRequestPurger interface {
	PurgeContactRequests(ctx context.Context, now time.Time) (int64, error)
}
