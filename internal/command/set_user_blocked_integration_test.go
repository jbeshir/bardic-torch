//go:build integration

package command

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetUserBlocked_UserWithoutPublicIDGetsOne requires a block or unblock of a user whose row has no public ID,
// as a user created before public IDs has until their next request, to return the user with one, stored, and kept
// by the next block or unblock.
func TestSetUserBlocked_UserWithoutPublicIDGetsOne(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	repo := mysql.New(db)
	for _, u := range []struct {
		id   string
		role domain.Role
	}{
		{moderatorUser.ID, domain.RoleModerator}, {resolvedUserID, domain.RoleContributor},
	} {
		_, err := db.ExecContext(t.Context(),
			"INSERT INTO users (id, display_name, role, created_at) VALUES (?, ?, ?, ?)",
			u.id, "A "+string(u.role), string(u.role), fixedNow)
		require.NoError(t, err)
	}
	cmd := &SetUserBlocked{Blocker: repo, Unblocker: repo, Getter: repo, PublicIDSetter: repo,
		PublicIDs: domain.RandomPublicID, Now: fixedClock}

	var assigned string
	for _, blocked := range []bool{true, false} {
		user, err := cmd.Execute(t.Context(), SetUserBlockedRequest{Principal: interactiveMod, UserID: resolvedUserID,
			Blocked: blocked})
		require.NoError(t, err)
		require.Truef(t, domain.IsPublicID(user.PublicID), "public_id %q", user.PublicID)
		assert.Equal(t, blocked, user.IsBlocked())
		if assigned == "" {
			assigned = user.PublicID
		}
		assert.Equal(t, assigned, user.PublicID, "the public ID, once given, is kept")

		stored, err := repo.GetUser(t.Context(), resolvedUserID)
		require.NoError(t, err)
		assert.Equal(t, user, stored)
	}
}
