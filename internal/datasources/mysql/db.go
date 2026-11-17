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

// ErrMalformedDatabaseURL is returned by ConfigFromURL for a database URL that does not parse.
var ErrMalformedDatabaseURL = errors.New("database URL is malformed (the URL is not shown, as it holds the password)")

// maxExecutionTimeParam is the session variable limiting how long a SELECT may run, in milliseconds, and
// maxExecutionTimeMillis the limit every connection ConfigFromURL describes sets. Anonymous catalogue reads are
// the server's costliest statements, and a slow one holds a pooled connection that /readyz and authentication
// also need, so none may run long.
const (
	maxExecutionTimeParam  = "max_execution_time"
	maxExecutionTimeMillis = "2000"
)

// ConfigFromURL parses a database URL of the form mysql://user:password@host[:port]/database, as provided by
// the dokku mysql plugin, into a driver config. The user and password are URL-decoded and the port defaults to
// 3306.
//
// Connections made with the returned config parse DATETIME columns into time.Time in UTC, use a UTC session
// time zone, use the utf8mb4 character set, report matched rather than changed rows as rows affected, and have
// the server stop any SELECT that runs longer than maxExecutionTimeMillis (error 3024, datasources.ErrQueryTimeout).
// The limit applies to every SELECT, locking reads and GET_LOCK waits included, but not to INSERT, UPDATE, DELETE
// or DDL statements, nor to a SELECT within them: a connection that must wait longer sets max_execution_time
// itself, as the migrator does.
//
// Errors never quote the URL or a part of it, which may be the password: url.Parse's errors do ("invalid port"
// quotes what follows the last colon of a password holding "/"), so they are replaced by ErrMalformedDatabaseURL.
func ConfigFromURL(rawURL string) (*mysqldriver.Config, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, ErrMalformedDatabaseURL
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
	cfg.Params = map[string]string{"time_zone": "'+00:00'", maxExecutionTimeParam: maxExecutionTimeMillis}

	return cfg, nil
}

// PoolLimits are the limits of a connection pool. Every connection of a pool, idle ones included, takes one of the
// server's max_connections (151 on the stock mysql:8.4 image).
type PoolLimits struct {
	// MaxOpenConns caps the pool's connections; a query that finds them all busy waits for one.
	MaxOpenConns int
	// MaxIdleConns caps the connections kept open while unused.
	MaxIdleConns int
	// ConnMaxLifetime closes a connection this long after it was opened.
	ConnMaxLifetime time.Duration
	// ConnMaxIdleTime closes a connection that has been idle this long.
	ConnMaxIdleTime time.Duration
}

// ProductionPool returns the limits of the API's and the importer's pools. At most two API processes overlap during
// a deploy, beside the migrate, an import and an operator's session, so ten each stays far under 151. Idle
// connections close after five minutes, well under the server's wait_timeout, and every connection is replaced
// after 30 minutes so that a restarted service container is reached.
func ProductionPool() PoolLimits {
	return PoolLimits{
		MaxOpenConns:    10,
		MaxIdleConns:    10,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

// Connect opens a connection pool using cfg, with limits, and checks that the database is reachable.
func Connect(ctx context.Context, cfg *mysqldriver.Config, limits PoolLimits) (*sql.DB, error) {
	connector, err := mysqldriver.NewConnector(cfg)
	if err != nil {
		return nil, fmt.Errorf("configuring MySQL connector: %w", err)
	}

	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(limits.MaxOpenConns)
	db.SetConnMaxLifetime(limits.ConnMaxLifetime)
	db.SetConnMaxIdleTime(limits.ConnMaxIdleTime)
	db.SetMaxIdleConns(limits.MaxIdleConns)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("checking MySQL DB connection: %w", err)
	}

	return db, nil
}
