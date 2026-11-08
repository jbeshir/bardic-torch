//go:build integration

package app

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setSetupEnv sets the environment Setup reads, with the given database and moderators to bootstrap, and the
// HTTP server on a free port, with the contact form disabled.
func setSetupEnv(t *testing.T, databaseURL, bootstrapModerators string) {
	t.Setenv("DATABASE_URL", databaseURL)
	t.Setenv(bootstrapModeratorsEnv, bootstrapModerators)
	t.Setenv(authDriversEnv, "api_token")
	t.Setenv("PORT", "0")
	// The contact form is disabled, so that no test calls Cloudflare.
	t.Setenv(turnstileSecretKeyEnv, "")
	t.Setenv(turnstileHostnamesEnv, "")
}

// connectRepository returns a repository connected to the database at databaseURL.
func connectRepository(t *testing.T, databaseURL string) *mysql.Repository {
	t.Helper()

	db, err := mysqltest.Connect(t.Context(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return mysql.New(db)
}

// Not parallel: it sets environment variables.
func TestSetup_BootstrapsModeratorsAndServes(t *testing.T) {
	databaseURL := mysqltest.NewDatabaseURL(t)
	setSetupEnv(t, databaseURL, " auth0|m1 ,auth0|m1,, google-oauth2|m2")
	retained, purged := seedContactRequests(t, connectRepository(t, databaseURL))

	components, err := Setup(t.Context())
	require.NoError(t, err)
	require.Len(t, components, 1)

	repository := connectRepository(t, databaseURL)
	for _, userID := range []string{"auth0|m1", "google-oauth2|m2"} {
		user, err := repository.GetUser(t.Context(), userID)
		require.NoError(t, err, userID)
		assert.Equal(t, domain.RoleModerator, user.Role, userID)
	}

	httpServer, ok := components[0].(*server.Server)
	require.True(t, ok, "the component is the HTTP server")
	ctx, cancel := context.WithCancel(t.Context())
	runErr := make(chan error, 1)
	go func() { runErr <- httpServer.Run(ctx) }()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+httpServer.Listener.Addr().String()+"/readyz", nil)
	require.NoError(t, err)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = res.Body.Close()
	assert.Equal(t, http.StatusOK, res.StatusCode)

	// Startup purged the request past its retention and kept the open one.
	list, err := repository.ListContactRequests(t.Context(), datasources.ContactRequestQuery{
		Page: domain.Page{Limit: domain.MaxPageLimit}})
	require.NoError(t, err)
	purgedByID := map[int64]bool{}
	for _, r := range list.Requests {
		purgedByID[r.ID] = r.Purged
	}
	assert.Equal(t, map[int64]bool{retained: false, purged: true}, purgedByID)

	// With an empty TURNSTILE_SECRET_KEY the contact form answers 503 and stores nothing.
	req, err = http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+httpServer.Listener.Addr().String()+"/v1/contact", strings.NewReader(
			`{"kind":"general","email":"wren@example.org","message":"Hello.","turnstile_token":"`+testTurnstileToken+`"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = res.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)

	cancel()
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("the server did not stop after its context was cancelled")
	}
}

// Not parallel: it sets environment variables.
func TestSetup_BootstrapFailureFailsStartup(t *testing.T) {
	// The ID is longer than the users.id column, so strict mode rejects the insert.
	setSetupEnv(t, mysqltest.NewDatabaseURL(t), "auth0|"+strings.Repeat("a", 256))

	components, err := Setup(t.Context())

	require.ErrorContains(t, err, "bootstrapping moderators")
	assert.Nil(t, components)
}

// TestStoredTime_RoundTripsThroughDatabase checks that a token created at a time with fractional seconds is
// returned with the creation time later read back for it.
func TestStoredTime_RoundTripsThroughDatabase(t *testing.T) {
	t.Parallel()

	repository := mysql.New(mysqltest.NewDatabase(t))
	_, err := repository.EnsureUser(t.Context(), datasources.NewUser{
		ID:          "auth0|owner",
		DisplayName: "Owner",
		CreatedAt:   storedTime(time.Now()),
	})
	require.NoError(t, err)

	// MySQL would round this up to the next second if it were stored as is.
	fractional := time.Date(2026, 10, 31, 12, 0, 0, 734512345, time.UTC)
	create := &command.CreateAPIToken{
		Counter: repository,
		Creator: repository,
		Now:     func() time.Time { return storedTime(fractional) },
	}
	created, err := create.Execute(t.Context(), command.CreateAPITokenRequest{UserID: "auth0|owner"})
	require.NoError(t, err)

	listed, err := repository.ListUserActiveAPITokens(t.Context(), "auth0|owner")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, created.Token.CreatedAt, listed[0].CreatedAt)
	assert.Equal(t, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), listed[0].CreatedAt)
}

// seedContactRequests stores two contact requests created and, for one, resolved, longer ago than their
// retention: the open one is retained, and the resolved one is purged at the next startup.
func seedContactRequests(t *testing.T, repository *mysql.Repository) (retained, purged int64) {
	t.Helper()

	longAgo := storedTime(time.Now().Add(-domain.ContactRetention - 24*time.Hour))
	err := repository.PromoteToModerator(t.Context(), datasources.NewUser{ID: "auth0|resolver", DisplayName: "Rowan",
		CreatedAt: longAgo})
	require.NoError(t, err)
	request := datasources.NewContactRequest{Kind: domain.ContactRequestKindGeneral, Email: "wren@example.org",
		Message: "Hello.", Now: longAgo}
	retained, err = repository.CreateContactRequest(t.Context(), request)
	require.NoError(t, err)
	purged, err = repository.CreateContactRequest(t.Context(), request)
	require.NoError(t, err)
	_, err = repository.ResolveContactRequest(t.Context(), datasources.ResolveContactRequestRequest{
		ModeratorUserID: "auth0|resolver", RequestID: purged, Status: domain.ContactRequestStatusResolved,
		Now: longAgo})
	require.NoError(t, err)

	return retained, purged
}

// Not parallel: it sets environment variables.
func TestSetup_GivesOwnerlessPerformancesTheirOwner(t *testing.T) {
	databaseURL := mysqltest.NewDatabaseURL(t)
	setSetupEnv(t, databaseURL, "auth0|m1")
	db, err := mysqltest.Connect(t.Context(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	exec := func(query string, args ...any) int64 {
		t.Helper()
		result, err := db.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
		id, err := result.LastInsertId()
		require.NoError(t, err)

		return id
	}
	pieceID := exec("INSERT INTO pieces (slug, title, sort_title, sort_letter, kind) " +
		"VALUES ('the-copper-kettle', 'The Copper Kettle', 'Copper Kettle', 'C', 'song')")
	linkID := exec("INSERT INTO links (url, normalized_url) VALUES ('https://example.org/kettle', " +
		"'https://example.org/kettle')")
	exec("INSERT INTO link_attachments (link_id, piece_id, role, position) VALUES (?, ?, 'performance', 0)",
		linkID, pieceID)
	// A performance as a release that does not set owners stores it.
	performanceID := exec("INSERT INTO performances (link_id, piece_id, description_verbatim) VALUES (?, ?, "+
		"'Sung at the revel')", linkID, pieceID)

	components, err := Setup(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = components[0].(*server.Server).Listener.Close() })

	var owner *int64
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT owner_piece_id FROM performances WHERE id = ?",
		performanceID).Scan(&owner))
	require.NotNil(t, owner)
	assert.Equal(t, pieceID, *owner)
}
