package command

import (
	"context"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	resolvedUserID  = "google-oauth2|123"
	testDisplayName = "Morwen"
)

var fixedNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func fixedClock() time.Time {
	return fixedNow
}

// storedBy is a repository whose EnsureUser finds the user another request just created.
type storedBy struct {
	*fakerepo.Repo
	stored domain.User
}

func (r storedBy) EnsureUser(ctx context.Context, user datasources.NewUser) (domain.User, error) {
	r.Users[r.stored.ID] = r.stored
	return r.Repo.EnsureUser(ctx, user)
}

func TestResolveUser_CreatesMissingUser(t *testing.T) {
	t.Parallel()

	repo := &fakerepo.Repo{}
	user, err := (&ResolveUser{Users: repo, Now: fixedClock}).Execute(t.Context(), resolvedUserID)

	require.NoError(t, err)
	want := domain.User{ID: resolvedUserID, DisplayName: domain.DefaultDisplayName(resolvedUserID),
		Role: domain.RoleContributor, CreatedAt: fixedNow}
	assert.Equal(t, want, user)
	assert.Equal(t, want, repo.Users[resolvedUserID])
}

func TestResolveUser_ReturnsUserStoredByConcurrentRequest(t *testing.T) {
	t.Parallel()

	// Another request created the user between the read and the insert: EnsureUser returns the stored user,
	// whatever it was created with.
	stored := domain.User{ID: resolvedUserID, DisplayName: testDisplayName, Role: domain.RoleModerator, CreatedAt: fixedNow}
	repo := storedBy{Repo: &fakerepo.Repo{Users: map[string]domain.User{}}, stored: stored}

	user, err := (&ResolveUser{Users: repo, Now: fixedClock}).Execute(t.Context(), resolvedUserID)

	require.NoError(t, err)
	assert.Equal(t, stored, user)
}
