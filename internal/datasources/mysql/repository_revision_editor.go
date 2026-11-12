package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// slugEntityTypes are the entity types addressed by slug.
var slugEntityTypes = map[domain.EntityType]domain.SlugEntityType{
	domain.EntityTypePiece: domain.SlugEntityTypePiece, domain.EntityTypePerson: domain.SlugEntityTypePerson,
	domain.EntityTypeTerm: domain.SlugEntityTypeTerm, domain.EntityTypeEvent: domain.SlugEntityTypeEvent,
}

// GetEditable returns the editable document of a public entity: the caller's own pending revision of it, or its
// current revision, in the editor format without hidden links.
func (r *Repository) GetEditable(
	ctx context.Context, entityType domain.EntityType, key, userID string,
) (datasources.Editable, error) {
	var editable datasources.Editable
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		id, err := c.editableEntityID(ctx, entityType, key)
		if err != nil {
			return err
		}
		t := queries.RevisionsEntityType(entityType)
		var snapshot []byte
		pending, err := c.q.GetOwnPendingRevision(ctx, queries.GetOwnPendingRevisionParams{EntityType: t,
			EntityID: someID(id), AuthorUserID: sql.NullString{String: userID, Valid: true}})
		switch {
		case err == nil:
			pendingID := c.signedID(pending.ID)
			editable.PendingRevisionID = &pendingID
			editable.BaseRevisionID = pending.BaseRevisionID.Int64
			snapshot = pending.SnapshotJson
		case errors.Is(err, sql.ErrNoRows):
			current, err := currentRevisionID(ctx, c.q, t, someID(id))
			if err != nil {
				return err
			}
			row, err := getRevision(ctx, c.q, *current)
			if err != nil {
				return err
			}
			editable.BaseRevisionID = *current
			snapshot = row.SnapshotJson
		default:
			return fmt.Errorf("fetching pending revision: %w", err)
		}
		doc, err := decodeDocument(t, snapshot)
		if err != nil {
			return err
		}
		editable.Document = editorDocument(doc, c.visibility(ctx, documentReferences(doc, id)))
		return c.err
	})
	if err != nil {
		return datasources.Editable{}, fmt.Errorf("getting editable %s [%s]: %w", entityType, key, err)
	}

	return editable, nil
}

// editableEntityID returns the ID of the public entity of entityType with key, a slug or a collection's ID.
func (c *catalogueReader) editableEntityID(
	ctx context.Context, entityType domain.EntityType, key string,
) (int64, error) {
	if t, ok := slugEntityTypes[entityType]; ok {
		id, err := c.publicEntityID(ctx, t, key)
		return c.signedID(id), err
	}
	id, err := strconv.ParseInt(key, 10, 64)
	if entityType != domain.EntityTypeCollection || err != nil || id < 1 {
		return 0, datasources.ErrNotFound
	}
	found, err := c.q.GetPublicCollectionID(ctx, uint64(id))
	if err != nil {
		return 0, lookupError("collection", err)
	}

	return c.signedID(found), nil
}

// GetRevisionBase returns an approved revision of an entity of entityType, as a submission's base.
func (r *Repository) GetRevisionBase(
	ctx context.Context, entityType domain.EntityType, baseRevisionID int64,
) (datasources.RevisionBase, error) {
	var base datasources.RevisionBase
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		row, err := getRevision(ctx, c.q, baseRevisionID)
		if err != nil {
			return err
		}
		if row.Status != queries.RevisionsStatusApproved || row.EntityType != queries.RevisionsEntityType(entityType) ||
			!row.EntityID.Valid {
			return datasources.ErrNotFound
		}
		doc, err := decodeDocument(row.EntityType, row.SnapshotJson)
		if err != nil {
			return err
		}
		base = datasources.RevisionBase{EntityID: row.EntityID.Int64, Document: doc}
		return nil
	})
	if err != nil {
		return datasources.RevisionBase{}, fmt.Errorf("getting base revision [%d]: %w", baseRevisionID, err)
	}

	return base, nil
}

// documentReferences returns what a document of the entity entityID references.
func documentReferences(doc document, entityID int64) domain.SnapshotReferences {
	var refs domain.SnapshotReferences
	switch d := doc.(type) {
	case domain.PieceDocument:
		d.AddReferences(&refs)
	case domain.PersonDocument:
		d.AddReferences(entityID, &refs)
	case domain.TermDocument:
		d.AddReferences(&refs)
	case domain.CollectionDocument:
		d.AddReferences(&refs)
	}

	return refs
}

