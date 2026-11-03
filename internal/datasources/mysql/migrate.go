package mysql

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jbeshir/bardic-torch/migrations"
)

// NewMigrator returns a migrator applying the embedded migrations to the database described by cfg.
//
// The migrator opens and owns its own connection pool, separate from any pool the caller uses, so closing it
// never affects the application's connections. Callers must Close it.
func NewMigrator(cfg *mysqldriver.Config) (*migrate.Migrate, error) {
	migrateCfg := migratorConfig(cfg)

	connector, err := mysqldriver.NewConnector(migrateCfg)
	if err != nil {
		return nil, fmt.Errorf("configuring MySQL connector: %w", err)
	}
	db := sql.OpenDB(connector)

	driver, err := migratemysql.WithInstance(db, &migratemysql.Config{})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating migration database driver: %w", err)
	}

	src, err := migrationSource()
	if err != nil {
		_ = driver.Close()
		return nil, err
	}

	m, err := migrate.NewWithInstance("iofs", src, "mysql", driver)
	if err != nil {
		_ = src.Close()
		_ = driver.Close()
		return nil, fmt.Errorf("creating migrator: %w", err)
	}

	return m, nil
}

// migratorConfig returns the config of the migrator's connections: cfg, with multiple statements per query for
// the migration files, and without the SELECT time limit, which would cut short golang-migrate's wait for its
// lock (GET_LOCK) and any long SELECT a migration runs.
func migratorConfig(cfg *mysqldriver.Config) *mysqldriver.Config {
	migrateCfg := cfg.Clone()
	migrateCfg.MultiStatements = true
	if migrateCfg.Params == nil {
		migrateCfg.Params = make(map[string]string)
	}
	migrateCfg.Params[maxExecutionTimeParam] = "0"

	return migrateCfg
}

// NewestMigrationVersion returns the highest version among the embedded migrations.
func NewestMigrationVersion() (uint, error) {
	src, err := migrationSource()
	if err != nil {
		return 0, err
	}
	defer func() { _ = src.Close() }()

	version, err := src.First()
	if err != nil {
		return 0, fmt.Errorf("reading first embedded migration: %w", err)
	}
	for {
		next, err := src.Next(version)
		if errors.Is(err, fs.ErrNotExist) {
			return version, nil
		}
		if err != nil {
			return 0, fmt.Errorf("reading embedded migration after version %d: %w", version, err)
		}
		version = next
	}
}

// migrationSource returns a source reading the embedded migrations. Callers must Close it.
func migrationSource() (source.Driver, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("reading embedded migrations: %w", err)
	}
	return src, nil
}
