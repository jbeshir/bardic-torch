package router

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/ratelimit"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// RateLimits are the limiters of the rate-limited routes, one per tier (ratelimit.Tiers says what each limits).
type RateLimits struct {
	Contact             *ratelimit.Limiter
	ContactUnattributed *ratelimit.Limiter
	LinkReport          *ratelimit.Limiter
	DraftWrite          *ratelimit.Limiter
	RevisionWrite       *ratelimit.Limiter
	RepertoireWrite     *ratelimit.Limiter
	ProfileWrite        *ratelimit.Limiter
	MCP                 *ratelimit.Limiter
}

// rateLimitedMessage is the message of every 429 response. Like the rest of the response, it names neither the
// tier nor the key.
const rateLimitedMessage = "too many requests; retry after the number of seconds in the Retry-After header"

// unattributedWarningEvery is how often, at most, the contact form warns that it is limiting requests the FE did
// not attribute.
const unattributedWarningEvery = time.Minute

// limitedByUser lets a request through if its user has a token left in limiter, and otherwise answers 429. It
// must run behind requireAuth, which rejects anonymous and blocked users before they spend a token.
func limitedByUser(limiter *ratelimit.Limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// requireAuth has already checked that there is a principal.
		principal, _ := domain.PrincipalFromContext(r.Context())
		serveLimited(w, r, limiter, principal.User.ID, next)
	})
}

// limitedByClientIP lets a contact form request through if its client IP has a token left, and otherwise answers
// 429. A client IP the FE attributed spends from the attributed limiter; any other spends from the unattributed
// one, which allows more, as until the FE attributes its requests every user's are from the FE's IPs. The first
// unattributed request of a minute is logged at warn, naming only the tier.
func limitedByClientIP(
	limits RateLimits, cfg ClientIPConfig, now func() time.Time, next http.Handler,
) http.Handler {
	warning := &throttledWarning{every: unattributedWarningEvery, now: now}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, attributed := ClientIP(r, cfg)
		limiter := limits.Contact
		if !attributed {
			limiter = limits.ContactUnattributed
			if warning.due() {
				ctx := r.Context()
				domain.LoggerFromContext(ctx).WarnContext(ctx,
					"contact request limited by an unattributed client IP: the FE did not attribute it",
					"tier", limiter.Name())
			}
		}
		serveLimited(w, r, limiter, clientIPKey(ip), next)
	})
}

// serveLimited serves r with next if key has a token left in limiter. Otherwise it answers 429 rate_limited with a
// Retry-After header, and logs only the tier: never the key, which is a user ID or a client IP.
func serveLimited(w http.ResponseWriter, r *http.Request, limiter *ratelimit.Limiter, key string, next http.Handler) {
	retryAfter, ok := limiter.Allow(key)
	if ok {
		next.ServeHTTP(w, r)
		return
	}

	ctx := r.Context()
	domain.LoggerFromContext(ctx).InfoContext(ctx, "request rate limited", "tier", limiter.Name())
	w.Header().Set("Retry-After", strconv.Itoa(ratelimit.RetryAfterSeconds(retryAfter)))
	httpjson.WriteError(ctx, w, http.StatusTooManyRequests, httpjson.CodeRateLimited, rateLimitedMessage)
}

// throttledWarning decides when a repeated warning is logged: at most once every interval. It is safe for
// concurrent use.
type throttledWarning struct {
	every time.Duration
	now   func() time.Time

	mu     sync.Mutex
	logged time.Time
}

// due reports whether the warning should be logged now, and if so notes that it was.
func (t *throttledWarning) due() bool {
	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.logged.IsZero() && now.Sub(t.logged) < t.every {
		return false
	}
	t.logged = now
	return true
}
