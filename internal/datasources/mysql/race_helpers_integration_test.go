//go:build integration

package mysql_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

// The forced race tests decide the order in which concurrent writes reach a contested row instead of leaving it to
// the scheduler: a transaction of the test's own holds the row (holdRows), possibly writing in it as a concurrent
// write would, the write under test starts and is seen waiting for the row (waitUntilBlocked), and only then is the
// holder committed. Each test so runs one known interleaving, the same every time, and fails on the
// code without the lock or the lock order it covers.

// lockWaitCap bounds how long waitUntilBlocked waits for the writes to block: well under the session's 3 s
// innodb_lock_wait_timeout, after which a blocked write would give up and retry.
const lockWaitCap = 2 * time.Second

// heldRows is a READ COMMITTED transaction of the test's own, holding row locks that the writes under test wait for.
type heldRows struct {
	t  *testing.T
	tx *sql.Tx
}

// holdRows begins a transaction on db and runs query in it, a locking read (SELECT … FOR UPDATE or FOR SHARE) of the
// contested rows. The transaction is rolled back when the test ends unless commit ends it first.
func holdRows(t *testing.T, db *sql.DB, query string, args ...any) *heldRows {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	rows, err := tx.QueryContext(t.Context(), query, args...)
	require.NoError(t, err)
	held := 0
	for rows.Next() {
		held++
	}
	require.NoError(t, rows.Err())
	require.Positive(t, held, "the holder locks no row")
	require.NoError(t, rows.Close())

	return &heldRows{t: t, tx: tx}
}

// commit commits the holder's transaction, and so lets the waiting writes go on.
func (h *heldRows) commit() {
	h.t.Helper()
	require.NoError(h.t, h.tx.Commit())
}

// waitUntilBlocked waits until n transactions on db's database are waiting for a row lock, failing the test after
// lockWaitCap. It reads performance_schema.data_lock_waits, and without access to it the InnoDB transaction list;
// with neither, which the test servers' accounts always have, it fails rather than skips.
func waitUntilBlocked(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	deadline := time.Now().Add(lockWaitCap)
	for {
		waiting := lockWaits(t, db)
		if waiting >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d transactions are waiting for a row lock after %s, want %d", waiting, lockWaitCap, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The queries counting the transactions of the session's database that wait for a row lock.
const (
	dataLockWaitsQuery = "SELECT COUNT(DISTINCT w.REQUESTING_ENGINE_TRANSACTION_ID) " +
		"FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l " +
		"ON l.ENGINE_LOCK_ID = w.REQUESTING_ENGINE_LOCK_ID WHERE l.OBJECT_SCHEMA = DATABASE()"
	innodbTrxQuery = "SELECT COUNT(*) FROM information_schema.INNODB_TRX t " +
		"JOIN performance_schema.threads th ON th.PROCESSLIST_ID = t.trx_mysql_thread_id " +
		"WHERE t.trx_state = 'LOCK WAIT' AND th.PROCESSLIST_DB = DATABASE()"
)

// lockWaits returns how many transactions of db's database wait for a row lock.
func lockWaits(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	err := db.QueryRowContext(t.Context(), dataLockWaitsQuery).Scan(&n)
	if err == nil {
		return n
	}
	require.NoError(t, db.QueryRowContext(t.Context(), innodbTrxQuery).Scan(&n),
		"neither performance_schema.data_lock_waits (%v) nor INNODB_TRX is readable", err)

	return n
}

// isDeadlock is whether err is MySQL's ER_LOCK_DEADLOCK, which InnoDB gives the transaction it rolls back to break
// a deadlock.
func isDeadlock(err error) bool {
	var mysqlErr *mysqldriver.MySQLError

	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1213
}

// started runs fn on its own goroutine and returns a channel that receives its error.
func started(ctx context.Context, fn func(ctx context.Context) error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- fn(ctx) }()

	return done
}

// inOrder forces first to commit before second reads the rows they contend for: first runs in a holder
// transaction (holdRows with query and args, a locking read of a contested row), second starts and is seen
// waiting for a row lock, and only then does the holder commit. It returns second's error; first's must be nil.
// A second that never waits, because it takes no lock first holds, fails the test.
func inOrder(t *testing.T, db *sql.DB, query string, args []any, first func(ctx context.Context, tx *sql.Tx) error,
	second func(ctx context.Context) error,
) error {
	t.Helper()
	holder := holdRows(t, db, query, args...)
	require.NoError(t, first(t.Context(), holder.tx), "the first write")
	done := started(t.Context(), second)
	waitUntilBlocked(t, db, 1)
	holder.commit()

	return <-done
}

// The locking reads of inOrder's holders: of a revision, a piece, a person and a user row.
const (
	lockRevision = "SELECT id FROM revisions WHERE id = ? FOR UPDATE"
	lockPiece    = "SELECT id FROM pieces WHERE id = ? FOR UPDATE"
	lockPerson   = "SELECT id FROM persons WHERE id = ? FOR UPDATE"
	lockUser     = "SELECT id FROM users WHERE id = ? FOR UPDATE"
)
