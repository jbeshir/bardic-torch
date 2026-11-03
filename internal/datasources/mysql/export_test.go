package mysql

import (
	"context"
	"database/sql"

	"github.com/jbeshir/bardic-torch/internal/domain"
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

// WriteApprovedDocument writes doc over the rows of the existing entity of its type with id, in tx, as an approval
// does, with the approval's error mapping, for the external tests.
func (r *Repository) WriteApprovedDocument(ctx context.Context, tx *sql.Tx, id int64, doc domain.AnyDocument) error {
	w := &entityWriter{ctx: ctx, q: r.queries.WithTx(tx)}
	w.write(approvedEntity{entity: domain.EntityRef{Type: doc.EntityType(), ID: id}, doc: doc, current: doc})

	return approvalWriteError(w.err)
}
