package router

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

const unmatchedRoute = "unmatched"

// requestLogger stores a logger annotated with the request's method, route template and a fresh request ID in
// the request context, and logs the response status and duration once the request completes. routes is used
// only to look up the route template.
func requestLogger(routes *mux.Router) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx := r.Context()

			logger := domain.LoggerFromContext(ctx).With(
				"method", r.Method,
				"route", routeTemplate(routes, r),
				"request_id", uuid.NewString(),
			)
			ctx = domain.ContextWithLogger(ctx, logger)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(ctx))

			logger.InfoContext(ctx, "request completed",
				"status", rec.status,
				"duration", time.Since(start),
			)
		})
	}
}

func routeTemplate(routes *mux.Router, r *http.Request) string {
	var match mux.RouteMatch
	if !routes.Match(r, &match) || match.Route == nil {
		return unmatchedRoute
	}

	template, err := match.Route.GetPathTemplate()
	if err != nil {
		return unmatchedRoute
	}

	return template
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
