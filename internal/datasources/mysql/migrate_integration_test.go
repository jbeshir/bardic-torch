//go:build integration

package mysql_test

import (
	"database/sql"
	"maps"
	"slices"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migratedTables are the tables the migrations create, besides golang-migrate's own schema_migrations.
var migratedTables = []string{
	"users",
	"revisions", "revision_contributors",
	"terms", "term_aliases", "term_ancestors",
	"collections", "events",
	"persons", "person_aliases", "person_roles", "person_affiliations", "person_relations",
	"pieces", "piece_secondary_kinds", "piece_aliases", "piece_terms", "piece_relations", "piece_credits",
	"conservation_history",
	"links", "link_attachments", "performances", "performance_performers",
	"contact_requests",
}

// migratedViews are the views the migrations create.
var migratedViews = []string{"public_terms", "listed_persons", "public_link_attachments"}

func TestMigrations_UpDownUp(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, err := mysql.ConfigFromURL(mysqltest.NewDatabaseURL(t))
	require.NoError(t, err)
	db, err := mysql.Connect(ctx, cfg, mysqltest.Pool())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.ElementsMatch(t, migratedTables, listTables(t, db, baseTable))
	require.ElementsMatch(t, migratedViews, listTables(t, db, view))
	requireViewsReadable(t, db)

	m, err := mysql.NewMigrator(cfg)
	require.NoError(t, err)
	require.NoError(t, m.Down())
	assert.Empty(t, listTables(t, db, baseTable))
	assert.Empty(t, listTables(t, db, view))
	require.NoError(t, m.Up())
	assert.ElementsMatch(t, migratedTables, listTables(t, db, baseTable))
	assert.ElementsMatch(t, migratedViews, listTables(t, db, view))
	requireViewsReadable(t, db)

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

// The table types listTables lists.
const (
	baseTable = "BASE TABLE"
	view      = "VIEW"
)

// listTables returns the tables of tableType in the current database, except golang-migrate's schema_migrations.
func listTables(t *testing.T, db *sql.DB, tableType string) []string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), "SELECT table_name FROM information_schema.tables "+
		"WHERE table_schema = DATABASE() AND table_type = ? AND table_name <> 'schema_migrations'", tableType)
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
// use a NO PAD collation, so that trailing spaces are not ignored, as do link URLs, so that a snapshot's URL
// matches a link's only exactly. Slugs, sort letters, normalized URLs and platforms are ASCII and binary.
const (
	binaryCollation      = "utf8mb4_0900_bin"
	asciiBinaryCollation = "ascii_bin"
	textCollation        = "utf8mb4_0900_ai_ci"
)

// identifierCollations lists every collated column that does not use textCollation.
var identifierCollations = map[string]string{
	"users.id":                             binaryCollation,
	"revisions.author_user_id":             binaryCollation,
	"revisions.reviewer_user_id":           binaryCollation,
	"contact_requests.resolved_by_user_id": binaryCollation,
	"terms.slug":                           asciiBinaryCollation,
	"collections.sort_letter":              asciiBinaryCollation,
	"events.slug":                          asciiBinaryCollation,
	"persons.slug":                         asciiBinaryCollation,
	"persons.sort_letter":                  asciiBinaryCollation,
	"pieces.slug":                          asciiBinaryCollation,
	"pieces.sort_letter":                   asciiBinaryCollation,
	"links.url":                            binaryCollation,
	"links.normalized_url":                 asciiBinaryCollation,
	"links.platform":                       asciiBinaryCollation,
}

// textColumns spot-checks that text whose comparison users see is case- and accent-insensitive; every collated
// column outside identifierCollations is checked to be textCollation as well.
var textColumns = []string{
	"users.display_name", "terms.name", "terms.display_name", "collections.name", "collections.sort_name",
	"persons.name", "persons.sort_name", "person_aliases.name", "pieces.title", "pieces.sort_title",
	"pieces.notes_md", "piece_aliases.text", "links.title",
}

func TestMigrations_ColumnCollations(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	// Views' columns have their tables' collations.
	rows, err := db.QueryContext(t.Context(), "SELECT CONCAT(c.table_name, '.', c.column_name), c.collation_name "+
		"FROM information_schema.columns c JOIN information_schema.tables t ON t.table_schema = c.table_schema "+
		"AND t.table_name = c.table_name WHERE c.table_schema = DATABASE() AND t.table_type = 'BASE TABLE' "+
		"AND c.collation_name IS NOT NULL AND c.table_name <> 'schema_migrations'")
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

// TestMigrations_ViewsCheckTheReader checks that every view checks the privileges of the user reading it, so that
// no view depends on the user that created it still existing.
func TestMigrations_ViewsCheckTheReader(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	rows, err := db.QueryContext(t.Context(),
		"SELECT table_name, security_type FROM information_schema.views WHERE table_schema = DATABASE()")
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })

	var views []string
	for rows.Next() {
		var name, security string
		require.NoError(t, rows.Scan(&name, &security))
		assert.Equal(t, "INVOKER", security, name)
		views = append(views, name)
	}
	require.NoError(t, rows.Err())
	assert.ElementsMatch(t, migratedViews, views)
}

// requireViewsReadable reads from every view, which fails once a table column a view selects has been dropped or
// renamed: MySQL lets the ALTER TABLE succeed and fails the view's reads instead (error 1356).
func requireViewsReadable(t *testing.T, db *sql.DB) {
	t.Helper()

	require.ElementsMatch(t, migratedViews, slices.Collect(maps.Keys(viewReads)), "a read for every view")
	for name, read := range viewReads {
		_, err := db.ExecContext(t.Context(), read)
		require.NoError(t, err, "reading view [%s]", name)
	}
}

// viewReads reads each of migratedViews, without returning a row.
var viewReads = map[string]string{
	"public_terms":            "SELECT * FROM public_terms LIMIT 0",
	"listed_persons":          "SELECT * FROM listed_persons LIMIT 0",
	"public_link_attachments": "SELECT * FROM public_link_attachments LIMIT 0",
}

// TestMigrations_DroppedViewColumnBreaksReads pins the MySQL behaviour that migration 009 and DEPLOY.md warn of:
// dropping a column a view selects succeeds, and reads of the view then fail, so only reading the views (as
// TestMigrations_UpDownUp does) catches a migration that forgets to recreate them.
func TestMigrations_DroppedViewColumnBreaksReads(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	_, err := db.ExecContext(t.Context(), "ALTER TABLE persons DROP COLUMN pronouns")
	require.NoError(t, err, "MySQL does not refuse to drop a column a view selects")

	_, err = db.ExecContext(t.Context(), viewReads["listed_persons"])
	require.ErrorContains(t, err, "1356")
}
