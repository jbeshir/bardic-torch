//go:build integration

package app

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setSetupEnv sets the environment Setup reads, with the given database and moderators to bootstrap, and the
// HTTP server on a free port.
func setSetupEnv(t *testing.T, databaseURL, bootstrapModerators string) {
	t.Setenv("DATABASE_URL", databaseURL)
	t.Setenv(bootstrapModeratorsEnv, bootstrapModerators)
	t.Setenv(authDriversEnv, "")
	t.Setenv("TURNSTILE_SECRET_KEY", "")
	t.Setenv("TURNSTILE_HOSTNAME", "")
	t.Setenv("PORT", "0")
}

// connectRepository returns a repository connected to the database at databaseURL.
func connectRepository(t *testing.T, databaseURL string) *mysql.Repository {
	t.Helper()

	cfg, err := mysql.ConfigFromURL(databaseURL)
	require.NoError(t, err)
	db, err := mysql.Connect(t.Context(), cfg, mysqltest.Pool())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return mysql.New(db)
}

// Not parallel: it sets environment variables.
func TestSetup_BootstrapsModeratorsAndServes(t *testing.T) {
	databaseURL := mysqltest.NewDatabaseURL(t)
	setSetupEnv(t, databaseURL, " auth0|m1 ,auth0|m1,, google-oauth2|m2")

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

// TestStoredTime_RoundTripsThroughDatabase checks that a user created at a time with fractional seconds is
// returned with the creation time later read back for it.
func TestStoredTime_RoundTripsThroughDatabase(t *testing.T) {
	t.Parallel()

	repository := mysql.New(mysqltest.NewDatabase(t))
	// MySQL would round this up to the next second if it were stored as is.
	fractional := time.Date(2026, 10, 31, 12, 0, 0, 734512345, time.UTC)
	created, err := repository.EnsureUser(t.Context(), datasources.NewUser{
		ID:          "auth0|fractional",
		DisplayName: "Fractional",
		CreatedAt:   storedTime(fractional),
	})
	require.NoError(t, err)

	read, err := repository.GetUser(t.Context(), "auth0|fractional")
	require.NoError(t, err)
	assert.Equal(t, created.CreatedAt, read.CreatedAt)
	assert.Equal(t, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), read.CreatedAt)
}
