package command

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ Command[ContactSubmission, int64]                             = (*SubmitContactRequest)(nil)
	_ Command[ContactRequestListRequest, domain.ContactRequestList] = (*ListContactRequests)(nil)
	_ Command[ResolveContactRequestRequest, domain.ContactRequest]  = (*ResolveContactRequest)(nil)
	_ Command[Empty, Empty]                                         = (*PurgeContactRequests)(nil)
)

// ErrContactUnavailable is a contact request that cannot be taken now, through no fault of its sender: the
// contact form is disabled, Turnstile cannot verify the token, or too many requests are open. Nothing is stored.
var ErrContactUnavailable = errors.New("the contact form is unavailable")

// ContactSubmission is a message sent through the public contact form (D21), already validated. Name, Email,
// Message, URL and TurnstileToken are personal data or secrets: they are never logged or named in an error.
// URL is stored absolute: one typed without a scheme ("www.example.org/x") gets https (domain.WithScheme).
type ContactSubmission struct {
	Kind           domain.ContactRequestKind
	Name           *string
	Email          string
	Message        string
	URL            *string
	TurnstileToken string
}

// SubmitContactRequest stores a contact form message once its Turnstile token passes. Anyone may send one; no
// account is needed.
type SubmitContactRequest struct {
	Verifier datasources.TurnstileVerifier
	Creator  datasources.ContactRequestCreator
	Now      func() time.Time
}

// Execute verifies the token, then stores the request and returns its ID. A token that does not pass fails with
// datasources.ErrTurnstileRejected and stores nothing; a disabled form, a failed verification or the open
// requests' cap fails with ErrContactUnavailable.
func (c *SubmitContactRequest) Execute(ctx context.Context, req ContactSubmission) (int64, error) {
	err := c.Verifier.VerifyTurnstile(ctx, req.TurnstileToken)
	switch {
	case errors.Is(err, datasources.ErrTurnstileUnavailable):
		return 0, fmt.Errorf("%w: %w", ErrContactUnavailable, err)
	case err != nil:
		return 0, fmt.Errorf("verifying the turnstile token: %w", err)
	}

	id, err := c.Creator.CreateContactRequest(ctx, datasources.NewContactRequest{
		Kind: req.Kind, Name: req.Name, Email: req.Email, Message: req.Message, URL: absoluteURL(req.URL),
		Now: c.Now(),
	})
	switch {
	case errors.Is(err, datasources.ErrContactRequestLimitReached):
		return 0, fmt.Errorf("%w: %w", ErrContactUnavailable, err)
	case err != nil:
		return 0, fmt.Errorf("storing contact request: %w", err)
	}

	return id, nil
}

// absoluteURL is the URL raw with a scheme (domain.WithScheme), or nil for none.
func absoluteURL(raw *string) *string {
	if raw == nil {
		return nil
	}
	absolute := domain.WithScheme(*raw)

	return &absolute
}

// ContactRequestListRequest is Principal's request for a page of the contact requests.
type ContactRequestListRequest struct {
	Principal domain.Principal
	Query     datasources.ContactRequestQuery
}

// ListContactRequests lists the contact requests for moderators, newest first.
type ListContactRequests struct {
	Purger datasources.ContactRequestPurger
	Lister datasources.ContactRequestLister
	Now    func() time.Time
}

// Execute purges the personal data that is past its retention, in its own write, then returns the page of
// contact requests req.Query asks for. A failed purge is logged and does not fail the list: the next list,
// resolution or startup purges again.
func (c *ListContactRequests) Execute(
	ctx context.Context, req ContactRequestListRequest,
) (domain.ContactRequestList, error) {
	if _, err := moderatorActor(req.Principal); err != nil {
		return domain.ContactRequestList{}, err
	}
	purgeOrWarn(ctx, c.Purger, c.Now())
	list, err := c.Lister.ListContactRequests(ctx, req.Query)
	if err != nil {
		return domain.ContactRequestList{}, fmt.Errorf("listing contact requests: %w", err)
	}

	return list, nil
}

// ResolveContactRequestRequest is Principal's resolution of the open contact request RequestID as Status,
// resolved or dismissed, with an optional note.
type ResolveContactRequestRequest struct {
	Principal domain.Principal
	RequestID int64
	Status    domain.ContactRequestStatus
	Note      *string
}

// ResolveContactRequest resolves or dismisses an open contact request.
type ResolveContactRequest struct {
	Resolver datasources.ContactRequestResolver
	Purger   datasources.ContactRequestPurger
	Now      func() time.Time
}

// Execute resolves the request and returns it, failing with the errors of datasources.ContactRequestResolver.
// Once the resolution commits it purges the personal data that is past its retention, in its own write; a failed
// purge is logged and does not fail the resolution.
func (c *ResolveContactRequest) Execute(
	ctx context.Context, req ResolveContactRequestRequest,
) (domain.ContactRequest, error) {
	actor, err := moderatorActor(req.Principal)
	if err != nil {
		return domain.ContactRequest{}, err
	}
	now := c.Now()
	request, err := c.Resolver.ResolveContactRequest(ctx, datasources.ResolveContactRequestRequest{
		ModeratorUserID: actor.UserID, RequestID: req.RequestID, Status: req.Status, Note: req.Note, Now: now,
	})
	if err != nil {
		return domain.ContactRequest{}, fmt.Errorf("resolving contact request [%d]: %w", req.RequestID, err)
	}
	purgeOrWarn(ctx, c.Purger, now)

	return request, nil
}

// PurgeContactRequests deletes the personal data of contact requests past their retention. It runs at startup.
type PurgeContactRequests struct {
	Purger datasources.ContactRequestPurger
	Now    func() time.Time
}

// Execute purges, logging how many requests it purged.
func (c *PurgeContactRequests) Execute(ctx context.Context, _ Empty) (Empty, error) {
	purged, err := c.Purger.PurgeContactRequests(ctx, c.Now())
	if err != nil {
		return Empty{}, fmt.Errorf("purging contact requests: %w", err)
	}
	domain.LoggerFromContext(ctx).InfoContext(ctx, "purged contact requests past their retention", "purged", purged)

	return Empty{}, nil
}

// purgeOrWarn purges the contact requests past their retention at now, logging a failure at warn.
func purgeOrWarn(ctx context.Context, purger datasources.ContactRequestPurger, now time.Time) {
	if _, err := purger.PurgeContactRequests(ctx, now); err != nil {
		domain.LoggerFromContext(ctx).WarnContext(ctx, "unable to purge contact requests", "error", err)
	}
}
