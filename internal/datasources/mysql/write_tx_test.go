package mysql

import (
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
