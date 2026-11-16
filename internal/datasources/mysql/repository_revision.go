package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// errNotSlugAddressed is returned for a history request by slug for an entity type addressed otherwise.
var errNotSlugAddressed = errors.New("entity type is not addressed by slug")

// ListPublicHistory lists one page of the approved revisions of the public piece, person, term or event with
// slug, newest first, returning datasources.ErrNotFound if no public entity of entityType has the slug.
func (r *Repository) ListPublicHistory(
	ctx context.Context, entityType domain.SlugEntityType, slug string, page domain.Page,
) (domain.RevisionHistory, error) {
	var history domain.RevisionHistory
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		id, err := c.publicEntityID(ctx, entityType, slug)
		if err != nil {
			return err
		}
		history = c.history(ctx, queries.RevisionsEntityType(entityType.EntityType()), id, page)
		return c.err
	})
	if err != nil {
		return domain.RevisionHistory{}, fmt.Errorf("listing public history of %s [%s]: %w", entityType, slug, err)
	}

	return history, nil
}

// ListPublicCollectionHistory lists one page of the approved revisions of the public collection with id, newest
// first, returning datasources.ErrNotFound if no public collection has the ID.
func (r *Repository) ListPublicCollectionHistory(
	ctx context.Context, id int64, page domain.Page,
) (domain.RevisionHistory, error) {
	if id < 1 {
		return domain.RevisionHistory{}, fmt.Errorf("listing public history of collection [%d]: %w", id,
			datasources.ErrNotFound)
	}
	key := uint64(id)

	var history domain.RevisionHistory
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		found, err := c.q.GetPublicCollectionID(ctx, key)
		if err != nil {
			return lookupError("collection", err)
		}
		history = c.history(ctx, queries.RevisionsEntityTypeCollection, found, page)
		return c.err
	})
	if err != nil {
		return domain.RevisionHistory{}, fmt.Errorf("listing public history of collection [%d]: %w", id, err)
	}

	return history, nil
}

// lookupError returns datasources.ErrNotFound for no rows, and err described otherwise.
func lookupError(what string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return datasources.ErrNotFound
	}

	return fmt.Errorf("fetching %s: %w", what, err)
}

// publicEntityID returns the ID of the public entity of entityType with slug.
func (c *catalogueReader) publicEntityID(
	ctx context.Context, entityType domain.SlugEntityType, slug string,
) (uint64, error) {
	var lookup func(context.Context, string) (uint64, error)
	switch entityType {
	case domain.SlugEntityTypePiece:
		lookup = c.q.GetPublicPieceID
	case domain.SlugEntityTypePerson:
		lookup = c.q.GetPublicPersonID
	case domain.SlugEntityTypeTerm:
		lookup = c.q.GetPublicTermID
	case domain.SlugEntityTypeEvent:
		lookup = c.q.GetPublicEventID
	default:
		return 0, fmt.Errorf("%w [%s]", errNotSlugAddressed, entityType)
	}

	id, err := lookup(ctx, slug)
	if err != nil {
		return 0, lookupError(entityType.String(), err)
	}

	return id, nil
}

