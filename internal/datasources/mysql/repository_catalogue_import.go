package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ datasources.CatalogueImporter = (*Repository)(nil)

// CatalogueTables are the tables an import writes, in the order it writes them: each table's rows before those of
// any table referring to it. An import needs every one of them empty.
var CatalogueTables = []string{
	"revisions", "revision_contributors",
	"terms", "term_aliases", "term_ancestors",
	"collections", "events",
	"persons", "person_aliases", "person_roles", "person_affiliations", "person_relations", "person_preferences",
	"pieces", "piece_secondary_kinds", "piece_aliases", "piece_terms", "piece_relations", "piece_credits",
	"piece_preferences", "conservation_history",
	"links", "link_attachments", "performances", "performance_performers",
}

// importLockName is the advisory lock (GET_LOCK) an import holds: one per database, as named locks are
// server-wide and a server may hold several catalogues. Lock names are at most 64 characters.
const importLockName = "LEFT(CONCAT('bardic-torch-import:', DATABASE()), 64)"

// lockWaitMillis is the execution time limit of the import's locking reads, overriding the session's
// (maxExecutionTimeMillis), which would stop a read waiting for a row lock that a running write holds. It is
// above the session's innodb_lock_wait_timeout (lockWaitTimeoutSeconds), which bounds the wait instead.
const lockWaitMillis = 60000

// ImportDatabaseError is returned, wrapped, for every MySQL error of an import (a refused row, a failed lock or
// commit). It names the step and the MySQL error number but not the server's message, which can quote the
// row's values ("Duplicate entry '…'") and so may hold a private name.
type ImportDatabaseError struct {
	Step   string
	Number uint16
}

func (e *ImportDatabaseError) Error() string {
	return fmt.Sprintf("%s: MySQL error %d", e.Step, e.Number)
}

// errImportLockFailed is returned when GET_LOCK fails (returns NULL) rather than finding the lock taken.
var errImportLockFailed = errors.New("the import lock could not be requested")

// withoutServerMessage is err for step, as an ImportDatabaseError if it is a MySQL error.
func withoutServerMessage(step string, err error) error {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return &ImportDatabaseError{Step: step, Number: mysqlErr.Number}
	}

	return fmt.Errorf("%s: %w", step, err)
}

// errImportUnknownLink is returned, wrapped, when a document's link has no links row in the import, which
// CatalogueImport.Validate rules out.
var errImportUnknownLink = errors.New("link has no links row in the import")

// ImportCatalogue writes imp into the empty catalogue in one transaction. It first takes the database's import
// lock (ErrImportInProgress if another import holds it), then, in the transaction, locks every catalogue table
// with a locking read of its first row (ErrCatalogueNotEmpty if one has a row), so that no other writer can
// insert into the tables until the import commits. It then inserts every row with the import's IDs, in the order
// of CatalogueTables, and commits. Any error rolls everything back. Errors never quote a value.
func (r *Repository) ImportCatalogue(ctx context.Context, imp domain.CatalogueImport) error {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return withoutServerMessage("connecting for the catalogue import", err)
	}
	defer func() { _ = conn.Close() }()

	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK("+importLockName+", 0)").Scan(&locked); err != nil {
		return withoutServerMessage("taking the import lock", err)
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
		return withoutServerMessage("beginning the catalogue import", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := requireEmptyCatalogue(ctx, tx); err != nil {
		return err
	}
	w := &importWriter{entityWriter: entityWriter{ctx: ctx, q: r.queries.WithTx(tx)}}
	w.write(imp)
	if w.err != nil {
		var writeErr *writeError
		if errors.As(w.err, &writeErr) {
			return fmt.Errorf("importing catalogue: %w", withoutServerMessage("writing "+writeErr.table, writeErr.err))
		}
		return fmt.Errorf("importing catalogue: %w", w.err)
	}
	if err := tx.Commit(); err != nil {
		return withoutServerMessage("committing the catalogue import", err)
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
			return withoutServerMessage("locking "+table, err)
		default:
			return fmt.Errorf("%w: %s has rows", datasources.ErrCatalogueNotEmpty, table)
		}
	}

	return nil
}

