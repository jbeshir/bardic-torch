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
	"persons", "person_aliases", "person_roles", "person_affiliations", "person_relations",
	"pieces", "piece_secondary_kinds", "piece_aliases", "piece_terms", "piece_relations", "piece_credits",
	"conservation_history",
	"links", "link_attachments", "performances", "performance_performers",
}

// importLockName is the advisory lock (GET_LOCK) an import holds: one per database, as named locks are
// server-wide and a server may hold several catalogues. Lock names are at most 64 characters.
const importLockName = "LEFT(CONCAT('bardic-torch-import:', DATABASE()), 64)"

// lockWaitMillis is the execution time limit of the import's locking reads, overriding the session's
// (maxExecutionTimeMillis), which would stop a read waiting for a row lock that a running write holds. It is
// above InnoDB's default lock wait timeout (50 s), which bounds the wait instead.
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

// errImportOutOfRange is returned, wrapped, when an import's value does not fit its column's type, which
// CatalogueImport.Validate rules out.
var errImportOutOfRange = errors.New("value out of range for its column")

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
	w := &importWriter{ctx: ctx, q: r.queries.WithTx(tx)}
	w.write(imp)
	if w.err != nil {
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
// the error.
type importWriter struct {
	ctx context.Context
	q   *queries.Queries
	err error
	// linkIDs are the IDs of the links rows, by normalised URL.
	linkIDs map[string]int64
	// performers are the performers of each performance inserted, in order.
	performers []importedPerformers
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

// insert runs one insert into table, unless an earlier error stopped the writer. A MySQL error becomes an
// ImportDatabaseError, without its message.
func (w *importWriter) insert(table string, run func() error) {
	if w.err != nil {
		return
	}
	if err := run(); err != nil {
		w.err = withoutServerMessage("writing "+table, err)
	}
}

// fail stops the writer with err, unless an earlier error stopped it.
func (w *importWriter) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}

// id returns an ID as the type of the ID columns.
func (w *importWriter) id(id int64) uint64 {
	if id < 1 {
		w.fail(fmt.Errorf("ID [%d]: %w", id, errImportOutOfRange))
		return 0
	}

	return uint64(id)
}

// nullableID returns an optional ID as the type of the nullable ID columns.
func nullableID(id *int64) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}

	return sql.NullInt64{Int64: *id, Valid: true}
}

// position returns a list index as the type of the position columns.
func (w *importWriter) position(k int) uint16 {
	if k < 0 || k > math.MaxUint16 {
		w.fail(fmt.Errorf("position [%d]: %w", k, errImportOutOfRange))
		return 0
	}

	return uint16(k)
}

// legacyPageID returns a legacy page's ID as the type of the legacy_wiki_pageid columns, NULL for none.
func (w *importWriter) legacyPageID(p *domain.LegacyPage) sql.NullInt32 {
	if p == nil || p.PageID == nil {
		return sql.NullInt32{}
	}
	if *p.PageID > math.MaxInt32 {
		w.fail(fmt.Errorf("legacy page ID [%d]: %w", *p.PageID, errImportOutOfRange))
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
			EntityType: queries.RevisionsEntityType(r.entityType), EntityID: w.id(r.entityID),
			SnapshotJson: snapshot, Summary: r.rev.Summary, CreatedAt: r.rev.CreatedAt}
		w.insert("revisions", func() error { return w.q.InsertImportedRevision(w.ctx, p) })
	}
	for _, r := range all {
		for k, name := range r.rev.Contributors {
			p := queries.InsertRevisionContributorParams{RevisionID: w.id(r.rev.ID), Position: w.position(k),
				Name: name}
			w.insert("revision_contributors", func() error { return w.q.InsertRevisionContributor(w.ctx, p) })
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
		w.insert("terms", func() error { return w.q.InsertImportedTerm(w.ctx, p) })
	}
	for _, t := range imp.Terms {
		for _, alias := range t.Doc.Aliases {
			p := queries.InsertTermAliasParams{TermID: w.id(t.ID), Name: alias}
			w.insert("term_aliases", func() error { return w.q.InsertTermAlias(w.ctx, p) })
		}
	}
	ancestors, err := imp.TermAncestors()
	if err != nil {
		w.fail(fmt.Errorf("terms: %w", err))

		return
	}
	for _, a := range ancestors {
		if a.Depth < 0 || a.Depth > math.MaxUint8 {
			w.fail(fmt.Errorf("term depth [%d]: %w", a.Depth, errImportOutOfRange))
			return
		}
		p := queries.InsertTermAncestorParams{AncestorID: w.id(a.AncestorID), DescendantID: w.id(a.DescendantID),
			Depth: uint8(a.Depth)}
		w.insert("term_ancestors", func() error { return w.q.InsertTermAncestor(w.ctx, p) })
	}
}