// history returns one page of the approved revisions of the entity of entityType with id, newest first.
func (c *catalogueReader) history(
	ctx context.Context, entityType queries.RevisionsEntityType, id uint64, page domain.Page,
) domain.RevisionHistory {
	rows := fetch(ctx, c, "revisions", c.q.ListApprovedRevisions, queries.ListApprovedRevisionsParams{
		EntityType: entityType, EntityID: sql.NullInt64{Int64: c.signedID(id), Valid: true}, Limit: page.Limit,
		Offset: page.Offset,
	})
	ids := make([]uint64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	contributors := c.contributors(ctx, ids)

	revisions := make([]domain.RevisionSummary, 0, len(rows))
	for _, row := range rows {
		revisions = append(revisions, c.revisionSummary(contributors, row.ID, row.Source, row.Summary, row.CreatedAt,
			row.AuthorDisplayName))
	}

	return domain.RevisionHistory{
		Revisions: revisions,
		Total: fetch(ctx, c, "revision count", c.q.CountApprovedRevisions,
			queries.CountApprovedRevisionsParams{EntityType: entityType,
				EntityID: sql.NullInt64{Int64: c.signedID(id), Valid: true}}),
	}
}

// contributors returns the contributors of each of the revisions, in order, by revision ID.
func (c *catalogueReader) contributors(ctx context.Context, revisionIDs []uint64) map[uint64][]string {
	contributors := make(map[uint64][]string)
	if len(revisionIDs) == 0 {
		return contributors
	}
	for _, row := range fetch(ctx, c, "contributors", c.q.ListRevisionContributors, revisionIDs) {
		contributors[row.RevisionID] = append(contributors[row.RevisionID], row.Name)
	}

	return contributors
}

func (c *catalogueReader) revisionSummary(
	contributors map[uint64][]string, id uint64, source queries.RevisionsSource, summary string,
	createdAt time.Time, author sql.NullString,
) domain.RevisionSummary {
	revision := domain.RevisionSummary{
		ID:           c.signedID(id),
		Source:       enumOf[domain.RevisionSource](c, source),
		Summary:      summary,
		Contributors: contributors[id],
		CreatedAt:    createdAt,
	}
	if author.Valid {
		revision.Author = &domain.RevisionAuthor{DisplayName: author.String}
	}

	return revision
}

// GetPublicRevision retrieves an approved revision of a public entity with its snapshot views, returning
// datasources.ErrNotFound if there is none with id. A snapshot that cannot be decoded fails the read.
func (r *Repository) GetPublicRevision(ctx context.Context, id int64) (domain.Revision, error) {
	var revision domain.Revision
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		row, err := getRevision(ctx, c.q, id)
		if err != nil {
			return err
		}
		if row.Status != queries.RevisionsStatusApproved || !c.entityIsPublic(ctx, row) {
			return datasources.ErrNotFound
		}
		_, revision, err = c.revisionView(ctx, row, false)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("getting public revision [%d]: %w", id, err)
	}

	return revision, nil
}

// getRevision returns the revision id, or datasources.ErrNotFound.
func getRevision(ctx context.Context, q *queries.Queries, id int64) (queries.GetRevisionRow, error) {
	if id < 1 {
		return queries.GetRevisionRow{}, datasources.ErrNotFound
	}
	row, err := q.GetRevision(ctx, uint64(id))
	if err != nil {
		return queries.GetRevisionRow{}, lookupError("revision", err)
	}

	return row, nil
}

// entityIsPublic reports whether a revision's entity is public. It is decided before the revision's snapshots
// are decoded, so a revision of an entity that is not public is not found whatever its snapshots hold.
func (c *catalogueReader) entityIsPublic(ctx context.Context, row queries.GetRevisionRow) bool {
	vis := c.visibility(ctx, entityReferences(row.EntityType, row.EntityID))
	id := row.EntityID.Int64
	_, piece := vis.Pieces[id]
	_, person := vis.Persons[id]
	_, term := vis.Terms[id]
	_, collection := vis.Collections[id]
	_, event := vis.Events[id]

	return piece || person || term || collection || event
}

