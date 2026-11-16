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

// noNameWrites is a repository that fails the test if a display name is written.
type noNameWrites struct {
	datasources.Users
	t *testing.T
}

func (r noNameWrites) UpdateDisplayName(context.Context, string, string) error {
	r.t.Error("display name written unexpectedly")
	return nil
}

func TestResolveUser_CreatesMissingUserWithTheirName(t *testing.T) {
	t.Parallel()

	repo := &fakerepo.Repo{}
	user, err := (&ResolveUser{Users: repo, Now: fixedClock}).Execute(t.Context(),
		ResolveUserRequest{UserID: resolvedUserID, DisplayName: testDisplayName})

	require.NoError(t, err)
	want := domain.User{ID: resolvedUserID, DisplayName: testDisplayName, Role: domain.RoleContributor,
		CreatedAt: fixedNow}
	assert.Equal(t, want, user)
	assert.Equal(t, want, repo.Users[resolvedUserID])
}

func TestResolveUser_ReturnsUserStoredByConcurrentRequest(t *testing.T) {
	t.Parallel()

	// Another request created the user between the read and the insert: EnsureUser returns the stored user,
	// whatever role it was created with.
	stored := domain.User{ID: resolvedUserID, DisplayName: testDisplayName, Role: domain.RoleModerator, CreatedAt: fixedNow}
	repo := storedBy{Repo: &fakerepo.Repo{Users: map[string]domain.User{}}, stored: stored}

	user, err := (&ResolveUser{Users: noNameWrites{Users: repo, t: t}, Now: fixedClock}).Execute(t.Context(),
		ResolveUserRequest{UserID: resolvedUserID, DisplayName: testDisplayName})
	require.NoError(t, err)
	assert.Equal(t, stored, user)
}

func TestResolveUser_ReturnsUnchangedUserWithoutWriting(t *testing.T) {
	t.Parallel()

	stored := domain.User{ID: resolvedUserID, DisplayName: testDisplayName, Role: domain.RoleContributor,
		CreatedAt: fixedNow}
	repo := noNameWrites{Users: &fakerepo.Repo{Users: map[string]domain.User{resolvedUserID: stored}}, t: t}

	user, err := (&ResolveUser{Users: repo, Now: fixedClock}).Execute(t.Context(),
		ResolveUserRequest{UserID: resolvedUserID, DisplayName: testDisplayName})

	require.NoError(t, err)
	assert.Equal(t, stored, user)
}

func TestResolveUser_UpdatesChangedNameOnly(t *testing.T) {
	t.Parallel()

	blockedAt := fixedNow.Add(-time.Hour)
	stored := domain.User{ID: resolvedUserID, DisplayName: "Old name", Role: domain.RoleContributor,
		CreatedAt: fixedNow.Add(-24 * time.Hour), BlockedAt: &blockedAt}
	repo := &fakerepo.Repo{Users: map[string]domain.User{resolvedUserID: stored}}

	user, err := (&ResolveUser{Users: repo, Now: fixedClock}).Execute(t.Context(),
		ResolveUserRequest{UserID: resolvedUserID, DisplayName: testDisplayName})

	require.NoError(t, err)
	want := stored
	want.DisplayName = testDisplayName
	assert.Equal(t, want, user)
	assert.Equal(t, want, repo.Users[resolvedUserID])
}
