//go:build integration

package mysqltest

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	_, cfg := newDatabaseConfig(t)
	admin := connectAdmin(t, cfg)
	for _, name := range names {
		var found string
		err := admin.QueryRowContext(t.Context(),
			"SELECT schema_name FROM information_schema.schemata WHERE schema_name = ?", name).Scan(&found)
		assert.ErrorIs(t, err, sql.ErrNoRows, "database [%s] was not dropped", name)
	}
}
