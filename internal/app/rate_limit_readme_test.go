package app

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/ratelimit"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readmeRateLimit is a tier and the operations it limits, with the words the README gives them.
type readmeRateLimit struct {
	tier       ratelimit.Tier
	operations string
	// covers are the operations the spec documents 429 for that the tier limits.
	covers []openapitest.Operation
}

// readmeRateLimits are the README's rate limit table, in its order, with each tier as DefaultTiers has it.
func readmeRateLimits() []readmeRateLimit {
	tiers := ratelimit.DefaultTiers()
	post := func(path string) openapitest.Operation {
		return openapitest.Operation{Method: http.MethodPost, Path: path}
	}
	op := func(method, path string) openapitest.Operation {
		return openapitest.Operation{Method: method, Path: path}
	}

	return []readmeRateLimit{
		{tier: tiers.Contact, operations: "`POST /v1/contact`, from a client IP the FE attributed",
			covers: []openapitest.Operation{post("/v1/contact")}},
		{tier: tiers.ContactUnattributed, operations: "`POST /v1/contact`, from any other client IP",
			covers: []openapitest.Operation{post("/v1/contact")}},
		{tier: tiers.LinkReport, operations: "`POST /v1/link-reports`",
			covers: []openapitest.Operation{post("/v1/link-reports")}},
		{tier: tiers.DraftWrite, operations: "`POST /v1/drafts`, `PUT /v1/drafts/{draft_id}`",
			covers: []openapitest.Operation{post("/v1/drafts"), op(http.MethodPut, "/v1/drafts/{draft_id}")}},
		{tier: tiers.RevisionWrite, operations: "`POST /v1/revisions`, `POST /v1/revisions/{revision_id}/withdraw`, " +
			"`POST /v1/revisions/{revision_id}/revert`",
			covers: []openapitest.Operation{post("/v1/revisions"), post("/v1/revisions/{revision_id}/withdraw"),
				post("/v1/revisions/{revision_id}/revert")}},
		{tier: tiers.RepertoireWrite, operations: "`PUT /v1/me/repertoire/{slug}`, `DELETE /v1/me/repertoire/{slug}`",
			covers: []openapitest.Operation{op(http.MethodPut, "/v1/me/repertoire/{slug}"),
				op(http.MethodDelete, "/v1/me/repertoire/{slug}")}},
		{tier: tiers.ProfileWrite, operations: "`PATCH /v1/me`",
			covers: []openapitest.Operation{op(http.MethodPatch, "/v1/me")}},
		{tier: tiers.MCP, operations: "`POST /v1/mcp`", covers: []openapitest.Operation{post("/v1/mcp")}},
	}
}

// refillWords says how often a tier gains a request: "5 an hour", or "30 a minute" for a tier that refills faster.
func refillWords(interval time.Duration) string {
	if interval >= time.Minute {
		return fmt.Sprintf("%d an hour", time.Hour/interval)
	}

	return fmt.Sprintf("%d a minute", time.Minute/interval)
}

// TestRateLimitTable_MatchesReadme checks that the README's rate limit table lists every tier of
// ratelimit.DefaultTiers with its burst and refill, and that the tiers cover every operation the spec documents 429
// for.
func TestRateLimitTable_MatchesReadme(t *testing.T) {
	t.Parallel()

	readme, err := fs.ReadFile(repositoryRoot(), "README.md")
	require.NoError(t, err)
	_, section, found := strings.Cut(string(readme), "\n### Rate limits\n")
	require.True(t, found, "the README has no Rate limits section")
	section, _, _ = strings.Cut(section, "\n## ")

	var listed []string
	for line := range strings.Lines(section) {
		if strings.HasPrefix(line, "| `") {
			listed = append(listed, strings.TrimRight(line, "\n"))
		}
	}

	var want []string
	covered := map[openapitest.Operation]bool{}
	for _, limit := range readmeRateLimits() {
		want = append(want, fmt.Sprintf("| `%s` | %s | %d | %s |", limit.tier.Name, limit.operations, limit.tier.Burst,
			refillWords(limit.tier.Interval)))
		for _, o := range limit.covers {
			covered[o] = true
		}
	}
	assert.Equal(t, want, listed, "the README's rate limit table must be:\n%s", strings.Join(want, "\n"))

	contract := openapitest.New(t)
	for _, o := range contract.Operations() {
		if contract.Documents(o, http.StatusTooManyRequests) {
			assert.True(t, covered[o], "%s %s documents 429 but no tier of the README table limits it", o.Method, o.Path)
		}
	}
	for o := range covered {
		assert.True(t, contract.Documents(o, http.StatusTooManyRequests), "%s %s is in the table but documents no 429", o.Method, o.Path)
	}
}
