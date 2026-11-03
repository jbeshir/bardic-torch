package turnstile

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cloudflare's documented test secret keys and the dummy token the test site keys produce. Siteverify answers a
// test secret the same way whatever the token.
const (
	passSecret      = "1x0000000000000000000000000000000AA"
	failSecret      = "2x0000000000000000000000000000000AA"
	spentSecret     = "3x0000000000000000000000000000000AA"
	dummyToken      = "XXXX.DUMMY.TOKEN.XXXX"
	siteHostname    = "bardictorch.org"
	previewHostname = "preview.bardictorch.org"
)

// otherSecret is a secret the fake siteverify answers with the case's own answer.
const otherSecret = "other"

// The levels of the records the verifier logs, as the JSON handler writes them.
var (
	infoLevel  = slog.LevelInfo.String()
	warnLevel  = slog.LevelWarn.String()
	errorLevel = slog.LevelError.String()
)

// productionHostnames is the allowlist production sets.
var productionHostnames = []string{siteHostname, previewHostname}

// passing is the answer to a token that passes, minted on hostname for action.
func passing(hostname, action string) string {
	return `{"success":true,"challenge_ts":"2026-11-03T09:00:00.000Z","hostname":"` + hostname +
		`","error-codes":[],"action":"` + action + `","cdata":""}`
}

// failing is the answer to a token that fails with codes.
func failing(codes ...string) string {
	quoted := make([]string, len(codes))
	for i, code := range codes {
		quoted[i] = strconv.Quote(code)
	}
	return `{"success":false,"error-codes":[` + strings.Join(quoted, ",") + `]}`
}

// fakeSiteverify is a loopback siteverify. It answers Cloudflare's test secrets as Cloudflare does: passSecret
// passes (with hostname and action, as a real widget's token would carry them), failSecret fails with
// invalid-input-response and spentSecret with timeout-or-duplicate. Any other secret is answered with other.
// It records every form it receives.
type fakeSiteverify struct {
	hostname, action, other string

	mu    sync.Mutex
	forms []url.Values
}

func (f *fakeSiteverify) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.forms = append(f.forms, r.PostForm)
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch r.PostForm.Get("secret") {
	case passSecret:
		_, _ = w.Write([]byte(passing(f.hostname, f.action)))
	case failSecret:
		_, _ = w.Write([]byte(failing("invalid-input-response")))
	case spentSecret:
		_, _ = w.Write([]byte(failing("timeout-or-duplicate")))
	default:
		_, _ = w.Write([]byte(f.other))
	}
}

