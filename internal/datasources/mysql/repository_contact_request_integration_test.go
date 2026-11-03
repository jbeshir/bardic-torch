//go:build integration

package mysql_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// contactModeratorID is the moderator who resolves the contact requests.
const contactModeratorID = "google-oauth2|contact-moderator"

// contactModeratorName is the display name of the moderator who resolves the contact requests.
const contactModeratorName = "Rowan"

// contactFixture is an empty catalogue with one moderator, for the contact request tests.
type contactFixture struct {
	db   *sql.DB
	repo *mysql.Repository
	now  time.Time
}

func newContactFixture(t *testing.T) contactFixture {
	t.Helper()
	db := catalogtest.Writable(t)
	_, err := db.ExecContext(t.Context(), "INSERT INTO users (id, display_name, role, created_at) "+
		"VALUES (?, ?, 'moderator', ?)", contactModeratorID, contactModeratorName, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	return contactFixture{db: db, repo: mysql.New(db), now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
}

// create stores a contact request of kind with the message at now, returning its ID.
func (f contactFixture) create(t *testing.T, kind domain.ContactRequestKind, message string, now time.Time) int64 {
	t.Helper()
	id, err := f.repo.CreateContactRequest(t.Context(), datasources.NewContactRequest{Kind: kind,
		Name: new("Wren Placeholder"), Email: "wren@example.org", Message: message,
		URL: new("https://example.org/the-copper-kettle"), Now: now})
	require.NoError(t, err)

	return id
}

// resolve resolves or dismisses the request id at now.
func (f contactFixture) resolve(
	t *testing.T, id int64, status domain.ContactRequestStatus, now time.Time,
) (domain.ContactRequest, error) {
	t.Helper()
	return f.repo.ResolveContactRequest(t.Context(), datasources.ResolveContactRequestRequest{
		ModeratorUserID: contactModeratorID, RequestID: id, Status: status, Note: new("Done."), Now: now})
}

// get returns the request id as the inbox lists it.
func (f contactFixture) get(t *testing.T, id int64) domain.ContactRequest {
	t.Helper()
	list, err := f.repo.ListContactRequests(t.Context(), datasources.ContactRequestQuery{
		Page: domain.Page{Limit: domain.MaxPageLimit}})
	require.NoError(t, err)
	for _, r := range list.Requests {
		if r.ID == id {
			return r
		}
	}
	require.Failf(t, "contact request not listed", "%d", id)
	return domain.ContactRequest{}
}

// TestRepository_ContactRequests walks the contact request queries on one database: storing, the inbox's order
// and filters, resolving once, and purging the personal data of requests resolved more than ContactRetention ago.
func TestRepository_ContactRequests(t *testing.T) {
	t.Parallel()
	f := newContactFixture(t)

	first := f.create(t, domain.ContactRequestKindTakedown, "Please take down the recording.", f.now)
	second, err := f.repo.CreateContactRequest(t.Context(), datasources.NewContactRequest{
		Kind: domain.ContactRequestKindGeneral, Email: "ash@example.org", Message: "Hello.", Now: f.now.Add(time.Hour)})
	require.NoError(t, err)

	list, err := f.repo.ListContactRequests(t.Context(), datasources.ContactRequestQuery{Page: domain.Page{Limit: 10}})
	require.NoError(t, err)
	assert.Equal(t, domain.ContactRequestList{Total: 2, Requests: []domain.ContactRequest{
		{ID: second, Kind: domain.ContactRequestKindGeneral, Email: new("ash@example.org"), Message: new("Hello."),
			Status: domain.ContactRequestStatusOpen, CreatedAt: f.now.Add(time.Hour)},
		{ID: first, Kind: domain.ContactRequestKindTakedown, Name: new("Wren Placeholder"),
			Email: new("wren@example.org"), Message: new("Please take down the recording."),
			URL: new("https://example.org/the-copper-kettle"), Status: domain.ContactRequestStatusOpen, CreatedAt: f.now},
	}}, list, "newest first, with the sender's data")

	takedown := domain.ContactRequestKindTakedown
	list, err = f.repo.ListContactRequests(t.Context(), datasources.ContactRequestQuery{Kind: &takedown,
		Page: domain.Page{Limit: 10}})
	require.NoError(t, err)
	require.Len(t, list.Requests, 1)
	assert.Equal(t, first, list.Requests[0].ID)
	assert.Equal(t, int64(1), list.Total)
	page, err := f.repo.ListContactRequests(t.Context(), datasources.ContactRequestQuery{
		Page: domain.Page{Limit: 1, Offset: 1}})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total)
	require.Len(t, page.Requests, 1)
	assert.Equal(t, first, page.Requests[0].ID)

	resolvedAt := f.now.Add(2 * time.Hour)
	resolved, err := f.resolve(t, first, domain.ContactRequestStatusResolved, resolvedAt)
	require.NoError(t, err)
	assert.Equal(t, domain.ContactRequestStatusResolved, resolved.Status)
	assert.Equal(t, &resolvedAt, resolved.ResolvedAt)
	assert.Equal(t, &domain.ModerationAuthor{ID: contactModeratorID, DisplayName: contactModeratorName, Role: domain.RoleModerator},
		resolved.ResolvedBy)
	assert.Equal(t, new("Done."), resolved.Note)
	assert.Equal(t, new("wren@example.org"), resolved.Email, "resolving keeps the data until its retention ends")
	_, err = f.resolve(t, first, domain.ContactRequestStatusDismissed, resolvedAt)
	require.ErrorIs(t, err, datasources.ErrContactRequestNotOpen, "a request is resolved once")
	for _, id := range []int64{second + 100, 0, -1} {
		_, err = f.resolve(t, id, domain.ContactRequestStatusResolved, resolvedAt)
		require.ErrorIs(t, err, datasources.ErrNotFound, id)
	}

	open := domain.ContactRequestStatusOpen
	list, err = f.repo.ListContactRequests(t.Context(), datasources.ContactRequestQuery{Status: &open,
		Page: domain.Page{Limit: 10}})
	require.NoError(t, err)
	require.Len(t, list.Requests, 1)
	assert.Equal(t, second, list.Requests[0].ID)

	// Nothing is purged until the retention has passed, and an open request never is, however old.
	purged, err := f.repo.PurgeContactRequests(t.Context(), resolvedAt.Add(domain.ContactRetention-time.Second))
	require.NoError(t, err)
	assert.Zero(t, purged)
	purgeAt := resolvedAt.Add(domain.ContactRetention + time.Second)
	purged, err = f.repo.PurgeContactRequests(t.Context(), purgeAt.Add(365*24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), purged)
	assert.Equal(t, domain.ContactRequest{ID: first, Kind: domain.ContactRequestKindTakedown,
		Status: domain.ContactRequestStatusResolved, CreatedAt: f.now, ResolvedAt: &resolvedAt,
		ResolvedBy: resolved.ResolvedBy, Note: new("Done."), Purged: true}, f.get(t, first))
	assert.Equal(t, new("ash@example.org"), f.get(t, second).Email)
	var stored sql.NullString
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT CONCAT_WS('|', name, email, message, url) "+
		"FROM contact_requests WHERE id = ?", first).Scan(&stored))
	assert.Empty(t, stored.String, "the personal data is gone from the row")
	purged, err = f.repo.PurgeContactRequests(t.Context(), purgeAt.Add(365*24*time.Hour))
	require.NoError(t, err)
	assert.Zero(t, purged, "a purged request is not purged again")

	// A dismissed request is purged like a resolved one.
	_, err = f.resolve(t, second, domain.ContactRequestStatusDismissed, resolvedAt)
	require.NoError(t, err)
	purged, err = f.repo.PurgeContactRequests(t.Context(), purgeAt)
	require.NoError(t, err)
	assert.Equal(t, int64(1), purged)
	assert.True(t, f.get(t, second).Purged)
}

