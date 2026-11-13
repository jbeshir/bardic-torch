package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ datasources.CatalogueImporter = (*Repository)(nil)

// CatalogueTables are the tables an import writes, in the order it writes them: each table's rows before those of
// any table referring to it. An import needs every one of them empty.
var CatalogueTables = []string{
	"revisions", "revision_contributors",
	"terms", "term_ancestors",
	"collections", "events",
	"persons", "person_aliases", "person_roles", "person_affiliations",
	"pieces", "piece_aliases", "piece_terms", "piece_credits",
	"links", "link_attachments", "performances", "performance_performers",
}

// importLockName is the advisory lock (GET_LOCK) an import holds: one per database, as named locks are
// server-wide and a server may hold several catalogues. Lock names are at most 64 characters.
const importLockName = "LEFT(CONCAT('bardic-torch-import:', DATABASE()), 64)"

// lockWaitMillis is the execution time limit of the import's locking reads, overriding the session's
// (maxExecutionTimeMillis), which would stop a read waiting for a row lock that a running write holds. It is
// above InnoDB's default lock wait timeout (50 s), which bounds the wait instead.
const lockWaitMillis = 60000

// errImportLockFailed is returned when GET_LOCK fails (returns NULL) rather than finding the lock taken.
var errImportLockFailed = errors.New("the import lock could not be requested")

// ImportCatalogue writes imp into the empty catalogue in one transaction. It first takes the database's import
// lock (ErrImportInProgress if another import holds it), then, in the transaction, locks every catalogue table
// with a locking read of its first row (ErrCatalogueNotEmpty if one has a row), so that no other writer can
// insert into the tables until the import commits. It then inserts every row with the import's IDs, in the order
// of CatalogueTables, and commits. Any error rolls everything back.
func (r *Repository) ImportCatalogue(ctx context.Context, imp domain.CatalogueImport) error {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("connecting for the catalogue import: %w", err)
	}
	defer func() { _ = conn.Close() }()

	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK("+importLockName+", 0)").Scan(&locked); err != nil {
		return fmt.Errorf("taking the import lock: %w", err)
	}
	switch {
	case !locked.Valid:
		return errImportLockFailed
	case locked.Int64 != 1:
		return datasources.ErrImportInProgress
	}
	defer releaseImportLock(ctx, conn)

	return r.importInTransaction(ctx, conn, imp)
}

// releaseImportLock releases the import lock conn holds. If it cannot, the connection is discarded, which
// ends its session and so releases the lock.
func releaseImportLock(ctx context.Context, conn *sql.Conn) {
	if _, err := conn.ExecContext(context.WithoutCancel(ctx), "DO RELEASE_LOCK("+importLockName+")"); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
}

func (r *Repository) importInTransaction(ctx context.Context, conn *sql.Conn, imp domain.CatalogueImport) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning the catalogue import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := requireEmptyCatalogue(ctx, tx); err != nil {
		return err
	}
	w := &importWriter{entityWriter: entityWriter{ctx: ctx, q: r.queries.WithTx(tx)}}
	w.write(imp)
	if w.err != nil {
		return fmt.Errorf("importing catalogue: %w", w.err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing the catalogue import: %w", err)
	}

	return nil
}

// requireEmptyCatalogue returns ErrCatalogueNotEmpty, wrapped, if a catalogue table has a row. Each read locks
// the first row, or for an empty table the gap before the end of its index, until the transaction ends.
func requireEmptyCatalogue(ctx context.Context, tx *sql.Tx) error {
	for _, table := range CatalogueTables {
		var one int
		err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT /*+ MAX_EXECUTION_TIME(%d) */ 1 FROM %s LIMIT 1 FOR UPDATE",
			lockWaitMillis, table)).Scan(&one)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("locking %s: %w", table, err)
		default:
			return fmt.Errorf("%w: %s has rows", datasources.ErrCatalogueNotEmpty, table)
		}
	}

	return nil
}

// importWriter writes an import's rows through an entityWriter, table by table.
type importWriter struct {
	entityWriter
	// linkIDs are the IDs of the links rows, by normalised URL.
	linkIDs map[string]int64
}

