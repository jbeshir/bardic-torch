package controller

import "github.com/jbeshir/bardic-torch/internal/domain"

// User is the JSON representation of a user.
type User struct {
	ID          string      `json:"id"`
	DisplayName string      `json:"display_name"`
	Role        domain.Role `json:"role"`
}

func userFromDomain(user domain.User) User {
	return User{
		ID:          user.ID,
		DisplayName: user.DisplayName,
		Role:        user.Role,
	}
}
