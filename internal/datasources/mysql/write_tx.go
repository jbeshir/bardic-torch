package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources"
)

// The MySQL errors a write transaction is retried for.
const (
	// errDeadlock is ER_LOCK_DEADLOCK: InnoDB rolled the transaction back to break a deadlock.
	errDeadlock = 1213
	// errLockWaitTimeout is ER_LOCK_WAIT_TIMEOUT: a lock was not granted within innodb_lock_wait_timeout.
	errLockWaitTimeout = 1205
)

// The budget of a write transaction, which must end before the server's WriteTimeout, or the client sees a
// dropped connection while the transaction may still commit (docs/PLAN.md §5.1 "Approval").
const (
	// WriteTxDeadline is how long a write transaction, retries included, may take at most.
	WriteTxDeadline = 11 * time.Second
	// PostCommitReserve is how much of the request's deadline a write transaction leaves unused, for the reads
	// that build the response after it commits.
	PostCommitReserve = 1500 * time.Millisecond
	// MinWriteTx is the least time a write transaction is started with: with less of the request's deadline left,
	// after PostCommitReserve, the write is not tried.
	MinWriteTx = 500 * time.Millisecond
	// maxWriteTxRetries is how many times a write transaction is run again after a deadlock or lock wait timeout.
	maxWriteTxRetries = 2
	// lockingReadMillis is the execution time limit of a locking read, overriding the session's
	// maxExecutionTimeMillis, which would stop a read waiting for a row lock. It is above the session's
	// innodb_lock_wait_timeout, so that InnoDB's lock wait timeout (error 1205) ends a wait first.
	lockingReadMillis = 4000
)

// withWriteTx runs fn in a READ COMMITTED transaction and commits it if fn returns nil. A deadlock or lock wait
// timeout rolls the transaction back and runs fn again, up to maxWriteTxRetries more times, all within the
// transaction's deadline (writeTxDeadline). When the retries or the deadline run out, or too little of ctx's
// deadline is left to start, the error is datasources.ErrUnavailable, naming the MySQL error number but not the
// server's message; a failure to reach the database is datasources.ErrUnavailable too (unavailableIfDisconnected).
// Any other error from fn is returned as it is, so fn must be safe to run again from the start.
// fn must use the context it is given, which ends at the deadline; a statement run with another context is not
// stopped by it.
func (r *Repository) withWriteTx(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	deadline := writeTxDeadline(ctx, time.Now())
	if deadline < MinWriteTx {
		return fmt.Errorf("%w: %s of the request's deadline left for a write transaction", datasources.ErrUnavailable,
			deadline.Round(time.Millisecond))
	}
	txCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	for attempt := 0; ; attempt++ {
		err := r.runWriteTx(txCtx, fn)
		if err == nil {
			return nil
		}
		if txCtx.Err() != nil && ctx.Err() == nil {
			return fmt.Errorf("%w: write transaction passed its deadline", datasources.ErrUnavailable)
		}

		var mysqlErr *mysqldriver.MySQLError
		if !errors.As(err, &mysqlErr) || (mysqlErr.Number != errDeadlock && mysqlErr.Number != errLockWaitTimeout) {
			return unavailableIfDisconnected(err)
		}
		if attempt == maxWriteTxRetries {
			return fmt.Errorf("%w: write transaction: MySQL error %d", datasources.ErrUnavailable, mysqlErr.Number)
		}
	}
}

// writeTxDeadline is how long a write transaction started at now on ctx may take: WriteTxDeadline, or less if ctx's
// deadline, less PostCommitReserve, comes first.
func writeTxDeadline(ctx context.Context, now time.Time) time.Duration {
	end, ok := ctx.Deadline()
	if !ok {
		return WriteTxDeadline
	}

	return min(WriteTxDeadline, end.Sub(now)-PostCommitReserve)
}

// runWriteTx runs fn once in a READ COMMITTED transaction, rolling it back if fn or the commit fails.
func (r *Repository) runWriteTx(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(ctx, tx); err != nil {
		return err
	}

	return tx.Commit()
}