// editorDocument returns a document in the editor format, its references by the slugs vis holds and without the
// links vis holds hidden.
func editorDocument(doc document, vis domain.SnapshotVisibility) any {
	s := domain.Slugs{Pieces: map[int64]string{}, Persons: map[int64]string{}, Terms: map[int64]string{},
		Events: map[int64]string{}}
	for id, p := range vis.Pieces {
		s.Pieces[id] = p.Slug
	}
	for id, p := range vis.Persons {
		s.Persons[id] = p.Slug
	}
	for id, t := range vis.Terms {
		s.Terms[id] = t.Slug
	}
	for id, e := range vis.Events {
		s.Events[id] = e.Slug
	}
	hidden := func(url string) bool {
		normalized, err := domain.NormalizeURL(url)
		return err == nil && vis.HiddenLinks[normalized]
	}

	switch d := doc.(type) {
	case domain.PieceDocument:
		d.Links = slices.DeleteFunc(d.Links, func(l domain.DocPieceLink) bool { return hidden(l.URL) })
		return d.ToEditor(s)
	case domain.PersonDocument:
		d.Links = slices.DeleteFunc(d.Links, func(l domain.DocPersonLink) bool { return hidden(l.URL) })
		return d.ToEditor(s)
	case domain.TermDocument:
		return d.ToEditor(s)
	case domain.CollectionDocument:
		return d.ToEditor(s)
	case domain.EventDocument:
		return d.ToEditor(s)
	}

	return nil
}

// ResolveReferences returns the IDs of the public entities with the slugs refs holds.
func (r *Repository) ResolveReferences(
	ctx context.Context, refs domain.SlugReferences,
) (domain.IDs, error) {
	ids := domain.IDs{Pieces: map[string]int64{}, Persons: map[string]int64{}, Terms: map[string]int64{},
		Events: map[string]int64{}}
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		if len(refs.Pieces) > 0 {
			for _, row := range fetch(ctx, c, "pieces", c.q.ListPublicPieceIDsBySlug, refs.Pieces) {
				ids.Pieces[row.Slug] = c.signedID(row.ID)
			}
		}
		if len(refs.Persons) > 0 {
			for _, row := range fetch(ctx, c, "persons", c.q.ListPublicPersonIDsBySlug, refs.Persons) {
				ids.Persons[row.Slug] = c.signedID(row.ID)
			}
		}
		if len(refs.Terms) > 0 {
			for _, row := range fetch(ctx, c, "terms", c.q.ListPublicTermIDsBySlug, refs.Terms) {
				ids.Terms[row.Slug] = c.signedID(row.ID)
			}
		}
		if len(refs.Events) > 0 {
			for _, row := range fetch(ctx, c, "events", c.q.ListPublicEventIDsBySlug, refs.Events) {
				ids.Events[row.Slug] = c.signedID(row.ID)
			}
		}
		return c.err
	})
	if err != nil {
		return domain.IDs{}, fmt.Errorf("resolving references: %w", err)
	}

	return ids, nil
}

// ValidationContext returns every term and every collection's parent, for validating a document of the entity
// entityID.
func (r *Repository) ValidationContext(ctx context.Context, entityID int64) (domain.ValidationContext, error) {
	var vctx domain.ValidationContext
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		var err error
		vctx, err = validationContext(ctx, c.q, entityID)
		return err
	})

	return vctx, err
}

func validationContext(ctx context.Context, q *queries.Queries, entityID int64) (domain.ValidationContext, error) {
	terms, err := q.ListTerms(ctx)
	if err != nil {
		return domain.ValidationContext{}, fmt.Errorf("listing terms: %w", err)
	}
	collections, err := q.ListCollectionParents(ctx)
	if err != nil {
		return domain.ValidationContext{}, fmt.Errorf("listing collections: %w", err)
	}
	vctx := domain.ValidationContext{ID: entityID, Terms: make(map[int64]domain.TermInfo, len(terms)),
		CollectionParents: make(map[int64]*int64, len(collections))}
	for _, t := range terms {
		vctx.Terms[t.ID] = domain.TermInfo{Facet: domain.Facet(t.Facet), ParentID: int64Ptr(t.ParentID),
			Name: t.Name}
	}
	for _, c := range collections {
		vctx.CollectionParents[c.ID] = int64Ptr(c.ParentID)
	}

	return vctx, nil
}

