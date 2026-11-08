package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrincipal_Actor(t *testing.T) {
	t.Parallel()

	moderator := User{ID: "google-oauth2|moderator", Role: RoleModerator}
	contributor := User{ID: "google-oauth2|contributor", Role: RoleContributor}

	assert.Equal(t, Actor{UserID: moderator.ID, Role: RoleModerator},
		Principal{User: moderator, Method: AuthMethodAuth0}.Actor())
	// An API token has contributor powers only.
	assert.Equal(t, Actor{UserID: moderator.ID, Role: RoleContributor},
		Principal{User: moderator, Method: AuthMethodAPIToken}.Actor())
	assert.Equal(t, Actor{UserID: contributor.ID, Role: RoleContributor},
		Principal{User: contributor, Method: AuthMethodAuth0}.Actor())
	assert.Equal(t, Actor{UserID: contributor.ID, Role: RoleContributor},
		Principal{User: contributor, Method: AuthMethodAPIToken}.Actor())
}
