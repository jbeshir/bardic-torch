package mysql

import (
	"context"
	"database/sql"
)

// RunSlowStatementWithHint runs, in a read-only transaction of db, a SELECT that takes far longer than the
// search statements' execution time limit, carrying their hint, through the runner the searches use.
func RunSlowStatementWithHint(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	s := &statement{}
	s.write("SELECT ", executionTimeHint, " COUNT(*) FROM information_schema.columns a, "+
		"information_schema.columns b, information_schema.columns c")

	return txRunner(tx)(ctx, s, func(scan func(dest ...any) error) error {
		var n int64
		return scan(&n)
	})
}

// WithWriteTx runs fn with withWriteTx, for the external tests.
func (r *Repository) WithWriteTx(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	return r.withWriteTx(ctx, fn)
}