// revisionView returns the views of a revision, as its author sees it or, if moderator, as a moderator does, and
// as the public sees it, which is nil if its entity is not public.
func (c *catalogueReader) revisionView(
	ctx context.Context, row queries.GetRevisionRow, moderator bool,
) (domain.RevisionView, domain.Revision, error) {
	entity := c.visibility(ctx, entityReferences(row.EntityType, row.EntityID))
	if c.err != nil {
		return nil, nil, c.err
	}

	switch row.EntityType {
	case queries.RevisionsEntityTypePiece:
		return views(entityView(ctx, c, row, entity.Pieces, pieceSnapshots, moderator))
	case queries.RevisionsEntityTypePerson:
		// A revision links to its own person's page, which a person who prefers no page has too.
		persons := map[int64]domain.PersonSummary{}
		if person, ok := entity.Persons[row.EntityID.Int64]; ok {
			person.NoLink = false
			persons[row.EntityID.Int64] = person
		}
		return views(entityView(ctx, c, row, persons, personSnapshots(row.EntityID.Int64), moderator))
	case queries.RevisionsEntityTypeTerm:
		return views(entityView(ctx, c, row, entity.Terms, termSnapshots, moderator))
	case queries.RevisionsEntityTypeCollection:
		collections := make(map[int64]domain.CollectionRef, len(entity.Collections))
		for id, collection := range entity.Collections {
			collections[id] = domain.CollectionRef{ID: collection.ID, Name: collection.Name}
		}
		return views(entityView(ctx, c, row, collections, collectionSnapshots, moderator))
	case queries.RevisionsEntityTypeEvent:
		return views(entityView(ctx, c, row, entity.Events, eventSnapshots, moderator))
	default:
		return nil, nil, fmt.Errorf("%w [%s] of revision entity type", domain.ErrUnknownEnumValue, row.EntityType)
	}
}

// views returns a revision's view and its public form, nil if its entity is not public.
func views[E, S any](
	v domain.EntityRevisionView[E, S], err error,
) (domain.RevisionView, domain.Revision, error) {
	if err != nil {
		return nil, nil, err
	}
	if v.Entity == nil {
		return v, nil, nil
	}

	return v, v.Public(), nil
}

// entityReferences returns the references of the entity of entityType with id alone, none for no entity.
func entityReferences(entityType queries.RevisionsEntityType, id sql.NullInt64) domain.SnapshotReferences {
	var refs domain.SnapshotReferences
	if !id.Valid {
		return refs
	}
	switch entityType {
	case queries.RevisionsEntityTypePiece:
		refs.PieceIDs = append(refs.PieceIDs, id.Int64)
	case queries.RevisionsEntityTypePerson:
		refs.PersonIDs = append(refs.PersonIDs, id.Int64)
	case queries.RevisionsEntityTypeTerm:
		refs.TermIDs = append(refs.TermIDs, id.Int64)
	case queries.RevisionsEntityTypeCollection:
		refs.CollectionIDs = append(refs.CollectionIDs, id.Int64)
	case queries.RevisionsEntityTypeEvent:
		refs.EventIDs = append(refs.EventIDs, id.Int64)
	}

	return refs
}

// snapshotKind is how the documents of one entity type are turned into views: what they reference, and the view
// of one given the visibility of what it references.
type snapshotKind[D domain.Document, S any] struct {
	addReferences func(D, *domain.SnapshotReferences)
	view          func(D, domain.SnapshotVisibility) (S, error)
}

var (
	pieceSnapshots = snapshotKind[domain.PieceDocument, domain.PieceSnapshot]{
		addReferences: domain.PieceDocument.AddReferences,
		view: func(d domain.PieceDocument, vis domain.SnapshotVisibility) (domain.PieceSnapshot, error) {
			return domain.NewPieceSnapshot(d, vis), nil
		},
	}
	termSnapshots = snapshotKind[domain.TermDocument, domain.TermSnapshot]{
		addReferences: domain.TermDocument.AddReferences,
		view: func(d domain.TermDocument, vis domain.SnapshotVisibility) (domain.TermSnapshot, error) {
			return domain.NewTermSnapshot(d, vis), nil
		},
	}
	collectionSnapshots = snapshotKind[domain.CollectionDocument, domain.CollectionSnapshot]{
		addReferences: domain.CollectionDocument.AddReferences,
		view: func(d domain.CollectionDocument, vis domain.SnapshotVisibility) (domain.CollectionSnapshot, error) {
			return domain.NewCollectionSnapshot(d, vis), nil
		},
	}
	eventSnapshots = snapshotKind[domain.EventDocument, domain.EventSnapshot]{
		addReferences: func(domain.EventDocument, *domain.SnapshotReferences) {},
		view: func(d domain.EventDocument, _ domain.SnapshotVisibility) (domain.EventSnapshot, error) {
			return domain.NewEventSnapshot(d), nil
		},
	}
)