// importWriter inserts an import's rows. The first error stops it: every later insert is skipped, and err holds
// importWriter inserts an import's rows, table by table. The first error stops it: every later insert is skipped,
// and err holds the error, a *writeError for a refused row.
type importWriter struct {
	entityWriter
	// linkIDs are the IDs of the links rows, by normalised URL.
	linkIDs map[string]int64
	// performerLists are the performers of each performance inserted, in order.
	performerLists []importedPerformers
}

// importedPerformers are the performers, in order, of a performance the writer inserted.
type importedPerformers struct {
	performanceID int64
	personIDs     []int64
}

func (w *importWriter) write(imp domain.CatalogueImport) {
	w.revisions(imp)
	w.terms(imp)
	w.collections(imp.Collections)
	w.events(imp.Events)
	w.persons(imp.Persons)
	w.pieces(imp.Pieces)
	w.links(imp)
}

// nullableID returns an optional ID as the type of the nullable ID columns.
func nullableID(id *int64) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}

	return sql.NullInt64{Int64: *id, Valid: true}
}

// legacyPageID returns a legacy page's ID as the type of the legacy_wiki_pageid columns, NULL for none.
func (w *importWriter) legacyPageID(p *domain.LegacyPage) sql.NullInt32 {
	if p == nil || p.PageID == nil {
		return sql.NullInt32{}
	}
	if *p.PageID > math.MaxInt32 {
		w.fail(fmt.Errorf("legacy page ID [%d]: %w", *p.PageID, errOutOfRange))
		return sql.NullInt32{}
	}

	return sql.NullInt32{Int32: int32(*p.PageID), Valid: true}
}

// legacyTitle returns a legacy page's title, NULL for none.
func legacyTitle(p *domain.LegacyPage) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}

	return sql.NullString{String: p.Title, Valid: true}
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}

	return sql.NullString{String: *s, Valid: true}
}

func nullInt16(n *int16) sql.NullInt16 {
	if n == nil {
		return sql.NullInt16{}
	}

	return sql.NullInt16{Int16: *n, Valid: true}
}

// revisionID returns an imported entity's revision as its current revision.
func revisionID(rev domain.ImportedRevision) sql.NullInt64 {
	return sql.NullInt64{Int64: rev.ID, Valid: true}
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
			EntityType: queries.RevisionsEntityType(r.entityType), EntityID: nullableID(&r.entityID),
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
		d := t.Doc
		p := queries.InsertImportedTermParams{ID: w.id(t.ID), Facet: queries.TermsFacet(d.Facet), Name: d.Name,
			DisplayName: d.DisplayName, Slug: t.Slug, ParentID: nullableID(d.ParentID),
			RelatedAffiliationID: nullableID(d.RelatedAffiliationID), DescriptionMd: nullString(d.DescriptionMD),
			Position: d.Position, CurrentRevisionID: revisionID(t.Revision),
			LegacyWikiPageid: w.legacyPageID(t.Legacy), LegacyTitle: legacyTitle(t.Legacy)}
		w.exec("terms", func() error { return w.q.InsertImportedTerm(w.ctx, p) })
	}
	for _, t := range imp.Terms {
		w.termAliases(t.ID, t.Doc.Aliases)
	}
	ancestors, err := imp.TermAncestors()
	if err != nil {
		w.fail(fmt.Errorf("terms: %w", err))

		return
	}
	w.termAncestors(ancestors)
}

func (w *importWriter) collections(collections []domain.ImportedCollection) {
	for _, c := range collections {
		d := c.Doc
		p := queries.InsertImportedCollectionParams{ID: w.id(c.ID), Name: d.Name, SortName: c.SortName,
			SortLetter: string(c.SortLetter), Url: nullString(d.URL), Kind: queries.CollectionsKind(d.Kind),
			DescriptionMd: nullString(d.DescriptionMD), RegionTermID: nullableID(d.RegionTermID),
			ParentID: nullableID(d.ParentID), IsBardicResource: d.IsBardicResource,
			Enumerable: queries.CollectionsEnumerable(d.Enumerable), CurrentRevisionID: revisionID(c.Revision)}
		w.exec("collections", func() error { return w.q.InsertImportedCollection(w.ctx, p) })
	}
}

