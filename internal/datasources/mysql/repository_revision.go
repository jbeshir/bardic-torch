package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ datasources.PublicHistoryLister           = (*Repository)(nil)
	_ datasources.PublicCollectionHistoryLister = (*Repository)(nil)
	_ datasources.PublicRevisionGetter          = (*Repository)(nil)
)

// errNotSlugAddressed is returned for a history request by slug for an entity type addressed otherwise.
var errNotSlugAddressed = errors.New("entity type is not addressed by slug")

// ListPublicHistory lists one page of the approved revisions of the public piece, person, term or event with
// slug, newest first, returning datasources.ErrNotFound if no public entity of entityType has the slug.
func (r *Repository) ListPublicHistory(
	ctx context.Context, entityType domain.EntityType, slug string, page domain.Page,
) (domain.RevisionHistory, error) {
	var history domain.RevisionHistory
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		id, err := c.publicEntityID(ctx, entityType, slug)
		if err != nil {
			return err
		}
		history = c.history(ctx, queries.RevisionsEntityType(entityType), id, page)
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
	ctx context.Context, entityType domain.EntityType, slug string,
) (uint64, error) {
	var lookup func(context.Context, string) (uint64, error)
	switch entityType {
	case domain.EntityTypePiece:
		lookup = c.q.GetPublicPieceID
	case domain.EntityTypePerson:
		lookup = c.q.GetPublicPersonID
	case domain.EntityTypeTerm:
		lookup = c.q.GetPublicTermID
	case domain.EntityTypeEvent:
		lookup = c.q.GetPublicEventID
	case domain.EntityTypeCollection:
		return 0, fmt.Errorf("%w [%s]", errNotSlugAddressed, entityType)
	default:
		return 0, fmt.Errorf("%w [%s]", errNotSlugAddressed, entityType)
	}

	id, err := lookup(ctx, slug)
	if err != nil {
		return 0, lookupError(string(entityType), err)
	}

	return id, nil
}

// history returns one page of the approved revisions of the entity of entityType with id, newest first.
func (c *catalogueReader) history(
	ctx context.Context, entityType queries.RevisionsEntityType, id uint64, page domain.Page,
) domain.RevisionHistory {
	rows := fetch(ctx, c, "revisions", c.q.ListApprovedRevisions, queries.ListApprovedRevisionsParams{
		EntityType: entityType, EntityID: id, Limit: page.Limit, Offset: page.Offset,
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
			queries.CountApprovedRevisionsParams{EntityType: entityType, EntityID: id}),
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
	if id < 1 {
		return nil, fmt.Errorf("getting public revision [%d]: %w", id, datasources.ErrNotFound)
	}
	key := uint64(id)

	var revision domain.Revision
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		row, err := c.q.GetApprovedRevision(ctx, key)
		if err != nil {
			return lookupError("revision", err)
		}
		revision, err = c.revision(ctx, row)
		if err != nil {
			return err
		}
		return c.err
	})
	if err != nil {
		return nil, fmt.Errorf("getting public revision [%d]: %w", id, err)
	}

	return revision, nil
}

// revision returns the views of an approved revision, or datasources.ErrNotFound if its entity is not public.
// That is decided before its snapshots are decoded, so a revision of an entity that is not public is
// not found whatever its snapshots hold.
func (c *catalogueReader) revision(ctx context.Context, row queries.GetApprovedRevisionRow) (domain.Revision, error) {
	entityID := c.signedID(row.EntityID)
	entity := c.visibility(ctx, entityReferences(row.EntityType, entityID))
	if c.err != nil {
		return nil, c.err
	}

	switch row.EntityType {
	case queries.RevisionsEntityTypePiece:
		return entityRevision(ctx, c, row, entity.Pieces, pieceSnapshots)
	case queries.RevisionsEntityTypePerson:
		// A person who prefers no page is public: their revisions show their identity only.
		persons := maps.Clone(entity.Persons)
		maps.Copy(persons, entity.NoPagePersons)
		return entityRevision(ctx, c, row, persons, personSnapshots(entityID))
	case queries.RevisionsEntityTypeTerm:
		return entityRevision(ctx, c, row, entity.Terms, termSnapshots)
	case queries.RevisionsEntityTypeCollection:
		collections := make(map[int64]domain.CollectionRef, len(entity.Collections))
		for id, collection := range entity.Collections {
			collections[id] = domain.CollectionRef{ID: collection.ID, Name: collection.Name}
		}
		return entityRevision(ctx, c, row, collections, collectionSnapshots)
	case queries.RevisionsEntityTypeEvent:
		return entityRevision(ctx, c, row, entity.Events, eventSnapshots)
	default:
		return nil, fmt.Errorf("%w [%s] of revision entity type", errUnknownEnumValue, row.EntityType)
	}
}

