package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const runTimeout = 5 * time.Second

// closeSignallingListener closes closed when the server closes it, which it does when shutdown begins.
type closeSignallingListener struct {
	net.Listener
	once   sync.Once
	closed chan struct{}
}

func (l *closeSignallingListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return l.Listener.Close()
}

// startServer runs a Server for handler until the returned cancel is called, returning its listener and the
// channel Run's result is sent on.
func startServer(t *testing.T, handler http.Handler) (*closeSignallingListener, context.CancelFunc, <-chan error) {
	t.Helper()

	inner, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listener := &closeSignallingListener{Listener: inner, closed: make(chan struct{})}

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	runErr := make(chan error, 1)
	go func() {
		runErr <- (&Server{Listener: listener, Handler: handler}).Run(ctx)
	}()

	return listener, cancel, runErr
}

// get sends GET / with header to listener, returning the response status and body.
func get(ctx context.Context, listener net.Listener, header http.Header) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+listener.Addr().String()+"/", nil)
	if err != nil {
		return 0, "", err
	}
	for name, values := range header {
		req.Header[name] = values
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)

	return resp.StatusCode, string(body), err
}

func requireRunReturns(t *testing.T, runErr <-chan error) error {
	t.Helper()

	select {
	case err := <-runErr:
		return err
	case <-time.After(runTimeout):
		require.FailNow(t, "server did not shut down after cancellation")
		return nil
	}
}

func servedHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "served")
	})
}

func TestServer_RunServesAndShutsDownOnCancel(t *testing.T) {
	t.Parallel()

	listener, cancel, runErr := startServer(t, servedHandler())

	status, body, err := get(t.Context(), listener, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "served", body)

	cancel()
	require.NoError(t, requireRunReturns(t, runErr))

	_, err = listener.Accept()
	require.ErrorIs(t, err, net.ErrClosed, "listener should be closed after shutdown")
}

func TestServer_ShutdownWaitsForInFlightRequest(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	listener, cancel, runErr := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "finished")
	}))

	type response struct {
		status int
		body   string
		err    error
	}
	responses := make(chan response, 1)
	go func() {
		status, body, err := get(t.Context(), listener, nil)
		responses <- response{status: status, body: body, err: err}
	}()

	<-started
	cancel()

	// Shutdown has begun once the listener is closed, and must then wait for the request.
	<-listener.closed
	select {
	case err := <-runErr:
		require.FailNow(t, "Run returned while a request was in flight", "error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	assert.Equal(t, response{status: http.StatusOK, body: "finished"}, <-responses)
	require.NoError(t, requireRunReturns(t, runErr))
}

func TestServer_HeaderSizeLimit(t *testing.T) {
	t.Parallel()

	listener, _, _ := startServer(t, servedHandler())

	cases := []struct {
		name       string
		headerSize int
		wantStatus int
	}{
		// An Authorization header with the largest Auth0 token the validator parses.
		{name: "large_token", headerSize: 8 << 10, wantStatus: http.StatusOK},
		{name: "oversized", headerSize: 2 * maxHeaderBytes, wantStatus: http.StatusRequestHeaderFieldsTooLarge},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			status, _, err := get(t.Context(), listener,
				http.Header{"Authorization": {"Bearer " + strings.Repeat("a", tc.headerSize)}})

			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, status)
		})
	}
}

func TestServer_RunReturnsServeError(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, listener.Close())

	s := &Server{Listener: listener, Handler: http.NotFoundHandler()}

	assert.ErrorContains(t, s.Run(t.Context()), "serving HTTP")
}

// TestWithRequestBudget requires every request's context to end within RequestBudget.
func TestWithRequestBudget(t *testing.T) {
	t.Parallel()

	var deadline time.Time
	var hasDeadline bool
	handler := withRequestBudget(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		deadline, hasDeadline = r.Context().Deadline()
	}))
	start := time.Now()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	end := time.Now()

	require.True(t, hasDeadline)
	assert.WithinRange(t, deadline, start.Add(RequestBudget), end.Add(RequestBudget))
}
