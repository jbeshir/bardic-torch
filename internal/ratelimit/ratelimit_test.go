package ratelimit

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTier is the name of the tiers made up for a test.
const testTier = "test"

// fakeClock is a clock tests move by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// requireAllowed spends n of key's tokens, failing if any is refused.
func requireAllowed(t *testing.T, l *Limiter, key string, n int) {
	t.Helper()
	for i := range n {
		retryAfter, ok := l.Allow(key)
		require.Truef(t, ok, "request %d of %s refused", i+1, key)
		require.Zero(t, retryAfter)
	}
}

// requireRefused checks that key's next request is refused, with retryAfter to wait.
func requireRefused(t *testing.T, l *Limiter, key string, retryAfter time.Duration) {
	t.Helper()
	got, ok := l.Allow(key)
	require.Falsef(t, ok, "request of %s allowed", key)
	require.Equal(t, retryAfter, got)
}

// TestLimiter_Tiers checks every production tier: its settings, that a key may make a burst of requests at once
// and is refused the next with an exact wait, that it gains one request each interval and a full burst after
// burst intervals, and that keys do not share tokens.
func TestLimiter_Tiers(t *testing.T) {
	t.Parallel()

	tiers := []struct {
		field    string
		name     string
		burst    int
		interval time.Duration
		maxKeys  int
	}{
		{"Contact", "contact_ip", 3, 12 * time.Minute, 50_000},
		{"ContactUnattributed", "contact_unattributed_ip", 30, time.Minute, 50_000},
		{"LinkReport", "link_report_user", 10, 6 * time.Minute, 10_000},
		{"DraftWrite", "draft_write_user", 60, 2 * time.Second, 10_000},
		{"RevisionWrite", "revision_write_user", 30, time.Minute, 10_000},
		{"RepertoireWrite", "repertoire_write_user", 60, time.Second, 10_000},
		{"ProfileWrite", "profile_write_user", 10, 6 * time.Minute, 10_000},
	}

	defaults := reflect.ValueOf(DefaultTiers())
	require.Len(t, tiers, defaults.NumField(), "every tier needs a row")

	for _, tc := range tiers {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			field := defaults.FieldByName(tc.field)
			require.Truef(t, field.IsValid(), "no tier %s", tc.field)
			tier, ok := field.Interface().(Tier)
			require.True(t, ok)
			assert.Equal(t, Tier{Name: tc.name, Burst: tc.burst, Interval: tc.interval, MaxKeys: tc.maxKeys}, tier)

			clock := newFakeClock()
			l := New(tier, clock.Now)
			assert.Equal(t, tc.name, l.Name())

			// The burst, then a refusal until the first token comes back.
			requireAllowed(t, l, "alice", tc.burst)
			requireRefused(t, l, "alice", tc.interval)
			assert.Equal(t, int(tc.interval/time.Second), RetryAfterSeconds(tc.interval))

			// Another key has its own burst.
			requireAllowed(t, l, "bryony", tc.burst)
			requireRefused(t, l, "bryony", tc.interval)

			// One token an interval.
			clock.Advance(tc.interval - time.Nanosecond)
			requireRefused(t, l, "alice", time.Nanosecond)
			clock.Advance(time.Nanosecond)
			requireAllowed(t, l, "alice", 1)
			requireRefused(t, l, "alice", tc.interval)

			// A whole burst after burst intervals, and never more.
			clock.Advance(time.Duration(tc.burst+1) * tc.interval)
			requireAllowed(t, l, "alice", tc.burst)
			requireRefused(t, l, "alice", tc.interval)
		})
	}
}

// TestLimiter_PartialRefillKeepsRemainder checks that the time towards the next token is kept when a bucket
// gains some tokens, so a client spending as it refills gets the tier's rate exactly.
func TestLimiter_PartialRefillKeepsRemainder(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	l := New(Tier{Name: testTier, Burst: 3, Interval: 10 * time.Second, MaxKeys: 10}, clock.Now)

	requireAllowed(t, l, "alice", 3)
	clock.Advance(25 * time.Second)
	requireAllowed(t, l, "alice", 2)
	requireRefused(t, l, "alice", 5*time.Second)
}

func TestRetryAfterSeconds(t *testing.T) {
	t.Parallel()

	for wait, want := range map[time.Duration]int{
		0:                                 1,
		time.Nanosecond:                   1,
		time.Second:                       1,
		time.Second + time.Nanosecond:     2,
		12 * time.Minute:                  720,
		12*time.Minute - time.Millisecond: 720,
	} {
		assert.Equalf(t, want, RetryAfterSeconds(wait), "wait %s", wait)
	}
}

