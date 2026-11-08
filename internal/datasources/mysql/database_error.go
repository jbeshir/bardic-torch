package mysql

import (
	"errors"
	"fmt"

	mysqldriver "github.com/go-sql-driver/mysql"
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

// databaseError is err for step, as a DatabaseError if it is a MySQL error.
func databaseError(step string, err error) error {
	if number := mysqlErrorNumber(err); number != 0 {
		return &DatabaseError{Step: step, Number: number}
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
