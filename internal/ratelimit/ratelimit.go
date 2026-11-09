// Package ratelimit limits how often a key, such as a user ID or a client IP, may do something: one token bucket
// per key, per tier, held in memory (docs/PLAN.md §5.1 "Rate limits").
//
// A bucket holds up to a tier's burst of tokens and gains one token every interval. Each allowed request spends
// one; a request finding the bucket empty is refused, and told how long until the next token. Every tier caps how
// many keys it holds, so memory stays bounded whatever keys clients send: a minute's sweep forgets the buckets
// that have refilled, which hold nothing a fresh bucket would not, and a new key arriving at the cap first sweeps,
// unless a sweep ran in the last second, and then, if the tier is still full, evicts the least recently seen of a
// sample of keys. A new key is never refused for want of room.
package ratelimit

import (
	"sync"
	"time"
)

// sweepEvery is how often a limiter forgets the buckets that have refilled.
const sweepEvery = time.Minute

// capSweepEvery is how often, at most, a limiter at its key cap sweeps to make room for a new key. A sweep visits
// every bucket under the limiter's mutex, and at a cap whose buckets are all spent it frees nothing, so a new key
// arriving sooner goes straight to eviction.
const capSweepEvery = time.Second

// evictionSample is how many keys a full limiter samples to choose the one to evict.
const evictionSample = 64

// Tier is how one class of requests is limited.
type Tier struct {
	// Name identifies the tier in logs. It is never a key.
	Name string
	// Burst is how many requests a key may make at once, the bucket's capacity. It is at least 1.
	Burst int
	// Interval is how long a bucket takes to gain one token: the tier's sustained rate is one request per
	// Interval. It is positive.
	Interval time.Duration
	// MaxKeys is how many keys the tier holds buckets for at most. It is at least 1.
	MaxKeys int
}

// Tiers are the tiers of the rate-limited routes.
type Tiers struct {
	// Contact limits contact form messages from a client IP the FE attributed.
	Contact Tier
	// ContactUnattributed limits contact form messages from any other client IP: one the API derived itself,
	// which is the FE's for every user until the FE attributes its requests.
	ContactUnattributed Tier
	// LinkReport limits a user's link reports.
	LinkReport Tier
	// DraftWrite limits a user's draft saves, which the FE makes as the user types.
	DraftWrite Tier
	// RevisionWrite limits a user's submissions, withdrawals and reverts.
	RevisionWrite Tier
	// RepertoireWrite limits a user's repertoire changes.
	RepertoireWrite Tier
	// ProfileWrite limits a user's profile changes.
	ProfileWrite Tier
	// MCP limits a user's MCP requests. Each tool call also spends the tier of the REST route it calls.
	MCP Tier
}

// The key caps: client IPs are cheap to vary, so the tiers keyed by them hold more keys than those keyed by user.
const (
	ipKeyCap   = 50_000
	userKeyCap = 10_000
)

// DefaultTiers returns the production tiers.
func DefaultTiers() Tiers {
	return Tiers{
		Contact:             perHour("contact_ip", 3, 5, ipKeyCap),
		ContactUnattributed: perHour("contact_unattributed_ip", 30, 60, ipKeyCap),
		LinkReport:          perHour("link_report_user", 10, 10, userKeyCap),
		DraftWrite:          Tier{Name: "draft_write_user", Burst: 60, Interval: time.Minute / 30, MaxKeys: userKeyCap},
		RevisionWrite:       perHour("revision_write_user", 30, 60, userKeyCap),
		RepertoireWrite: Tier{
			Name: "repertoire_write_user", Burst: 60, Interval: time.Minute / 60, MaxKeys: userKeyCap,
		},
		ProfileWrite: perHour("profile_write_user", 10, 10, userKeyCap),
		MCP:          Tier{Name: "mcp_user", Burst: 60, Interval: time.Minute / 120, MaxKeys: userKeyCap},
	}
}

// perHour returns the tier allowing burst requests at once and perHour requests an hour after that.
func perHour(name string, burst, perHour, maxKeys int) Tier {
	return Tier{Name: name, Burst: burst, Interval: time.Hour / time.Duration(perHour), MaxKeys: maxKeys}
}

// Limiter limits the requests of one tier, by key. It is safe for concurrent use.
type Limiter struct {
	tier Tier
	now  func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	// swept is when the buckets were last swept.
	swept time.Time
}

// bucket is one key's tokens.
type bucket struct {
	tokens int
	// counted is when tokens was last brought up to date: the bucket gains a token each interval after it, and
	// a full bucket's is when it was last spent from.
	counted time.Time
	// seen is when the key last made a request, which eviction compares.
	seen time.Time
}

