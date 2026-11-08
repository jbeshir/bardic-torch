package router

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/ratelimit"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/mcp"
	"github.com/jbeshir/bardic-torch/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	limitedAlice   = "auth0|alice"
	limitedBryony  = "auth0|bryony"
	limitedBlocked = "auth0|blocked"
	// limitedInterval is every test tier's interval; each has a burst of 1.
	limitedInterval = time.Minute
	// limitedProxySecret is the FE proxy secret of the rate-limit fixture.
	limitedProxySecret = "a-proxy-secret-for-tests"
	// limitedProxy is an address in the fixture's trusted proxy network.
	limitedProxy = "172.17.0.1:41000"

	rateLimitedBody = `{"code":"rate_limited",` +
		`"message":"too many requests; retry after the number of seconds in the Retry-After header"}`
	rateLimitedLog = "request rate limited"
)

// limitClock is the rate-limit fixture's clock, moved by hand.
type limitClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *limitClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *limitClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// testRateLimits returns a limiter for every tier, each allowing tier, timed by now; each limiter is named after its
// RateLimits field.
func testRateLimits(burst int, now func() time.Time) RateLimits {
	tier := func(name string) *ratelimit.Limiter {
		return ratelimit.New(ratelimit.Tier{Name: name, Burst: burst, Interval: limitedInterval, MaxKeys: 100}, now)
	}
	return RateLimits{
		Contact: tier("Contact"), ContactUnattributed: tier("ContactUnattributed"), LinkReport: tier("LinkReport"),
		DraftWrite: tier("DraftWrite"), RevisionWrite: tier("RevisionWrite"), RepertoireWrite: tier("RepertoireWrite"),
		ProfileWrite: tier("ProfileWrite"), MCP: tier("MCP"),
	}
}

// unlimitedRateLimits are rate limits the routing tests never reach.
func unlimitedRateLimits() RateLimits {
	return testRateLimits(1<<30, time.Now)
}

// unavailableCommand counts its calls and fails each as if the database could not be reached, which every
// rate-limited operation answers with a documented 503.
type unavailableCommand[Req, Res any] struct{ calls *atomic.Int64 }

func (c unavailableCommand[Req, Res]) Execute(context.Context, Req) (Res, error) {
	c.calls.Add(1)
	var zero Res
	return zero, fmt.Errorf("writing: %w", datasources.ErrUnavailable)
}

// limitFixture is a router whose rate-limited operations count the requests that reach them.
type limitFixture struct {
	clock   *limitClock
	limits  RateLimits
	calls   atomic.Int64
	handler http.Handler
}

func newLimitFixture() *limitFixture {
	f := &limitFixture{clock: &limitClock{now: testNow}}
	f.limits = testRateLimits(1, f.clock.Now)
	f.handler = New(f.dependencies())
	return f
}

func (f *limitFixture) dependencies() Dependencies {
	calls := &f.calls
	blockedAt := testNow
	return Dependencies{
		// "Bearer token:<user ID>" authenticates as the user with an API token, any other "Bearer <user ID>" with
		// Auth0.
		AuthValidators: []AuthValidator{func(r *http.Request) (*Identity, error) {
			credential := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if userID, ok := strings.CutPrefix(credential, "token:"); ok {
				return &Identity{UserID: userID, Method: domain.AuthMethodAPIToken}, nil
			}
			return &Identity{UserID: credential, Method: domain.AuthMethodAuth0}, nil
		}},
		ResolveUser: resolveUserFunc(func(_ context.Context, userID string) (domain.User, error) {
			user := domain.User{ID: userID, DisplayName: testUser.DisplayName, Role: domain.RoleContributor, CreatedAt: testNow}
			if userID == limitedBlocked {
				user.BlockedAt = &blockedAt
			}
			return user, nil
		}),
		UpdateDisplayName:     unavailableCommand[command.UpdateDisplayNameRequest, domain.User]{calls},
		SaveDraft:             unavailableCommand[command.SaveDraftRequest, command.SavedDraft]{calls},
		UpdateDraft:           unavailableCommand[command.UpdateDraftRequest, command.DraftResult]{calls},
		SubmitRevision:        unavailableCommand[command.SubmitRevisionRequest, command.SubmittedRevision]{calls},
		WithdrawRevision:      unavailableCommand[command.RevisionRequest, domain.MyRevision]{calls},
		RevertTo:              unavailableCommand[command.RevertRequest, command.SubmittedRevision]{calls},
		ReportLink:            unavailableCommand[command.ReportLinkRequest, command.ReportedLink]{calls},
		SubmitContactRequest:  unavailableCommand[command.ContactSubmission, int64]{calls},
		PutRepertoireEntry:    unavailableCommand[command.PutRepertoireEntryRequest, command.PutRepertoireEntryResult]{calls},
		RemoveRepertoireEntry: unavailableCommand[command.RepertoireEntryRequest, command.Empty]{calls},
		MCPTools:              testMCPTools(),
		RateLimits:            f.limits,
		ClientIP: ClientIPConfig{
			TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}, ProxySecret: limitedProxySecret,
		},
		Now: f.clock.Now,
	}
}