// personSnapshots are the views of the documents of the person personID, or of a new person if it is 0, who is
// shown as a person with a page.
func personSnapshots(personID int64) snapshotKind[domain.PersonDocument, domain.PersonSnapshot] {
	return snapshotKind[domain.PersonDocument, domain.PersonSnapshot]{
		addReferences: func(d domain.PersonDocument, refs *domain.SnapshotReferences) {
			d.AddReferences(personID, refs)
		},
		view: func(d domain.PersonDocument, vis domain.SnapshotVisibility) (domain.PersonSnapshot, error) {
			if personID == 0 {
				vis.Persons[0] = domain.PersonSummary{Name: d.Name}
			}
			return domain.NewPersonSnapshot(personID, d, vis)
		},
	}
}

// entityView decodes a revision's snapshot, its base's and, for a moderator when the revision is stale, its
// entity's current revision's, loads the visibility of what they reference, and returns the revision with their
// views. entities are the public entities of the revision's type.
func entityView[D domain.Document, E, S any](
	ctx context.Context, c *catalogueReader, row queries.GetRevisionRow, entities map[int64]E,
	kind snapshotKind[D, S], moderator bool,
) (domain.EntityRevisionView[E, S], error) {
	var view domain.EntityRevisionView[E, S]
	if entity, ok := entities[row.EntityID.Int64]; ok && row.EntityID.Valid {
		view.Entity = &entity
	}
	docs, err := revisionDocuments[D](ctx, c, row, moderator)
	if err != nil {
		return view, err
	}
	var refs domain.SnapshotReferences
	for _, d := range docs {
		if d != nil {
			kind.addReferences(d.doc, &refs)
		}
	}
	vis := c.visibility(ctx, refs)
	if c.err != nil {
		return view, c.err
	}

	view.RevisionSummary = c.revisionSummary(c.contributors(ctx, []uint64{row.ID}), row.ID, row.Source,
		row.Summary, row.CreatedAt, row.AuthorDisplayName)
	view.Status = enumOf[domain.RevisionStatus](c, row.Status)
	view.ReviewNote = stringPtr(row.ReviewNote)
	view.ReviewedAt = timePtr(row.ReviewedAt)
	if view.Snapshot, err = kind.view(docs[0].doc, vis); err != nil {
		return view, fmt.Errorf("snapshot: %w", err)
	}
	if view.Base, err = documentView(docs[1], kind, vis); err != nil {
		return view, err
	}
	if view.Current, err = documentView(docs[2], kind, vis); err != nil {
		return view, err
	}
	if moderator {
		view.AuthorUserID = stringPtr(row.AuthorUserID)
		view.PossibleDuplicates, err = newEntityDuplicates(ctx, c, row, docs[0].doc)
	}

	return view, errors.Join(err, c.err)
}

// newEntityDuplicates returns the public entities a pending revision of a new entity may duplicate, and nil for any
// other revision.
func newEntityDuplicates[D domain.Document](
	ctx context.Context, c *catalogueReader, row queries.GetRevisionRow, doc D,
) ([]domain.DuplicateCandidate, error) {
	if row.EntityID.Valid || row.Status != queries.RevisionsStatusPending {
		return nil, nil
	}

	return c.possibleDuplicates(ctx, row.EntityType, document(doc))
}

// revisionDocuments returns a revision's document, its base's (nil for none) and, for a moderator when the
// revision is stale, its entity's current revision's (nil otherwise).
func revisionDocuments[D domain.Document](
	ctx context.Context, c *catalogueReader, row queries.GetRevisionRow, moderator bool,
) ([3]*revisionDocument[D], error) {
	var docs [3]*revisionDocument[D]
	var err error
	if docs[0], err = decodeRevision[D](c.signedID(row.ID), row.SnapshotJson); err != nil {
		return docs, err
	}
	if docs[1], err = baseDocument[D](ctx, c, row.ID); err != nil {
		return docs, err
	}
	if moderator {
		docs[2], err = staleCurrentDocument[D](ctx, c, row)
	}

	return docs, err
}