// New returns a limiter for tier, timed by now. It panics if the tier's burst, interval or key cap is not
// positive.
func New(tier Tier, now func() time.Time) *Limiter {
	if tier.Burst < 1 || tier.Interval <= 0 || tier.MaxKeys < 1 {
		panic("ratelimit: tier " + tier.Name + " needs a positive burst, interval and key cap")
	}

	return &Limiter{tier: tier, now: now, buckets: map[string]*bucket{}, swept: now()}
}

// Limiters are the limiters of the rate-limited routes, one per tier of Tiers, which says what each limits.
type Limiters struct {
	Contact             *Limiter
	ContactUnattributed *Limiter
	LinkReport          *Limiter
	DraftWrite          *Limiter
	RevisionWrite       *Limiter
	RepertoireWrite     *Limiter
	ProfileWrite        *Limiter
	MCP                 *Limiter
}

// NewLimiters returns a limiter for each of tiers, timed by now. It panics as New does.
func NewLimiters(tiers Tiers, now func() time.Time) Limiters {
	return Limiters{
		Contact:             New(tiers.Contact, now),
		ContactUnattributed: New(tiers.ContactUnattributed, now),
		LinkReport:          New(tiers.LinkReport, now),
		DraftWrite:          New(tiers.DraftWrite, now),
		RevisionWrite:       New(tiers.RevisionWrite, now),
		RepertoireWrite:     New(tiers.RepertoireWrite, now),
		ProfileWrite:        New(tiers.ProfileWrite, now),
		MCP:                 New(tiers.MCP, now),
	}
}

// Name is the limiter's tier's name.
func (l *Limiter) Name() string {
	return l.tier.Name
}

// Allow spends one of key's tokens and reports whether it had one. If it had none, retryAfter is how long until it
// gains one; it is otherwise zero.
func (l *Limiter) Allow(key string) (retryAfter time.Duration, ok bool) {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.swept) >= sweepEvery {
		l.sweep(now)
	}

	b, found := l.buckets[key]
	if !found {
		l.makeRoom(now)
		b = &bucket{tokens: l.tier.Burst, counted: now}
		l.buckets[key] = b
	}
	b.seen = now
	b.refill(now, l.tier)

	if b.tokens == 0 {
		// A clock that went backwards could otherwise make the wait longer than an interval.
		return min(l.tier.Interval, l.tier.Interval-now.Sub(b.counted)), false
	}
	if b.tokens == l.tier.Burst {
		// A full bucket starts gaining tokens from its first spend.
		b.counted = now
	}
	b.tokens--

	return 0, true
}

// refill adds the tokens b gained by now.
func (b *bucket) refill(now time.Time, tier Tier) {
	if b.tokens >= tier.Burst {
		return
	}
	gained := int64(now.Sub(b.counted) / tier.Interval)
	if gained <= 0 {
		return
	}
	if gained >= int64(tier.Burst-b.tokens) {
		b.tokens, b.counted = tier.Burst, now
		return
	}
	b.tokens += int(gained)
	b.counted = b.counted.Add(time.Duration(gained) * tier.Interval)
}

// full reports whether b would be full at now.
func (b *bucket) full(now time.Time, tier Tier) bool {
	return b.tokens >= tier.Burst || int64(now.Sub(b.counted)/tier.Interval) >= int64(tier.Burst-b.tokens)
}

// sweep forgets the buckets that are full at now: a fresh bucket is the same.
func (l *Limiter) sweep(now time.Time) {
	for key, b := range l.buckets {
		if b.full(now, l.tier) {
			delete(l.buckets, key)
		}
	}
	l.swept = now
}

// makeRoom makes room for a new key if the limiter holds its cap: it sweeps, unless it swept less than
// capSweepEvery ago, and if that frees nothing, it evicts the least recently seen of a sample of keys. Map
// iteration order is unspecified and varies between iterations, so the sample does too.
func (l *Limiter) makeRoom(now time.Time) {
	if len(l.buckets) < l.tier.MaxKeys {
		return
	}
	if now.Sub(l.swept) >= capSweepEvery {
		l.sweep(now)
	}
	if len(l.buckets) < l.tier.MaxKeys {
		return
	}

	var oldestKey string
	var oldest *bucket
	sampled := 0
	for key, b := range l.buckets {
		if oldest == nil || b.seen.Before(oldest.seen) {
			oldestKey, oldest = key, b
		}
		if sampled++; sampled == evictionSample {
			break
		}
	}
	delete(l.buckets, oldestKey)
}

// RetryAfterSeconds is wait in whole seconds, rounded up and at least 1: the value of a Retry-After header.
func RetryAfterSeconds(wait time.Duration) int {
	return max(1, int((wait+time.Second-1)/time.Second))
}
