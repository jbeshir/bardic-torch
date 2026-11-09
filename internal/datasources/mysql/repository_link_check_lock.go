package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
)

var _ datasources.LinkCheckLocker = (*Repository)(nil)

// The lock SQL is not in queries.sql: GET_LOCK and RELEASE_LOCK are session functions, run on one pinned connection.

// linkCheckLockName is the advisory lock (GET_LOCK) a run of the link checker holds: one per database, as named
// locks are server-wide. Lock names are at most 64 characters.
const linkCheckLockName = "LEFT(CONCAT('bardic-torch-check-links:', DATABASE()), 64)"

// errLinkCheckLockFailed is returned when GET_LOCK fails (returns NULL) rather than finding the lock taken.
var errLinkCheckLockFailed = errors.New("the link check lock could not be requested")

// linkCheckLock is the lock and the connection whose session holds it. The connection is taken out of the pool
// for as long as the lock is held, so the pool's idle and lifetime limits never close it under the run.
type linkCheckLock struct {
	conn *sql.Conn
}

// LockLinkCheck takes the link checker's lock on a connection of its own, without waiting: ErrLinkCheckInProgress
// if another session holds it.
func (r *Repository) LockLinkCheck(ctx context.Context) (datasources.LinkCheckLock, error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, databaseError("connecting for the link check lock", err)
	}

	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK("+linkCheckLockName+", 0)").Scan(&locked); err != nil {
		_ = conn.Close()
		return nil, databaseError("taking the link check lock", err)
	}
	switch {
	case !locked.Valid:
		_ = conn.Close()
		return nil, errLinkCheckLockFailed
	case locked.Int64 != 1:
		_ = conn.Close()
		return nil, datasources.ErrLinkCheckInProgress
	}

	return &linkCheckLock{conn: conn}, nil
}

// Ping checks that this connection's session still holds the lock.
func (l *linkCheckLock) Ping(ctx context.Context) error {
	var held sql.NullBool
	err := l.conn.QueryRowContext(ctx, "SELECT IS_USED_LOCK("+linkCheckLockName+") = CONNECTION_ID()").Scan(&held)
	if err != nil {
		return errors.Join(datasources.ErrLinkCheckLockLost, databaseError("checking the link check lock", err))
	}
	if !held.Valid || !held.Bool {
		return datasources.ErrLinkCheckLockLost
	}

	return nil
}

// Release releases the lock and returns the connection. If the lock cannot be released, the connection is
// discarded, which ends its session and so releases the lock.
func (l *linkCheckLock) Release(ctx context.Context) {
	if _, err := l.conn.ExecContext(context.WithoutCancel(ctx), "DO RELEASE_LOCK("+linkCheckLockName+")"); err != nil {
		_ = l.conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	_ = l.conn.Close()
}

// LinkCheckPool returns the limits of the link checker's pool, for a run of at most maxDuration: one connection
// holds the run's lock (LockLinkCheck) and two serve its reads and writes, so a run adds three connections to the
// server's count. Neither limit closes a connection during a run: a connection the pool closed and reopened would
// be harmless for the writes, but the lock's session must last the whole run.
func LinkCheckPool(maxDuration time.Duration) PoolLimits {
	const connections = 3
	keep := maxDuration + 5*time.Minute

	return PoolLimits{
		MaxOpenConns: connections, MaxIdleConns: connections, ConnMaxLifetime: keep, ConnMaxIdleTime: keep,
	}
}
