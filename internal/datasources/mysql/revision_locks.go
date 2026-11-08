package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The global lock order of every write transaction (docs/PLAN.md §5.1 "Approval"): users rows; revisions rows by
// ascending ID; entity rows by entity type, in entityLockOrder, then ascending ID, written and referenced rows in
// one pass, except that the terms of a facet whose closure the write may rebuild are locked first among the term
// rows, all of them; then links rows by normalized URL and link_reports rows by ID. A row found only after a later
// class is locked is still locked; the deadlocks that can then happen are retried by withWriteTx.

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

// entityLocks are the entity rows a write locks: those of the entities written FOR UPDATE, those of the entities
// refs references FOR SHARE, and every term of each of facets FOR UPDATE: the facets whose closure it may rebuild.
type entityLocks struct {
	written []domain.EntityRef
	refs    domain.SnapshotReferences
	facets  []domain.Facet
}

// lockedEntities are what lockEntities read under its locks: the rows of the written entities that exist, and the
// parents of each locked facet's terms, by term ID.
type lockedEntities struct {
	rows   map[domain.EntityRef]entityRow
	facets map[domain.Facet]map[int64]*int64
}

// lockEntities takes the entity locks in the global lock order. Per entity type it makes one pass over the IDs,
// written or referenced, in ascending order, with one statement per run of IDs of one lock mode, so that two writes
// whose entities reference each other wait for one another rather than deadlock. Before any other term row it locks
// the terms of the facets, and it takes no other lock on those.
func (c *catalogueReader) lockEntities(ctx context.Context, locks entityLocks) (lockedEntities, error) {
	referenced := map[domain.EntityType][]int64{
		domain.EntityTypePiece: locks.refs.PieceIDs, domain.EntityTypePerson: locks.refs.PersonIDs,
		domain.EntityTypeTerm: locks.refs.TermIDs, domain.EntityTypeCollection: locks.refs.CollectionIDs,
		domain.EntityTypeEvent: locks.refs.EventIDs,
	}
	out := lockedEntities{rows: map[domain.EntityRef]entityRow{}, facets: map[domain.Facet]map[int64]*int64{}}
	for _, entityType := range entityLockOrder {
		held := map[uint64]bool{}
		if entityType == domain.EntityTypeTerm {
			if err := c.lockFacets(ctx, locks.facets, out.facets, held); err != nil {
				return lockedEntities{}, err
			}
		}
		written := map[uint64]bool{}
		for _, e := range locks.written {
			if id, ok := positiveID(e.ID); ok && e.Type == entityType {
				written[id] = true
			}
		}
		ids := append(slices.Collect(maps.Keys(written)), unsignedIDs(referenced[entityType])...)
		ids = slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(ids))),
			func(id uint64) bool { return held[id] })
		for run := range lockRuns(ids, written) {
			if err := c.lockRun(ctx, entityType, run, out.rows); err != nil {
				return lockedEntities{}, err
			}
		}
		if err := c.readHeldRows(ctx, entityType, written, held, out.rows); err != nil {
			return lockedEntities{}, err
		}
	}

	return out, c.err
}

// lockRun is a run of ascending IDs of one entity type locked by one statement: FOR UPDATE if exclusive, else FOR
// SHARE.
type lockRun struct {
	ids       []uint64
	exclusive bool
}

// lockRuns splits the ascending ids into maximal runs of one lock mode, exclusive for the written IDs.
func lockRuns(ids []uint64, written map[uint64]bool) iter.Seq[lockRun] {
	return func(yield func(lockRun) bool) {
		for start := 0; start < len(ids); {
			end := start + 1
			for end < len(ids) && written[ids[end]] == written[ids[start]] {
				end++
			}
			if !yield(lockRun{ids: ids[start:end], exclusive: written[ids[start]]}) {
				return
			}
			start = end
		}
	}
}

