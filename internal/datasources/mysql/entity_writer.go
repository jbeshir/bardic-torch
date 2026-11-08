package mysql

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// entityWriter writes catalogue rows: the per-table inserts of an entity's child rows, which the import and the
// approval writer share, and the approval writer's replacement of an entity's rows with its approved document
// . The first error stops it: every later write is skipped, and err holds the error.
type entityWriter struct {
	ctx context.Context
	q   *queries.Queries
	err error
}

// writeError is a failed write of a table, wrapping the database's error: callers name the step and the MySQL
// error number of an error leaving them (databaseError), never the server's message.
type writeError struct {
	table string
	err   error
}

func (e *writeError) Error() string {
	return "writing " + e.table + ": " + e.err.Error()
}

func (e *writeError) Unwrap() error {
	return e.err
}

// exec runs one write of table, unless an earlier error stopped the writer.
func (w *entityWriter) exec(table string, run func() error) {
	if w.err != nil {
		return
	}
	if err := run(); err != nil {
		w.err = &writeError{table: table, err: err}
	}
}

// fail stops the writer with err, unless an earlier error stopped it.
func (w *entityWriter) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}

// errOutOfRange is returned, wrapped, when a value does not fit its column's type, which validation rules out.
var errOutOfRange = errors.New("value out of range for its column")

// id returns an ID as the type of the ID columns.
func (w *entityWriter) id(id int64) uint64 {
	unsigned, ok := positiveID(id)
	if !ok {
		w.fail(fmt.Errorf("ID [%d]: %w", id, errOutOfRange))
	}

	return unsigned
}

// position returns a list index as the type of the position columns.
func (w *entityWriter) position(k int) uint16 {
	if k < 0 || k > math.MaxUint16 {
		w.fail(fmt.Errorf("position [%d]: %w", k, errOutOfRange))
		return 0
	}

	return uint16(k)
}

