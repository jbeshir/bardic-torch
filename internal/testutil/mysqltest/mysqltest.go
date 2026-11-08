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

// NewDatabase creates a migrated database as NewDatabaseURL does and returns a connection pool to it (Connect),
// with the application's session settings and the limits of Pool. The pool is closed when the test finishes.
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
// call drop once no test uses it. It is for a database several tests share (see catalogtest.Shared). No connection
// is kept open between the two: the admin connection is opened for the CREATE and again for the DROP, so a database
// that lives as long as the test binary does not also hold an admin connection that long.
func CreateDatabase(ctx context.Context) (dbURL string, drop func(context.Context) error, err error) {
	dbURL, cfg, err := newDatabaseConfig()
	if err != nil {
		return "", nil, err
	}

	adminCfg := cfg.Clone()
	adminCfg.DBName = ""
	quotedName := "`" + cfg.DBName + "`"
	if err := adminExec(ctx, adminCfg, "CREATE DATABASE "+quotedName); err != nil {
		return "", nil, fmt.Errorf("creating test database: %w", err)
	}
	drop = func(ctx context.Context) error {
		if err := adminExec(ctx, adminCfg, "DROP DATABASE "+quotedName); err != nil {
			return fmt.Errorf("dropping test database: %w", err)
		}

		return nil
	}

	if err := migrate(cfg); err != nil {
		return "", nil, errors.Join(err, drop(ctx))
	}

	return dbURL, drop, nil
}

// adminExec runs statement on a connection of its own to the server of adminCfg, closed before it returns.
func adminExec(ctx context.Context, adminCfg *mysqldriver.Config, statement string) error {
	admin, err := mysql.Connect(ctx, adminCfg, adminPool())
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", AdminURLEnv, err)
	}
	defer func() { _ = admin.Close() }()
	if _, err := admin.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("running %s: %w", statement, err)
	}

	return nil
}

// Pool returns the limits of the pools the helpers open. Every package's test binary runs its tests in parallel
// (go test's -parallel, GOMAXPROCS by default) and go test runs several such binaries at once (-p, also GOMAXPROCS),
// all against one server, whose max_connections is 151 when it is the stock mysql:8.4. A pool of the application's
// size (mysql.ProductionPool keeps ten idle connections) per test database ran a 20-core machine's suite out of
// connections: the databases catalogtest keeps for later tests and its shared databases live as long as the test
// binary, and their idle connections added up. A test pool so keeps one idle connection, enough for a test's
// statements one after another, and opens at most four, enough for the forced race tests (a transaction holding
// rows, the write waiting for them and the query watching it wait) and few enough that the tests running at once
// in every binary stay far under the server's limit. Connections never expire: a test database lives for one test
// binary at most.
func Pool() mysql.PoolLimits {
	return mysql.PoolLimits{MaxOpenConns: 4, MaxIdleConns: 1}
}

// adminPool is the limits of an admin pool: one connection for one CREATE or DROP DATABASE, closed with the pool.
func adminPool() mysql.PoolLimits {
	return mysql.PoolLimits{MaxOpenConns: 1}
}

// Connect opens a connection pool to the database at dbURL, with the application's session settings and the
// limits of Pool.
func Connect(ctx context.Context, dbURL string) (*sql.DB, error) {
	cfg, err := mysql.ConfigFromURL(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parsing test database URL: %w", err)
	}

	return ConnectConfig(ctx, cfg)
}

// ConnectConfig opens a connection pool as Connect does, from a config rather than a URL, for a test that changes
// the config (catalogtest.Shared's read-only pool).
func ConnectConfig(ctx context.Context, cfg *mysqldriver.Config) (*sql.DB, error) {
	db, err := mysql.Connect(ctx, cfg, Pool())
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
