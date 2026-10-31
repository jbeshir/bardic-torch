package domain

import "fmt"

// Role is a user's permission level.
type Role string

const (
	// RoleContributor is the role every user starts with.
	RoleContributor Role = "contributor"
	// RoleModerator may moderate other users.
	RoleModerator Role = "moderator"
)

// ParseRole returns the Role named by s, or an error if s is not a known role.
func ParseRole(s string) (Role, error) {
	switch r := Role(s); r {
	case RoleContributor, RoleModerator:
		return r, nil
	default:
		return "", fmt.Errorf("unknown role [%s]", s)
	}
}
