package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
)

var _ datasources.PerformanceOwnerBackfiller = (*Repository)(nil)

// BackfillPerformanceOwners gives the performances with no owner their owner, in one short write: those an earlier
// release stored, before migration 010 added the owner columns or after it. app.Setup runs it before serving.
func (r *Repository) BackfillPerformanceOwners(ctx context.Context) (int64, error) {
	var owned int64
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		q := r.queries.WithTx(tx)
		ofPieces, err := q.BackfillPerformanceOwnersOfPieces(ctx)
		if err != nil {
			return fmt.Errorf("owning performances of pieces: %w", err)
		}
		ofPersons, err := q.BackfillPerformanceOwnersOfPersons(ctx)
		if err != nil {
			return fmt.Errorf("owning performances of persons: %w", err)
		}
		owned = ofPieces + ofPersons

		return nil
	})
	if err != nil {
		return 0, databaseError("backfilling performance owners", err)
	}

	return owned, nil
}
