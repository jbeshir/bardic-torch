package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ datasources.ContactRequestCreator  = (*Repository)(nil)
	_ datasources.ContactRequestLister   = (*Repository)(nil)
	_ datasources.ContactRequestResolver = (*Repository)(nil)
	_ datasources.ContactRequestPurger   = (*Repository)(nil)
)

// CreateContactRequest stores an open contact request (D21), refusing it while domain.MaxOpenContactRequests are
// open. The count is not locked, so concurrent requests may pass it together; the Turnstile verifier's slots bound
// how many can. Errors never hold the request's fields, or the server's message, which could quote them.
func (r *Repository) CreateContactRequest(ctx context.Context, request datasources.NewContactRequest) (int64, error) {
	var id int64
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		q := r.queries.WithTx(tx)
		open, err := q.CountOpenContactRequests(ctx)
		if err != nil {
			return fmt.Errorf("counting open contact requests: %w", err)
		}
		if open >= domain.MaxOpenContactRequests {
			return datasources.ErrContactRequestLimitReached
		}
		id, err = q.InsertContactRequest(ctx, queries.InsertContactRequestParams{
			Kind: queries.ContactRequestsKind(request.Kind), Name: nullString(request.Name),
			Email: nullString(&request.Email), Message: nullString(&request.Message), Url: nullString(request.URL),
			CreatedAt: request.Now,
		})
		if err != nil {
			return fmt.Errorf("inserting contact request: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, withoutServerMessage("creating contact request", err)
	}

	return id, nil
}

// ListContactRequests lists one page of contact requests, newest first, then by ID descending.
func (r *Repository) ListContactRequests(
	ctx context.Context, query datasources.ContactRequestQuery,
) (domain.ContactRequestList, error) {
	var (
		status queries.NullContactRequestsStatus
		kind   queries.NullContactRequestsKind
	)
	if query.Status != nil {
		status = queries.NullContactRequestsStatus{
			ContactRequestsStatus: queries.ContactRequestsStatus(*query.Status), Valid: true,
		}
	}
	if query.Kind != nil {
		kind = queries.NullContactRequestsKind{ContactRequestsKind: queries.ContactRequestsKind(*query.Kind), Valid: true}
	}
	var list domain.ContactRequestList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		ids := fetch(ctx, c, "contact requests", c.q.ListContactRequests, queries.ListContactRequestsParams{
			Status: status, Kind: kind, Limit: query.Page.Limit, Offset: query.Page.Offset,
		})
		list.Requests = make([]domain.ContactRequest, 0, len(ids))
		for _, id := range ids {
			request, err := c.contactRequest(ctx, id)
			if err != nil {
				return err
			}
			list.Requests = append(list.Requests, request)
		}
		list.Total = fetch(ctx, c, "contact request count", c.q.CountContactRequests,
			queries.CountContactRequestsParams{Status: status, Kind: kind})
		return c.err
	})
	if err != nil {
		return domain.ContactRequestList{}, withoutServerMessage("listing contact requests", err)
	}

	return list, nil
}

// ResolveContactRequest resolves or dismisses an open contact request (D21): it locks the request's row, checks
// it is still open and updates it. It returns the request as the transaction leaves it.
func (r *Repository) ResolveContactRequest(
	ctx context.Context, req datasources.ResolveContactRequestRequest,
) (domain.ContactRequest, error) {
	var request domain.ContactRequest
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
		id, ok := positiveID(req.RequestID)
		if !ok {
			return datasources.ErrNotFound
		}
		status, err := c.q.LockContactRequest(ctx, id)
		if err != nil {
			return lookupError("contact request", err)
		}
		if status != queries.ContactRequestsStatusOpen {
			return datasources.ErrContactRequestNotOpen
		}
		err = requireMatched(c.q.ResolveContactRequest(ctx, queries.ResolveContactRequestParams{
			Status: queries.ContactRequestsStatus(req.Status), ResolvedByUserID: nullString(&req.ModeratorUserID),
			ResolvedAt: nullTime(req.Now), ResolutionNote: nullString(req.Note), ID: id,
		}))
		if err != nil {
			return fmt.Errorf("resolving contact request: %w", err)
		}
		request, err = c.contactRequest(ctx, id)
		return err
	})
	if err != nil {
		return domain.ContactRequest{}, withoutServerMessage(
			fmt.Sprintf("resolving contact request [%d]", req.RequestID), err)
	}

	return request, nil
}

// PurgeContactRequests deletes the personal data of the contact requests resolved or dismissed more than
// domain.ContactRetention before now, in one short write.
func (r *Repository) PurgeContactRequests(ctx context.Context, now time.Time) (int64, error) {
	var purged int64
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		purged, err = r.queries.WithTx(tx).PurgeContactRequests(ctx, queries.PurgeContactRequestsParams{
			Now: nullTime(now), Cutoff: nullTime(now.Add(-domain.ContactRetention)),
		})
		if err != nil {
			return fmt.Errorf("purging contact requests: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, withoutServerMessage("purging contact requests", err)
	}

	return purged, nil
}

// contactRequest returns the contact request id.
func (c *catalogueReader) contactRequest(ctx context.Context, id uint64) (domain.ContactRequest, error) {
	row, err := c.q.GetContactRequest(ctx, id)
	if err != nil {
		return domain.ContactRequest{}, lookupError("contact request", err)
	}
	resolver, err := optionalModerationAuthor(row.ResolvedByUserID, row.ResolverDisplayName, row.ResolverRole,
		row.ResolverBlockedAt)
	if err != nil {
		return domain.ContactRequest{}, err
	}
	request := domain.ContactRequest{
		ID: c.signedID(row.ID), Kind: enumOf[domain.ContactRequestKind](c, row.Kind), Name: stringPtr(row.Name),
		Email: stringPtr(row.Email), Message: stringPtr(row.Message), URL: stringPtr(row.Url),
		Status: enumOf[domain.ContactRequestStatus](c, row.Status), CreatedAt: row.CreatedAt,
		ResolvedAt: timePtr(row.ResolvedAt), ResolvedBy: resolver, Note: stringPtr(row.ResolutionNote),
		Purged: row.PurgedAt.Valid,
	}

	return request, c.err
}
