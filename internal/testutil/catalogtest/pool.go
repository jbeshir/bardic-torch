//go:build integration

package catalogtest

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
)

// reusable is the migrated databases a test returned empty, ready for the next Writable call.
var reusable struct {
	mu   sync.Mutex
	free []*pooledDatabase
}

type pooledDatabase struct {
	db *sql.DB
}

// Writable returns a pool to an empty migrated database that only t uses, like mysqltest.NewDatabase, for tests
// that write. When t finishes the database is emptied (every row deleted and the auto-increment counters
// restarted, so ids start again) and handed to a later test instead of being dropped, so a test binary migrates
// about as many databases as it runs tests in parallel rather than one per test: a migrated database costs
// seconds and MySQL serialises much of that DDL, while deleting rows is cheap. A test that changes anything but
// rows (a schema, a session-wide setting) must use mysqltest.NewDatabase instead. Needs Main as the package's
// TestMain, which drops the databases.
func Writable(t testing.TB) *sql.DB {
	t.Helper()

	if !mainRunning.Load() {
		t.Fatal("catalogtest: a Writable database needs catalogtest.Main as the package's TestMain")
	}
	p := takeFree()
	if p == nil {
		p = createPooled(t)
	}
	t.Cleanup(func() {
		if err := empty(context.WithoutCancel(t.Context()), p.db); err != nil {
			// A database that could not be emptied is left out of the pool, and dropped by Main.
			t.Errorf("catalogtest: emptying a Writable database: %v", err)
			return
		}
		reusable.mu.Lock()
		defer reusable.mu.Unlock()
		reusable.free = append(reusable.free, p)
	})

	return p.db
}

func takeFree() *pooledDatabase {
	reusable.mu.Lock()
	defer reusable.mu.Unlock()
	if len(reusable.free) == 0 {
		return nil
	}
	p := reusable.free[len(reusable.free)-1]
	reusable.free = reusable.free[:len(reusable.free)-1]

	return p
}

func createPooled(t testing.TB) *pooledDatabase {
	t.Helper()

	// The database outlives t, so it is created and dropped with a context t does not cancel.
	dbURL, drop, err := mysqltest.CreateDatabase(context.WithoutCancel(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	registerCleanup(func() error { return drop(context.Background()) })
	db, err := mysqltest.Connect(context.WithoutCancel(t.Context()), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	// Registered after drop, so it runs before it.
	registerCleanup(db.Close)

	return &pooledDatabase{db: db}
}

// empty deletes every row of every base table of db's database except the migrator's own bookkeeping table, and
// restarts the auto-increment counters. It deletes instead of truncating because TRUNCATE is DDL, which for the
// FULLTEXT tables costs about as much as migrating a new database.
func empty(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// The server writes the statements, so no table name is spliced into SQL by Go.
	rows, err := conn.QueryContext(ctx,
		"SELECT CONCAT('DELETE FROM `', t.table_name, '`') FROM information_schema.tables t "+
			"WHERE t.table_schema = DATABASE() AND t.table_type = 'BASE TABLE' AND t.table_name <> 'schema_migrations' "+
			"UNION ALL "+
			"SELECT CONCAT('ALTER TABLE `', c.table_name, '` AUTO_INCREMENT = 1') FROM information_schema.columns c "+
			"JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name "+
			"WHERE c.table_schema = DATABASE() AND t.table_type = 'BASE TABLE' AND c.extra LIKE '%auto_increment%' "+
			"ORDER BY 1 DESC")
	if err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}
	var statements []string
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			_ = rows.Close()
			return fmt.Errorf("listing tables: %w", err)
		}
		statements = append(statements, statement)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("listing tables: %w", err)
	}
	_ = rows.Close()

	// ORDER BY 1 DESC lists every DELETE before any ALTER, as a counter restarts only on an empty table. Checks are
	// off so tables can be emptied in any order; the setting is per session and conn goes back to db's pool, so it
	// is turned back on below.
	if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		return fmt.Errorf("disabling foreign key checks: %w", err)
	}
	for _, statement := range statements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("emptying (%s): %w", statement, err)
		}
	}
	if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1"); err != nil {
		return fmt.Errorf("enabling foreign key checks: %w", err)
	}

	return nil
}
