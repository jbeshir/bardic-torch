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

func TestHashAPITokenSecret(t *testing.T) {
	t.Parallel()

	// The expected hash is `printf 'user_api|0123456789abcdef' | sha256sum`.
	assert.Equal(t, "df74d56c5a5b52715862a5862de54d234b5a4f18a68f1277d364cf1a86ee89ec",
		HashAPITokenSecret("user_api|0123456789abcdef"))
}
