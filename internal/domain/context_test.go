package domain

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoggerFromContext_DefaultAndStored(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	assert.Same(t, slog.Default(), LoggerFromContext(ctx))

	logger := slog.New(slog.DiscardHandler)
	assert.Same(t, logger, LoggerFromContext(ContextWithLogger(ctx, logger)))
}

func TestPrincipalContext_RoundTrip(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	_, ok := PrincipalFromContext(ctx)
	assert.False(t, ok, "a context without a principal is anonymous")

	principal := Principal{
		User: User{ID: "google-oauth2|123", DisplayName: "Bard 1a2b3c4d", Role: RoleModerator},
	}
	got, ok := PrincipalFromContext(ContextWithPrincipal(ctx, principal))
	assert.True(t, ok)
	assert.Equal(t, principal, got)
}
