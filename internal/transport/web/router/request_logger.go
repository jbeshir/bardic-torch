package router

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

const unmatchedRoute = "unmatched"

type requestRecordContextKey struct{}

// requestRecord collects what middleware further down the chain learns about a request, for the completion log.
// requestLogger creates one per request; it is written on the request's goroutine before the handler chain
// returns, and read only after.
type requestRecord struct {
	userID string
}

// recordUserID notes the authenticated user's ID for the completion log of the request ctx belongs to.
func recordUserID(ctx context.Context, userID string) {
	if record, ok := ctx.Value(requestRecordContextKey{}).(*requestRecord); ok {
		record.userID = userID
	}
}

// requestLogger stores a logger annotated with the request's method, route template and a fresh request ID in
// the request context, and logs the response status, duration and, for authenticated requests, the user ID
// once the request completes. lookup returns the route template, or false for requests no route matches.
func requestLogger(lookup func(*http.Request) (string, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx := r.Context()

			route, ok := lookup(r)
			if !ok {
				route = unmatchedRoute
			}

			logger := domain.LoggerFromContext(ctx).With(
				"method", r.Method,
				"route", route,
				"request_id", uuid.NewString(),
			)
			ctx = domain.ContextWithLogger(ctx, logger)

			record := &requestRecord{}
			ctx = context.WithValue(ctx, requestRecordContextKey{}, record)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(ctx))

			attrs := []any{"status", rec.status, "duration", time.Since(start)}
			if record.userID != "" {
				attrs = append(attrs, "user_id", record.userID)
			}
			logger.InfoContext(ctx, "request completed", attrs...)
		})
	}
}

// statusRecorder records the status code written through it.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(status int) {
	if !s.wroteHeader {
		s.status = status
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