// entityReferences returns the references of the entity of entityType with id alone.
func entityReferences(entityType queries.RevisionsEntityType, id int64) domain.SnapshotReferences {
	var refs domain.SnapshotReferences
	switch entityType {
	case queries.RevisionsEntityTypePiece:
		refs.PieceIDs = append(refs.PieceIDs, id)
	case queries.RevisionsEntityTypePerson:
		refs.PersonIDs = append(refs.PersonIDs, id)
	case queries.RevisionsEntityTypeTerm:
		refs.TermIDs = append(refs.TermIDs, id)
	case queries.RevisionsEntityTypeCollection:
		refs.CollectionIDs = append(refs.CollectionIDs, id)
	case queries.RevisionsEntityTypeEvent:
		refs.EventIDs = append(refs.EventIDs, id)
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

// personSnapshots are the views of the documents of the person personID.
func personSnapshots(personID int64) snapshotKind[domain.PersonDocument, domain.PersonSnapshot] {
	return snapshotKind[domain.PersonDocument, domain.PersonSnapshot]{
		addReferences: func(d domain.PersonDocument, refs *domain.SnapshotReferences) {
			d.AddReferences(personID, refs)
		},
		view: func(d domain.PersonDocument, vis domain.SnapshotVisibility) (domain.PersonSnapshot, error) {
			return domain.NewPersonSnapshot(personID, d, vis)
		},
	}
}

// entityRevision decodes a revision's snapshot and base snapshot, loads the visibility of what they reference,
// and returns the revision with their views. entities are the public entities of the revision's type, which it
// returns datasources.ErrNotFound unless the revision's entity is among.
func entityRevision[D domain.Document, E, S any](
	ctx context.Context, c *catalogueReader, row queries.GetApprovedRevisionRow, entities map[int64]E,
	kind snapshotKind[D, S],
) (domain.Revision, error) {
	entity, ok := entities[c.signedID(row.EntityID)]
	if !ok {
		return nil, datasources.ErrNotFound
	}

	var refs domain.SnapshotReferences
	snapshot, err := domain.DecodeDocument[D](row.SnapshotJson)
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	kind.addReferences(snapshot, &refs)

	baseID, base, err := baseDocument[D](ctx, c, row.ID)
	if err != nil {
		return nil, err
	}
	if base != nil {
		kind.addReferences(*base, &refs)
	}

	vis := c.visibility(ctx, refs)
	if c.err != nil {
		return nil, c.err
	}

	revision := domain.EntityRevision[E, S]{
		RevisionSummary: c.revisionSummary(c.contributors(ctx, []uint64{row.ID}), row.ID, row.Source,
			row.Summary, row.CreatedAt, row.AuthorDisplayName),
		Entity: entity,
	}
	if revision.Snapshot, err = kind.view(snapshot, vis); err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	if base != nil {
		view, err := kind.view(*base, vis)
		if err != nil {
			return nil, fmt.Errorf("base snapshot [%d]: %w", baseID, err)
		}
		revision.BaseRevisionID, revision.BaseSnapshot = &baseID, &view
	}

	return revision, c.err
}

// baseDocument returns the ID and decoded snapshot of the base of the revision with id, or a nil document if it has
// no base that is an approved revision of the same entity.
func baseDocument[D domain.Document](ctx context.Context, c *catalogueReader, id uint64) (int64, *D, error) {
	row, err := c.q.GetApprovedBaseRevision(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, fmt.Errorf("fetching base revision: %w", err)
	}

	baseID := c.signedID(row.ID)
	doc, err := domain.DecodeDocument[D](row.SnapshotJson)
	if err != nil {
		return 0, nil, fmt.Errorf("base snapshot [%d]: %w", baseID, err)
	}

	return baseID, &doc, nil
}

// visibility loads the current state of what refs references, running a query only for each kind of reference
// there is.
func (c *catalogueReader) visibility(ctx context.Context, refs domain.SnapshotReferences) domain.SnapshotVisibility {
	vis := domain.SnapshotVisibility{
		Pieces:         make(map[int64]domain.PieceSummary),
		Persons:        make(map[int64]domain.PersonSummary),
		NoPagePersons:  make(map[int64]domain.PersonSummary),
		PublicAliases:  make(map[int64]map[string]bool),
		Terms:          make(map[int64]domain.TermSummary),
		Collections:    make(map[int64]domain.CollectionSummary),
		Events:         make(map[int64]domain.EventSummary),
		HiddenLinkURLs: make(map[string]bool),
	}

	if ids := unsignedIDs(refs.PieceIDs); len(ids) > 0 {
		c.pieceVisibility(ctx, ids, &vis)
	}
	if ids := unsignedIDs(refs.PersonIDs); len(ids) > 0 {
		c.personVisibility(ctx, ids, &vis)
	}
	if ids := unsignedIDs(refs.AliasPersonIDs); len(ids) > 0 {
		c.aliasVisibility(ctx, ids, &vis)
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
	kinds := c.secondaryKinds(ctx, slugs)
	for _, row := range rows {
		vis.Pieces[c.signedID(row.ID)] = c.pieceSummary(kinds, row.Slug, row.Title, row.Kind, row.ConservationStatus,
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
		vis.Collections[id] = domain.CollectionSummary{
			ID: id, Name: row.Name, Kind: enumOf[domain.CollectionKind](c, row.Kind), URL: stringPtr(row.Url),
			IsBardicResource: row.IsBardicResource,
		}
	}
}

// eventVisibility loads the referenced public events.
func (c *catalogueReader) eventVisibility(ctx context.Context, ids []uint64, vis *domain.SnapshotVisibility) {
	for _, row := range fetch(ctx, c, "referenced events", c.q.ListPublicEventsByID, ids) {
		vis.Events[c.signedID(row.ID)] = domain.EventSummary{
			Slug: row.Slug, Name: row.Name, Kind: enumOf[domain.EventKind](c, row.Kind),
		}
	}
}

// linkVisibility loads which of the referenced URLs are hidden links': a link is matched on its URL as entered
// or its normalized URL, so a snapshot that wrote the URL in either form is redacted.
func (c *catalogueReader) linkVisibility(ctx context.Context, urls []string, vis *domain.SnapshotVisibility) {
	for _, row := range fetch(ctx, c, "hidden links", c.q.ListHiddenLinkURLs,
		queries.ListHiddenLinkURLsParams{Urls: urls, NormalizedUrls: urls}) {
		vis.HiddenLinkURLs[row.Url] = true
		vis.HiddenLinkURLs[row.NormalizedUrl] = true
	}
}

// personVisibility loads the referenced public persons, with and without a page.
func (c *catalogueReader) personVisibility(ctx context.Context, ids []uint64, vis *domain.SnapshotVisibility) {
	for _, row := range fetch(ctx, c, "referenced persons", c.q.ListPublicPersonsByID, ids) {
		id := c.signedID(row.ID)
		status := enumOf[domain.PersonStatus](c, row.Status)
		summary := domain.PersonSummary{
			Slug: row.Slug, Name: row.Name, Kind: enumOf[domain.PersonKind](c, row.Kind),
			IsStub: domain.PersonIsStub(row.IsStub, status),
		}
		if status == domain.PersonStatusPrefersNoPage {
			vis.NoPagePersons[id] = summary
		} else {
			vis.Persons[id] = summary
		}
	}
}

// aliasVisibility loads the public alias names of the persons with ids.
func (c *catalogueReader) aliasVisibility(ctx context.Context, ids []uint64, vis *domain.SnapshotVisibility) {
	for _, row := range fetch(ctx, c, "public alias names", c.q.ListPublicAliasNames, ids) {
		id := c.signedID(row.PersonID)
		if vis.PublicAliases[id] == nil {
			vis.PublicAliases[id] = make(map[string]bool)
		}
		vis.PublicAliases[id][row.Name] = true
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