func (w *importWriter) events(events []domain.ImportedEvent) {
	for _, e := range events {
		p := queries.InsertImportedEventParams{ID: w.id(e.ID), Slug: e.Slug, Name: e.Doc.Name,
			Kind: queries.EventsKind(e.Doc.Kind), DescriptionMd: nullString(e.Doc.DescriptionMD),
			CurrentRevisionID: revisionID(e.Revision), LegacyWikiPageid: w.legacyPageID(e.Legacy),
			LegacyTitle: legacyTitle(e.Legacy)}
		w.exec("events", func() error { return w.q.InsertImportedEvent(w.ctx, p) })
	}
}

func (w *importWriter) persons(persons []domain.ImportedPerson) {
	for _, person := range persons {
		d := person.Doc
		var statusDate sql.NullTime
		if d.StatusDate != nil {
			statusDate = sql.NullTime{Time: d.StatusDate.Time, Valid: true}
		}
		p := queries.InsertImportedPersonParams{ID: w.id(person.ID), Slug: person.Slug, Name: d.Name,
			SortName: person.SortName, SortLetter: string(person.SortLetter), Kind: queries.PersonsKind(d.Kind),
			Status: queries.PersonsStatus(d.Status), StatusDate: statusDate,
			StatusSourceUrl: nullString(d.StatusSourceURL), Pronouns: nullString(d.Pronouns),
			NotesMd: nullString(d.NotesMD), LicensingNotesMd: nullString(d.LicensingNotesMD),
			LicensingStatus: queries.PersonsLicensingStatus(d.LicensingStatus), IsStub: d.IsStub,
			CurrentRevisionID: revisionID(person.Revision), LegacyWikiPageid: w.legacyPageID(person.Legacy),
			LegacyTitle: legacyTitle(person.Legacy)}
		w.exec("persons", func() error { return w.q.InsertImportedPerson(w.ctx, p) })
	}
	for _, person := range persons {
		w.personAliases(person.ID, person.Doc.Aliases)
	}
	for _, person := range persons {
		w.personRoles(person.ID, person.Doc.Roles)
	}
	for _, person := range persons {
		w.personAffiliations(person.ID, person.Doc.AffiliationIDs)
	}
	for _, person := range persons {
		w.personRelations(person.ID, person.Doc.Relations)
	}
	for _, person := range persons {
		w.personPreferences(person.ID, person.Doc.Preferences)
	}
}

func (w *importWriter) pieces(pieces []domain.ImportedPiece) {
	for _, piece := range pieces {
		d := piece.Doc
		var licence queries.NullPiecesLicence
		if d.Licence != nil {
			licence = queries.NullPiecesLicence{PiecesLicence: queries.PiecesLicence(*d.Licence), Valid: true}
		}
		p := queries.InsertImportedPieceParams{ID: w.id(piece.ID), Slug: piece.Slug, Title: d.Title,
			SortTitle: piece.SortTitle, SortLetter: string(piece.SortLetter), Kind: queries.PiecesKind(d.Kind),
			ConservationStatus: queries.PiecesConservationStatus(d.Conservation.Status), IsStub: d.IsStub,
			NotesMd: nullString(d.NotesMD), VersionNotesMd: nullString(d.VersionNotesMD),
			LicensingNotesMd: nullString(d.LicensingNotesMD),
			LicensingStatus:  queries.PiecesLicensingStatus(d.LicensingStatus), Licence: licence,
			HistoricalSourceMd: nullString(d.HistoricalSourceMD), PeriodStart: nullInt16(d.PeriodStart),
			PeriodEnd: nullInt16(d.PeriodEnd), LegacyHadInlineLyrics: piece.LegacyHadInlineLyrics,
			CurrentRevisionID: revisionID(piece.Revision), LegacyWikiPageid: w.legacyPageID(piece.Legacy),
			LegacyTitle: legacyTitle(piece.Legacy)}
		w.exec("pieces", func() error { return w.q.InsertImportedPiece(w.ctx, p) })
	}
	for _, piece := range pieces {
		w.pieceSecondaryKinds(piece.ID, piece.Doc.SecondaryKinds)
	}
	for _, piece := range pieces {
		w.pieceAliases(piece.ID, piece.Doc.Aliases)
	}
	for _, piece := range pieces {
		w.pieceTerms(piece.ID, piece.Doc.Terms)
	}
	for _, piece := range pieces {
		w.pieceRelations(piece.ID, piece.Doc.Relations)
	}
	for _, piece := range pieces {
		w.pieceCredits(piece.ID, piece.Doc.Credits)
	}
	for _, piece := range pieces {
		w.piecePreferences(piece.ID, piece.Doc.Preferences)
	}
	w.conservation(pieces)
}

