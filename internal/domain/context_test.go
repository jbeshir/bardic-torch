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