func (w *entityWriter) termAliases(termID int64, aliases []string) {
	for _, alias := range aliases {
		p := queries.InsertTermAliasParams{TermID: w.id(termID), Name: alias}
		w.exec("term_aliases", func() error { return w.q.InsertTermAlias(w.ctx, p) })
	}
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

func (w *entityWriter) personAliases(personID int64, aliases []domain.DocPersonAlias) {
	for _, a := range aliases {
		p := queries.InsertPersonAliasParams{PersonID: w.id(personID), Name: a.Name,
			Kind: queries.PersonAliasesKind(a.Kind), IsPublic: a.IsPublic}
		w.exec("person_aliases", func() error { return w.q.InsertPersonAlias(w.ctx, p) })
	}
}

func (w *entityWriter) personRoles(personID int64, roles []domain.PersonRole) {
	for _, role := range roles {
		p := queries.InsertPersonRoleParams{PersonID: w.id(personID), Role: queries.PersonRolesRole(role)}
		w.exec("person_roles", func() error { return w.q.InsertPersonRole(w.ctx, p) })
	}
}

func (w *entityWriter) personAffiliations(personID int64, termIDs []int64) {
	for _, termID := range termIDs {
		p := queries.InsertPersonAffiliationParams{PersonID: w.id(personID), TermID: w.id(termID)}
		w.exec("person_affiliations", func() error { return w.q.InsertPersonAffiliation(w.ctx, p) })
	}
}

func (w *entityWriter) personRelations(personID int64, relations []domain.DocPersonRelation) {
	for _, rel := range relations {
		p := queries.InsertPersonRelationParams{FromPersonID: w.id(personID), ToPersonID: w.id(rel.ToPersonID),
			Type: queries.PersonRelationsType(rel.Type)}
		w.exec("person_relations", func() error { return w.q.InsertPersonRelation(w.ctx, p) })
	}
}

func (w *entityWriter) personPreferences(personID int64, preferences []domain.DocPreference) {
	for _, pref := range preferences {
		p := queries.InsertPersonPreferenceParams{PersonID: w.id(personID),
			Dimension: queries.PersonPreferencesDimension(pref.Dimension), ValueJson: w.preferenceValue(pref),
			SourceUrl: pref.SourceURL}
		w.exec("person_preferences", func() error { return w.q.InsertPersonPreference(w.ctx, p) })
	}
}

func (w *entityWriter) pieceSecondaryKinds(pieceID int64, kinds []domain.PieceKind) {
	for _, kind := range kinds {
		p := queries.InsertPieceSecondaryKindParams{PieceID: w.id(pieceID), Kind: queries.PieceSecondaryKindsKind(kind)}
		w.exec("piece_secondary_kinds", func() error { return w.q.InsertPieceSecondaryKind(w.ctx, p) })
	}
}

func (w *entityWriter) pieceAliases(pieceID int64, aliases []domain.DocPieceAlias) {
	for _, a := range aliases {
		p := queries.InsertPieceAliasParams{PieceID: w.id(pieceID), Text: a.Text, Kind: queries.PieceAliasesKind(a.Kind)}
		w.exec("piece_aliases", func() error { return w.q.InsertPieceAlias(w.ctx, p) })
	}
}

func (w *entityWriter) pieceTerms(pieceID int64, terms []domain.DocPieceTerm) {
	for k, term := range terms {
		var part queries.NullPieceTermsPart
		if term.Part != nil {
			part = queries.NullPieceTermsPart{PieceTermsPart: queries.PieceTermsPart(*term.Part), Valid: true}
		}
		p := queries.InsertPieceTermParams{PieceID: w.id(pieceID), TermID: w.id(term.TermID),
			IsPrimary: term.IsPrimary, Part: part, Position: w.position(k)}
		w.exec("piece_terms", func() error { return w.q.InsertPieceTerm(w.ctx, p) })
	}
}

func (w *entityWriter) pieceRelations(pieceID int64, relations []domain.DocPieceRelation) {
	for _, rel := range relations {
		p := queries.InsertPieceRelationParams{FromPieceID: w.id(pieceID), ToPieceID: w.id(rel.ToPieceID),
			Type: queries.PieceRelationsType(rel.Type)}
		w.exec("piece_relations", func() error { return w.q.InsertPieceRelation(w.ctx, p) })
	}
}

func (w *entityWriter) pieceCredits(pieceID int64, credits []domain.DocCredit) {
	for k, credit := range credits {
		var part queries.NullPieceCreditsPart
		if credit.Part != nil {
			part = queries.NullPieceCreditsPart{PieceCreditsPart: queries.PieceCreditsPart(*credit.Part), Valid: true}
		}
		p := queries.InsertPieceCreditParams{PieceID: w.id(pieceID), PersonID: nullableID(credit.PersonID),
			Role: queries.PieceCreditsRole(credit.Role), Attribution: queries.PieceCreditsAttribution(credit.Attribution),
			Certainty: queries.PieceCreditsCertainty(credit.Certainty), Part: part, Note: nullString(credit.Note),
			Position: w.position(k)}
		w.exec("piece_credits", func() error { return w.q.InsertPieceCredit(w.ctx, p) })
	}
}

func (w *entityWriter) piecePreferences(pieceID int64, preferences []domain.DocPreference) {
	for _, pref := range preferences {
		p := queries.InsertPiecePreferenceParams{PieceID: w.id(pieceID),
			Dimension: queries.PiecePreferencesDimension(pref.Dimension), ValueJson: w.preferenceValue(pref),
			SourceUrl: pref.SourceURL}
		w.exec("piece_preferences", func() error { return w.q.InsertPiecePreference(w.ctx, p) })
	}
}

// linkAttachments inserts the attachments of the link linkID at position k of a piece's or a person's document,
// one per role.
func (w *entityWriter) linkAttachments(linkID uint64, roles []domain.LinkRole, k int, pieceID, personID sql.NullInt64) {
	for _, role := range roles {
		p := queries.InsertLinkAttachmentParams{LinkID: linkID, PieceID: pieceID, PersonID: personID,
			Role: queries.LinkAttachmentsRole(role), Position: w.position(k)}
		w.exec("link_attachments", func() error { return w.q.InsertLinkAttachment(w.ctx, p) })
	}
}

// performance inserts a performance recorded by the link linkID, of the piece pieceID if not nil, owned by the
// piece ownerPieceID or the person ownerPersonID (the document whose link lists it), one of which is not nil. It
// returns the performance's ID, 0 if the writer has stopped.
func (w *entityWriter) performance(
	linkID uint64, pieceID *int64, perf domain.DocPerformance, ownerPieceID, ownerPersonID *int64,
) int64 {
	p := queries.InsertPerformanceParams{LinkID: linkID, PieceID: nullableID(pieceID),
		EventID: nullableID(perf.EventID), Year: nullInt16(perf.Year), Uploader: nullString(perf.Uploader),
		DescriptionVerbatim: perf.DescriptionVerbatim, OwnerPieceID: nullableID(ownerPieceID),
		OwnerPersonID: nullableID(ownerPersonID)}
	var id int64
	w.exec("performances", func() error {
		var err error
		id, err = w.q.InsertPerformance(w.ctx, p)
		return err
	})

	return id
}

// performers inserts the performers of the performance performanceID, in order.
func (w *entityWriter) performers(performanceID int64, personIDs []int64) {
	for k, personID := range personIDs {
		p := queries.InsertPerformancePerformerParams{PerformanceID: w.id(performanceID), PersonID: w.id(personID),
			Position: w.position(k)}
		w.exec("performance_performers", func() error { return w.q.InsertPerformancePerformer(w.ctx, p) })
	}
}

// approvedEntity is what an approval writes of one entity: its approved document, and its current document, nil
// for a new entity, whose placeholder row the approval fills in.
type approvedEntity struct {
	entity  domain.EntityRef
	doc     domain.AnyDocument
	current domain.AnyDocument
}

// write replaces the rows of the entity with its approved document: the entity row (and a new
// entity's slug), its own child rows, its links, link attachments and owned performances. The entity's row is
// locked FOR UPDATE.
func (w *entityWriter) write(e approvedEntity) {
	id := e.entity.ID
	switch d := e.doc.(type) {
	case domain.PieceDocument:
		var current *domain.PieceDocument
		if c, ok := e.current.(domain.PieceDocument); ok {
			current = &c
		}
		w.writePiece(id, d, current)
	case domain.PersonDocument:
		var current *domain.PersonDocument
		if c, ok := e.current.(domain.PersonDocument); ok {
			current = &c
		}
		w.writePerson(id, d, current)
	case domain.TermDocument:
		w.writeTerm(id, d, e.current == nil)
	case domain.CollectionDocument:
		w.writeCollection(id, d)
	case domain.EventDocument:
		w.writeEvent(id, d, e.current == nil)
	}
}

func (w *entityWriter) writePiece(id int64, d domain.PieceDocument, current *domain.PieceDocument) {
	sortTitle := domain.SortTitle(d.Title)
	key := w.id(id)
	p := queries.UpdatePieceFromDocumentParams{Title: d.Title, SortTitle: sortTitle,
		SortLetter: string(domain.SortLetter(sortTitle)), Kind: queries.PiecesKind(d.Kind),
		ConservationStatus: queries.PiecesConservationStatus(d.Conservation.Status), IsStub: d.IsStub,
		NotesMd: nullString(d.NotesMD), VersionNotesMd: nullString(d.VersionNotesMD),
		LicensingNotesMd: nullString(d.LicensingNotesMD), LicensingStatus: queries.PiecesLicensingStatus(d.LicensingStatus),
		Licence: nullLicence(d.Licence), HistoricalSourceMd: nullString(d.HistoricalSourceMD),
		PeriodStart: nullInt16(d.PeriodStart),
		PeriodEnd:   nullInt16(d.PeriodEnd), ID: key}
	w.exec("pieces", func() error { return requireMatched(w.q.UpdatePieceFromDocument(w.ctx, p)) })
	if current == nil {
		w.allocateSlug(domain.SlugEntityTypePiece, id, d.Title, func(ctx context.Context, slug string, rowID uint64) error {
			return w.q.TrySetSlugPiece(ctx, queries.TrySetSlugPieceParams{Slug: slug, ID: rowID})
		})
	}
	w.exec("piece_secondary_kinds", func() error { return w.q.DeletePieceSecondaryKinds(w.ctx, key) })
	w.pieceSecondaryKinds(id, d.SecondaryKinds)
	w.exec("piece_aliases", func() error { return w.q.DeletePieceAliases(w.ctx, key) })
	w.pieceAliases(id, d.Aliases)
	w.exec("piece_terms", func() error { return w.q.DeletePieceTerms(w.ctx, key) })
	w.pieceTerms(id, d.Terms)
	w.exec("piece_relations", func() error { return w.q.DeletePieceRelations(w.ctx, key) })
	w.pieceRelations(id, d.Relations)
	w.exec("piece_credits", func() error { return w.q.DeletePieceCredits(w.ctx, key) })
	w.pieceCredits(id, d.Credits)
	w.exec("piece_preferences", func() error { return w.q.DeletePiecePreferences(w.ctx, key) })
	w.piecePreferences(id, d.Preferences)

	var prev []domain.DocLink
	if current != nil {
		prev = docLinks(current.Links, func(l domain.DocPieceLink) domain.DocLink { return l.DocLink })
	}
	linkIDs := w.upsertLinks(docLinks(d.Links, func(l domain.DocPieceLink) domain.DocLink { return l.DocLink }), prev)
	w.exec("link_attachments", func() error { return w.q.DeleteLinkAttachmentsOfPiece(w.ctx, w.nullID(id)) })
	w.exec("performances", func() error { return w.q.DeletePerformancesOwnedByPiece(w.ctx, w.nullID(id)) })
	for k, l := range d.Links {
		w.linkAttachments(linkIDs[k], l.Roles, k, w.nullID(id), sql.NullInt64{})
		for _, perf := range l.Performances {
			w.performers(w.performance(linkIDs[k], &id, perf, &id, nil), perf.PerformerIDs)
		}
	}
}

func (w *entityWriter) writePerson(id int64, d domain.PersonDocument, current *domain.PersonDocument) {
	key := w.id(id)
	p := queries.UpdatePersonFromDocumentParams{Name: d.Name, SortName: d.Name,
		SortLetter: string(domain.SortLetter(d.Name)), Kind: queries.PersonsKind(d.Kind),
		Status: queries.PersonsStatus(d.Status), StatusDate: nullDate(d.StatusDate),
		StatusSourceUrl: nullString(d.StatusSourceURL), Pronouns: nullString(d.Pronouns),
		NotesMd: nullString(d.NotesMD), LicensingNotesMd: nullString(d.LicensingNotesMD),
		LicensingStatus: queries.PersonsLicensingStatus(d.LicensingStatus), IsStub: d.IsStub, ID: key}
	w.exec("persons", func() error { return requireMatched(w.q.UpdatePersonFromDocument(w.ctx, p)) })
	if current == nil {
		w.allocateSlug(domain.SlugEntityTypePerson, id, d.Name, func(ctx context.Context, slug string, rowID uint64) error {
			return w.q.TrySetSlugPerson(ctx, queries.TrySetSlugPersonParams{Slug: slug, ID: rowID})
		})
	}
	w.exec("person_aliases", func() error { return w.q.DeletePersonAliases(w.ctx, key) })
	w.personAliases(id, d.Aliases)
	w.exec("person_roles", func() error { return w.q.DeletePersonRoles(w.ctx, key) })
	w.personRoles(id, d.Roles)
	w.exec("person_affiliations", func() error { return w.q.DeletePersonAffiliations(w.ctx, key) })
	w.personAffiliations(id, d.AffiliationIDs)
	w.exec("person_relations", func() error { return w.q.DeletePersonRelations(w.ctx, key) })
	w.personRelations(id, d.Relations)
	w.exec("person_preferences", func() error { return w.q.DeletePersonPreferences(w.ctx, key) })
	w.personPreferences(id, d.Preferences)

	var prev []domain.DocLink
	if current != nil {
		prev = docLinks(current.Links, func(l domain.DocPersonLink) domain.DocLink { return l.DocLink })
	}
	linkIDs := w.upsertLinks(docLinks(d.Links, func(l domain.DocPersonLink) domain.DocLink { return l.DocLink }), prev)
	w.exec("link_attachments", func() error { return w.q.DeleteLinkAttachmentsOfPerson(w.ctx, w.nullID(id)) })
	w.exec("performances", func() error { return w.q.DeletePerformancesOwnedByPerson(w.ctx, w.nullID(id)) })
	for k, l := range d.Links {
		w.linkAttachments(linkIDs[k], l.Roles, k, sql.NullInt64{}, w.nullID(id))
		for _, perf := range l.Performances {
			w.performers(w.performance(linkIDs[k], perf.PieceID, perf.DocPerformance, nil, &id), perf.PerformerIDs)
		}
	}
}

func (w *entityWriter) writeTerm(id int64, d domain.TermDocument, isNew bool) {
	key := w.id(id)
	p := queries.UpdateTermFromDocumentParams{Facet: queries.TermsFacet(d.Facet), Name: d.Name,
		DisplayName: d.DisplayName, ParentID: nullableID(d.ParentID),
		RelatedAffiliationID: nullableID(d.RelatedAffiliationID), DescriptionMd: nullString(d.DescriptionMD),
		Position: d.Position, ID: key}
	w.exec("terms", func() error {
		err := requireMatched(w.q.UpdateTermFromDocument(w.ctx, p))
		if mysqlErrorNumber(err) == erDuplicateEntry {
			return &domain.DocumentError{Problems: []domain.DocumentProblem{{Path: "/name",
				Code: domain.ProblemDuplicate}}}
		}
		return err
	})
	if isNew {
		w.allocateSlug(domain.SlugEntityTypeTerm, id, d.Name, func(ctx context.Context, slug string, rowID uint64) error {
			return w.q.TrySetSlugTerm(ctx, queries.TrySetSlugTermParams{Slug: slug, ID: rowID})
		})
	}
	w.exec("term_aliases", func() error { return w.q.DeleteTermAliases(w.ctx, key) })
	w.termAliases(id, d.Aliases)
}

func (w *entityWriter) writeCollection(id int64, d domain.CollectionDocument) {
	sortName := domain.SortTitle(d.Name)
	p := queries.UpdateCollectionFromDocumentParams{Name: d.Name, SortName: sortName,
		SortLetter: string(domain.SortLetter(sortName)), Url: nullString(d.URL), Kind: queries.CollectionsKind(d.Kind),
		DescriptionMd: nullString(d.DescriptionMD), RegionTermID: nullableID(d.RegionTermID),
		ParentID: nullableID(d.ParentID), IsBardicResource: d.IsBardicResource,
		Enumerable: queries.CollectionsEnumerable(d.Enumerable), ID: w.id(id)}
	w.exec("collections", func() error { return requireMatched(w.q.UpdateCollectionFromDocument(w.ctx, p)) })
}

func (w *entityWriter) writeEvent(id int64, d domain.EventDocument, isNew bool) {
	p := queries.UpdateEventFromDocumentParams{Name: d.Name, Kind: queries.EventsKind(d.Kind),
		DescriptionMd: nullString(d.DescriptionMD), ID: w.id(id)}
	w.exec("events", func() error { return requireMatched(w.q.UpdateEventFromDocument(w.ctx, p)) })
	if isNew {
		w.allocateSlug(domain.SlugEntityTypeEvent, id, d.Name, func(ctx context.Context, slug string, rowID uint64) error {
			return w.q.TrySetSlugEvent(ctx, queries.TrySetSlugEventParams{Slug: slug, ID: rowID})
		})
	}
}

// nullID returns an entity's ID as the type of the nullable ID columns.
func (w *entityWriter) nullID(id int64) sql.NullInt64 {
	return sql.NullInt64{Int64: id, Valid: true}
}

// docLinks returns the editor-owned parts of a document's links.
func docLinks[L any](links []L, part func(L) domain.DocLink) []domain.DocLink {
	out := make([]domain.DocLink, len(links))
	for i, l := range links {
		out[i] = part(l)
	}

	return out
}

// upsertLinks writes the links rows of a document's links, given the links of the entity's current document prev
// (nil for a new entity), and returns their IDs, in the order of links. Rows are locked in ascending normalized URL
// order: each is inserted with the link's fields if there is none, and an existing row's shared fields are
// written by the link write rule (domain.LinkRowUpdate). A row's hidden and liveness state is never written.
func (w *entityWriter) upsertLinks(links, prev []domain.DocLink) []uint64 {
	normalized := make([]string, len(links))
	for i, l := range links {
		n, err := domain.NormalizeURL(l.URL)
		if err != nil {
			w.fail(fmt.Errorf("link [%d]: %w", i, err))
			return make([]uint64, len(links))
		}
		normalized[i] = n
	}
	previous := map[string]domain.DocLink{}
	for _, l := range prev {
		if n, err := domain.NormalizeURL(l.URL); err == nil {
			previous[n] = l
		}
	}
	order := make([]int, len(links))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(normalized[a], normalized[b]) })

	ids := make([]uint64, len(links))
	for _, i := range order {
		var prevLink *domain.DocLink
		if l, ok := previous[normalized[i]]; ok {
			prevLink = &l
		}
		ids[i] = w.upsertLink(links[i], normalized[i], prevLink)
	}

	return ids
}