// conservation inserts the conservation history row of each piece whose status is assessed: at its revision,
// with ImportConservationReason and the document's source.
func (w *importWriter) conservation(pieces []domain.ImportedPiece) {
	for _, piece := range pieces {
		c := piece.Doc.Conservation
		if c.Status == domain.ConservationStatusUnassessed {
			continue
		}
		p := queries.InsertConservationHistoryParams{PieceID: w.id(piece.ID),
			Status: queries.ConservationHistoryStatus(c.Status), Reason: domain.ImportConservationReason,
			SourceUrl: nullString(c.SourceURL), RevisionID: revisionID(piece.Revision),
			At: piece.Revision.CreatedAt}
		w.exec("conservation_history", func() error { return w.q.InsertConservationHistory(w.ctx, p) })
	}
}

// links inserts the links rows, then the documents' link attachments, performances and performers.
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
		for k, l := range piece.Doc.Links {
			w.linkAttachments(w.linkID(l.URL), l.Roles, k, w.nullID(piece.ID), sql.NullInt64{})
		}
	}
	for _, person := range imp.Persons {
		for k, l := range person.Doc.Links {
			w.linkAttachments(w.linkID(l.URL), l.Roles, k, sql.NullInt64{}, w.nullID(person.ID))
		}
	}
	w.performances(imp)
	for _, perf := range w.performerLists {
		w.performers(perf.performanceID, perf.personIDs)
	}
}

// linkID returns the ID of the links row of a document's link.
func (w *importWriter) linkID(url string) uint64 {
	normalized, err := domain.NormalizeURL(url)
	id, ok := w.linkIDs[normalized]
	if err != nil || !ok {
		w.fail(errImportUnknownLink)
		return 0
	}

	return w.id(id)
}

// performances inserts the performances of the documents' links, those of a piece's links being owned by the piece,
// those of a person's links being owned by the person, and records their performers, which are inserted after
// every performance.
func (w *importWriter) performances(imp domain.CatalogueImport) {
	for _, piece := range imp.Pieces {
		for _, l := range piece.Doc.Links {
			for _, perf := range l.Performances {
				w.importPerformance(w.performance(w.linkID(l.URL), &piece.ID, perf, &piece.ID, nil), perf)
			}
		}
	}
	for _, person := range imp.Persons {
		for _, l := range person.Doc.Links {
			for _, perf := range l.Performances {
				w.importPerformance(w.performance(w.linkID(l.URL), perf.PieceID, perf.DocPerformance, nil, &person.ID),
					perf.DocPerformance)
			}
		}
	}
}

// importPerformance records the performers of the performance perf inserted as id, unless the writer has stopped.
func (w *importWriter) importPerformance(id int64, perf domain.DocPerformance) {
	if w.err == nil {
		w.performerLists = append(w.performerLists, importedPerformers{performanceID: id, personIDs: perf.PerformerIDs})
	}
}
