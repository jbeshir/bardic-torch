package mysql

import (
	"database/sql"
	"fmt"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jbeshir/bardic-torch/migrations"
)

// NewMigrator returns a migrator applying the embedded migrations to the database described by cfg.
//
// The migrator opens and owns its own connection pool, separate from any pool the caller uses, so closing it
// never affects the application's connections. Callers must Close it.
func NewMigrator(cfg *mysqldriver.Config) (*migrate.Migrate, error) {
	migrateCfg := cfg.Clone()
	migrateCfg.MultiStatements = true

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

	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		_ = driver.Close()
		return nil, fmt.Errorf("reading embedded migrations: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "mysql", driver)
	if err != nil {
		_ = source.Close()
		_ = driver.Close()
		return nil, fmt.Errorf("creating migrator: %w", err)
	}

	return m, nil
}
