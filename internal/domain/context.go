package domain

import (
	"context"
	"log/slog"
)

type contextKey string

const loggerContextKey contextKey = "logger"

// ContextWithLogger returns a copy of ctx carrying logger.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey, logger)
}

// LoggerFromContext returns the logger stored in ctx, or slog.Default() if there is none.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerContextKey).(*slog.Logger); ok {
		return logger
	}

	return slog.Default()
}
