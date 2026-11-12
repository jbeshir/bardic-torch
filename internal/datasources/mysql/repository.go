package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
)

//go:generate go tool sqlc generate

var _ datasources.Repository = (*Repository)(nil)

// Repository implements the datasources interfaces on top of a MySQL connection pool.
type Repository struct {
	db      *sql.DB
	queries *queries.Queries
}

// New returns a Repository using db.
func New(db *sql.DB) *Repository {
	return &Repository{db: db, queries: queries.New(db)}
}

// Ping checks that the database is reachable.
func (r *Repository) Ping(ctx context.Context) error {
	if err := r.db.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging MySQL DB: %w", err)
	}

	return nil
}

// requireMatched returns ErrNotFound if an update matched no rows. Connections count matched rather than
// changed rows (see ConfigFromURL), so an update that leaves a row as it was still counts.
func requireMatched(rowsMatched int64, err error) error {
	if err != nil {
		return err
	}
	if rowsMatched == 0 {
		return datasources.ErrNotFound
	}

	return nil
}

func nullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: true}
}

func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func stringPtr(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

func int16Ptr(n sql.NullInt16) *int16 {
	if !n.Valid {
		return nil
	}
	return &n.Int16
}

func int64Ptr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}