// DuplicateCandidates returns the public entities of entityType (terms of facet) with the duplicate keys of their
// current documents.
func (r *Repository) DuplicateCandidates(
	ctx context.Context, entityType domain.EntityType, facet domain.Facet,
) ([]domain.DuplicateCandidate, error) {
	var candidates []domain.DuplicateCandidate
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		var err error
		candidates, err = c.duplicateCandidates(ctx, queries.RevisionsEntityType(entityType), facet)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("listing duplicate candidates: %w", err)
	}

	return candidates, nil
}

func (c *catalogueReader) duplicateCandidates(
	ctx context.Context, entityType queries.RevisionsEntityType, facet domain.Facet,
) ([]domain.DuplicateCandidate, error) {
	rows := fetch(ctx, c, "current documents", c.q.ListCurrentDocuments, entityType)
	var refs domain.SnapshotReferences
	for _, row := range rows {
		r := entityReferences(entityType, row.EntityID)
		refs.PieceIDs = append(refs.PieceIDs, r.PieceIDs...)
		refs.PersonIDs = append(refs.PersonIDs, r.PersonIDs...)
		refs.TermIDs = append(refs.TermIDs, r.TermIDs...)
		refs.CollectionIDs = append(refs.CollectionIDs, r.CollectionIDs...)
		refs.EventIDs = append(refs.EventIDs, r.EventIDs...)
	}
	vis := c.visibility(ctx, refs)

	var candidates []domain.DuplicateCandidate
	for _, row := range rows {
		doc, err := decodeDocument(entityType, row.SnapshotJson)
		if err != nil {
			return nil, err
		}
		candidate, ok := duplicateCandidate(domain.EntityType(entityType), doc, row.EntityID.Int64, vis, facet)
		if ok {
			candidates = append(candidates, candidate)
		}
	}

	return candidates, c.err
}

// duplicateCandidate returns the current document of the entity id of entityType as a candidate, if the entity
// is public (and a term of facet).
func duplicateCandidate(
	entityType domain.EntityType, doc document, id int64, vis domain.SnapshotVisibility, facet domain.Facet,
) (domain.DuplicateCandidate, bool) {
	var slug, name string
	var public, noPage bool
	switch d := doc.(type) {
	case domain.PieceDocument:
		name, slug = d.Title, vis.Pieces[id].Slug
	case domain.PersonDocument:
		name, slug, noPage = d.Name, vis.Persons[id].Slug, d.Status == domain.PersonStatusPrefersNoPage
	case domain.TermDocument:
		if d.Facet == facet {
			name, slug = d.Name, vis.Terms[id].Slug
		}
	case domain.CollectionDocument:
		name, public = d.Name, vis.Collections[id].ID != 0
	case domain.EventDocument:
		name, slug = d.Name, vis.Events[id].Slug
	}
	candidate := domain.DuplicateCandidate{EntityType: entityType, Name: name,
		Keys: doc.DuplicateKeys(), PrefersNoPage: noPage}
	if slug != "" {
		candidate.Slug = &slug
	} else {
		candidate.CollectionID = &id
	}

	return candidate, public || slug != ""
}

// possibleDuplicates returns the public entities a new entity's document may duplicate.
func (c *catalogueReader) possibleDuplicates(
	ctx context.Context, entityType queries.RevisionsEntityType, doc document,
) ([]domain.DuplicateCandidate, error) {
	var facet domain.Facet
	if term, ok := doc.(domain.TermDocument); ok {
		facet = term.Facet
	}
	candidates, err := c.duplicateCandidates(ctx, entityType, facet)
	if err != nil {
		return nil, err
	}

	return domain.MatchDuplicates(doc.DuplicateKeys(), candidates), nil
}

