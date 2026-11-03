//go:build integration

// Package mysqltest gives each integration test its own migrated MySQL database, so tests can run in
// parallel without sharing state.
package mysqltest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
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

	db, err := Connect(t.Context(), NewDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return db
}

// NewDatabaseURL creates a database with a random name, applies every migration to it, and returns its URL.
// The database is dropped when the test finishes.
func NewDatabaseURL(t testing.TB) string {
	t.Helper()

	dbURL, drop, err := CreateDatabase(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := drop(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})

	return dbURL
}

// CreateDatabase creates a migrated database as NewDatabaseURL does, but not tied to a test: the caller must
// call drop once no test uses it. It is for a database several tests share (see catalogtest.Shared).
func CreateDatabase(ctx context.Context) (dbURL string, drop func(context.Context) error, err error) {
	dbURL, cfg, err := newDatabaseConfig()
	if err != nil {
		return "", nil, err
	}

	adminCfg := cfg.Clone()
	adminCfg.DBName = ""
	admin, err := mysql.Connect(ctx, adminCfg)
	if err != nil {
		return "", nil, fmt.Errorf("connecting to %s: %w", AdminURLEnv, err)
	}

	quotedName := "`" + cfg.DBName + "`"
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+quotedName); err != nil {
		_ = admin.Close()
		return "", nil, fmt.Errorf("creating test database: %w", err)
	}
	drop = func(ctx context.Context) error {
		defer func() { _ = admin.Close() }()
		if _, err := admin.ExecContext(ctx, "DROP DATABASE "+quotedName); err != nil {
			return fmt.Errorf("dropping test database: %w", err)
		}

		return nil
	}

	if err := migrate(cfg); err != nil {
		return "", nil, errors.Join(err, drop(ctx))
	}

	return dbURL, drop, nil
}

// Connect opens a connection pool to the database at dbURL, configured as the application configures its own.
func Connect(ctx context.Context, dbURL string) (*sql.DB, error) {
	cfg, err := mysql.ConfigFromURL(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parsing test database URL: %w", err)
	}
	db, err := mysql.Connect(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connecting to test database: %w", err)
	}

	return db, nil
}

// migrate applies every migration to the database of cfg.
func migrate(cfg *mysqldriver.Config) error {
	m, err := mysql.NewMigrator(cfg)
	if err != nil {
		return fmt.Errorf("creating migrator: %w", err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil {
		return fmt.Errorf("migrating test database: %w", err)
	}

	return nil
}

// newDatabaseConfig picks a new random database name on the server at AdminURLEnv, returning its URL and
// the config parsed from it.
func newDatabaseConfig() (string, *mysqldriver.Config, error) {
	adminURL, ok := os.LookupEnv(AdminURLEnv)
	if !ok {
		return "", nil, fmt.Errorf(
			"%s is not set; run the integration tests with make test-integration or make test-integration-local",
			AdminURLEnv)
	}
	u, err := url.Parse(adminURL)
	if err != nil {
		return "", nil, fmt.Errorf("parsing %s: %w", AdminURLEnv, err)
	}
	if u.Path != "" && u.Path != "/" {
		return "", nil, fmt.Errorf("%s must not name a database", AdminURLEnv)
	}

	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("generating database name: %w", err)
	}
	u.Path = "/" + databaseNamePrefix + hex.EncodeToString(b)
	cfg, err := mysql.ConfigFromURL(u.String())
	if err != nil {
		return "", nil, fmt.Errorf("parsing %s: %w", AdminURLEnv, err)
	}

	return u.String(), cfg, nil
}
