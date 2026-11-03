//go:build integration

package mysql_test

import (
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConnect_SelectTimeLimit checks the session limit ConfigFromURL sets: the server stops a SELECT after two
// seconds, but not a write that runs longer. SLEEP returns 1 when the server interrupts it, and 0 otherwise.
func TestConnect_SelectTimeLimit(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	ctx := t.Context()

	var limit int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT @@SESSION.max_execution_time").Scan(&limit))
	assert.Equal(t, int64(2000), limit)

	var lockWait int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT @@SESSION.innodb_lock_wait_timeout").Scan(&lockWait))
	assert.Equal(t, int64(3), lockWait)

	start := time.Now()
	var interrupted int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT SLEEP(5)").Scan(&interrupted))
	assert.Equal(t, int64(1), interrupted, "the SELECT is stopped")
	assert.Less(t, time.Since(start), 4*time.Second)

	_, err := db.ExecContext(ctx, "CREATE TABLE probe_writes (slept BIGINT NOT NULL) "+
		"ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci")
	require.NoError(t, err)
	start = time.Now()
	_, err = db.ExecContext(ctx, "INSERT INTO probe_writes (slept) SELECT SLEEP(3)")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), 3*time.Second, "the write runs to the end")
	var slept int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT slept FROM probe_writes").Scan(&slept))
	assert.Equal(t, int64(0), slept)
}
