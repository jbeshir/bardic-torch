package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// errOutOfRange is returned, wrapped, when a value does not fit its column's type, which validation rules out.
var errOutOfRange = errors.New("value out of range for its column")

// errUnknownLink is returned, wrapped, when a document's link has no links row, which validation rules out.
var errUnknownLink = errors.New("link has no links row")

// errUnknownDocument is returned for a document that is not one of the domain document types.
var errUnknownDocument = errors.New("not a catalogue document")

// entityWriter writes documents into their entity rows and child rows, for the import and for approval. The first
// error stops it: every later write is skipped, and err holds the error.
type entityWriter struct {
	ctx context.Context
	q   *queries.Queries
	err error
}

// exec runs one write of table, unless an earlier error stopped the writer.
func (w *entityWriter) exec(table string, run func() error) {
	if w.err != nil {
		return
	}
	if err := run(); err != nil {
		w.err = fmt.Errorf("writing %s: %w", table, err)
	}
}

// insert runs one insert into table returning the row's ID, or 0 if the writer has stopped.
func (w *entityWriter) insert(table string, run func() (int64, error)) int64 {
	var id int64
	w.exec(table, func() error {
		var err error
		id, err = run()
		return err
	})

	return id
}

// fail stops the writer with err, unless an earlier error stopped it.
func (w *entityWriter) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}

// id returns an ID as the type of the ID columns.
func (w *entityWriter) id(id int64) uint64 {
	if id < 1 {
		w.fail(fmt.Errorf("ID [%d]: %w", id, errOutOfRange))
		return 0
	}

	return uint64(id)
}

// position returns a list index as the type of the position columns.
func (w *entityWriter) position(k int) uint16 {
	if k < 0 || k > math.MaxUint16 {
		w.fail(fmt.Errorf("position [%d]: %w", k, errOutOfRange))
		return 0
	}

	return uint16(k)
}

// legacyPageID returns a legacy page's ID as the type of the legacy_wiki_pageid columns, NULL for none.
func (w *entityWriter) legacyPageID(p *domain.LegacyPage) sql.NullInt32 {
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

// nullableID returns an optional ID as the type of the nullable ID columns.
func nullableID(id *int64) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}

	return sql.NullInt64{Int64: *id, Valid: true}
}