// revisionDocument is a revision's decoded snapshot.
type revisionDocument[D domain.Document] struct {
	id  int64
	doc D
}

// documentView returns the view of a revision's document, or nil for no revision.
func documentView[D domain.Document, S any](
	d *revisionDocument[D], kind snapshotKind[D, S], vis domain.SnapshotVisibility,
) (*domain.RevisionBase[S], error) {
	if d == nil {
		return nil, nil
	}
	view, err := kind.view(d.doc, vis)
	if err != nil {
		return nil, fmt.Errorf("snapshot [%d]: %w", d.id, err)
	}

	return &domain.RevisionBase[S]{ID: d.id, Snapshot: view}, nil
}

// baseDocument returns the base of the revision with id, or nil if it has no base that is an approved revision of
// the same entity.
func baseDocument[D domain.Document](ctx context.Context, c *catalogueReader, id uint64) (*revisionDocument[D], error) {
	row, err := c.q.GetApprovedBaseRevision(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetching base revision: %w", err)
	}

	return decodeRevision[D](c.signedID(row.ID), row.SnapshotJson)
}

// staleCurrentDocument returns the current revision of a revision's entity if the revision is stale, and nil
// otherwise.
func staleCurrentDocument[D domain.Document](
	ctx context.Context, c *catalogueReader, row queries.GetRevisionRow,
) (*revisionDocument[D], error) {
	current, err := currentRevisionID(ctx, c.q, row.EntityType, row.EntityID)
	if err != nil || !domain.IsStale(int64Ptr(row.BaseRevisionID), current) {
		return nil, err
	}
	currentRow, err := getRevision(ctx, c.q, *current)
	if err != nil {
		return nil, fmt.Errorf("current revision [%d]: %w", *current, err)
	}

	return decodeRevision[D](*current, currentRow.SnapshotJson)
}

// currentRevisionID returns the current revision of the entity entityID of entityType, nil for none.
func currentRevisionID(
	ctx context.Context, q *queries.Queries, entityType queries.RevisionsEntityType, entityID sql.NullInt64,
) (*int64, error) {
	if !entityID.Valid || entityID.Int64 < 1 {
		return nil, nil
	}
	current, err := q.GetCurrentRevisionID(ctx, queries.GetCurrentRevisionIDParams{EntityType: string(entityType),
		ID: uint64(entityID.Int64)})
	if err != nil {
		return nil, fmt.Errorf("fetching current revision: %w", err)
	}

	return int64Ptr(current), nil
}

func decodeRevision[D domain.Document](id int64, snapshot []byte) (*revisionDocument[D], error) {
	doc, err := domain.DecodeDocument[D](snapshot)
	if err != nil {
		return nil, fmt.Errorf("snapshot [%d]: %w", id, err)
	}

	return &revisionDocument[D]{id: id, doc: doc}, nil
}

// visibility loads the current state of what refs references, running a query only for each kind of reference
// there is.
func (c *catalogueReader) visibility(ctx context.Context, refs domain.SnapshotReferences) domain.SnapshotVisibility {
	vis := domain.SnapshotVisibility{
		Pieces:      make(map[int64]domain.PieceSummary),
		Persons:     make(map[int64]domain.PersonSummary),
		Terms:       make(map[int64]domain.TermSummary),
		Collections: make(map[int64]domain.CollectionSummary),
		Events:      make(map[int64]domain.EventSummary),
		HiddenLinks: make(map[string]bool),
	}

	if ids := unsignedIDs(refs.PieceIDs); len(ids) > 0 {
		c.pieceVisibility(ctx, ids, &vis)
	}
	if ids := unsignedIDs(refs.PersonIDs); len(ids) > 0 {
		c.personVisibility(ctx, ids, &vis)
	}
	if ids := unsignedIDs(refs.TermIDs); len(ids) > 0 {
		c.termVisibility(ctx, ids, &vis)
	}
	if ids := unsignedIDs(refs.CollectionIDs); len(ids) > 0 {
		c.collectionVisibility(ctx, ids, &vis)
	}
	if ids := unsignedIDs(refs.EventIDs); len(ids) > 0 {
		c.eventVisibility(ctx, ids, &vis)
	}
	if len(refs.LinkURLs) > 0 {
		c.linkVisibility(ctx, refs.LinkURLs, &vis)
	}

	return vis
}

