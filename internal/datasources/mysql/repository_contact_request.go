package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ datasources.ContactRequestCreator   = (*Repository)(nil)
	_ datasources.ContactRequestLister    = (*Repository)(nil)
	_ datasources.ContactRequestResolver  = (*Repository)(nil)
	_ datasources.ContactRequestDismisser = (*Repository)(nil)
	_ datasources.ContactRequestPurger    = (*Repository)(nil)
)

// CreateContactRequest stores an open contact request, refusing it while domain.MaxOpenContactRequests are
// open. The count is not locked: concurrent requests can exceed the cap by about as many as are inserting at once,
// and the Turnstile verifier's slots do not bound that, since a slot is released before the insert. It is a soft
// backstop against spam; BE5 adds rate limits. Errors never hold the request's fields, or the server's message,
// which could quote them.
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
		return 0, databaseError("creating contact request", err)
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
		return domain.ContactRequestList{}, databaseError("listing contact requests", err)
	}

	return list, nil
}

// ResolveContactRequest resolves or dismisses an open contact request: it locks the moderator's users row
// (lockModerator), then the request's row, checks it is still open and updates it. It returns the request as the
// transaction leaves it.
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
		if err := c.lockModerator(ctx, req.ModeratorUserID); err != nil {
			return err
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
		return domain.ContactRequest{}, databaseError(
			fmt.Sprintf("resolving contact request [%d]", req.RequestID), err)
	}

	return request, nil
}

// DismissContactRequests dismisses the open requests among req.RequestIDs in one write: it locks the moderator's
// users row (lockModerator), then the requests' rows in ascending ID order, and dismisses those still open with the
// moderator, the note and now as their resolution, so each one's personal data is purged ContactRetention later,
// as a single dismissal's is.
func (r *Repository) DismissContactRequests(
	ctx context.Context, req datasources.DismissContactRequestsRequest,
) (domain.ContactRequestDismissal, error) {
	named := slices.Clone(req.RequestIDs)
	slices.Sort(named)
	named = slices.Compact(named)
	var dismissal domain.ContactRequestDismissal
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
		if err := c.lockModerator(ctx, req.ModeratorUserID); err != nil {
			return err
		}
		dismissed, err := c.dismissOpenContactRequests(ctx, req, named)
		if err != nil {
			return err
		}
		dismissal = domain.ContactRequestDismissal{Dismissed: make([]int64, 0, len(dismissed)),
			NotOpen: make([]int64, 0, len(named)-len(dismissed))}
		for _, id := range named {
			if dismissed[id] {
				dismissal.Dismissed = append(dismissal.Dismissed, id)
			} else {
				dismissal.NotOpen = append(dismissal.NotOpen, id)
			}
		}
		return c.err
	})
	if err != nil {
		return domain.ContactRequestDismissal{}, databaseError("dismissing contact requests", err)
	}

	return dismissal, nil
}

// dismissOpenContactRequests locks the rows of the requests named, which are sorted and distinct, in ascending ID
// order, dismisses those that are open as req says, and returns the set of their IDs.
func (c *catalogueReader) dismissOpenContactRequests(
	ctx context.Context, req datasources.DismissContactRequestsRequest, named []int64,
) (map[int64]bool, error) {
	ids := make([]uint64, 0, len(named))
	for _, id := range named {
		if positive, ok := positiveID(id); ok {
			ids = append(ids, positive)
		}
	}
	rows, err := c.q.LockContactRequests(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("locking contact requests: %w", err)
	}
	dismissed := make(map[int64]bool, len(rows))
	openIDs := make([]uint64, 0, len(rows))
	for _, row := range rows {
		if row.Status == queries.ContactRequestsStatusOpen {
			dismissed[c.signedID(row.ID)] = true
			openIDs = append(openIDs, row.ID)
		}
	}
	if len(openIDs) == 0 {
		return dismissed, c.err
	}
	count, err := c.q.DismissOpenContactRequests(ctx, queries.DismissOpenContactRequestsParams{
		ResolvedByUserID: nullString(&req.ModeratorUserID), ResolvedAt: nullTime(req.Now),
		ResolutionNote: nullString(req.Note), Ids: openIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("dismissing contact requests: %w", err)
	}
	if count != int64(len(openIDs)) {
		return nil, fmt.Errorf("dismissing contact requests: %d of %d locked open requests dismissed", count,
			len(openIDs))
	}

	return dismissed, c.err
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
		return 0, databaseError("purging contact requests", err)
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
