package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteTxBudget_NestsLimits checks that each limit of a write transaction is under the next: the lock wait
// (innodb_lock_wait_timeout) under the locking reads' execution time limit, which is under the transaction's
// deadline.
func TestWriteTxBudget_NestsLimits(t *testing.T) {
	t.Parallel()

	lockWait, err := time.ParseDuration(lockWaitTimeoutSeconds + "s")
	require.NoError(t, err)
	assert.Less(t, lockWait, lockingReadMillis*time.Millisecond)
	assert.Less(t, lockingReadMillis*time.Millisecond, WriteTxDeadline)
}

// TestWriteTxDeadline checks that a write transaction's deadline is WriteTxDeadline, or less when the request's
// deadline, less PostCommitReserve, comes first.
func TestWriteTxDeadline(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	withDeadline := func(left time.Duration) context.Context {
		ctx, cancel := context.WithDeadline(t.Context(), now.Add(left))
		t.Cleanup(cancel)
		return ctx
	}

	assert.Equal(t, WriteTxDeadline, writeTxDeadline(t.Context(), now), "no request deadline")
	assert.Equal(t, WriteTxDeadline, writeTxDeadline(withDeadline(13*time.Second), now), "a fresh request")
	assert.Equal(t, 1500*time.Millisecond, writeTxDeadline(withDeadline(3*time.Second), now))
	assert.Less(t, writeTxDeadline(withDeadline(time.Second), now), MinWriteTx, "too little left to start")
}
