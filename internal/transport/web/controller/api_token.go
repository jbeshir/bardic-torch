package controller

import (
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// APIToken is the JSON representation of an API token. The secret is never included.
type APIToken struct {
	ID         string     `json:"id"`
	Prefix     string     `json:"prefix"`
	Name       *string    `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func apiTokenFromDomain(token domain.APIToken) APIToken {
	return APIToken{
		ID:         token.ID,
		Prefix:     token.Prefix,
		Name:       token.Name,
		CreatedAt:  token.CreatedAt,
		LastUsedAt: token.LastUsedAt,
	}
}