// limitedCaller is who a rate-limit case's request is from.
type limitedCaller int

const (
	// callerFirst is the first caller.
	callerFirst limitedCaller = iota
	// callerFirstAgain shares the first caller's key another way: the same user with another credential, or
	// another client IP of the same key.
	callerFirstAgain
	// callerSecond has a key of its own.
	callerSecond
	// callerBlocked is a blocked user, for the routes limited by user.
	callerBlocked
)

// asUser makes a request from the user of caller.
func asUser(req *http.Request, caller limitedCaller) {
	credential := map[limitedCaller]string{
		callerFirst: limitedAlice, callerFirstAgain: "token:" + limitedAlice, callerSecond: limitedBryony,
		callerBlocked: limitedBlocked,
	}[caller]
	req.Header.Set("Authorization", "Bearer "+credential)
}

// asPeer returns a function making a request directly from the peer of each caller, unattributed.
func asPeer(first, firstAgain, second string) func(*http.Request, limitedCaller) {
	return func(req *http.Request, caller limitedCaller) {
		req.RemoteAddr = map[limitedCaller]string{callerFirst: first, callerFirstAgain: firstAgain, callerSecond: second}[caller]
	}
}

// asAttributed makes a request through the trusted proxy from the FE, attributed to the client IP of caller.
func asAttributed(req *http.Request, caller limitedCaller) {
	req.RemoteAddr = limitedProxy
	req.Header.Set(forwardedForHeader, nginxEntry)
	req.Header.Set(proxyProofHeader, limitedProxySecret)
	req.Header.Set(clientIPHeader, map[limitedCaller]string{
		callerFirst: attributedIP, callerFirstAgain: "::ffff:203.0.113.7", callerSecond: "203.0.113.8",
	}[caller])
}

// rateLimitCase is one rate-limited operation and how to make its requests.
type rateLimitCase struct {
	name     string
	method   string
	template string
	target   string
	body     string
	// tier is the limiter the operation spends from, and other one it must not.
	tier, other func(RateLimits) *ratelimit.Limiter
	as          func(*http.Request, limitedCaller)
	byUser      bool
	// reached is the status of a request that reaches the operation, if not the 503 of its unavailable command.
	reached int
}

// reachedStatus is the status of a request that reaches the operation.
func (tc rateLimitCase) reachedStatus() int {
	return cmp.Or(tc.reached, http.StatusServiceUnavailable)
}

// asTokenUser makes an MCP request from the user of caller, with an API token: the MCP server takes no other
// credential.
func asTokenUser(req *http.Request, caller limitedCaller) {
	userID := map[limitedCaller]string{
		callerFirst: limitedAlice, callerFirstAgain: limitedAlice, callerSecond: limitedBryony,
		callerBlocked: limitedBlocked,
	}[caller]
	req.Header.Set("Authorization", "Bearer token:"+userID)
	req.Header.Set("Accept", "application/json, text/event-stream")
}

// testMCPTools are the MCP tools of the embedded spec.
var testMCPTools = sync.OnceValue(func() *mcp.Toolset {
	tools, err := mcp.NewToolset(openapi.Spec)
	if err != nil {
		panic(err)
	}
	return tools
})

