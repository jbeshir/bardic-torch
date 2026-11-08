//go:build integration

package mysql_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MySQL errors a write transaction is retried for.
const (
	errDeadlock        = 1213
	errLockWaitTimeout = 1205
)

// newProbeTable creates a table the tests write to from a write transaction, and drops it when the test ends.
func newProbeTable(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(t.Context(), "CREATE TABLE probe_writes (n BIGINT NOT NULL) "+
		"ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci")
	require.NoError(t, err)
	// The database is reused by later tests, which expect to find only the migrated tables.
	t.Cleanup(func() {
		_, err := db.ExecContext(context.WithoutCancel(t.Context()), "DROP TABLE probe_writes")
		require.NoError(t, err)
	})
}

func probeRows(t *testing.T, db *sql.DB) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM probe_writes").Scan(&n))

	return n
}

func TestWithWriteTx_RetriesADeadlock(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	newProbeTable(t, db)
	repo := mysql.New(db)

	attempts := 0
	var isolation string
	err := repo.WithWriteTx(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		attempts++
		if _, err := tx.ExecContext(ctx, "INSERT INTO probe_writes (n) VALUES (?)", attempts); err != nil {
			return err
		}
		if attempts == 1 {
			return &mysqldriver.MySQLError{Number: errDeadlock}
		}

		return tx.QueryRowContext(ctx, "SELECT trx_isolation_level FROM information_schema.innodb_trx "+
			"WHERE trx_mysql_thread_id = CONNECTION_ID()").Scan(&isolation)
	})

	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
	assert.Equal(t, "READ COMMITTED", isolation)
	assert.Equal(t, 1, probeRows(t, db), "the failed attempt's write was rolled back")
}

func TestWithWriteTx_GivesUpAfterTwoRetries(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	newProbeTable(t, db)
	repo := mysql.New(db)

	attempts := 0
	err := repo.WithWriteTx(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		attempts++
		if _, err := tx.ExecContext(ctx, "INSERT INTO probe_writes (n) VALUES (?)", attempts); err != nil {
			return err
		}

		return &mysqldriver.MySQLError{Number: errLockWaitTimeout, Message: "Lock wait timeout for row 'Wren'"}
	})

	require.ErrorIs(t, err, datasources.ErrUnavailable)
	assert.Equal(t, 3, attempts, "one attempt and two retries")
	assert.Zero(t, probeRows(t, db))
	assert.NotContains(t, err.Error(), "Wren", "the server's message is not reported")
	assert.Contains(t, err.Error(), "1205")
}

func TestWithWriteTx_OtherErrorsAreNotRetried(t *testing.T) {
	t.Parallel()

	repo := mysql.New(catalogtest.Writable(t))
	errOther := errors.New("something else")

	attempts := 0
	err := repo.WithWriteTx(t.Context(), func(context.Context, *sql.Tx) error {
		attempts++

		return errOther
	})

	require.ErrorIs(t, err, errOther)
	require.NotErrorIs(t, err, datasources.ErrUnavailable)
	assert.Equal(t, 1, attempts)

	attempts = 0
	err = repo.WithWriteTx(t.Context(), func(context.Context, *sql.Tx) error {
		attempts++

		return &mysqldriver.MySQLError{Number: 1452}
	})

	var mysqlErr *mysqldriver.MySQLError
	require.ErrorAs(t, err, &mysqlErr)
	assert.Equal(t, uint16(1452), mysqlErr.Number)
	assert.Equal(t, 1, attempts)
}

// TestWithWriteTx_DeadlinePassed requires a transaction still running at WriteTxDeadline to end as unavailable.
func TestWithWriteTx_DeadlinePassed(t *testing.T) {
	t.Parallel()

	repo := mysql.New(catalogtest.Writable(t))

	start := time.Now()
	attempts := 0
	err := repo.WithWriteTx(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		attempts++
		// DO is not a SELECT, so the session's execution time limit does not stop it.
		_, err := tx.ExecContext(ctx, "DO SLEEP(30)")

		return err
	})

	require.ErrorIs(t, err, datasources.ErrUnavailable)
	assert.Equal(t, 1, attempts, "a passed deadline is not retried")
	assert.GreaterOrEqual(t, time.Since(start), mysql.WriteTxDeadline)
	assert.Less(t, time.Since(start), mysql.WriteTxDeadline+3*time.Second)
}

func TestWithWriteTx_CommitsOnSuccess(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	newProbeTable(t, db)
	repo := mysql.New(db)

	require.NoError(t, repo.WithWriteTx(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO probe_writes (n) VALUES (1)")

		return err
	}))
	assert.Equal(t, 1, probeRows(t, db))
}

// TestWithWriteTx_HonoursRequestDeadline requires a write transaction to end PostCommitReserve before the request's
// deadline, as unavailable, so that the response is still written; and a write with too little of the request left
// not to be tried at all.
func TestWithWriteTx_HonoursRequestDeadline(t *testing.T) {
	t.Parallel()

	repo := mysql.New(catalogtest.Writable(t))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	start := time.Now()
	err := repo.WithWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		// DO is not a SELECT, so the session's execution time limit does not stop it.
		_, err := tx.ExecContext(ctx, "DO SLEEP(30)")

		return err
	})
	require.ErrorIs(t, err, datasources.ErrUnavailable)
	assert.Less(t, time.Since(start), 3*time.Second-mysql.PostCommitReserve+time.Second)
	require.NoError(t, ctx.Err(), "the request still has time to answer")

	late, cancelLate := context.WithTimeout(t.Context(), mysql.PostCommitReserve+mysql.MinWriteTx/2)
	defer cancelLate()
	attempts := 0
	err = repo.WithWriteTx(late, func(context.Context, *sql.Tx) error {
		attempts++
		return nil
	})
	require.ErrorIs(t, err, datasources.ErrUnavailable)
	assert.Zero(t, attempts, "a write with too little time left is not tried")
}
