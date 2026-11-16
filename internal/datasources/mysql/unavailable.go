package mysql

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"net"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources"
)

// The MySQL errors with which a server refuses or ends a connection because it cannot serve one now. It sends them
// in place of its handshake, so the driver reports them as MySQL errors rather than network errors.
const (
	// erConCount is ER_CON_COUNT_ERROR, "Too many connections": the server has max_connections connections.
	erConCount = 1040
	// erTooManyUserConnections is ER_TOO_MANY_USER_CONNECTIONS: the account has max_user_connections connections.
	erTooManyUserConnections = 1203
	// erServerShutdown is ER_SERVER_SHUTDOWN: the server is shutting down.
	erServerShutdown = 1053
)

// unavailableIfDisconnected returns err wrapped as datasources.ErrUnavailable if it is a failure to reach the
// database: a connection that could not be made or that broke, or one the server turned away. Any other error,
// including a query that failed on a working connection, is returned as it is, as is nil.
func unavailableIfDisconnected(err error) error {
	if err == nil {
		return nil
	}
	var opErr *net.OpError
	var mysqlErr *mysqldriver.MySQLError
	refused := errors.As(err, &mysqlErr) &&
		(mysqlErr.Number == erConCount || mysqlErr.Number == erTooManyUserConnections ||
			mysqlErr.Number == erServerShutdown)
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysqldriver.ErrInvalidConn) ||
		errors.As(err, &opErr) || refused {
		return fmt.Errorf("%w: %w", datasources.ErrUnavailable, err)
	}

	return err
}