const limitedRepertoireEntry = "/v1/me/repertoire/the-copper-kettle"

func rateLimitCases() []rateLimitCase {
	profile := func(l RateLimits) *ratelimit.Limiter { return l.ProfileWrite }
	draft := func(l RateLimits) *ratelimit.Limiter { return l.DraftWrite }
	revision := func(l RateLimits) *ratelimit.Limiter { return l.RevisionWrite }
	linkReport := func(l RateLimits) *ratelimit.Limiter { return l.LinkReport }
	repertoire := func(l RateLimits) *ratelimit.Limiter { return l.RepertoireWrite }
	mcpTier := func(l RateLimits) *ratelimit.Limiter { return l.MCP }
	contact := func(l RateLimits) *ratelimit.Limiter { return l.Contact }
	unattributed := func(l RateLimits) *ratelimit.Limiter { return l.ContactUnattributed }
	const event = `{"name":"Midwinter Revel","kind":"event","description_md":null}`
	const contactBody = `{"kind":"general","email":"a@b","message":"Hello.","turnstile_token":"t"}`

	byUser := func(name, method, template, target, body string, tier func(RateLimits) *ratelimit.Limiter) rateLimitCase {
		return rateLimitCase{name: name, method: method, template: template, target: target, body: body, tier: tier,
			other: profile, as: asUser, byUser: true}
	}
	cases := []rateLimitCase{
		byUser("update me", http.MethodPatch, "/v1/me", "/v1/me", `{"display_name":"Bob"}`, profile),
		byUser("create draft", http.MethodPost, "/v1/drafts", "/v1/drafts",
			`{"entity_type":"event","entity":{"slug":"midwinter-revel","collection_id":null},"base_revision_id":46,`+
				`"document":`+event+`,"summary":"Describe the revel."}`, draft),
		byUser("update draft", http.MethodPut, draftPath, "/v1/drafts/51",
			`{"entity_type":"event","document":`+event+`,"summary":"Describe the revel."}`, draft),
		byUser("submit revision", http.MethodPost, "/v1/revisions", "/v1/revisions",
			`{"draft_id":51,"acknowledge_duplicates":false,"acknowledged_flags":[]}`, revision),
		byUser("withdraw revision", http.MethodPost, "/v1/revisions/{revision_id}/withdraw",
			"/v1/revisions/51/withdraw", "", revision),
		byUser("revert revision", http.MethodPost, "/v1/revisions/{revision_id}/revert", "/v1/revisions/51/revert",
			`{"summary":null,"acknowledged_flags":[]}`, revision),
		byUser("report link", http.MethodPost, "/v1/link-reports", "/v1/link-reports",
			`{"url":"https://example.org/the-copper-kettle","reason":"broken","message":"The video is gone."}`, linkReport),
		byUser("put repertoire entry", http.MethodPut, repertoireEntryPath, limitedRepertoireEntry,
			`{"notes":null,"visibility":"private"}`, repertoire),
		byUser("delete repertoire entry", http.MethodDelete, repertoireEntryPath, limitedRepertoireEntry, "",
			repertoire),
		// A tool call reaches the MCP server, which answers 200 with the tool's error; the call also spends the
		// tier of the route it calls, which allows as many.
		{name: "mcp", method: http.MethodPost, template: routingMCP, target: routingMCP,
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/call",` +
				`"params":{"name":"withdraw_revision","arguments":{"revision_id":51}}}`,
			tier: mcpTier, other: profile, as: asTokenUser, byUser: true, reached: http.StatusOK},
		{name: "contact attributed", method: http.MethodPost, template: routingContact, target: routingContact,
			body: contactBody, tier: contact, other: unattributed, as: asAttributed},
		{name: "contact unattributed IPv4", method: http.MethodPost, template: routingContact, target: routingContact,
			body: contactBody, tier: unattributed, other: contact,
			as: asPeer("198.51.100.7:4711", "[::ffff:198.51.100.7]:4712", "198.51.100.8:4711")},
		{name: "contact unattributed IPv6 /56", method: http.MethodPost, template: routingContact,
			target: routingContact, body: contactBody, tier: unattributed, other: contact,
			as: asPeer("[2001:db8:1:100::1]:4711", "[2001:db8:1:1ff::9]:4711", "[2001:db8:1:200::1]:4711")},
		{name: "contact from the proxy without the secret", method: http.MethodPost, template: routingContact,
			target: routingContact, body: contactBody, tier: unattributed, other: contact,
			as: func(req *http.Request, caller limitedCaller) {
				asAttributed(req, caller)
				req.Header.Del(proxyProofHeader)
				req.Header.Set(forwardedForHeader, map[limitedCaller]string{
					callerFirst: "192.0.2.7", callerFirstAgain: "10.0.0.1, 192.0.2.7", callerSecond: "192.0.2.8",
				}[caller])
			}},
	}

	return cases
}

