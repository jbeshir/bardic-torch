package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
)

var _ datasources.Pinger = (*Repository)(nil)

// Repository implements the datasources interfaces on top of a MySQL connection pool.
type Repository struct {
	db *sql.DB
}

// New returns a Repository using db.
func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Ping checks that the database is reachable.
func (r *Repository) Ping(ctx context.Context) error {
	if err := r.db.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging MySQL DB: %w", err)
	}

	return nil
}