func (w *importWriter) collections(collections []domain.ImportedCollection) {
	for _, c := range collections {
		d := c.Doc
		p := queries.InsertImportedCollectionParams{ID: w.id(c.ID), Name: d.Name, SortName: c.SortName,
			SortLetter: string(c.SortLetter), Url: nullString(d.URL), Kind: queries.CollectionsKind(d.Kind),
			DescriptionMd: nullString(d.DescriptionMD), RegionTermID: nullableID(d.RegionTermID),
			ParentID: nullableID(d.ParentID), IsBardicResource: d.IsBardicResource,
			Enumerable: queries.CollectionsEnumerable(d.Enumerable), CurrentRevisionID: revisionID(c.Revision)}
		w.insert("collections", func() error { return w.q.InsertImportedCollection(w.ctx, p) })
	}
}

func (w *importWriter) events(events []domain.ImportedEvent) {
	for _, e := range events {
		p := queries.InsertImportedEventParams{ID: w.id(e.ID), Slug: e.Slug, Name: e.Doc.Name,
			Kind: queries.EventsKind(e.Doc.Kind), DescriptionMd: nullString(e.Doc.DescriptionMD),
			CurrentRevisionID: revisionID(e.Revision), LegacyWikiPageid: w.legacyPageID(e.Legacy),
			LegacyTitle: legacyTitle(e.Legacy)}
		w.insert("events", func() error { return w.q.InsertImportedEvent(w.ctx, p) })
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
		w.insert("persons", func() error { return w.q.InsertImportedPerson(w.ctx, p) })
	}
	for _, person := range persons {
		for _, a := range person.Doc.Aliases {
			p := queries.InsertPersonAliasParams{PersonID: w.id(person.ID), Name: a.Name,
				Kind: queries.PersonAliasesKind(a.Kind)}
			w.insert("person_aliases", func() error { return w.q.InsertPersonAlias(w.ctx, p) })
		}
	}
	for _, person := range persons {
		for _, role := range person.Doc.Roles {
			p := queries.InsertPersonRoleParams{PersonID: w.id(person.ID), Role: queries.PersonRolesRole(role)}
			w.insert("person_roles", func() error { return w.q.InsertPersonRole(w.ctx, p) })
		}
	}
	for _, person := range persons {
		for _, termID := range person.Doc.AffiliationIDs {
			p := queries.InsertPersonAffiliationParams{PersonID: w.id(person.ID), TermID: w.id(termID)}
			w.insert("person_affiliations", func() error { return w.q.InsertPersonAffiliation(w.ctx, p) })
		}
	}
	w.personRelations(persons)
}

func (w *importWriter) personRelations(persons []domain.ImportedPerson) {
	for _, person := range persons {
		for _, rel := range person.Doc.Relations {
			p := queries.InsertPersonRelationParams{FromPersonID: w.id(person.ID), ToPersonID: w.id(rel.ToPersonID),
				Type: queries.PersonRelationsType(rel.Type)}
			w.insert("person_relations", func() error { return w.q.InsertPersonRelation(w.ctx, p) })
		}
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
		w.insert("pieces", func() error { return w.q.InsertImportedPiece(w.ctx, p) })
	}
	for _, piece := range pieces {
		for _, kind := range piece.Doc.SecondaryKinds {
			p := queries.InsertPieceSecondaryKindParams{PieceID: w.id(piece.ID),
				Kind: queries.PieceSecondaryKindsKind(kind)}
			w.insert("piece_secondary_kinds", func() error { return w.q.InsertPieceSecondaryKind(w.ctx, p) })
		}
	}
	for _, piece := range pieces {
		for _, a := range piece.Doc.Aliases {
			p := queries.InsertPieceAliasParams{PieceID: w.id(piece.ID), Text: a.Text,
				Kind: queries.PieceAliasesKind(a.Kind)}
			w.insert("piece_aliases", func() error { return w.q.InsertPieceAlias(w.ctx, p) })
		}
	}
	w.pieceTermsAndRelations(pieces)
	w.pieceCredits(pieces)
	w.pieceConservation(pieces)
}

func (w *importWriter) pieceTermsAndRelations(pieces []domain.ImportedPiece) {
	for _, piece := range pieces {
		for k, term := range piece.Doc.Terms {
			var part queries.NullPieceTermsPart
			if term.Part != nil {
				part = queries.NullPieceTermsPart{PieceTermsPart: queries.PieceTermsPart(*term.Part), Valid: true}
			}
			p := queries.InsertPieceTermParams{PieceID: w.id(piece.ID), TermID: w.id(term.TermID),
				IsPrimary: term.IsPrimary, Part: part, Position: w.position(k)}
			w.insert("piece_terms", func() error { return w.q.InsertPieceTerm(w.ctx, p) })
		}
	}
	for _, piece := range pieces {
		for _, rel := range piece.Doc.Relations {
			p := queries.InsertPieceRelationParams{FromPieceID: w.id(piece.ID), ToPieceID: w.id(rel.ToPieceID),
				Type: queries.PieceRelationsType(rel.Type)}
			w.insert("piece_relations", func() error { return w.q.InsertPieceRelation(w.ctx, p) })
		}
	}
}