func (tc rateLimitCase) request(t *testing.T, caller limitedCaller) *http.Request {
	t.Helper()

	var req *http.Request
	if tc.body == "" {
		req = httptest.NewRequestWithContext(t.Context(), tc.method, tc.target, nil)
	} else {
		req = httptest.NewRequestWithContext(t.Context(), tc.method, tc.target, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", jsonContentType)
	}
	tc.as(req, caller)
	return req
}

// key is the rate-limit key of the first caller's requests.
func (tc rateLimitCase) key(t *testing.T) string {
	t.Helper()

	if tc.byUser {
		return limitedAlice
	}
	ip, _ := ClientIP(tc.request(t, callerFirst), ClientIPConfig{
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}, ProxySecret: limitedProxySecret,
	})
	return clientIPKey(ip)
}

// TestRouter_RateLimitTiers checks every rate-limited route through the contract helper, with tiers of a burst of
// 1: the first request reaches the operation; the next from the same key, by another credential or client IP of
// the key, is refused with 429 and the Retry-After of the tier, without reaching it, and with a body naming neither
// the tier nor the key; another key is unaffected; the key is allowed again an interval later; a blocked user gets
// 403 without spending a token; and only the route's own tier is spent.
func TestRouter_RateLimitTiers(t *testing.T) {
	t.Parallel()

	contract := openapitest.New(t)
	for _, tc := range rateLimitCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLimitFixture()
			serve := func(caller limitedCaller, wantStatus int, wantCalls int64) *httptest.ResponseRecorder {
				t.Helper()
				rec := contract.Serve(t, f.handler, tc.request(t, caller))
				require.Equal(t, wantStatus, rec.Code, rec.Body.String())
				require.Equal(t, wantCalls, f.calls.Load(), "requests reaching the operation")
				return rec
			}

			serve(callerFirst, tc.reachedStatus(), 1)

			refused := serve(callerFirstAgain, http.StatusTooManyRequests, 1)
			assert.Equal(t, "60", refused.Header().Get("Retry-After"))
			assert.JSONEq(t, rateLimitedBody, refused.Body.String())
			key := tc.key(t)
			for _, leaked := range []string{tc.tier(f.limits).Name(), key, strings.TrimPrefix(key, "auth0|")} {
				assert.NotContains(t, refused.Body.String(), leaked, "the 429 body names the tier or key")
			}

			serve(callerSecond, tc.reachedStatus(), 2)

			f.clock.Advance(limitedInterval - time.Second)
			serve(callerFirst, http.StatusTooManyRequests, 2)
			f.clock.Advance(time.Second)
			serve(callerFirst, tc.reachedStatus(), 3)

			if tc.byUser {
				serve(callerBlocked, http.StatusForbidden, 3)
				serve(callerBlocked, http.StatusForbidden, 3)
				_, ok := tc.tier(f.limits).Allow(limitedBlocked)
				assert.True(t, ok, "a blocked user's request spent a token")
			}
			if tc.other(f.limits) != tc.tier(f.limits) {
				_, ok := tc.other(f.limits).Allow(key)
				assert.True(t, ok, "the request spent a token of another tier")
			}
		})
	}
}