func TestNew_RejectsUnusableTiers(t *testing.T) {
	t.Parallel()

	for _, tier := range []Tier{
		{Name: "no burst", Burst: 0, Interval: time.Second, MaxKeys: 1},
		{Name: "no interval", Burst: 1, Interval: 0, MaxKeys: 1},
		{Name: "no keys", Burst: 1, Interval: time.Second, MaxKeys: 0},
	} {
		assert.Panicsf(t, func() { New(tier, time.Now) }, "tier %s", tier.Name)
	}
}

// TestLimiter_EvictsIdleFullBuckets checks that the minute's sweep forgets refilled buckets, and only those.
func TestLimiter_EvictsIdleFullBuckets(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	l := New(Tier{Name: testTier, Burst: 2, Interval: 40 * time.Second, MaxKeys: 10}, clock.Now)

	requireAllowed(t, l, "refills", 1)
	requireAllowed(t, l, "drained", 2)
	clock.Advance(sweepEvery - time.Nanosecond)
	requireAllowed(t, l, "newcomer", 1)
	assert.Len(t, l.buckets, 3, "no sweep before a minute has passed")

	// At a minute, "refills" has gained its token back and is forgotten; "drained" has gained one of its two
	// and is kept, as is the key making the request.
	clock.Advance(time.Nanosecond)
	requireAllowed(t, l, "newcomer", 1)
	assert.ElementsMatch(t, []string{"drained", "newcomer"}, bucketKeys(l))

	// The kept bucket still has only the token it gained.
	requireAllowed(t, l, "drained", 1)
	requireRefused(t, l, "drained", 20*time.Second)
}

// TestLimiter_KeyCapEvictsOldestSampled checks that a limiter never holds more keys than its cap: a new key at
// the cap sweeps refilled buckets first and otherwise evicts the least recently seen key.
func TestLimiter_KeyCapEvictsOldestSampled(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	l := New(Tier{Name: testTier, Burst: 1, Interval: 10 * time.Second, MaxKeys: 2}, clock.Now)

	requireAllowed(t, l, "first", 1)
	clock.Advance(time.Second)
	requireAllowed(t, l, "second", 1)
	clock.Advance(time.Second)
	// "first" is seen again, so "second" is now the least recently seen.
	requireRefused(t, l, "first", 8*time.Second)
	clock.Advance(time.Second)

	requireAllowed(t, l, "third", 1)
	assert.ElementsMatch(t, []string{"first", "third"}, bucketKeys(l))

	// An evicted key starts again with a full bucket: the cap trades exactness for bounded memory.
	clock.Advance(time.Second)
	requireAllowed(t, l, "second", 1)
	assert.ElementsMatch(t, []string{"third", "second"}, bucketKeys(l))

	// A refilled bucket is swept to make room before any other key is evicted, within the minute between
	// regular sweeps: "third" has refilled, "second" has not.
	clock.Advance(9 * time.Second)
	requireRefused(t, l, "second", time.Second)
	requireAllowed(t, l, "fourth", 1)
	assert.ElementsMatch(t, []string{"second", "fourth"}, bucketKeys(l))
}

// TestLimiter_ConcurrentAllow checks, under -race, that concurrent requests spend exactly the burst, and that
// eviction under concurrent new keys keeps the cap.
func TestLimiter_ConcurrentAllow(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	shared := New(Tier{Name: testTier, Burst: 50, Interval: time.Hour, MaxKeys: 8}, clock.Now)
	l := New(Tier{Name: testTier, Burst: 1, Interval: time.Hour, MaxKeys: 8}, clock.Now)

	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := range 200 {
		wg.Go(func() {
			if _, ok := shared.Allow("shared"); ok {
				allowed.Add(1)
			}
			_, _ = l.Allow(fmt.Sprintf("key-%d", i))
			if i%10 == 0 {
				clock.Advance(time.Millisecond)
			}
		})
	}
	wg.Wait()

	assert.Equal(t, int64(50), allowed.Load())
	assert.LessOrEqual(t, len(bucketKeys(l)), 8)
}

func bucketKeys(l *Limiter) []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	keys := make([]string, 0, len(l.buckets))
	for key := range l.buckets {
		keys = append(keys, key)
	}
	return keys
}
