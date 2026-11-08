package mysql

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources"
)

// unavailableIfDisconnected returns err wrapped as datasources.ErrUnavailable if it is a failure to reach the
// database: a connection that could not be made (a *net.OpError, such as a refused or timed-out dial, or a MySQL
// error with which the server turned it away, such as too many connections: serverUnavailable) or that broke
// (driver.ErrBadConn, the driver's ErrInvalidConn). Any other error, including a query that failed on a working
// connection and a request whose context was canceled, is returned as it is, as is nil.
func unavailableIfDisconnected(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, datasources.ErrUnavailable) {
		return err
	}
	var opErr *net.OpError
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysqldriver.ErrInvalidConn) || errors.As(err, &opErr) ||
		serverUnavailable(mysqlErrorNumber(err)) {
		return fmt.Errorf("%w: %w", datasources.ErrUnavailable, err)
	}

	return err
}

// serverUnavailable reports whether a MySQL error number is one with which the server refuses or ends a connection
// because it cannot serve one now.
func serverUnavailable(number uint16) bool {
	switch number {
	case erConCount, erTooManyUserConnections, erServerShutdown:
		return true
	default:
		return false
	}
}
