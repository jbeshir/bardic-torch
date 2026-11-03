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

// lockWaitTimeoutParam is the session variable limiting how long a statement waits for a row lock, in seconds, and
// lockWaitTimeoutSeconds the limit every connection ConfigFromURL describes sets. It is far below InnoDB's 50
// seconds, so that a write transaction (withWriteTx) fails and retries well within the server's write timeout.
const (
	lockWaitTimeoutParam   = "innodb_lock_wait_timeout"
	lockWaitTimeoutSeconds = "3"
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
// itself, as the migrator does. Every statement waits at most lockWaitTimeoutSeconds
// for a row lock (error 1205), so that write transactions fail and retry within the server's write timeout.
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
	cfg.Params = map[string]string{
		"time_zone":           "'+00:00'",
		maxExecutionTimeParam: maxExecutionTimeMillis,
		lockWaitTimeoutParam:  lockWaitTimeoutSeconds,
	}

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
