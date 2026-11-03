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
// dropped connection while the transaction may still commit.
const (
	// WriteTxDeadline is how long a write transaction, retries included, may take.
	WriteTxDeadline = 11 * time.Second
	// maxWriteTxRetries is how many times a write transaction is run again after a deadlock or lock wait timeout.
	maxWriteTxRetries = 2
	// lockingReadMillis is the execution time limit of a locking read, overriding the session's
	// maxExecutionTimeMillis, which would stop a read waiting for a row lock. It is above the session's
	// innodb_lock_wait_timeout, so that InnoDB's lock wait timeout (error 1205) ends a wait first.
	lockingReadMillis = 4000
)

// withWriteTx runs fn in a READ COMMITTED transaction and commits it if fn returns nil. A deadlock or lock wait
// timeout rolls the transaction back and runs fn again, up to maxWriteTxRetries more times, all within
// WriteTxDeadline. When the retries or the deadline run out the error is datasources.ErrUnavailable, naming the
// MySQL error number but not the server's message. Any other error from fn is returned as it is, so fn must be
// safe to run again from the start. fn must use the context it is given, which ends at the deadline; a statement
// run with another context is not stopped by it.
func (r *Repository) withWriteTx(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	txCtx, cancel := context.WithTimeout(ctx, WriteTxDeadline)
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
			return err
		}
		if attempt == maxWriteTxRetries {
			return fmt.Errorf("%w: write transaction: MySQL error %d", datasources.ErrUnavailable, mysqlErr.Number)
		}
	}
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