// TestRepository_ContactRequests_Cap checks that no request is stored while MaxOpenContactRequests are open, and
// that resolving one makes room again.
func TestRepository_ContactRequests_Cap(t *testing.T) {
	t.Parallel()
	f := newContactFixture(t)

	_, err := f.db.ExecContext(t.Context(), "INSERT INTO contact_requests (kind, email, message, created_at) "+
		"WITH RECURSIVE n (i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?) "+
		"SELECT 'general', 'wren@example.org', 'Hello.', ? FROM n", domain.MaxOpenContactRequests-1, f.now)
	require.NoError(t, err)
	last := f.create(t, domain.ContactRequestKindGeneral, "The last one.", f.now)

	_, err = f.repo.CreateContactRequest(t.Context(), datasources.NewContactRequest{
		Kind: domain.ContactRequestKindGeneral, Email: "wren@example.org", Message: "One too many.", Now: f.now})
	require.ErrorIs(t, err, datasources.ErrContactRequestLimitReached)
	assert.NotContains(t, err.Error(), "One too many.")

	_, err = f.resolve(t, last, domain.ContactRequestStatusDismissed, f.now)
	require.NoError(t, err)
	f.create(t, domain.ContactRequestKindGeneral, "Room again.", f.now)
}

// TestContactRequests_ListAndResolvePurge checks that the inbox list and a resolution purge the requests past
// their retention, through the commands and this repository, with the clock passed in.
func TestContactRequests_ListAndResolvePurge(t *testing.T) {
	t.Parallel()
	f := newContactFixture(t)
	moderator := domain.Principal{User: domain.User{ID: contactModeratorID, Role: domain.RoleModerator},
		Method: domain.AuthMethodAuth0}

	old := f.create(t, domain.ContactRequestKindOptOut, "Please hide my page.", f.now)
	_, err := f.resolve(t, old, domain.ContactRequestStatusResolved, f.now)
	require.NoError(t, err)
	later := f.now.Add(domain.ContactRetention + time.Minute)
	list := &command.ListContactRequests{Purger: f.repo, Lister: f.repo, Now: func() time.Time { return later }}
	listed, err := list.Execute(t.Context(), command.ContactRequestListRequest{Principal: moderator,
		Query: datasources.ContactRequestQuery{Page: domain.Page{Limit: 10}}})
	require.NoError(t, err)
	require.Len(t, listed.Requests, 1)
	assert.True(t, listed.Requests[0].Purged, "the list purged before it read")
	assert.Nil(t, listed.Requests[0].Message)

	other := f.create(t, domain.ContactRequestKindCorrection, "A correction.", f.now)
	_, err = f.resolve(t, other, domain.ContactRequestStatusResolved, f.now)
	require.NoError(t, err)
	open := f.create(t, domain.ContactRequestKindGeneral, "Still open.", f.now)
	resolve := &command.ResolveContactRequest{Resolver: f.repo, Purger: f.repo, Now: func() time.Time { return later }}
	resolved, err := resolve.Execute(t.Context(), command.ResolveContactRequestRequest{Principal: moderator,
		RequestID: open, Status: domain.ContactRequestStatusDismissed})
	require.NoError(t, err)
	assert.False(t, resolved.Purged, "the request just resolved keeps its data")
	assert.True(t, f.get(t, other).Purged, "the resolution purged the request past its retention")
}
