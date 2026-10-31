package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

const defaultPort = "3306"

// ConfigFromURL parses a database URL of the form mysql://user:password@host[:port]/database, as provided by
// the dokku mysql plugin, into a driver config. The user and password are URL-decoded and the port defaults to
// 3306.
//
// Connections made with the returned config parse DATETIME columns into time.Time in UTC, use a UTC session
// time zone, use the utf8mb4 character set, and report matched rather than changed rows as rows affected.
func ConfigFromURL(rawURL string) (*mysqldriver.Config, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing database URL: %w", err)
	}
	if u.Scheme != "mysql" {
		return nil, fmt.Errorf("database URL scheme must be mysql, got [%s]", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, errors.New("database URL has no host")
	}
	if u.RawQuery != "" {
		return nil, errors.New("database URL must not have query parameters")
	}

	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" || strings.Contains(dbName, "/") {
		return nil, errors.New("database URL path must be a single database name")
	}

	port := u.Port()
	if port == "" {
		port = defaultPort
	}

	cfg := mysqldriver.NewConfig()
	cfg.User = u.User.Username()
	cfg.Passwd, _ = u.User.Password()
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(u.Hostname(), port)
	cfg.DBName = dbName
	cfg.Collation = "utf8mb4_0900_ai_ci"
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.ClientFoundRows = true
	cfg.Params = map[string]string{"time_zone": "'+00:00'"}

	return cfg, nil
}

// Connect opens a connection pool using cfg and checks that the database is reachable.
func Connect(ctx context.Context, cfg *mysqldriver.Config) (*sql.DB, error) {
	connector, err := mysqldriver.NewConnector(cfg)
	if err != nil {
		return nil, fmt.Errorf("configuring MySQL connector: %w", err)
	}

	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("checking MySQL DB connection: %w", err)
	}

	return db, nil
}