// TestRouter_RateLimitCoverage checks that TestRouter_RateLimitTiers has a case for every operation the spec
// documents 429 for, and documents 429 for every operation it has a case for.
func TestRouter_RateLimitCoverage(t *testing.T) {
	t.Parallel()

	contract := openapitest.New(t)
	var tested []openapitest.Operation
	for _, tc := range rateLimitCases() {
		if op := (openapitest.Operation{Method: tc.method, Path: tc.template}); !slices.Contains(tested, op) {
			tested = append(tested, op)
		}
	}
	var documented []openapitest.Operation
	for _, op := range contract.Operations() {
		if contract.Documents(op, http.StatusTooManyRequests) {
			documented = append(documented, op)
		}
	}

	assert.ElementsMatch(t, documented, tested)
}

// logEntries decodes JSON log lines.
func logEntries(t *testing.T, logs string) []map[string]any {
	t.Helper()

	var entries []map[string]any
	for line := range strings.Lines(logs) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		entries = append(entries, entry)
	}
	return entries
}

// TestRateLimit_LogLineHoldsTierOnly checks that a refusal is logged with the tier and the request logger's
// attributes only: never the user ID or client IP it was keyed by.
func TestRateLimit_LogLineHoldsTierOnly(t *testing.T) {
	t.Parallel()

	for _, tc := range rateLimitCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLimitFixture()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			for _, caller := range []limitedCaller{callerFirst, callerFirstAgain} {
				req := tc.request(t, caller)
				f.handler.ServeHTTP(httptest.NewRecorder(), req.WithContext(domain.ContextWithLogger(req.Context(), logger)))
			}

			var refusals []map[string]any
			for _, entry := range logEntries(t, logs.String()) {
				if entry["msg"] == rateLimitedLog {
					refusals = append(refusals, entry)
				}
			}
			require.Len(t, refusals, 1)
			refusal := refusals[0]
			assert.Equal(t, tc.tier(f.limits).Name(), refusal["tier"])
			assert.Equal(t, tc.template, refusal["route"])
			attrs := make([]string, 0, len(refusal))
			for attr := range refusal {
				attrs = append(attrs, attr)
			}
			// The line adds only the tier to the request logger's attributes. For an authenticated request those hold the
			// user ID, as every line of the request does; a client IP is never among them.
			wantAttrs := []string{"time", "level", "msg", "method", "route", "request_id", "tier"}
			if tc.byUser {
				wantAttrs = append(wantAttrs, "user_id")
			}
			assert.ElementsMatch(t, wantAttrs, attrs)

			if !tc.byUser {
				line, err := json.Marshal(refusal)
				require.NoError(t, err)
				assert.NotContains(t, string(line), strings.Split(tc.key(t), "/")[0], "the line holds the client IP")
			}
		})
	}
}

// TestRouter_UnattributedContactWarnsOncePerMinute checks that contact requests the FE did not attribute are
// warned about at most once a minute, naming only the tier.
func TestRouter_UnattributedContactWarnsOncePerMinute(t *testing.T) {
	t.Parallel()

	f := newLimitFixture()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	contact := func(peer string, attributed bool) {
		req := httptest.NewRequestWithContext(domain.ContextWithLogger(t.Context(), logger), http.MethodPost,
			routingContact, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", jsonContentType)
		req.RemoteAddr = peer + ":4711"
		if attributed {
			req.Header.Set(proxyProofHeader, limitedProxySecret)
			req.Header.Set(clientIPHeader, peer)
		}
		f.handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	contact("198.51.100.1", false)
	f.clock.Advance(30 * time.Second)
	contact("198.51.100.2", false)
	contact("198.51.100.3", true)
	f.clock.Advance(30 * time.Second)
	contact("198.51.100.4", false)

	var warnings []map[string]any
	for _, entry := range logEntries(t, logs.String()) {
		if entry["level"] == "WARN" {
			warnings = append(warnings, entry)
		}
	}
	require.Len(t, warnings, 2)
	for _, warning := range warnings {
		assert.Equal(t, "ContactUnattributed", warning["tier"])
		assert.NotContains(t, fmt.Sprint(warning), "198.51.100")
	}
}
