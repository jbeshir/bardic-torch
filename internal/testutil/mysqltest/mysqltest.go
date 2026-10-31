//go:build integration

// Package mysqltest gives each integration test its own migrated MySQL database, so tests can run in
// parallel without sharing state.
package mysqltest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
)

// AdminURLEnv names the environment variable holding the URL of a MySQL account that may create and drop
// databases, in the form mysql://user:password@host:port with no database.
const AdminURLEnv = "TEST_MYSQL_ADMIN_URL"

const databaseNamePrefix = "bt_test_"

// NewDatabase creates a migrated database as NewDatabaseURL does and returns a connection pool to it,
// configured as the application configures its own. The pool is closed when the test finishes.
func NewDatabase(t testing.TB) *sql.DB {
	t.Helper()

	cfg, err := mysql.ConfigFromURL(NewDatabaseURL(t))
	if err != nil {
		t.Fatalf("parsing test database URL: %v", err)
	}

	db, err := mysql.Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return db
}

// NewDatabaseURL creates a database with a random name, applies every migration to it, and returns its URL.
// The database is dropped when the test finishes.
func NewDatabaseURL(t testing.TB) string {
	t.Helper()

	dbURL, cfg := newDatabaseConfig(t)
	admin := connectAdmin(t, cfg)

	quotedName := "`" + cfg.DBName + "`"
	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+quotedName); err != nil {
		t.Fatalf("creating test database: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		if _, err := admin.ExecContext(ctx, "DROP DATABASE "+quotedName); err != nil {
			t.Errorf("dropping test database: %v", err)
		}
	})

	m, err := mysql.NewMigrator(cfg)
	if err != nil {
		t.Fatalf("creating migrator: %v", err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil {
		t.Fatalf("migrating test database: %v", err)
	}

	return dbURL
}

// newDatabaseConfig picks a new random database name on the server at AdminURLEnv, returning its URL and
// the config parsed from it.
func newDatabaseConfig(t testing.TB) (string, *mysqldriver.Config) {
	t.Helper()

	adminURL, ok := os.LookupEnv(AdminURLEnv)
	if !ok {
		t.Fatalf("%s is not set; run the integration tests with make test-integration or make test-integration-local",
			AdminURLEnv)
	}
	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parsing %s: %v", AdminURLEnv, err)
	}
	if u.Path != "" && u.Path != "/" {
		t.Fatalf("%s must not name a database", AdminURLEnv)
	}

	u.Path = "/" + databaseNamePrefix + randomHex(t)
	cfg, err := mysql.ConfigFromURL(u.String())
	if err != nil {
		t.Fatalf("parsing %s: %v", AdminURLEnv, err)
	}

	return u.String(), cfg
}

// connectAdmin connects to the server of cfg without selecting a database. The pool is closed when the test
// finishes.
func connectAdmin(t testing.TB, cfg *mysqldriver.Config) *sql.DB {
	t.Helper()

	adminCfg := cfg.Clone()
	adminCfg.DBName = ""
	admin, err := mysql.Connect(t.Context(), adminCfg)
	if err != nil {
		t.Fatalf("connecting to %s: %v", AdminURLEnv, err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	return admin
}

func randomHex(t testing.TB) string {
	t.Helper()

	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generating database name: %v", err)
	}

	return hex.EncodeToString(b)
}
