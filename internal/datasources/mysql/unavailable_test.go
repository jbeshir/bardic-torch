package mysql

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/stretchr/testify/assert"
)

func TestUnavailableIfDisconnected(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"refused_dial":      {&net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}, true},
		"invalid_conn":      {fmt.Errorf("query: %w", mysqldriver.ErrInvalidConn), true},
		"too_many":          {&mysqldriver.MySQLError{Number: erConCount}, true},
		"too_many_for_user": {&mysqldriver.MySQLError{Number: erTooManyUserConnections}, true},
		"shutdown":          {&mysqldriver.MySQLError{Number: erServerShutdown}, true},
		"query_error":       {&mysqldriver.MySQLError{Number: 1146}, false},
		"canceled":          {fmt.Errorf("query: %w", context.Canceled), false},
	} {
		assert.Equal(t, tc.want, errors.Is(unavailableIfDisconnected(tc.err), datasources.ErrUnavailable), name)
	}
	assert.NoError(t, unavailableIfDisconnected(nil))
}