// logRecords collects the records logged through it as JSON lines.
type logRecords struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logRecords) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// levels returns the level of every record, in order.
func (l *logRecords) levels(t *testing.T) []string {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var levels []string
	for line := range strings.Lines(l.buf.String()) {
		var record struct {
			Level string `json:"level"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		levels = append(levels, record.Level)
	}
	return levels
}

func (l *logRecords) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// loggingContext returns a context whose logger writes every record, at every level, to records.
func loggingContext(t *testing.T, records *logRecords) context.Context {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(records, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return domain.ContextWithLogger(t.Context(), logger)
}

// verifierFor returns a Verifier with secret and hostnames that calls handler on a loopback server.
func verifierFor(t *testing.T, handler http.Handler, secret string, hostnames []string, client *http.Client) *Verifier {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	if client == nil {
		client = server.Client()
	}
	v := New(secret, hostnames, client)
	v.endpoint = server.URL
	return v
}

func TestVerifyTurnstile_Outcomes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		secret    string
		hostnames []string
		hostname  string
		action    string
		other     string
		want      error
		// wantLevel is the level of the one record logged, or empty for none.
		wantLevel string
	}{
		{name: "test_secret_passes", secret: passSecret, hostnames: productionHostnames, hostname: siteHostname,
			action: domain.TurnstileActionContact},
		{name: "second_allowlisted_hostname_passes", secret: passSecret, hostnames: productionHostnames,
			hostname: previewHostname, action: domain.TurnstileActionContact},
		{name: "empty_allowlist_skips_hostname_check", secret: passSecret, hostname: "example.com",
			action: domain.TurnstileActionContact},
		{name: "test_secret_fails", secret: failSecret, hostnames: productionHostnames,
			want: datasources.ErrTurnstileRejected, wantLevel: infoLevel},
		{name: "test_secret_spent", secret: spentSecret, hostnames: productionHostnames,
			want: datasources.ErrTurnstileRejected, wantLevel: infoLevel},
		{name: "missing_input_response", secret: otherSecret, other: failing("missing-input-response"),
			want: datasources.ErrTurnstileRejected, wantLevel: infoLevel},
		{name: "several_user_codes", secret: otherSecret, other: failing("invalid-input-response", "timeout-or-duplicate"),
			want: datasources.ErrTurnstileRejected, wantLevel: infoLevel},
		{name: "hostname_outside_allowlist", secret: passSecret, hostnames: productionHostnames,
			hostname: "example.com", action: domain.TurnstileActionContact,
			want: datasources.ErrTurnstileRejected, wantLevel: warnLevel},
		{name: "action_mismatch", secret: passSecret, hostnames: productionHostnames, hostname: siteHostname,
			action: "login", want: datasources.ErrTurnstileRejected, wantLevel: warnLevel},
		{name: "action_missing", secret: passSecret, hostname: siteHostname,
			want: datasources.ErrTurnstileRejected, wantLevel: warnLevel},
		{name: "invalid_input_secret", secret: otherSecret, other: failing("invalid-input-secret"),
			want: datasources.ErrTurnstileUnavailable, wantLevel: errorLevel},
		{name: "missing_input_secret", secret: otherSecret, other: failing("missing-input-secret"),
			want: datasources.ErrTurnstileUnavailable, wantLevel: errorLevel},
		{name: "internal_error", secret: otherSecret, other: failing("internal-error"),
			want: datasources.ErrTurnstileUnavailable, wantLevel: errorLevel},
		{name: "bad_request", secret: otherSecret, other: failing("bad-request"),
			want: datasources.ErrTurnstileUnavailable, wantLevel: errorLevel},
		{name: "unknown_code", secret: otherSecret, other: failing("a-code-from-the-future"),
			want: datasources.ErrTurnstileUnavailable, wantLevel: errorLevel},
		{name: "user_and_server_codes", secret: otherSecret, other: failing("invalid-input-response", "internal-error"),
			want: datasources.ErrTurnstileUnavailable, wantLevel: errorLevel},
		{name: "failure_without_codes", secret: otherSecret, other: failing(),
			want: datasources.ErrTurnstileUnavailable, wantLevel: errorLevel},
		{name: "malformed_json", secret: otherSecret, other: `{"success":`,
			want: datasources.ErrTurnstileUnavailable, wantLevel: warnLevel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeSiteverify{hostname: tc.hostname, action: tc.action, other: tc.other}
			records := &logRecords{}
			err := verifierFor(t, fake, tc.secret, tc.hostnames, nil).
				VerifyTurnstile(loggingContext(t, records), dummyToken)

			if tc.want == nil {
				require.NoError(t, err)
				assert.Empty(t, records.levels(t))
			} else {
				require.ErrorIs(t, err, tc.want)
				assert.Equal(t, []string{tc.wantLevel}, records.levels(t))
			}
			assert.NotContains(t, records.String(), dummyToken)
			assert.NotContains(t, records.String(), tc.secret)
			if err != nil {
				assert.NotContains(t, err.Error(), dummyToken)
				assert.NotContains(t, err.Error(), tc.secret)
			}
		})
	}
}

func TestVerifyTurnstile_SendsOnlySecretAndResponse(t *testing.T) {
	t.Parallel()

	fake := &fakeSiteverify{hostname: siteHostname, action: domain.TurnstileActionContact}
	var contentType string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Empty(t, r.URL.RawQuery)
		fake.ServeHTTP(w, r)
	})
	err := verifierFor(t, handler, passSecret, productionHostnames, nil).VerifyTurnstile(t.Context(), dummyToken)

	require.NoError(t, err)
	assert.Equal(t, "application/x-www-form-urlencoded", contentType)
	require.Len(t, fake.forms, 1)
	assert.Equal(t, url.Values{"secret": {passSecret}, "response": {dummyToken}}, fake.forms[0],
		"no remoteip and no idempotency_key")
}

func TestVerifyTurnstile_TransportFailuresAreUnavailable(t *testing.T) {
	t.Parallel()

	statusHandler := func(status int) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(passing(siteHostname, domain.TurnstileActionContact)))
		})
	}
	oversized := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"hostname":"bardictorch.org","action":"contact","cdata":"` +
			strings.Repeat("a", maxResponseBytes) + `"}`))
	})
	// blocked answers once the test releases it, after the client has given up.
	blocked := func(release <-chan struct{}) http.Handler {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release })
	}
	fixed := func(h http.Handler) func(<-chan struct{}) http.Handler {
		return func(<-chan struct{}) http.Handler { return h }
	}

	cases := []struct {
		name    string
		handler func(release <-chan struct{}) http.Handler
		client  *http.Client
	}{
		{name: "status_500", handler: fixed(statusHandler(http.StatusInternalServerError))},
		{name: "status_502", handler: fixed(statusHandler(http.StatusBadGateway))},
		{name: "status_429", handler: fixed(statusHandler(http.StatusTooManyRequests))},
		{name: "status_204", handler: fixed(statusHandler(http.StatusNoContent))},
		{name: "oversized_body", handler: fixed(oversized)},
		{name: "timeout", handler: blocked, client: &http.Client{Timeout: 50 * time.Millisecond}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			release := make(chan struct{})
			v := verifierFor(t, tc.handler(release), passSecret, productionHostnames, tc.client)
			t.Cleanup(func() { close(release) }) // before the server closes, which waits for its handlers
			records := &logRecords{}
			err := v.VerifyTurnstile(loggingContext(t, records), dummyToken)

			require.ErrorIs(t, err, datasources.ErrTurnstileUnavailable)
			assert.Equal(t, []string{warnLevel}, records.levels(t))
			assert.NotContains(t, records.String(), dummyToken)
			assert.NotContains(t, records.String(), passSecret)
		})
	}
}