// pieceVisibility loads the referenced public pieces.
func (c *catalogueReader) pieceVisibility(ctx context.Context, ids []uint64, vis *domain.SnapshotVisibility) {
	rows := fetch(ctx, c, "referenced pieces", c.q.ListPublicPiecesByID, ids)
	slugs := make([]string, 0, len(rows))
	for _, row := range rows {
		slugs = append(slugs, row.Slug)
	}
	creditNames := c.creditNames(ctx, slugs)
	for _, row := range rows {
		vis.Pieces[c.signedID(row.ID)] = c.pieceSummary(creditNames, row.Slug, row.Title, row.Kind, row.ConservationStatus,
			row.IsStub)
	}
}

// termVisibility loads the referenced public terms.
func (c *catalogueReader) termVisibility(ctx context.Context, ids []uint64, vis *domain.SnapshotVisibility) {
	for _, row := range fetch(ctx, c, "referenced terms", c.q.ListPublicTermsByID, ids) {
		vis.Terms[c.signedID(row.ID)] = c.termSummary(row.Slug, row.Facet, row.DisplayName)
	}
}

// collectionVisibility loads the referenced public collections.
func (c *catalogueReader) collectionVisibility(ctx context.Context, ids []uint64, vis *domain.SnapshotVisibility) {
	for _, row := range fetch(ctx, c, "referenced collections", c.q.ListPublicCollectionsByID, ids) {
		id := c.signedID(row.ID)
		vis.Collections[id] = c.collectionSummary(id, row.Name, row.Kind, row.Url)
	}
}

// eventVisibility loads the referenced public events.
func (c *catalogueReader) eventVisibility(ctx context.Context, ids []uint64, vis *domain.SnapshotVisibility) {
	for _, row := range fetch(ctx, c, "referenced events", c.q.ListPublicEventsByID, ids) {
		vis.Events[c.signedID(row.ID)] = domain.EventSummary{Slug: row.Slug, Name: row.Name}
	}
}

// linkVisibility loads which of the referenced URLs' normalized forms are hidden links'. A URL that does not
// normalize has no link, and is shown. Normalized URLs are ASCII, so none is refused by the ASCII column
// (MySQL error 3988).
func (c *catalogueReader) linkVisibility(ctx context.Context, urls []string, vis *domain.SnapshotVisibility) {
	normalized := make([]string, 0, len(urls))
	for _, url := range urls {
		if n, err := domain.NormalizeURL(url); err == nil {
			normalized = append(normalized, n)
		}
	}
	if len(normalized) == 0 {
		return
	}

	for _, url := range fetch(ctx, c, "hidden links", c.q.ListHiddenNormalizedURLs, normalized) {
		vis.HiddenLinks[url] = true
	}
}

// personVisibility loads the referenced public persons.
func (c *catalogueReader) personVisibility(ctx context.Context, ids []uint64, vis *domain.SnapshotVisibility) {
	for _, row := range fetch(ctx, c, "referenced persons", c.q.ListPublicPersonsByID, ids) {
		vis.Persons[c.signedID(row.ID)] = c.personSummary(row.Slug, row.Name, row.Kind, row.IsStub, row.Status)
	}
}

// unsignedIDs returns the positive IDs of ids as the type of the ID columns. A document's reference that is not
// positive references nothing, and is left out.
func unsignedIDs(ids []int64) []uint64 {
	unsigned := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			unsigned = append(unsigned, uint64(id))
		}
	}

	return unsigned
}
