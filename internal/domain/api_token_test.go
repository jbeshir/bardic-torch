package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAPIToken_IsActive(t *testing.T) {
	t.Parallel()

	revokedAt := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

	assert.True(t, APIToken{ID: "token"}.IsActive())
	assert.False(t, APIToken{ID: "token", RevokedAt: &revokedAt}.IsActive())
}
