package app

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/ratelimit"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// openTiers are the production tiers with a burst no test reaches, and their names.
func openTiers(t *testing.T) (ratelimit.Tiers, []string) {
	t.Helper()

	tiers := ratelimit.DefaultTiers()
	var names []string
	fields := reflect.ValueOf(&tiers).Elem()
	for i := range fields.NumField() {
		tier, ok := fields.Field(i).Interface().(ratelimit.Tier)
		require.True(t, ok)
		names = append(names, tier.Name)
		tier.Burst = 1 << 30
		fields.Field(i).Set(reflect.ValueOf(tier))
	}
	return tiers, names
}

// innerTierRows are the tools whose operations are rate limited, each with the tier of its route.
var innerTierRows = []struct {
	tool string
	op   openapitest.Operation
	tier func(*ratelimit.Tiers) *ratelimit.Tier
}{
	{"create_draft", openapitest.Operation{Method: http.MethodPost, Path: draftsPath}, draftWriteTier},
	{"update_draft", openapitest.Operation{Method: http.MethodPut, Path: draftPath}, draftWriteTier},
	{"submit_draft", openapitest.Operation{Method: http.MethodPost, Path: revisionsPath}, revisionWriteTier},
	{"withdraw_revision", openapitest.Operation{Method: http.MethodPost, Path: withdrawRoute},
		revisionWriteTier},
	{"revert_revision", openapitest.Operation{Method: http.MethodPost, Path: revertRoute},
		revisionWriteTier},
	{"report_link", openapitest.Operation{Method: http.MethodPost, Path: "/v1/link-reports"},
		func(t *ratelimit.Tiers) *ratelimit.Tier { return &t.LinkReport }},
	{"set_repertoire_entry", openapitest.Operation{Method: http.MethodPut, Path: repertoireEntryRoute},
		repertoireWriteTier},
	{"remove_repertoire_entry", openapitest.Operation{Method: http.MethodDelete, Path: repertoireEntryRoute},
		repertoireWriteTier},
}

func draftWriteTier(t *ratelimit.Tiers) *ratelimit.Tier      { return &t.DraftWrite }
func revisionWriteTier(t *ratelimit.Tiers) *ratelimit.Tier   { return &t.RevisionWrite }
func repertoireWriteTier(t *ratelimit.Tiers) *ratelimit.Tier { return &t.RepertoireWrite }

// unlimitedToolWrites are the tools' writing operations whose routes have no tier.
var unlimitedToolWrites = map[openapitest.Operation]bool{{Method: http.MethodDelete, Path: draftPath}: true}

// TestMCPInnerRequestsCountTowardRouteTiers checks, for every tool whose operation is rate limited, that a call
// spends the tier of the operation's route as a REST request would, with the mcp_user tier open: with a burst of
// one, the first call succeeds, the second is a tool error 429 with retry_after_seconds that names no tier, user or
// key and reaches no datasource, and the same user's REST request to the route is then limited too.
func TestMCPInnerRequestsCountTowardRouteTiers(t *testing.T) {
	t.Parallel()

	byOperation := toolsByOperation(t)
	rows := map[openapitest.Operation]bool{}
	for _, row := range innerTierRows {
		require.Equal(t, row.tool, byOperation[row.op].tool.Name, "%s %s", row.op.Method, row.op.Path)
		rows[row.op] = true
	}
	for op := range byOperation {
		if op.Method != http.MethodGet && !unlimitedToolWrites[op] {
			assert.True(t, rows[op], "%s %s writes, so it needs a row", op.Method, op.Path)
		}
	}

	for _, row := range innerTierRows {
		t.Run(row.tool, func(t *testing.T) {
			t.Parallel()

			tiers, names := openTiers(t)
			row.tier(&tiers).Burst = 1
			m := newLimitedHandlerMocks(t, rateLimits(tiers, handlerClock))
			matrix := authMatrix[row.op]
			to := byOperation[row.op]
			target := expandPath(row.op.Path, matrix.pathParams) + matrix.query
			call := func() mcpToolResult {
				m.expectUser(contributorID)
				m.expectUser(contributorID)
				rec := m.validate.Serve(t, m.handler, newMCPRequest(t.Context(), apiAuthContributor.authorization,
					mcpToolCallMessage(t, row.tool, toolArguments(t, to, target, matrix.body))))
				return decodeToolResult(t, rec)
			}

			matrix.expectSuccess(m, contributorID)
			first := call()
			require.False(t, first.IsError, first.text(t))
			calls := datasourceCalls(m)

			limited := call()
			toolError := limited.toolError(t)
			assert.Equal(t, string(httpjson.CodeRateLimited), toolError["code"])
			assert.InDelta(t, http.StatusTooManyRequests, toolError["status"], 0)
			seconds, ok := toolError["retry_after_seconds"].(float64)
			require.True(t, ok, "no retry_after_seconds: %s", limited.text(t))
			assert.GreaterOrEqual(t, seconds, float64(1))
			for _, private := range append(names, contributorID, "tier", "key") {
				assert.NotContains(t, limited.text(t), private)
			}
			assert.Equal(t, calls, datasourceCalls(m), "a limited call reached a datasource")

			m.expectUser(contributorID)
			req := httptestRequest(t, row.op.Method, target, matrix.body, auth0Contributor.authorization)
			rec := m.validate.Serve(t, m.handler, req)
			require.Equal(t, http.StatusTooManyRequests, rec.Code, "the route's tier is the user's, whichever way "+
				"they call it: %s", rec.Body.String())
		})
	}
}

