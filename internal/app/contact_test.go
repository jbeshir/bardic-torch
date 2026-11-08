package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Cloudflare's always-passing test secret and the dummy token its test site keys produce.
const (
	testTurnstileSecret = "1x0000000000000000000000000000000AA"
	testTurnstileToken  = "XXXX.DUMMY.TOKEN.XXXX"
)

// siteverifyAnswer is a loopback siteverify's answer: its status and body.
type siteverifyAnswer struct {
	status int
	body   string
}

// passingAnswer is the answer to a token minted on hostname for the contact form.
func passingAnswer(hostname string) siteverifyAnswer {
	return siteverifyAnswer{status: http.StatusOK, body: `{"success":true,"hostname":"` + hostname +
		`","action":"contact","error-codes":[]}`}
}

// failingAnswer is the answer to a token that fails with code.
func failingAnswer(code string) siteverifyAnswer {
	return siteverifyAnswer{status: http.StatusOK, body: `{"success":false,"error-codes":["` + code + `"]}`}
}

// loopbackSiteverify starts a loopback server that answers every siteverify call with answer, and returns a client
// whose requests to Cloudflare's siteverify go to that server instead, and the forms the server received. No
// request reaches the network.
func loopbackSiteverify(t *testing.T, answer siteverifyAnswer) (*http.Client, func() []url.Values) {
	t.Helper()

	var (
		mu    sync.Mutex
		forms []url.Values
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, r.ParseForm())
		mu.Lock()
		forms = append(forms, r.PostForm)
		mu.Unlock()
		w.WriteHeader(answer.status)
		_, _ = w.Write([]byte(answer.body))
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	require.NoError(t, err)

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "https://challenges.cloudflare.com/turnstile/v0/siteverify", req.URL.String())
		loopback := req.Clone(req.Context())
		loopback.URL.Scheme, loopback.URL.Host = target.Scheme, target.Host
		return server.Client().Transport.RoundTrip(loopback)
	})}

	return client, func() []url.Values {
		mu.Lock()
		defer mu.Unlock()
		return forms
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// Not parallel: it sets environment variables.
func TestContactVerifierFromEnv_EmptySecretDisablesContact(t *testing.T) {
	t.Setenv(turnstileSecretKeyEnv, "")
	t.Setenv(turnstileHostnamesEnv, "bardictorch.org")
	client, forms := loopbackSiteverify(t, passingAnswer("bardictorch.org"))

	var logs bytes.Buffer
	ctx := domain.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))
	verifier := contactVerifierFromEnv(ctx, client)

	assert.Equal(t, contactDisabled{}, verifier)
	require.ErrorIs(t, verifier.VerifyTurnstile(t.Context(), testTurnstileToken), datasources.ErrTurnstileUnavailable)
	assert.Empty(t, forms(), "a disabled form calls no one")
	assert.Contains(t, logs.String(), `"level":"WARN","msg":"the contact form is disabled: TURNSTILE_SECRET_KEY is empty"`)
}

// Not parallel: it sets environment variables.
func TestContactVerifierFromEnv_ParsesHostnames(t *testing.T) {
	t.Setenv(turnstileSecretKeyEnv, testTurnstileSecret)
	t.Setenv(turnstileHostnamesEnv, "bardictorch.org, preview.bardictorch.org")

	for hostname, want := range map[string]error{
		"bardictorch.org":         nil,
		"preview.bardictorch.org": nil,
		"example.com":             datasources.ErrTurnstileRejected,
	} {
		client, forms := loopbackSiteverify(t, passingAnswer(hostname))
		err := contactVerifierFromEnv(t.Context(), client).VerifyTurnstile(t.Context(), testTurnstileToken)
		if want == nil {
			require.NoError(t, err, hostname)
		} else {
			require.ErrorIs(t, err, want, hostname)
		}
		assert.Equal(t, []url.Values{{"secret": {testTurnstileSecret}, "response": {testTurnstileToken}}}, forms())
	}
}

// Not parallel: it sets environment variables.
func TestContactVerifierFromEnv_EmptyHostnamesSkipsTheCheck(t *testing.T) {
	t.Setenv(turnstileSecretKeyEnv, testTurnstileSecret)
	t.Setenv(turnstileHostnamesEnv, "")
	client, _ := loopbackSiteverify(t, passingAnswer("example.com"))

	var logs bytes.Buffer
	ctx := domain.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))
	require.NoError(t, contactVerifierFromEnv(ctx, client).VerifyTurnstile(t.Context(), testTurnstileToken))
	assert.Equal(t, 1, strings.Count(logs.String(), "\n"), "one record")
	assert.Contains(t, logs.String(), `"level":"WARN","msg":"the contact form accepts Turnstile tokens minted on `+
		`any hostname: TURNSTILE_HOSTNAMES is empty, which only development should do"`)
}

// Not parallel: it sets environment variables.
func TestContactVerifierFromEnv_HostnamesIgnoreCase(t *testing.T) {
	t.Setenv(turnstileSecretKeyEnv, testTurnstileSecret)
	t.Setenv(turnstileHostnamesEnv, "BardicTorch.org")
	client, _ := loopbackSiteverify(t, passingAnswer("bardictorch.org"))

	var logs bytes.Buffer
	ctx := domain.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))
	require.NoError(t, contactVerifierFromEnv(ctx, client).VerifyTurnstile(t.Context(), testTurnstileToken))
	assert.Empty(t, logs.String(), "an allowlist logs nothing at startup")
}