func TestVerifyTurnstile_UnreachableIsUnavailable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	endpoint := server.URL
	server.Close()
	v := New(passSecret, productionHostnames, server.Client())
	v.endpoint = endpoint

	require.ErrorIs(t, v.VerifyTurnstile(t.Context(), dummyToken), datasources.ErrTurnstileUnavailable)
}

func TestVerifyTurnstile_ConcurrencyLimit(t *testing.T) {
	t.Parallel()

	arrived := make(chan struct{}, domain.MaxConcurrentTurnstileVerifications)
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrived <- struct{}{}
		<-release
		_, _ = w.Write([]byte(passing(siteHostname, domain.TurnstileActionContact)))
	})
	v := verifierFor(t, handler, passSecret, productionHostnames, nil)
	releaseAll := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseAll) // before the server closes, which waits for its handlers

	results := make(chan error, domain.MaxConcurrentTurnstileVerifications)
	for range domain.MaxConcurrentTurnstileVerifications {
		go func() { results <- v.VerifyTurnstile(t.Context(), dummyToken) }()
	}
	for range domain.MaxConcurrentTurnstileVerifications {
		<-arrived
	}

	// Every slot is held by a call blocked in the handler, so the ninth is refused at once.
	require.ErrorIs(t, v.VerifyTurnstile(t.Context(), dummyToken), datasources.ErrTurnstileUnavailable)

	releaseAll()
	for range domain.MaxConcurrentTurnstileVerifications {
		require.NoError(t, <-results)
	}
	require.NoError(t, v.VerifyTurnstile(t.Context(), dummyToken), "slots are freed when calls finish")
}
