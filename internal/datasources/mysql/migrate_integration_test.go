//go:build integration

package mysql_test

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrations_UpDownUp(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, err := mysql.ConfigFromURL(mysqltest.NewDatabaseURL(t))
	require.NoError(t, err)
	db, err := mysql.Connect(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	countTables := func() int {
		var count int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables "+
			"WHERE table_schema = DATABASE() AND table_name IN ('users', 'api_tokens')").Scan(&count))
		return count
	}
	require.Equal(t, 2, countTables())

	m, err := mysql.NewMigrator(cfg)
	require.NoError(t, err)
	require.NoError(t, m.Down())
	assert.Zero(t, countTables())
	require.NoError(t, m.Up())
	assert.Equal(t, 2, countTables())

	version, dirty, err := m.Version()
	require.NoError(t, err)
	assert.Equal(t, uint(2), version)
	assert.False(t, dirty)

	sourceErr, dbErr := m.Close()
	require.NoError(t, sourceErr)
	require.NoError(t, dbErr)

	// Closing the migrator closes only its own pool.
	repo := mysql.New(db)
	require.NoError(t, repo.Ping(ctx))
	_, err = repo.EnsureUser(ctx, datasources.NewUser{ID: "auth0|after-migrate", DisplayName: "After",
		CreatedAt: baseTime})
	require.NoError(t, err)
}

// Identifier columns use binary collations so that IDs compare case-sensitively; text columns do not.
const (
	binaryCollation      = "utf8mb4_bin"
	asciiBinaryCollation = "ascii_bin"
	textCollation        = "utf8mb4_0900_ai_ci"
)

func TestMigrations_ColumnCollations(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	rows, err := db.QueryContext(t.Context(), "SELECT CONCAT(table_name, '.', column_name), collation_name "+
		"FROM information_schema.columns WHERE table_schema = DATABASE() AND collation_name IS NOT NULL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })

	got := map[string]string{}
	for rows.Next() {
		var column, collation string
		require.NoError(t, rows.Scan(&column, &collation))
		got[column] = collation
	}
	require.NoError(t, rows.Err())

	assert.Equal(t, map[string]string{
		"users.id":                binaryCollation,
		"users.display_name":      textCollation,
		"users.role":              textCollation,
		"api_tokens.id":           asciiBinaryCollation,
		"api_tokens.user_id":      binaryCollation,
		"api_tokens.token_hash":   asciiBinaryCollation,
		"api_tokens.token_prefix": asciiBinaryCollation,
		"api_tokens.name":         textCollation,
	}, got)
}
