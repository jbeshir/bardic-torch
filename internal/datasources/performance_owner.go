package datasources

import "context"

// PerformanceOwnerBackfiller gives the performances that have no owner their owner (the piece or person whose
// document lists the link recording them), and returns how many it gave one. A release that stores performances
// without owners can write them after the owner columns exist; an approval replaces the performances its entity
// owns, so without an owner a performance would stay beside the copy the approval writes. It is idempotent.
type PerformanceOwnerBackfiller interface {
	BackfillPerformanceOwners(ctx context.Context) (int64, error)
}