// TestMCP_ServerTierLimitsToolCalls checks that the mcp_user tier limits tool calls before any is dispatched: with
// a burst of one, a second call of a read is a 429 from the route that reaches no operation.
func TestMCP_ServerTierLimitsToolCalls(t *testing.T) {
	t.Parallel()

	tiers, _ := openTiers(t)
	tiers.MCP.Burst = 1
	m := newLimitedHandlerMocks(t, rateLimits(tiers, handlerClock))
	op := openapitest.Operation{Method: http.MethodGet, Path: pieceRoute}
	matrix := authMatrix[op]
	message := mcpToolCallMessage(t, "get_piece",
		toolArguments(t, toolsByOperation(t)[op], expandPath(op.Path, matrix.pathParams), ""))

	m.expectUser(contributorID)
	matrix.expectSuccess(m, contributorID)
	first := decodeToolResult(t, m.validate.Serve(t, m.handler,
		newMCPRequest(t.Context(), apiAuthContributor.authorization, message)))
	require.False(t, first.IsError, first.text(t))

	m.expectUser(contributorID)
	rec := m.validate.Serve(t, m.handler, newMCPRequest(t.Context(), apiAuthContributor.authorization, message))
	require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	assert.NotContains(t, rec.Body.String(), "mcp_user")
}

// handlerClock is the handler tests' clock, for limiters that must not refill during a test.
func handlerClock() time.Time { return handlerNow }

// datasourceCalls counts the calls made to the datasources that write or read the caller's own data.
func datasourceCalls(m handlerMocks) int {
	n := 0
	for _, recorded := range writeAndOwnDataMocks(m) {
		n += len(recorded.Calls)
	}
	return n
}

// httptestRequest is a REST request with an optional JSON body, as authorization.
func httptestRequest(t *testing.T, method, target, body, authorization string) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", authorization)
	return req
}

// TestMCPBatchCannotPassServerTier checks that one request cannot buy more tool calls than the mcp_user tier
// allows by batching them: a JSON-RPC batch of one call more than the tier's burst, sent without a protocol header
// (which the MCP SDK would read as 2025-03-26, a version with batches), is refused before any call is dispatched.
func TestMCPBatchCannotPassServerTier(t *testing.T) {
	t.Parallel()

	tiers, _ := openTiers(t)
	tiers.MCP = ratelimit.DefaultTiers().MCP
	m := newLimitedHandlerMocks(t, rateLimits(tiers, handlerClock))
	op := openapitest.Operation{Method: http.MethodGet, Path: pieceRoute}
	matrix := authMatrix[op]
	message := mcpToolCallMessage(t, "get_piece",
		toolArguments(t, toolsByOperation(t)[op], expandPath(op.Path, matrix.pathParams), ""))
	calls := make([]string, tiers.MCP.Burst+1)
	for i := range calls {
		calls[i] = message
	}

	// The inner requests a batch would make each resolve the user and read the piece; none may happen.
	m.users.EXPECT().GetUser(mock.Anything, contributorID).Return(storedUser(contributorID), nil).Maybe()
	m.pieces.EXPECT().GetPublicPiece(mock.Anything, matrixSlug).Return(matrixPiece, nil).Maybe()
	req := newMCPRequest(t.Context(), apiAuthContributor.authorization, "["+strings.Join(calls, ",")+"]")
	req.Header.Del(mcpProtocolHeader)
	rec := m.validate.ServeResponseOnly(t, m.handler, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "%.200s", rec.Body.String())
	assert.JSONEq(t, `{"code":"invalid_request","message":"JSON-RPC batches are not supported"}`, rec.Body.String())
	assert.Empty(t, m.pieces.Calls, "a batched call reached its operation")
	assert.LessOrEqual(t, len(m.users.Calls), 1, "only the MCP request itself resolves the user")
}
