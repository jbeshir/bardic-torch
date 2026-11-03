package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The global lock order of every write transaction (D15): users rows; revisions rows by ascending ID; entity rows
// by entity type, in entityLockOrder, then ID; then the term rows of a rebuilt facet, links rows by normalized URL
// and link_reports rows by ID. A row found only after a later class is locked is still locked; the deadlocks that
// can then happen are retried by withWriteTx.

// entityLockOrder is the order entity rows are locked in, by type: the entity_type ENUM's order.
var entityLockOrder = []domain.EntityType{
	domain.EntityTypePiece, domain.EntityTypePerson, domain.EntityTypeTerm, domain.EntityTypeCollection,
	domain.EntityTypeEvent,
}

// entityTables is the table of each entity type, and entitySlugColumns the expression giving an entity's slug:
// collections, addressed by ID, have none.
var (
	entityTables = map[domain.EntityType]sqlFragment{
		domain.EntityTypePiece: "pieces", domain.EntityTypePerson: "persons", domain.EntityTypeTerm: "terms",
		domain.EntityTypeCollection: "collections", domain.EntityTypeEvent: "events",
	}
	entitySlugColumns = map[domain.EntityType]sqlFragment{
		domain.EntityTypePiece: slugColumn, domain.EntityTypePerson: slugColumn, domain.EntityTypeTerm: slugColumn,
		domain.EntityTypeCollection: "''", domain.EntityTypeEvent: slugColumn,
	}
)

// slugColumn is the column holding an entity's slug.
const slugColumn sqlFragment = "slug"

// lockingReadHint is the execution time limit of a locking read (lockingReadMillis).
var lockingReadHint = sqlFragment(fmt.Sprintf("/*+ MAX_EXECUTION_TIME(%d) */ ", lockingReadMillis))

// entityRow is an entity's slug (empty for a collection) and current revision, nil if it has none (a pending new
// entity's placeholder).
type entityRow struct {
	slug            string
	currentRevision *int64
}

// entityRow reads the row of the entity of entityType with id, locking it FOR UPDATE if lock, and returns
// datasources.ErrNotFound if there is none.
func (c *catalogueReader) entityRow(ctx context.Context, entityType domain.EntityType, id int64, lock bool) (
	entityRow, error,
) {
	table, ok := entityTables[entityType]
	if !ok {
		return entityRow{}, fmt.Errorf("%w [%s] of entity type", domain.ErrUnknownEnumValue, entityType)
	}
	var s statement
	s.write("SELECT ")
	if lock {
		s.write(lockingReadHint)
	}
	s.write(entitySlugColumns[entityType], ", current_revision_id FROM ", table, " WHERE id = ")
	s.arg(id)
	if lock {
		s.write(" FOR UPDATE")
	}

	var row entityRow
	found := false
	err := c.run(ctx, &s, func(scan func(dest ...any) error) error {
		var current sql.NullInt64
		found = true
		if err := scan(&row.slug, &current); err != nil {
			return err
		}
		if current.Valid {
			row.currentRevision = &current.Int64
		}
		return nil
	})
	if err != nil {
		return entityRow{}, fmt.Errorf("reading %s [%d]: %w", entityType, id, err)
	}
	if !found {
		return entityRow{}, fmt.Errorf("%s [%d]: %w", entityType, id, datasources.ErrNotFound)
	}

	return row, nil
}

// lockReferencedEntities locks the rows of the entities refs references FOR SHARE, and the row of the entity own
// (if its ID is not 0) FOR UPDATE, in the global lock order, so that what they are cannot change until the
// transaction ends (D15). It returns the own entity's row, and datasources.ErrEntityGone if it no longer exists.
func (c *catalogueReader) lockReferencedEntities(
	ctx context.Context, own domain.EntityRef, refs domain.SnapshotReferences,
) (entityRow, error) {
	ids := map[domain.EntityType][]int64{
		domain.EntityTypePiece: refs.PieceIDs, domain.EntityTypePerson: refs.PersonIDs,
		domain.EntityTypeTerm: refs.TermIDs, domain.EntityTypeCollection: refs.CollectionIDs,
		domain.EntityTypeEvent: refs.EventIDs,
	}
	var ownRow entityRow
	for _, entityType := range entityLockOrder {
		if own.ID != 0 && own.Type == entityType {
			row, err := c.entityRow(ctx, entityType, own.ID, true)
			if err != nil {
				return entityRow{}, fmt.Errorf("%w: %w", datasources.ErrEntityGone, err)
			}
			ownRow = row
		}
		shared := slices.Compact(slices.Sorted(slices.Values(unsignedIDs(ids[entityType]))))
		if len(shared) == 0 {
			continue
		}
		var s statement
		s.write("SELECT ", lockingReadHint, "id FROM ", entityTables[entityType], " WHERE id IN (")
		list(&s, shared)
		s.write(") ORDER BY id FOR SHARE")
		if err := c.run(ctx, &s, func(func(dest ...any) error) error { return nil }); err != nil {
			return entityRow{}, fmt.Errorf("locking referenced %s rows: %w", entityType, err)
		}
	}

	return ownRow, nil
}

// deletePlaceholder deletes the placeholder of the pending new entity e, if it still is one, with its child rows
// (D14).
func (c *catalogueReader) deletePlaceholder(ctx context.Context, e domain.EntityRef) error {
	deletes := map[domain.EntityType]func(context.Context, uint64) error{
		domain.EntityTypePiece: c.q.DeletePiecePlaceholder, domain.EntityTypePerson: c.q.DeletePersonPlaceholder,
		domain.EntityTypeTerm: c.q.DeleteTermPlaceholder, domain.EntityTypeCollection: c.q.DeleteCollectionPlaceholder,
		domain.EntityTypeEvent: c.q.DeleteEventPlaceholder,
	}
	id, ok := positiveID(e.ID)
	if !ok {
		return fmt.Errorf("placeholder %s [%d]: %w", e.Type, e.ID, datasources.ErrNotFound)
	}
	if err := deletes[e.Type](ctx, id); err != nil {
		return fmt.Errorf("deleting placeholder %s [%d]: %w", e.Type, e.ID, err)
	}

	return nil
}

// positiveID returns id as the type of the ID columns, or false if it is not positive and so no row's ID.
func positiveID(id int64) (uint64, bool) {
	if id < 1 {
		return 0, false
	}

	return uint64(id), true
}
