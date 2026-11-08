package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/testutil/refusingmysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tcp is the network of the test connections.
const tcp = "tcp"

func TestUnavailableIfDisconnected(t *testing.T) {
	t.Parallel()

	refused := &net.OpError{Op: "dial", Net: tcp, Err: errors.New("connect: connection refused")}
	busy := fmt.Errorf("%w: write transaction: MySQL error 1213", datasources.ErrUnavailable)
	for _, tc := range []struct {
		name            string
		err             error
		wantUnavailable bool
	}{
		{name: "refused_dial", err: fmt.Errorf("beginning read-only transaction: %w", refused), wantUnavailable: true},
		{name: "bad_connection", err: driver.ErrBadConn, wantUnavailable: true},
		{name: "invalid_connection", err: fmt.Errorf("query: %w", mysqldriver.ErrInvalidConn), wantUnavailable: true},
		{name: "canceled_dial", err: &net.OpError{Op: "dial", Net: tcp, Err: context.Canceled}},
		{name: "canceled", err: fmt.Errorf("query: %w", context.Canceled)},
		{name: "too_many_connections", err: fmt.Errorf("beginning read-only transaction: %w",
			&mysqldriver.MySQLError{Number: erConCount, Message: "Too many connections"}), wantUnavailable: true},
		{name: "too_many_user_connections", err: &mysqldriver.MySQLError{Number: erTooManyUserConnections}, wantUnavailable: true},
		{name: "server_shutdown", err: &mysqldriver.MySQLError{Number: erServerShutdown}, wantUnavailable: true},
		{name: "query_error", err: &mysqldriver.MySQLError{Number: 1146, Message: "no such table"}},
		{name: "no_rows", err: sql.ErrNoRows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := unavailableIfDisconnected(tc.err)
			require.ErrorIs(t, got, tc.err, "the cause is kept")
			assert.Equal(t, tc.wantUnavailable, errors.Is(got, datasources.ErrUnavailable))
		})
	}

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, unavailableIfDisconnected(nil))
	})
	t.Run("already_unavailable", func(t *testing.T) {
		t.Parallel()
		assert.Same(t, busy, unavailableIfDisconnected(busy), "not wrapped again")
	})
}

// closedPortRepository is a Repository whose database listens on no port: every connection is refused.
func closedPortRepository(t *testing.T) *Repository {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), tcp, "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	cfg := mysqldriver.NewConfig()
	cfg.Net, cfg.Addr, cfg.DBName, cfg.Timeout = tcp, addr, "bardic_torch", 2*time.Second
	connector, err := mysqldriver.NewConnector(cfg)
	require.NoError(t, err)
	db := sql.OpenDB(connector)
	t.Cleanup(func() { _ = db.Close() })

	return New(db)
}

// TestRepository_ReadOnly_DatabaseDownIsUnavailable checks that reads, writes and the direct queries of a
// repository whose database cannot be reached fail with datasources.ErrUnavailable.
func TestRepository_ReadOnly_DatabaseDownIsUnavailable(t *testing.T) {
	t.Parallel()

	repo := closedPortRepository(t)
	_, err := repo.GetPublicPiece(t.Context(), "the-copper-kettle")
	require.ErrorIs(t, err, datasources.ErrUnavailable, "a read in a read-only transaction")
	_, err = repo.GetUser(t.Context(), "google-oauth2|123")
	require.ErrorIs(t, err, datasources.ErrUnavailable, "the user lookup of the auth middleware")
	_, err = repo.GetAPITokenByHash(t.Context(), "00")
	require.ErrorIs(t, err, datasources.ErrUnavailable, "the API token lookup of the auth middleware")
	err = repo.withWriteTx(t.Context(), func(context.Context, *sql.Tx) error { return nil })
	require.ErrorIs(t, err, datasources.ErrUnavailable, "a write transaction")
}

// TestDatabaseError_KeepsUnavailable checks that a write's MySQL error that is datasources.ErrUnavailable stays so
// once databaseError has replaced it by its number, and that any other MySQL error does not become so.
func TestDatabaseError_KeepsUnavailable(t *testing.T) {
	t.Parallel()

	refused := databaseError("approving revision [1]",
		unavailableIfDisconnected(&mysqldriver.MySQLError{Number: erConCount, Message: "Too many connections"}))
	require.ErrorIs(t, refused, datasources.ErrUnavailable)
	var dbErr *DatabaseError
	require.ErrorAs(t, refused, &dbErr)
	assert.Equal(t, &DatabaseError{Step: "approving revision [1]", Number: erConCount}, dbErr)
	assert.NotContains(t, refused.Error(), "Too many connections", "the server's message is not kept")

	duplicate := databaseError("saving draft of piece",
		unavailableIfDisconnected(&mysqldriver.MySQLError{Number: erDuplicateEntry, Message: "Duplicate entry 'x'"}))
	require.NotErrorIs(t, duplicate, datasources.ErrUnavailable)
	assert.Equal(t, &DatabaseError{Step: "saving draft of piece", Number: erDuplicateEntry}, duplicate)
}

// TestRepository_TooManyConnectionsIsUnavailable checks that reads, writes and the direct queries of a repository
// whose server turns every connection away with "Error 1040: Too many connections", as a MySQL server at
// max_connections does, fail with datasources.ErrUnavailable, as for a server that cannot be reached.
func TestRepository_TooManyConnectionsIsUnavailable(t *testing.T) {
	t.Parallel()

	repo := New(refusingmysql.DB(t))

	_, err := repo.GetPublicPiece(t.Context(), "the-copper-kettle")
	require.ErrorIs(t, err, datasources.ErrUnavailable, "a read in a read-only transaction")
	var mysqlErr *mysqldriver.MySQLError
	require.ErrorAs(t, err, &mysqlErr, "the cause is the server's error, not a network failure")
	assert.Equal(t, &mysqldriver.MySQLError{Number: refusingmysql.ErConCount, Message: refusingmysql.Message}, mysqlErr)
	_, err = repo.GetUser(t.Context(), "google-oauth2|123")
	require.ErrorIs(t, err, datasources.ErrUnavailable, "the user lookup of the auth middleware")
	_, err = repo.GetAPITokenByHash(t.Context(), "00")
	require.ErrorIs(t, err, datasources.ErrUnavailable, "the API token lookup of the auth middleware")
	err = repo.ApproveRevision(t.Context(), datasources.ApproveRequest{RevisionID: 1})
	require.ErrorIs(t, err, datasources.ErrUnavailable, "a write transaction, through databaseError")
}