// someID returns an ID as the type of the nullable ID columns.
func someID(id int64) sql.NullInt64 {
	return sql.NullInt64{Int64: id, Valid: true}
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

// entityRow is what an entity row holds besides its document.
type entityRow struct {
	// id is the imported ID, or NULL for a new auto-increment one.
	id         sql.NullInt64
	slug       string
	sortKey    string
	revisionID int64
	legacy     *domain.LegacyPage
	// hadInlineLyrics is the piece's legacy_had_inline_lyrics.
	hadInlineLyrics bool
}

func (w *entityWriter) insertTerm(row entityRow, d domain.TermDocument) int64 {
	p := queries.InsertTermParams{ID: row.id, Facet: queries.TermsFacet(d.Facet), Name: d.Name,
		DisplayName: d.DisplayName, Slug: row.slug, ParentID: nullableID(d.ParentID),
		RelatedAffiliationID: nullableID(d.RelatedAffiliationID), DescriptionMd: nullString(d.DescriptionMD),
		Position: d.Position, CurrentRevisionID: someID(row.revisionID),
		LegacyWikiPageid: w.legacyPageID(row.legacy), LegacyTitle: legacyTitle(row.legacy)}

	return w.insert("terms", func() (int64, error) { return w.q.InsertTerm(w.ctx, p) })
}

func (w *entityWriter) termAncestors(ancestors []domain.TermAncestor) {
	for _, a := range ancestors {
		if a.Depth < 0 || a.Depth > math.MaxUint8 {
			w.fail(fmt.Errorf("term depth [%d]: %w", a.Depth, errOutOfRange))
			return
		}
		p := queries.InsertTermAncestorParams{AncestorID: w.id(a.AncestorID), DescendantID: w.id(a.DescendantID),
			Depth: uint8(a.Depth)}
		w.exec("term_ancestors", func() error { return w.q.InsertTermAncestor(w.ctx, p) })
	}
}

func (w *entityWriter) insertCollection(row entityRow, d domain.CollectionDocument) int64 {
	p := queries.InsertCollectionParams{ID: row.id, Name: d.Name, SortName: row.sortKey,
		SortLetter: string(domain.SortLetter(row.sortKey)), Url: nullString(d.URL),
		Kind: queries.CollectionsKind(d.Kind), DescriptionMd: nullString(d.DescriptionMD),
		RegionTermID: nullableID(d.RegionTermID), CurrentRevisionID: someID(row.revisionID)}

	return w.insert("collections", func() (int64, error) { return w.q.InsertCollection(w.ctx, p) })
}

func (w *entityWriter) insertEvent(row entityRow, d domain.EventDocument) int64 {
	p := queries.InsertEventParams{ID: row.id, Slug: row.slug, Name: d.Name, CurrentRevisionID: someID(row.revisionID),
		LegacyWikiPageid: w.legacyPageID(row.legacy), LegacyTitle: legacyTitle(row.legacy)}

	return w.insert("events", func() (int64, error) { return w.q.InsertEvent(w.ctx, p) })
}

func (w *entityWriter) insertPerson(row entityRow, d domain.PersonDocument) int64 {
	p := queries.InsertPersonParams{ID: row.id, Slug: row.slug, Name: d.Name, SortName: row.sortKey,
		SortLetter: string(domain.SortLetter(row.sortKey)), Kind: queries.PersonsKind(d.Kind),
		Status: queries.PersonsStatus(d.Status), NotesMd: nullString(d.NotesMD),
		LicensingNotesMd: nullString(d.LicensingNotesMD), LicensingStatus: queries.PersonsLicensingStatus(d.LicensingStatus),
		IsStub: d.IsStub, CurrentRevisionID: someID(row.revisionID), LegacyWikiPageid: w.legacyPageID(row.legacy),
		LegacyTitle: legacyTitle(row.legacy)}

	return w.insert("persons", func() (int64, error) { return w.q.InsertPerson(w.ctx, p) })
}

// personChildren inserts a person's aliases, roles and affiliations.
func (w *entityWriter) personChildren(personID int64, d domain.PersonDocument) {
	for _, a := range d.Aliases {
		p := queries.InsertPersonAliasParams{PersonID: w.id(personID), Name: a.Name,
			Kind: queries.PersonAliasesKind(a.Kind)}
		w.exec("person_aliases", func() error { return w.q.InsertPersonAlias(w.ctx, p) })
	}
	for _, role := range d.Roles {
		p := queries.InsertPersonRoleParams{PersonID: w.id(personID), Role: queries.PersonRolesRole(role)}
		w.exec("person_roles", func() error { return w.q.InsertPersonRole(w.ctx, p) })
	}
	for _, termID := range d.AffiliationIDs {
		p := queries.InsertPersonAffiliationParams{PersonID: w.id(personID), TermID: w.id(termID)}
		w.exec("person_affiliations", func() error { return w.q.InsertPersonAffiliation(w.ctx, p) })
	}
}

func (w *entityWriter) insertPiece(row entityRow, d domain.PieceDocument) int64 {
	p := queries.InsertPieceParams{ID: row.id, Slug: row.slug, Title: d.Title, SortTitle: row.sortKey,
		SortLetter: string(domain.SortLetter(row.sortKey)), Kind: queries.PiecesKind(d.Kind),
		ConservationStatus: queries.PiecesConservationStatus(d.ConservationStatus), IsStub: d.IsStub,
		NotesMd: nullString(d.NotesMD), VersionNotesMd: nullString(d.VersionNotesMD),
		LicensingNotesMd: nullString(d.LicensingNotesMD),
		LicensingStatus:  queries.PiecesLicensingStatus(d.LicensingStatus), LegacyHadInlineLyrics: row.hadInlineLyrics,
		CurrentRevisionID: someID(row.revisionID), LegacyWikiPageid: w.legacyPageID(row.legacy),
		LegacyTitle: legacyTitle(row.legacy)}

	return w.insert("pieces", func() (int64, error) { return w.q.InsertPiece(w.ctx, p) })
}

// pieceChildren inserts a piece's aliases, terms and credits.
func (w *entityWriter) pieceChildren(pieceID int64, d domain.PieceDocument) {
	for _, a := range d.Aliases {
		p := queries.InsertPieceAliasParams{PieceID: w.id(pieceID), Text: a.Text, Kind: queries.PieceAliasesKind(a.Kind)}
		w.exec("piece_aliases", func() error { return w.q.InsertPieceAlias(w.ctx, p) })
	}
	for k, termID := range d.TermIDs {
		p := queries.InsertPieceTermParams{PieceID: w.id(pieceID), TermID: w.id(termID), Position: w.position(k)}
		w.exec("piece_terms", func() error { return w.q.InsertPieceTerm(w.ctx, p) })
	}
	for k, credit := range d.Credits {
		p := queries.InsertPieceCreditParams{PieceID: w.id(pieceID), PersonID: nullableID(credit.PersonID),
			Role: queries.PieceCreditsRole(credit.Role), Attribution: queries.PieceCreditsAttribution(credit.Attribution),
			Certainty: queries.PieceCreditsCertainty(credit.Certainty), Position: w.position(k)}
		w.exec("piece_credits", func() error { return w.q.InsertPieceCredit(w.ctx, p) })
	}
}

// linkID returns the ID, among linkIDs by normalized URL, of the links row of a document's link.
func (w *entityWriter) linkID(linkIDs map[string]int64, url string) uint64 {
	normalized, err := domain.NormalizeURL(url)
	id, ok := linkIDs[normalized]
	if err != nil || !ok {
		w.fail(errUnknownLink)
		return 0
	}

	return w.id(id)
}

// pieceLinks inserts the attachments of a piece's links, one per role, and the performances the piece owns.
func (w *entityWriter) pieceLinks(pieceID int64, links []domain.DocPieceLink, linkIDs map[string]int64) {
	for k, l := range links {
		linkID := w.linkID(linkIDs, l.URL)
		w.attachments(linkID, l.Roles, k, someID(pieceID), sql.NullInt64{})
		for _, perf := range l.Performances {
			w.performance(linkID, &pieceID, perf, someID(pieceID), sql.NullInt64{})
		}
	}
}

// personLinks inserts the attachments of a person's links, one per role, and the performances the person owns.
func (w *entityWriter) personLinks(personID int64, links []domain.DocPersonLink, linkIDs map[string]int64) {
	for k, l := range links {
		linkID := w.linkID(linkIDs, l.URL)
		w.attachments(linkID, l.Roles, k, sql.NullInt64{}, someID(personID))
		for _, perf := range l.Performances {
			w.performance(linkID, perf.PieceID, perf.DocPerformance, sql.NullInt64{}, someID(personID))
		}
	}
}

// attachments inserts the attachments of the link linkID at position k of a piece's or a person's document.
func (w *entityWriter) attachments(linkID uint64, roles []domain.LinkRole, k int, pieceID, personID sql.NullInt64) {
	for _, role := range roles {
		p := queries.InsertLinkAttachmentParams{LinkID: linkID, PieceID: pieceID, PersonID: personID,
			Role: queries.LinkAttachmentsRole(role), Position: w.position(k)}
		w.exec("link_attachments", func() error { return w.q.InsertLinkAttachment(w.ctx, p) })
	}
}

// performance inserts a performance recorded by the link linkID, of the piece pieceID if not nil, owned by the
// piece or person whose document lists it, with its performers in order.
func (w *entityWriter) performance(
	linkID uint64, pieceID *int64, perf domain.DocPerformance, ownerPieceID, ownerPersonID sql.NullInt64,
) {
	p := queries.InsertPerformanceParams{LinkID: linkID, PieceID: nullableID(pieceID),
		EventID: nullableID(perf.EventID), Year: nullInt16(perf.Year), Uploader: nullString(perf.Uploader),
		DescriptionVerbatim: perf.DescriptionVerbatim, OwnerPieceID: ownerPieceID, OwnerPersonID: ownerPersonID}
	id := w.insert("performances", func() (int64, error) { return w.q.InsertPerformance(w.ctx, p) })
	for k, personID := range perf.PerformerIDs {
		p := queries.InsertPerformancePerformerParams{PerformanceID: w.id(id), PersonID: w.id(personID),
			Position: w.position(k)}
		w.exec("performance_performers", func() error { return w.q.InsertPerformancePerformer(w.ctx, p) })
	}
}

// write writes the document the revision revisionID approves into its entity, the entity entityID or, if it is 0,
// a new one, and returns
// the entity's ID. An existing entity's slug is kept; its child rows, link attachments and owned performances are
// replaced.
func (w *entityWriter) write(entityID int64, doc document, revisionID int64) int64 {
	switch d := doc.(type) {
	case domain.PieceDocument:
		return w.writePiece(entityID, d, revisionID)
	case domain.PersonDocument:
		return w.writePerson(entityID, d, revisionID)
	case domain.TermDocument:
		return w.writeTerm(entityID, d, revisionID)
	case domain.CollectionDocument:
		sortKey := domain.SortTitle(d.Name)
		if entityID == 0 {
			return w.insertCollection(entityRow{sortKey: sortKey, revisionID: revisionID}, d)
		}
		p := queries.UpdateCollectionParams{Name: d.Name, SortName: sortKey,
			SortLetter: string(domain.SortLetter(sortKey)), Url: nullString(d.URL),
			Kind: queries.CollectionsKind(d.Kind), DescriptionMd: nullString(d.DescriptionMD),
			RegionTermID: nullableID(d.RegionTermID), CurrentRevisionID: someID(revisionID), ID: w.id(entityID)}
		w.exec("collections", func() error { return w.q.UpdateCollection(w.ctx, p) })
	case domain.EventDocument:
		if entityID == 0 {
			slug := w.newSlug(d.Name, domain.SlugEntityTypeEvent, w.q.ListEventSlugs)
			return w.insertEvent(entityRow{slug: slug, revisionID: revisionID}, d)
		}
		p := queries.UpdateEventParams{Name: d.Name, CurrentRevisionID: someID(revisionID), ID: w.id(entityID)}
		w.exec("events", func() error { return w.q.UpdateEvent(w.ctx, p) })
	default:
		w.fail(fmt.Errorf("%w: %T", errUnknownDocument, doc))
	}

	return entityID
}

// newSlug returns a new entity's slug: domain.Slugify(name), or the entity type if that is empty, with the first
// free numeric suffix among the slugs taken returns.
func (w *entityWriter) newSlug(
	name string, t domain.SlugEntityType, taken func(context.Context) ([]string, error),
) string {
	if w.err != nil {
		return ""
	}
	slugs, err := taken(w.ctx)
	if err != nil {
		w.fail(fmt.Errorf("listing %s slugs: %w", t, err))
		return ""
	}
	set := make(map[string]bool, len(slugs))
	for _, s := range slugs {
		set[s] = true
	}
	base := domain.Slugify(name)
	if base == "" {
		base = t.String()
	}

	return domain.FreeSlug(base, set)
}

func (w *entityWriter) writePiece(id int64, d domain.PieceDocument, revisionID int64) int64 {
	sortKey := domain.SortTitle(d.Title)
	if id == 0 {
		slug := w.newSlug(d.Title, domain.SlugEntityTypePiece, w.q.ListPieceSlugs)
		id = w.insertPiece(entityRow{slug: slug, sortKey: sortKey, revisionID: revisionID}, d)
	} else {
		key := w.id(id)
		p := queries.UpdatePieceParams{Title: d.Title, SortTitle: sortKey,
			SortLetter: string(domain.SortLetter(sortKey)), Kind: queries.PiecesKind(d.Kind),
			ConservationStatus: queries.PiecesConservationStatus(d.ConservationStatus), IsStub: d.IsStub,
			NotesMd: nullString(d.NotesMD), VersionNotesMd: nullString(d.VersionNotesMD),
			LicensingNotesMd:  nullString(d.LicensingNotesMD),
			LicensingStatus:   queries.PiecesLicensingStatus(d.LicensingStatus),
			CurrentRevisionID: someID(revisionID), ID: key}
		w.exec("pieces", func() error { return w.q.UpdatePiece(w.ctx, p) })
		w.deleteChildren(key, w.q.DeletePieceAliases, w.q.DeletePieceTerms, w.q.DeletePieceCredits)
	}
	w.pieceChildren(id, d)
	links := make([]domain.DocLink, len(d.Links))
	for k, l := range d.Links {
		links[k] = l.DocLink
	}
	linkIDs := w.upsertLinks(links)
	w.exec("link_attachments", func() error {
		return w.q.DeleteLinkAttachments(w.ctx, queries.DeleteLinkAttachmentsParams{PieceID: someID(id)})
	})
	w.exec("performances", func() error {
		return w.q.DeleteOwnedPerformances(w.ctx, queries.DeleteOwnedPerformancesParams{OwnerPieceID: someID(id)})
	})
	w.pieceLinks(id, d.Links, linkIDs)

	return id
}

func (w *entityWriter) writePerson(id int64, d domain.PersonDocument, revisionID int64) int64 {
	if id == 0 {
		slug := w.newSlug(d.Name, domain.SlugEntityTypePerson, w.q.ListPersonSlugs)
		id = w.insertPerson(entityRow{slug: slug, sortKey: d.Name, revisionID: revisionID}, d)
	} else {
		key := w.id(id)
		p := queries.UpdatePersonParams{Name: d.Name, SortName: d.Name, SortLetter: string(domain.SortLetter(d.Name)),
			Kind: queries.PersonsKind(d.Kind), Status: queries.PersonsStatus(d.Status), NotesMd: nullString(d.NotesMD),
			LicensingNotesMd: nullString(d.LicensingNotesMD),
			LicensingStatus:  queries.PersonsLicensingStatus(d.LicensingStatus), IsStub: d.IsStub,
			CurrentRevisionID: someID(revisionID), ID: key}
		w.exec("persons", func() error { return w.q.UpdatePerson(w.ctx, p) })
		w.deleteChildren(key, w.q.DeletePersonAliases, w.q.DeletePersonRoles, w.q.DeletePersonAffiliations)
	}
	w.personChildren(id, d)
	links := make([]domain.DocLink, len(d.Links))
	for k, l := range d.Links {
		links[k] = l.DocLink
	}
	linkIDs := w.upsertLinks(links)
	w.exec("link_attachments", func() error {
		return w.q.DeleteLinkAttachments(w.ctx, queries.DeleteLinkAttachmentsParams{PersonID: someID(id)})
	})
	w.exec("performances", func() error {
		return w.q.DeleteOwnedPerformances(w.ctx, queries.DeleteOwnedPerformancesParams{OwnerPersonID: someID(id)})
	})
	w.personLinks(id, d.Links, linkIDs)

	return id
}

// deleteChildren deletes the child rows of the entity id, one table per delete.
func (w *entityWriter) deleteChildren(id uint64, deletes ...func(context.Context, uint64) error) {
	for _, del := range deletes {
		w.exec("child rows", func() error { return del(w.ctx, id) })
	}
}

// writeTerm writes a term and rebuilds the whole term_ancestors closure.
func (w *entityWriter) writeTerm(id int64, d domain.TermDocument, revisionID int64) int64 {
	if id == 0 {
		slug := w.newSlug(d.Name, domain.SlugEntityTypeTerm, w.q.ListTermSlugs)
		id = w.insertTerm(entityRow{slug: slug, revisionID: revisionID}, d)
	} else {
		key := w.id(id)
		p := queries.UpdateTermParams{Facet: queries.TermsFacet(d.Facet), Name: d.Name, DisplayName: d.DisplayName,
			ParentID: nullableID(d.ParentID), RelatedAffiliationID: nullableID(d.RelatedAffiliationID),
			DescriptionMd: nullString(d.DescriptionMD), Position: d.Position, CurrentRevisionID: someID(revisionID),
			ID: key}
		w.exec("terms", func() error { return w.q.UpdateTerm(w.ctx, p) })
	}

	var terms []queries.ListTermsRow
	w.exec("term_ancestors", func() error {
		var err error
		terms, err = w.q.ListTerms(w.ctx)
		return err
	})
	parents := make(map[int64]*int64, len(terms))
	for _, t := range terms {
		parents[t.ID] = int64Ptr(t.ParentID)
	}
	ancestors, err := domain.TermClosure(parents)
	if err != nil {
		w.fail(fmt.Errorf("terms: %w", err))
	}
	w.exec("term_ancestors", func() error { return w.q.DeleteTermAncestors(w.ctx) })
	w.termAncestors(ancestors)

	return id
}

// upsertLinks inserts the links rows of a document's links, or updates the editor fields of those that exist, and
// returns their IDs by normalized URL.
func (w *entityWriter) upsertLinks(links []domain.DocLink) map[string]int64 {
	ids := make(map[string]int64, len(links))
	for _, l := range links {
		normalized, err := domain.NormalizeURL(l.URL)
		if err != nil {
			w.fail(fmt.Errorf("link %s: %w", l.URL, err))
			return ids
		}
		var placeType queries.NullLinksPlaceType
		if l.PlaceType != nil {
			placeType = queries.NullLinksPlaceType{LinksPlaceType: queries.LinksPlaceType(*l.PlaceType), Valid: true}
		}
		p := queries.UpsertLinkParams{Url: l.URL, NormalizedUrl: normalized, Title: nullString(l.Title),
			Description: nullString(l.Description), Platform: nullString(l.Platform), PlaceType: placeType,
			CollectionID: nullableID(l.CollectionID), Locator: nullString(l.Locator)}
		ids[normalized] = w.insert("links", func() (int64, error) { return w.q.UpsertLink(w.ctx, p) })
	}

	return ids
}
