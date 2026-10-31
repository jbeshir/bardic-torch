//go:build integration

package mysql_test

import (
	"database/sql"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migratedTables are the tables the migrations create, besides golang-migrate's own schema_migrations.
var migratedTables = []string{
	"users", "api_tokens",
	"revisions", "revision_contributors",
	"terms", "term_aliases", "term_ancestors",
	"collections", "events",
	"persons", "person_aliases", "person_roles", "person_affiliations", "person_relations", "person_preferences",
	"pieces", "piece_secondary_kinds", "piece_aliases", "piece_terms", "piece_relations", "piece_credits",
	"conservation_history", "piece_preferences",
	"links", "link_attachments", "performances", "performance_performers",
}

func TestMigrations_UpDownUp(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, err := mysql.ConfigFromURL(mysqltest.NewDatabaseURL(t))
	require.NoError(t, err)
	db, err := mysql.Connect(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.ElementsMatch(t, migratedTables, listTables(t, db))

	m, err := mysql.NewMigrator(cfg)
	require.NoError(t, err)
	require.NoError(t, m.Down())
	assert.Empty(t, listTables(t, db))
	require.NoError(t, m.Up())
	assert.ElementsMatch(t, migratedTables, listTables(t, db))

	version, dirty, err := m.Version()
	require.NoError(t, err)
	newest, err := mysql.NewestMigrationVersion()
	require.NoError(t, err)
	assert.Equal(t, newest, version)
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

// listTables returns the base tables of the current database, except golang-migrate's schema_migrations.
func listTables(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), "SELECT table_name FROM information_schema.tables "+
		"WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE' AND table_name <> 'schema_migrations'")
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })

	var tables []string
	for rows.Next() {
		var table string
		require.NoError(t, rows.Scan(&table))
		tables = append(tables, table)
	}
	require.NoError(t, rows.Err())

	return tables
}

// Identifier columns use binary collations so that IDs compare case-sensitively; text columns do not. User IDs
// use a NO PAD collation, so that trailing spaces are not ignored. Slugs, sort letters, normalized URLs and
// platforms are ASCII and binary.
const (
	binaryCollation      = "utf8mb4_0900_bin"
	asciiBinaryCollation = "ascii_bin"
	textCollation        = "utf8mb4_0900_ai_ci"
)

// identifierCollations lists every collated column that does not use textCollation.
var identifierCollations = map[string]string{
	"users.id":                   binaryCollation,
	"api_tokens.id":              asciiBinaryCollation,
	"api_tokens.user_id":         binaryCollation,
	"api_tokens.token_hash":      asciiBinaryCollation,
	"api_tokens.token_prefix":    asciiBinaryCollation,
	"revisions.author_user_id":   binaryCollation,
	"revisions.reviewer_user_id": binaryCollation,
	"terms.slug":                 asciiBinaryCollation,
	"collections.sort_letter":    asciiBinaryCollation,
	"events.slug":                asciiBinaryCollation,
	"persons.slug":               asciiBinaryCollation,
	"persons.sort_letter":        asciiBinaryCollation,
	"persons.claimed_by_user_id": binaryCollation,
	"pieces.slug":                asciiBinaryCollation,
	"pieces.sort_letter":         asciiBinaryCollation,
	"links.normalized_url":       asciiBinaryCollation,
	"links.platform":             asciiBinaryCollation,
}

// textColumns spot-checks that text whose comparison users see is case- and accent-insensitive; every collated
// column outside identifierCollations is checked to be textCollation as well.
var textColumns = []string{
	"users.display_name", "terms.name", "terms.display_name", "collections.name", "collections.sort_name",
	"persons.name", "persons.sort_name", "person_aliases.name", "pieces.title", "pieces.sort_title",
	"pieces.notes_md", "piece_aliases.text", "links.url",
}

func TestMigrations_ColumnCollations(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	rows, err := db.QueryContext(t.Context(), "SELECT CONCAT(table_name, '.', column_name), collation_name "+
		"FROM information_schema.columns WHERE table_schema = DATABASE() AND collation_name IS NOT NULL "+
		"AND table_name <> 'schema_migrations'")
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })

	got := map[string]string{}
	for rows.Next() {
		var column, collation string
		require.NoError(t, rows.Scan(&column, &collation))
		got[column] = collation
	}
	require.NoError(t, rows.Err())

	for column, want := range identifierCollations {
		assert.Equal(t, want, got[column], "column [%s]", column)
	}
	for _, column := range textColumns {
		assert.Contains(t, got, column)
	}
	for column, collation := range got {
		if _, ok := identifierCollations[column]; !ok {
			assert.Equal(t, textCollation, collation, "column [%s]", column)
		}
	}
}
