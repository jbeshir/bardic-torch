package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const (
	// WriteTimeout is how long the server gives a response to be written, from the end of the request headers.
	// Work behind a request must end before it, or the client sees a dropped connection.
	WriteTimeout = 15 * time.Second
	// RequestBudget is how long the work behind one request may take, its context's deadline: under WriteTimeout,
	// so that a request out of time still answers, 503 when a write could not be tried in what is left of it.
	// A write transaction takes what is left of it, at most mysql.WriteTxDeadline (docs/PLAN.md §5.1 "Approval").
	RequestBudget   = 13 * time.Second
	shutdownTimeout = 10 * time.Second
	// maxHeaderBytes bounds the request line and headers; the largest legitimate header is an Auth0 access token
	// of a few KiB. Larger requests get 431.
	maxHeaderBytes = 16 << 10
)

// Server serves plain HTTP on Listener; TLS is terminated in front of it.
type Server struct {
	Listener net.Listener
	Handler  http.Handler
}

// Run serves HTTP until ctx is cancelled, then shuts down gracefully, giving in-flight requests up to 10
// seconds to finish. It returns nil after a graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	server := &http.Server{
		Handler:           withRequestBudget(s.Handler),
		ReadHeaderTimeout: 5 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      WriteTimeout,
		IdleTimeout:       60 * time.Second,
		// Requests inherit ctx's values, such as the logger, but are not cancelled with it, so that they
		// can complete during graceful shutdown.
		BaseContext: func(net.Listener) context.Context {
			return context.WithoutCancel(ctx)
		},
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(s.Listener)
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serving HTTP: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down HTTP server: %w", err)
	}

	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving HTTP: %w", err)
	}

	return nil
}

// withRequestBudget gives each request handled by next a context ending RequestBudget after the request reached it.
func withRequestBudget(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), RequestBudget)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