// upsertLink writes the links row of the document link l, whose normalized URL is normalized, and returns its ID.
func (w *entityWriter) upsertLink(l domain.DocLink, normalized string, prev *domain.DocLink) uint64 {
	fields := linkFields(l)
	var id int64
	w.exec("links", func() error {
		var err error
		id, err = w.q.UpsertLink(w.ctx, queries.UpsertLinkParams{Url: fields.Url, NormalizedUrl: normalized,
			Title: fields.Title, Description: fields.Description, Platform: fields.Platform,
			PlaceType: fields.PlaceType, CollectionID: fields.CollectionID, Locator: fields.Locator})
		return err
	})
	key := w.id(id)
	var row queries.GetLinkFieldsRow
	w.exec("links", func() error {
		var err error
		row, err = w.q.GetLinkFields(w.ctx, key)
		return err
	})
	if w.err != nil {
		return 0
	}
	current := docLinkOf(row)
	if update := linkFields(domain.LinkRowUpdate(&current, prev, l)); update != row {
		w.exec("links", func() error {
			return w.q.UpdateLinkFields(w.ctx, queries.UpdateLinkFieldsParams{Url: update.Url, Title: update.Title,
				Description: update.Description, Platform: update.Platform, PlaceType: update.PlaceType,
				CollectionID: update.CollectionID, Locator: update.Locator, ID: key})
		})
	}

	return key
}

