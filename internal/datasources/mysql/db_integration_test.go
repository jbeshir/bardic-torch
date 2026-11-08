//go:build integration

package mysql_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
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

// TestConnect_AppliesPoolLimits checks that Connect gives the pool the limits it is passed, here the application's.
func TestConnect_AppliesPoolLimits(t *testing.T) {
	t.Parallel()

	cfg, err := mysql.ConfigFromURL(mysqltest.NewDatabaseURL(t))
	require.NoError(t, err)
	limits := mysql.ProductionPool()
	db, err := mysql.Connect(t.Context(), cfg, limits)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	assert.Equal(t, limits.MaxOpenConns, db.Stats().MaxOpenConnections)
	conns := make([]*sql.Conn, limits.MaxOpenConns)
	for i := range conns {
		conns[i], err = db.Conn(t.Context())
		require.NoError(t, err)
	}
	for _, conn := range conns {
		require.NoError(t, conn.Close())
	}
	assert.Equal(t, limits.MaxIdleConns, db.Stats().Idle, "every released connection up to the cap stays idle")
}