// TestContactSubmit_NeverLogsPII sends contact requests through the production handler, with the production
// Turnstile verifier calling a loopback siteverify, and checks that no log record, at any level down to debug,
// holds the sender's name, email address, message, URL or token: on success and on every failure.
//
// Not parallel: it sets environment variables.
func TestContactSubmit_NeverLogsPII(t *testing.T) {
	errDatabase := errors.New("connection refused")
	cases := []struct {
		name   string
		secret string
		answer siteverifyAnswer
		// body makes the request body from the case's personal data; nil sends a valid body.
		body       func(name, email, message, url, token string) string
		createErr  error
		wantStatus int
		// wantCreate is whether the request reaches the store.
		wantCreate bool
	}{
		{name: "received", answer: passingAnswer("bardictorch.org"), wantStatus: http.StatusAccepted,
			wantCreate: true},
		{name: "invalid_email", wantStatus: http.StatusBadRequest,
			body: func(name, email, message, url, token string) string {
				return contactJSON(name, strings.ReplaceAll(email, "@", " "), message, url, token)
			}},
		{name: "invalid_url", wantStatus: http.StatusBadRequest,
			body: func(name, email, message, url, token string) string {
				return contactJSON(name, email, message, "ftp:"+url, token)
			}},
		{name: "malformed", wantStatus: http.StatusBadRequest,
			body: func(name, email, message, url, token string) string {
				return strings.TrimSuffix(contactJSON(name, email, message, url, token), "}") + `,`
			}},
		{name: "wrong_type", wantStatus: http.StatusBadRequest,
			body: func(name, email, message, url, token string) string {
				return strings.Replace(contactJSON(name, email, message, url, token), `"kind":"takedown"`,
					`"kind":["`+name+`"]`, 1)
			}},
		{name: "unknown_field", wantStatus: http.StatusBadRequest,
			body: func(name, email, message, url, token string) string {
				return strings.TrimSuffix(contactJSON(name, email, message, url, token), "}") +
					`,"phone":"` + name + `"}`
			}},
		{name: "too_large", wantStatus: http.StatusRequestEntityTooLarge,
			body: func(name, email, message, url, token string) string {
				return contactJSON(name, email, strings.Repeat(message, 2000), url, token)
			}},
		{name: "token_rejected", answer: failingAnswer("invalid-input-response"),
			wantStatus: http.StatusBadRequest},
		{name: "token_spent", answer: failingAnswer("timeout-or-duplicate"), wantStatus: http.StatusBadRequest},
		{name: "wrong_hostname", answer: passingAnswer("example.com"), wantStatus: http.StatusBadRequest},
		{name: "misconfigured_secret", answer: failingAnswer("invalid-input-secret"),
			wantStatus: http.StatusServiceUnavailable},
		{name: "cloudflare_failure", answer: siteverifyAnswer{status: http.StatusBadGateway},
			wantStatus: http.StatusServiceUnavailable},
		{name: "disabled", secret: "-", wantStatus: http.StatusServiceUnavailable},
		{name: "cap", answer: passingAnswer("bardictorch.org"), createErr: datasources.ErrContactRequestLimitReached,
			wantStatus: http.StatusServiceUnavailable, wantCreate: true},
		{name: "database_busy", answer: passingAnswer("bardictorch.org"), createErr: datasources.ErrUnavailable,
			wantStatus: http.StatusServiceUnavailable, wantCreate: true},
		{name: "database_failure", answer: passingAnswer("bardictorch.org"), createErr: errDatabase,
			wantStatus: http.StatusInternalServerError, wantCreate: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			secret := testTurnstileSecret
			if tc.secret == "-" {
				secret = ""
			}
			t.Setenv(turnstileSecretKeyEnv, secret)
			t.Setenv(turnstileHostnamesEnv, "bardictorch.org,preview.bardictorch.org")

			// Every case has its own personal data, so a value logged by one case cannot hide in another's.
			name, email := "Wren Placeholder "+tc.name, "wren."+tc.name+"@example.org"
			message := "The second verse of " + tc.name + " is credited to the wrong person."
			pageURL, token := "https://example.org/kettle-"+tc.name, "token-"+tc.name+"-"+testTurnstileToken
			body := contactJSON(name, email, message, pageURL, token)
			if tc.body != nil {
				body = tc.body(name, email, message, pageURL, token)
			}

			client, _ := loopbackSiteverify(t, tc.answer)
			creator := mocks.NewContactRequestCreator(t)
			if tc.wantCreate {
				creator.EXPECT().CreateContactRequest(mock.Anything, mock.Anything).Return(7, tc.createErr).Once()
			}
			var logs bytes.Buffer
			ctx := domain.ContextWithLogger(context.Background(),
				slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			handler := NewHTTPHandler(HTTPDependencies{
				ContactRequestCreator: creator,
				TurnstileVerifier:     contactVerifierFromEnv(ctx, client),
				Now:                   func() time.Time { return handlerNow },
			})

			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/contact", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := openapitest.New(t).ServeResponseOnly(t, handler, req)

			assert.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			records := logs.String()
			require.Contains(t, records, `"msg":"request completed"`, "nothing was logged, so this proves nothing")
			for _, pii := range []string{name, email, message, pageURL, token} {
				assert.NotContains(t, records, pii, "a log record holds the sender's data")
				assert.NotContains(t, rec.Body.String(), pii, "the response holds the sender's data")
			}
		})
	}
}

// contactJSON is the body of a takedown contact request with the given fields.
func contactJSON(name, email, message, url, token string) string {
	return `{"kind":"takedown","name":"` + name + `","email":"` + email + `","message":"` + message +
		`","url":"` + url + `","turnstile_token":"` + token + `"}`
}
