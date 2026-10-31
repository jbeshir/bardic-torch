//go:build integration

package app

import (
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