// nullLicence returns an optional licence as the type of the licence column.
func nullLicence(l *domain.Licence) queries.NullPiecesLicence {
	if l == nil {
		return queries.NullPiecesLicence{}
	}

	return queries.NullPiecesLicence{PiecesLicence: queries.PiecesLicence(*l), Valid: true}
}

// nullDate returns an optional date as the type of the date columns.
func nullDate(d *domain.Date) sql.NullTime {
	if d == nil {
		return sql.NullTime{}
	}

	return sql.NullTime{Time: d.Time, Valid: true}
}

// nullPlaceType returns an optional link place type as the type of the place_type column.
func nullPlaceType(t *domain.LinkPlaceType) queries.NullLinksPlaceType {
	if t == nil {
		return queries.NullLinksPlaceType{}
	}

	return queries.NullLinksPlaceType{LinksPlaceType: queries.LinksPlaceType(*t), Valid: true}
}

// linkFields are the shared fields of a links row holding l.
func linkFields(l domain.DocLink) queries.GetLinkFieldsRow {
	return queries.GetLinkFieldsRow{Url: l.URL, Title: nullString(l.Title), Description: nullString(l.Description),
		Platform: nullString(l.Platform), PlaceType: nullPlaceType(l.PlaceType), CollectionID: nullableID(l.CollectionID),
		Locator: nullString(l.Locator)}
}