// lockRun locks the rows of the run of entityType in ascending ID order, and adds those it locks FOR UPDATE to rows.
func (c *catalogueReader) lockRun(
	ctx context.Context, entityType domain.EntityType, run lockRun, rows map[domain.EntityRef]entityRow,
) error {
	var s statement
	s.write("SELECT ", lockingReadHint, "id, ", entitySlugColumns[entityType], ", current_revision_id FROM ",
		entityTables[entityType], " WHERE id IN (")
	list(&s, run.ids)
	mode := sqlFragment(" FOR SHARE")
	if run.exclusive {
		mode = " FOR UPDATE"
	}
	s.write(") ORDER BY id", mode)
	err := c.run(ctx, &s, func(scan func(dest ...any) error) error {
		var id uint64
		var row entityRow
		var current sql.NullInt64
		if err := scan(&id, &row.slug, &current); err != nil {
			return err
		}
		if run.exclusive {
			row.currentRevision = int64Ptr(current)
			rows[domain.EntityRef{Type: entityType, ID: c.signedID(id)}] = row
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("locking %s rows: %w", entityType, err)
	}

	return nil
}

// readHeldRows adds to rows the rows of the written entities of entityType whose rows a facet lock already holds,
// read without taking another lock.
func (c *catalogueReader) readHeldRows(
	ctx context.Context, entityType domain.EntityType, written, held map[uint64]bool,
	rows map[domain.EntityRef]entityRow,
) error {
	for _, id := range slices.Sorted(maps.Keys(written)) {
		if !held[id] {
			continue
		}
		e := domain.EntityRef{Type: entityType, ID: c.signedID(id)}
		row, err := c.entityRow(ctx, e.Type, e.ID, false)
		if err != nil {
			return err
		}
		rows[e] = row
	}

	return nil
}

// lockFacets locks every term of each of facets FOR UPDATE, by facet and then ascending ID, recording each facet's
// terms' parents in parents and the terms' IDs in held.
func (c *catalogueReader) lockFacets(
	ctx context.Context, facets []domain.Facet, parents map[domain.Facet]map[int64]*int64, held map[uint64]bool,
) error {
	for _, facet := range slices.Compact(slices.Sorted(slices.Values(facets))) {
		rows, err := c.q.LockTermsOfFacet(ctx, queries.TermsFacet(facet))
		if err != nil {
			return fmt.Errorf("locking the terms of facet %s: %w", facet, err)
		}
		terms := map[int64]*int64{}
		for _, row := range rows {
			terms[c.signedID(row.ID)] = int64Ptr(row.ParentID)
			held[row.ID] = true
		}
		parents[facet] = terms
	}

	return nil
}

// rebuiltFacets returns the facets of the term documents among docs: those whose closure approving them may rebuild.
func rebuiltFacets(docs ...domain.AnyDocument) []domain.Facet {
	var facets []domain.Facet
	for _, doc := range docs {
		if d, ok := doc.(domain.TermDocument); ok {
			facets = append(facets, d.Facet)
		}
	}

	return facets
}

// lockOwnEntity is lockEntities for the one entity own written, with the terms of facets, returning own's row, or
// datasources.ErrEntityGone if it no longer exists.
func (c *catalogueReader) lockOwnEntity(
	ctx context.Context, own domain.EntityRef, refs domain.SnapshotReferences, facets []domain.Facet,
) (entityRow, error) {
	locked, err := c.lockEntities(ctx, entityLocks{written: []domain.EntityRef{own}, refs: refs, facets: facets})
	if err != nil {
		return entityRow{}, err
	}
	row, ok := locked.rows[own]
	if own.ID != 0 && !ok {
		return entityRow{}, fmt.Errorf("%s [%d]: %w", own.Type, own.ID, datasources.ErrEntityGone)
	}

	return row, nil
}

// currentDocument is an entity's current revision and its document, both nil for none.
type currentDocument struct {
	id  *int64
	doc domain.AnyDocument
}

// references adds what the current document, of the entity id of kind, references to refs.
func (d currentDocument) references(kind documentKind, id int64, refs *domain.SnapshotReferences) {
	if d.doc != nil {
		kind.addReferences(id, d.doc, refs)
	}
}

// peekCurrent reads the current revision of the entity e, of kind, and its document without a lock, so that a
// write can lock what that document references with its other entity rows; lockedCurrent then checks it against
// the locked row. An entity with no row, or no current revision, has none.
func (c *catalogueReader) peekCurrent(
	ctx context.Context, kind documentKind, e domain.EntityRef,
) (currentDocument, error) {
	if e.ID == 0 {
		return currentDocument{}, nil
	}
	row, err := c.entityRow(ctx, e.Type, e.ID, false)
	if errors.Is(err, datasources.ErrNotFound) || err == nil && row.currentRevision == nil {
		return currentDocument{}, nil
	}
	if err != nil {
		return currentDocument{}, err
	}
	doc, err := c.approvedDocument(ctx, kind, domain.RevisionEntity{Type: e.Type, ID: e.ID}, *row.currentRevision)
	if err != nil {
		return currentDocument{}, err
	}

	return currentDocument{id: row.currentRevision, doc: doc}, nil
}

// lockedCurrent returns the current document of the entity e, of kind, whose locked row is own: the one peeked if
// it is still current. Otherwise the revision approved since is read, and what its document references is locked
// FOR SHARE after the other entity rows, as a row found later.
func (c *catalogueReader) lockedCurrent(
	ctx context.Context, kind documentKind, e domain.EntityRef, own entityRow, peeked currentDocument,
) (currentDocument, error) {
	if domain.EqualPointers(own.currentRevision, peeked.id) {
		return peeked, nil
	}
	if own.currentRevision == nil {
		return currentDocument{}, nil
	}
	doc, err := c.approvedDocument(ctx, kind, domain.RevisionEntity{Type: e.Type, ID: e.ID}, *own.currentRevision)
	if err != nil {
		return currentDocument{}, err
	}
	if _, err := c.lockEntities(ctx, entityLocks{refs: referencesOf(kind, e.ID, doc)}); err != nil {
		return currentDocument{}, err
	}

	return currentDocument{id: own.currentRevision, doc: doc}, nil
}

// deletePlaceholder deletes the placeholder of the pending new entity e, if it still is one, with its child rows
// (docs/PLAN.md §5.1 "New entities").
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
