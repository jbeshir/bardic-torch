//go:build integration

package mysqltest

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireDropped fails unless no database is named name.
func requireDropped(t *testing.T, name string) {
	t.Helper()

	server, err := Connect(t.Context(), os.Getenv(AdminURLEnv)+"/information_schema")
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Close() })

	var found string
	err = server.QueryRowContext(t.Context(),
		"SELECT schema_name FROM information_schema.schemata WHERE schema_name = ?", name).Scan(&found)
	require.ErrorIs(t, err, sql.ErrNoRows, "database [%s] was not dropped", name)
}

func TestNewDatabase_IsolatedMigratedAndDropped(t *testing.T) {
	t.Parallel()

	var names []string
	for range 2 {
		t.Run("database", func(t *testing.T) {
			db := NewDatabase(t)

			var name string
			require.NoError(t, db.QueryRowContext(t.Context(), "SELECT DATABASE()").Scan(&name))
			assert.True(t, strings.HasPrefix(name, databaseNamePrefix), name)
			names = append(names, name)

			for _, table := range []string{"users", "api_tokens"} {
				var count int
				require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count))
				assert.Zero(t, count, table)
			}
		})
	}
	require.Len(t, names, 2)
	assert.NotEqual(t, names[0], names[1])

	for _, name := range names {
		requireDropped(t, name)
	}
}

func TestCreateDatabase_MigratedUntilDropped(t *testing.T) {
	t.Parallel()

	dbURL, drop, err := CreateDatabase(t.Context())
	require.NoError(t, err)

	db, err := Connect(t.Context(), dbURL)
	require.NoError(t, err)
	var name string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT DATABASE()").Scan(&name))
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM pieces").Scan(&count))
	assert.Zero(t, count)
	require.NoError(t, db.Close())

	require.NoError(t, drop(t.Context()))
	requireDropped(t, name)
}

// TestNewDatabase_PoolIsBounded checks that a test database's pool has Pool's limits rather than the application's:
// with the application's ten idle connections per test database, the suite ran the server out of connections.
func TestNewDatabase_PoolIsBounded(t *testing.T) {
	t.Parallel()

	db := NewDatabase(t)
	limits := Pool()
	assert.Equal(t, limits.MaxOpenConns, db.Stats().MaxOpenConnections)

	// More connections at once than the pool keeps idle: all but MaxIdleConns are closed once they are released.
	conns := make([]*sql.Conn, limits.MaxOpenConns)
	for i := range conns {
		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		conns[i] = conn
	}
	assert.Equal(t, limits.MaxOpenConns, db.Stats().OpenConnections)
	for _, conn := range conns {
		require.NoError(t, conn.Close())
	}
	assert.Equal(t, limits.MaxIdleConns, db.Stats().OpenConnections)
	assert.Less(t, limits.MaxIdleConns, limits.MaxOpenConns)
}
