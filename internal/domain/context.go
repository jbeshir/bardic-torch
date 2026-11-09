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

const principalContextKey contextKey = "principal"

// ContextWithPrincipal returns a copy of ctx carrying principal.
func ContextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey, principal)
}

// PrincipalFromContext returns the principal stored in ctx, and false if the request is anonymous.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey).(Principal)
	return principal, ok
}

const revisionSourceContextKey contextKey = "revision_source"

// ContextWithRevisionSource returns a copy of ctx carrying source, where the revisions a request creates come from:
// the MCP server marks each request it makes (docs/PLAN.md §4, revisions.source).
func ContextWithRevisionSource(ctx context.Context, source RevisionSource) context.Context {
	return context.WithValue(ctx, revisionSourceContextKey, source)
}

// RevisionSourceFromContext returns the revision source stored in ctx, or RevisionSourceUser if there is none: a
// request made through the API.
func RevisionSourceFromContext(ctx context.Context) RevisionSource {
	if source, ok := ctx.Value(revisionSourceContextKey).(RevisionSource); ok {
		return source
	}

	return RevisionSourceUser
}
