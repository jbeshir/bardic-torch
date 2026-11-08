package mysql

import (
	"errors"
	"fmt"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources"
)

// The MySQL error numbers the repository handles.
const (
	// erDuplicateEntry is ER_DUP_ENTRY: a UNIQUE index already holds the value.
	erDuplicateEntry = 1062
	// erNoReferencedRow is ER_NO_REFERENCED_ROW_2: a foreign key's parent row does not exist.
	erNoReferencedRow = 1452
	// errDeadlock is ER_LOCK_DEADLOCK: InnoDB rolled the transaction back to break a deadlock.
	errDeadlock = 1213
	// errLockWaitTimeout is ER_LOCK_WAIT_TIMEOUT: a lock was not granted within innodb_lock_wait_timeout.
	errLockWaitTimeout = 1205
	// erQueryTimeout is ER_QUERY_TIMEOUT: a statement exceeded its maximum execution time.
	erQueryTimeout = 3024

	// The errors with which the server refuses or ends a connection because it cannot serve one now. It sends them
	// in place of its handshake, so they are MySQL errors rather than network errors, but like a refused dial they
	// say nothing about the request, and trying again later may work (serverUnavailable).

	// erConCount is ER_CON_COUNT_ERROR, "Too many connections": the server has max_connections connections.
	erConCount = 1040
	// erTooManyUserConnections is ER_TOO_MANY_USER_CONNECTIONS: the account has max_user_connections connections.
	erTooManyUserConnections = 1203
	// erServerShutdown is ER_SERVER_SHUTDOWN: the server is shutting down.
	erServerShutdown = 1053
)

// DatabaseError is returned, wrapped, for every MySQL error of a write or an import (a refused row, a failed lock
// or commit): only its number is kept, never the server's message, which may quote data ("Duplicate entry '…'")
// and so may hold a private name. It names the step that failed.
type DatabaseError struct {
	Step   string
	Number uint16
}

func (e *DatabaseError) Error() string {
	return fmt.Sprintf("%s: MySQL error %d", e.Step, e.Number)
}

// databaseError is err for step, as a DatabaseError if it is a MySQL error. A MySQL error that is
// datasources.ErrUnavailable (unavailableIfDisconnected: the server refused the connection) stays so.
func databaseError(step string, err error) error {
	if number := mysqlErrorNumber(err); number != 0 {
		dbErr := &DatabaseError{Step: step, Number: number}
		if errors.Is(err, datasources.ErrUnavailable) {
			return fmt.Errorf("%w: %w", datasources.ErrUnavailable, dbErr)
		}

		return dbErr
	}

	return fmt.Errorf("%s: %w", step, err)
}

// mysqlErrorNumber returns the MySQL error number of err, 0 if it is not a MySQL error.
func mysqlErrorNumber(err error) uint16 {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number
	}

	return 0
}