// ListMyRevisions lists one page of a user's revisions, of a status if one is given, newest first.
func (r *Repository) ListMyRevisions(
	ctx context.Context, userID string, status *domain.RevisionStatus, page domain.Page,
) (domain.RevisionList, error) {
	author := sql.NullString{String: userID, Valid: true}
	var filter queries.NullRevisionsStatus
	if status != nil {
		filter = queries.NullRevisionsStatus{RevisionsStatus: queries.RevisionsStatus(*status), Valid: true}
	}
	var list domain.RevisionList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		rows := fetch(ctx, c, "revisions", c.q.ListAuthorRevisions, queries.ListAuthorRevisionsParams{
			AuthorUserID: author, Status: filter, Limit: page.Limit, Offset: page.Offset})
		for _, row := range rows {
			item := c.listItem(row.ID, row.EntityType, row.EntityID, row.BaseRevisionID, row.CurrentRevisionID,
				row.EntitySlug, row.EntityName)
			item.Status = enumOf[domain.RevisionStatus](c, row.Status)
			item.Summary, item.CreatedAt = row.Summary, row.CreatedAt
			item.ReviewNote, item.ReviewedAt = stringPtr(row.ReviewNote), timePtr(row.ReviewedAt)
			list.Revisions = append(list.Revisions, item)
		}
		list.Total = fetch(ctx, c, "revision count", c.q.CountAuthorRevisions,
			queries.CountAuthorRevisionsParams{AuthorUserID: author, Status: filter})
		return c.err
	})
	if err != nil {
		return domain.RevisionList{}, fmt.Errorf("listing revisions of [%s]: %w", userID, err)
	}

	return list, nil
}

// ListModerationQueue lists one page of the pending revisions, oldest first.
func (r *Repository) ListModerationQueue(ctx context.Context, page domain.Page) (domain.RevisionList, error) {
	var list domain.RevisionList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		rows := fetch(ctx, c, "pending revisions", c.q.ListPendingRevisions,
			queries.ListPendingRevisionsParams{Limit: page.Limit, Offset: page.Offset})
		for _, row := range rows {
			item := c.listItem(row.ID, row.EntityType, row.EntityID, row.BaseRevisionID, row.CurrentRevisionID,
				row.EntitySlug, row.EntityName)
			item.Status, item.Summary, item.CreatedAt = domain.RevisionStatusPending, row.Summary, row.CreatedAt
			item.Author = &domain.RevisionAuthorAccount{UserID: row.AuthorUserID.String,
				DisplayName: row.AuthorDisplayName}
			list.Revisions = append(list.Revisions, item)
		}
		if c.err != nil {
			return c.err
		}
		var err error
		if list.Total, err = c.q.CountPendingRevisions(ctx); err != nil {
			return fmt.Errorf("counting pending revisions: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.RevisionList{}, fmt.Errorf("listing the moderation queue: %w", err)
	}

	return list, nil
}

// listItem returns a listed revision with its entity, nil for a new entity's revision.
func (c *catalogueReader) listItem(
	id uint64, entityType queries.RevisionsEntityType, entityID, base, current sql.NullInt64, slug, name string,
) domain.RevisionListItem {
	item := domain.RevisionListItem{ID: c.signedID(id), EntityType: enumOf[domain.EntityType](c, entityType),
		IsStale: domain.IsStale(int64Ptr(base), int64Ptr(current))}
	if entityID.Valid {
		item.Entity = &domain.RevisionEntity{Name: name}
		if entityType == queries.RevisionsEntityTypeCollection {
			item.Entity.CollectionID = &entityID.Int64
		} else {
			item.Entity.Slug = &slug
		}
	}

	return item
}

// GetMyRevision returns a user's revision as its author sees it.
func (r *Repository) GetMyRevision(ctx context.Context, userID string, id int64) (domain.RevisionView, error) {
	var view domain.RevisionView
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		row, err := getRevision(ctx, c.q, id)
		if err != nil {
			return err
		}
		if !row.AuthorUserID.Valid || row.AuthorUserID.String != userID {
			return datasources.ErrNotFound
		}
		view, _, err = c.revisionView(ctx, row, false)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("getting revision [%d] of [%s]: %w", id, userID, err)
	}

	return view, nil
}

// GetModerationRevision returns a revision as a moderator sees it.
func (r *Repository) GetModerationRevision(ctx context.Context, id int64) (domain.RevisionView, error) {
	var view domain.RevisionView
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		row, err := getRevision(ctx, c.q, id)
		if err != nil {
			return err
		}
		view, _, err = c.revisionView(ctx, row, true)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("getting revision [%d] for moderation: %w", id, err)
	}

	return view, nil
}
