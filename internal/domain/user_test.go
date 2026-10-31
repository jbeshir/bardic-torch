package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUser_IsBlocked(t *testing.T) {
	t.Parallel()

	blockedAt := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

	assert.False(t, User{ID: "auth0|a"}.IsBlocked())
	assert.True(t, User{ID: "auth0|a", BlockedAt: &blockedAt}.IsBlocked())
}