func (w *importWriter) pieceCredits(pieces []domain.ImportedPiece) {
	for _, piece := range pieces {
		for k, credit := range piece.Doc.Credits {
			var part queries.NullPieceCreditsPart
			if credit.Part != nil {
				part = queries.NullPieceCreditsPart{PieceCreditsPart: queries.PieceCreditsPart(*credit.Part),
					Valid: true}
			}
			p := queries.InsertPieceCreditParams{PieceID: w.id(piece.ID), PersonID: nullableID(credit.PersonID),
				Role: queries.PieceCreditsRole(credit.Role), Attribution: queries.PieceCreditsAttribution(credit.Attribution),
				Certainty: queries.PieceCreditsCertainty(credit.Certainty), Part: part, Note: nullString(credit.Note),
				Position: w.position(k)}
			w.insert("piece_credits", func() error { return w.q.InsertPieceCredit(w.ctx, p) })
		}
	}
}

// pieceConservation inserts the conservation history row of each piece whose status is assessed: at its
// revision, with ImportConservationReason and the document's source.
func (w *importWriter) pieceConservation(pieces []domain.ImportedPiece) {
	for _, piece := range pieces {
		c := piece.Doc.Conservation
		if c.Status == domain.ConservationStatusUnassessed {
			continue
		}
		p := queries.InsertConservationHistoryParams{PieceID: w.id(piece.ID),
			Status: queries.ConservationHistoryStatus(c.Status), Reason: domain.ImportConservationReason,
			SourceUrl: nullString(c.SourceURL), RevisionID: revisionID(piece.Revision),
			At: piece.Revision.CreatedAt}
		w.insert("conservation_history", func() error { return w.q.InsertConservationHistory(w.ctx, p) })
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
		w.insert("links", func() error { return w.q.InsertImportedLink(w.ctx, p) })
	}
	for _, piece := range imp.Pieces {
		for k, l := range piece.Doc.Links {
			w.attachments(l.DocLink, k, sql.NullInt64{Int64: piece.ID, Valid: true}, sql.NullInt64{})
		}
	}
	for _, person := range imp.Persons {
		for k, l := range person.Doc.Links {
			w.attachments(l.DocLink, k, sql.NullInt64{}, sql.NullInt64{Int64: person.ID, Valid: true})
		}
	}
	w.performances(imp)
	for _, perf := range w.performers {
		for k, personID := range perf.personIDs {
			p := queries.InsertPerformancePerformerParams{PerformanceID: w.id(perf.performanceID),
				PersonID: w.id(personID), Position: w.position(k)}
			w.insert("performance_performers", func() error { return w.q.InsertPerformancePerformer(w.ctx, p) })
		}
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

// attachments inserts the attachments of the link at position k of a piece's or a person's document, one per
// role.
func (w *importWriter) attachments(l domain.DocLink, k int, pieceID, personID sql.NullInt64) {
	for _, role := range l.Roles {
		p := queries.InsertLinkAttachmentParams{LinkID: w.linkID(l.URL), PieceID: pieceID, PersonID: personID,
			Role: queries.LinkAttachmentsRole(role), Position: w.position(k)}
		w.insert("link_attachments", func() error { return w.q.InsertLinkAttachment(w.ctx, p) })
	}
}

// performances inserts the performances of the documents' links, those of a piece's links being of the piece,
// and records their performers.
func (w *importWriter) performances(imp domain.CatalogueImport) {
	for _, piece := range imp.Pieces {
		for _, l := range piece.Doc.Links {
			for _, perf := range l.Performances {
				w.performance(l.URL, &piece.ID, perf)
			}
		}
	}
	for _, person := range imp.Persons {
		for _, l := range person.Doc.Links {
			for _, perf := range l.Performances {
				w.performance(l.URL, perf.PieceID, perf.DocPerformance)
			}
		}
	}
}

func (w *importWriter) performance(url string, pieceID *int64, perf domain.DocPerformance) {
	p := queries.InsertPerformanceParams{LinkID: w.linkID(url), PieceID: nullableID(pieceID),
		EventID: nullableID(perf.EventID), Year: nullInt16(perf.Year), Uploader: nullString(perf.Uploader),
		DescriptionVerbatim: perf.DescriptionVerbatim}
	w.insert("performances", func() error {
		id, err := w.q.InsertPerformance(w.ctx, p)
		if err == nil {
			w.performers = append(w.performers, importedPerformers{performanceID: id, personIDs: perf.PerformerIDs})
		}

		return err
	})
}