func (w *importWriter) write(imp domain.CatalogueImport) {
	w.revisions(imp)
	w.terms(imp)
	for _, c := range imp.Collections {
		w.insertCollection(entityRow{id: someID(c.ID), sortKey: c.SortName, revisionID: c.Revision.ID}, c.Doc)
	}
	for _, e := range imp.Events {
		w.insertEvent(entityRow{id: someID(e.ID), slug: e.Slug, revisionID: e.Revision.ID, legacy: e.Legacy}, e.Doc)
	}
	for _, p := range imp.Persons {
		w.insertPerson(entityRow{id: someID(p.ID), slug: p.Slug, sortKey: p.SortName, revisionID: p.Revision.ID,
			legacy: p.Legacy}, p.Doc)
	}
	for _, p := range imp.Persons {
		w.personChildren(p.ID, p.Doc)
	}
	for _, p := range imp.Pieces {
		w.insertPiece(entityRow{id: someID(p.ID), slug: p.Slug, sortKey: p.SortTitle, revisionID: p.Revision.ID,
			legacy: p.Legacy, hadInlineLyrics: p.LegacyHadInlineLyrics}, p.Doc)
	}
	for _, p := range imp.Pieces {
		w.pieceChildren(p.ID, p.Doc)
	}
	w.links(imp)
}

// importedEntityRevision is an imported entity's revision with what it is the revision of.
type importedEntityRevision struct {
	entityType domain.EntityType
	entityID   int64
	doc        any
	rev        domain.ImportedRevision
}

func (w *importWriter) revisions(imp domain.CatalogueImport) {
	var all []importedEntityRevision
	for _, t := range imp.Terms {
		all = append(all, importedEntityRevision{domain.EntityTypeTerm, t.ID, t.Doc, t.Revision})
	}
	for _, c := range imp.Collections {
		all = append(all, importedEntityRevision{domain.EntityTypeCollection, c.ID, c.Doc, c.Revision})
	}
	for _, e := range imp.Events {
		all = append(all, importedEntityRevision{domain.EntityTypeEvent, e.ID, e.Doc, e.Revision})
	}
	for _, p := range imp.Persons {
		all = append(all, importedEntityRevision{domain.EntityTypePerson, p.ID, p.Doc, p.Revision})
	}
	for _, p := range imp.Pieces {
		all = append(all, importedEntityRevision{domain.EntityTypePiece, p.ID, p.Doc, p.Revision})
	}
	for _, r := range all {
		snapshot, err := json.Marshal(r.doc)
		if err != nil {
			w.fail(fmt.Errorf("encoding the snapshot of %s [%d]: %w", r.entityType, r.entityID, err))
		}
		p := queries.InsertImportedRevisionParams{ID: w.id(r.rev.ID),
			EntityType: queries.RevisionsEntityType(r.entityType), EntityID: someID(r.entityID),
			SnapshotJson: snapshot, Summary: r.rev.Summary, CreatedAt: r.rev.CreatedAt}
		w.exec("revisions", func() error { return w.q.InsertImportedRevision(w.ctx, p) })
	}
	for _, r := range all {
		for k, name := range r.rev.Contributors {
			p := queries.InsertRevisionContributorParams{RevisionID: w.id(r.rev.ID), Position: w.position(k),
				Name: name}
			w.exec("revision_contributors", func() error { return w.q.InsertRevisionContributor(w.ctx, p) })
		}
	}
}

func (w *importWriter) terms(imp domain.CatalogueImport) {
	for _, t := range imp.Terms {
		w.insertTerm(entityRow{id: someID(t.ID), slug: t.Slug, revisionID: t.Revision.ID, legacy: t.Legacy}, t.Doc)
	}
	ancestors, err := imp.TermAncestors()
	if err != nil {
		w.fail(fmt.Errorf("terms: %w", err))

		return
	}
	w.termAncestors(ancestors)
}

// links inserts the links rows, then the documents' link attachments and performances.
func (w *importWriter) links(imp domain.CatalogueImport) {
	w.linkIDs = map[string]int64{}
	for _, row := range imp.Links {
		l := row.Link
		normalized, err := domain.NormalizeURL(l.URL)
		if err != nil {
			w.fail(fmt.Errorf("link [%d]: %w", row.ID, err))
			return
		}
		w.linkIDs[normalized] = row.ID
		var placeType queries.NullLinksPlaceType
		if l.PlaceType != nil {
			placeType = queries.NullLinksPlaceType{LinksPlaceType: queries.LinksPlaceType(*l.PlaceType), Valid: true}
		}
		p := queries.InsertImportedLinkParams{ID: w.id(row.ID), Url: l.URL, NormalizedUrl: normalized,
			Title: nullString(l.Title), Description: nullString(l.Description), Platform: nullString(l.Platform),
			PlaceType: placeType, CollectionID: nullableID(l.CollectionID), Locator: nullString(l.Locator),
			IsHidden: row.IsHidden}
		w.exec("links", func() error { return w.q.InsertImportedLink(w.ctx, p) })
	}
	for _, piece := range imp.Pieces {
		w.pieceLinks(piece.ID, piece.Doc.Links, w.linkIDs)
	}
	for _, person := range imp.Persons {
		w.personLinks(person.ID, person.Doc.Links, w.linkIDs)
	}
}