// docLinkOf is the link a links row's shared fields hold, without roles.
func docLinkOf(row queries.GetLinkFieldsRow) domain.DocLink {
	var placeType *domain.LinkPlaceType
	if row.PlaceType.Valid {
		t := domain.LinkPlaceType(row.PlaceType.LinksPlaceType)
		placeType = &t
	}

	return domain.DocLink{URL: row.Url, Title: stringPtr(row.Title), Description: stringPtr(row.Description),
		Platform: stringPtr(row.Platform), PlaceType: placeType, CollectionID: int64Ptr(row.CollectionID),
		Locator: stringPtr(row.Locator)}
}

// maxSlugTries is how many slugs from a new entity's name its first approval tries before it falls back to
// "<entity type>-<id>".
const maxSlugTries = 50

// allocateSlug gives the new entity id of type t named name its slug: the first of domain.EntitySlug's
// candidates that set stores without a duplicate-key error, up to maxSlugTries, and then "<entity type>-<id>".
func (w *entityWriter) allocateSlug(
	t domain.SlugEntityType, id int64, name string, set func(ctx context.Context, slug string, id uint64) error,
) {
	if w.err != nil {
		return
	}
	for n := 1; n <= maxSlugTries; n++ {
		err := set(w.ctx, domain.EntitySlug(name, t, id, n), w.id(id))
		if err == nil {
			return
		}
		if mysqlErrorNumber(err) != erDuplicateEntry {
			w.fail(&writeError{table: "slug", err: err})
			return
		}
	}
	w.exec("slug", func() error {
		return set(w.ctx, domain.EntitySlug("", t, id, 1), w.id(id))
	})
}

// conservationChange appends a piece's conservation history entry, made by its approved revision revisionID at at.
func (w *entityWriter) conservationChange(pieceID int64, c domain.ConservationChange, revisionID int64, at time.Time) {
	p := queries.InsertConservationHistoryParams{PieceID: w.id(pieceID),
		Status: queries.ConservationHistoryStatus(c.Status), Reason: c.Reason, SourceUrl: nullString(c.SourceURL),
		RevisionID: w.nullID(revisionID), At: at}
	w.exec("conservation_history", func() error { return w.q.InsertConservationHistory(w.ctx, p) })
}

// preferenceValue is the value_json of a preference: its value as JSON.
func (w *entityWriter) preferenceValue(pref domain.DocPreference) json.RawMessage {
	value, err := json.Marshal(pref.Value)
	if err != nil {
		w.fail(fmt.Errorf("encoding a preference value: %w", err))
	}

	return value
}
