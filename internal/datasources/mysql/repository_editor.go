package mysql

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ datasources.EditorRefResolver = (*Repository)(nil)

// pendingNewEntity is a pending new entity, its placeholder, with its author and the document of the revision
// creating it. Slug is the placeholder's (empty for a collection).
type pendingNewEntity struct {
	entity   domain.EntityRef
	authorID string
	slug     string
	doc      domain.AnyDocument
}

// pendingNewEntities returns the pending new entities created by userID's revisions, or with anyAuthor by anyone's.
func (c *catalogueReader) pendingNewEntities(ctx context.Context, userID string, anyAuthor bool) []pendingNewEntity {
	rows := fetch(ctx, c, "pending new entities", c.q.ListPendingNewEntities, queries.ListPendingNewEntitiesParams{
		AuthorUserID: nullString(&userID), AnyAuthor: anyAuthor,
	})
	entities := make([]pendingNewEntity, 0, len(rows))
	for _, row := range rows {
		entityType := enumOf[domain.EntityType](c, row.EntityType)
		kind, err := kindFor(entityType)
		if err != nil {
			c.fail(err)
			return nil
		}
		doc, err := kind.decode(row.SnapshotJson)
		if err != nil {
			c.fail(fmt.Errorf("pending revision [%d] snapshot: %w", row.ID, err))
			return nil
		}
		slug := row.PieceSlug
		for _, s := range []sql.NullString{row.PersonSlug, row.TermSlug, row.EventSlug} {
			if s.Valid {
				slug = s
			}
		}
		entities = append(entities, pendingNewEntity{
			entity:   domain.EntityRef{Type: entityType, ID: row.EntityID.Int64},
			authorID: row.AuthorUserID.String, slug: slug.String, doc: doc,
		})
	}

	return entities
}

// ownPendingRefs are the pending new terms and persons among entities, as their author sees them (D5): under
// their placeholder slugs, with the names their pending documents give them.
func ownPendingRefs(entities []pendingNewEntity) domain.OwnPendingRefs {
	own := domain.OwnPendingRefs{Terms: map[int64]domain.TermSummary{}, Persons: map[int64]domain.PersonSummary{}}
	for _, e := range entities {
		switch d := e.doc.(type) {
		case domain.TermDocument:
			own.Terms[e.entity.ID] = domain.TermSummary{Slug: e.slug, Facet: d.Facet, DisplayName: d.DisplayName}
		case domain.PersonDocument:
			own.Persons[e.entity.ID] = domain.PersonSummary{Slug: e.slug, Name: d.Name, Kind: d.Kind,
				IsStub: domain.PersonIsStub(d.IsStub, d.Status)}
		}
	}

	return own
}

// ownPendingFor returns the pending new entities of entities authored by the actor.
func ownPendingFor(actor domain.Actor, entities []pendingNewEntity) []pendingNewEntity {
	return slices.DeleteFunc(slices.Clone(entities), func(e pendingNewEntity) bool { return e.authorID != actor.UserID })
}

// referencedPending returns the pending new entities an actor may reference and see: their own pending new terms
// and persons, or for an interactive moderator every pending new entity.
func (c *catalogueReader) referencedPending(ctx context.Context, actor domain.Actor) []pendingNewEntity {
	moderator := actor.Role == domain.RoleModerator
	entities := c.pendingNewEntities(ctx, actor.UserID, moderator)
	if moderator {
		return entities
	}

	return slices.DeleteFunc(entities, func(e pendingNewEntity) bool {
		return e.entity.Type != domain.EntityTypeTerm && e.entity.Type != domain.EntityTypePerson
	})
}

// ResolveEditorRefs returns the IDs of the references in refs the actor may make: public entities, the actor's own
// pending new terms and persons by placeholder slug, and for an interactive moderator every entity.
func (r *Repository) ResolveEditorRefs(
	ctx context.Context, actor domain.Actor, refs domain.EditorRefs,
) (domain.RefMap, error) {
	var resolved domain.RefMap
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		resolved = c.resolveEditorRefs(ctx, actor, refs)
		return c.err
	})
	if err != nil {
		return domain.RefMap{}, fmt.Errorf("resolving editor references: %w", err)
	}

	return resolved, nil
}

func (c *catalogueReader) resolveEditorRefs(
	ctx context.Context, actor domain.Actor, refs domain.EditorRefs,
) domain.RefMap {
	moderator := actor.Role == domain.RoleModerator
	resolved := domain.RefMap{Pieces: map[string]int64{}, Persons: map[string]int64{}, Terms: map[string]int64{},
		Events: map[string]int64{}, Collections: map[int64]bool{}}
	bySlug := func(slugs []string, query func(context.Context, []string, bool) []slugRow, into map[string]int64) {
		if len(slugs) == 0 {
			return
		}
		for _, row := range query(ctx, slices.Compact(slices.Sorted(slices.Values(slugs))), moderator) {
			into[row.slug] = c.signedID(row.id)
		}
	}
	bySlug(refs.PieceSlugs, c.editorPiecesBySlug, resolved.Pieces)
	bySlug(refs.PersonSlugs, c.editorPersonsBySlug, resolved.Persons)
	bySlug(refs.TermSlugs, c.editorTermsBySlug, resolved.Terms)
	bySlug(refs.EventSlugs, c.editorEventsBySlug, resolved.Events)
	if ids := unsignedIDs(refs.CollectionIDs); len(ids) > 0 {
		for _, row := range fetch(ctx, c, "editor collections", c.q.ListEditorCollectionsByID,
			queries.ListEditorCollectionsByIDParams{Ids: ids, IncludeUnapproved: moderator}) {
			resolved.Collections[c.signedID(row.ID)] = true
		}
	}
	if !moderator {
		// A contributor's own pending new terms and persons, by placeholder slug (D5).
		slugs := map[domain.EntityType][]string{domain.EntityTypeTerm: refs.TermSlugs,
			domain.EntityTypePerson: refs.PersonSlugs}
		into := map[domain.EntityType]map[string]int64{domain.EntityTypeTerm: resolved.Terms,
			domain.EntityTypePerson: resolved.Persons}
		for _, e := range c.referencedPending(ctx, actor) {
			if slices.Contains(slugs[e.entity.Type], e.slug) {
				into[e.entity.Type][e.slug] = e.entity.ID
			}
		}
	}

	return resolved
}

// slugRow is an entity's ID and slug.
type slugRow struct {
	id   uint64
	slug string
}

func (c *catalogueReader) editorPiecesBySlug(ctx context.Context, slugs []string, all bool) []slugRow {
	rows := fetch(ctx, c, "editor pieces", c.q.ListEditorPiecesBySlug,
		queries.ListEditorPiecesBySlugParams{Slugs: slugs, IncludeUnapproved: all})
	return mapRows(rows, func(r queries.ListEditorPiecesBySlugRow) slugRow { return slugRow{r.ID, r.Slug} })
}

func (c *catalogueReader) editorPersonsBySlug(ctx context.Context, slugs []string, all bool) []slugRow {
	rows := fetch(ctx, c, "editor persons", c.q.ListEditorPersonsBySlug,
		queries.ListEditorPersonsBySlugParams{Slugs: slugs, IncludeUnapproved: all})
	return mapRows(rows, func(r queries.ListEditorPersonsBySlugRow) slugRow { return slugRow{r.ID, r.Slug} })
}

func (c *catalogueReader) editorTermsBySlug(ctx context.Context, slugs []string, all bool) []slugRow {
	rows := fetch(ctx, c, "editor terms", c.q.ListEditorTermsBySlug,
		queries.ListEditorTermsBySlugParams{Slugs: slugs, IncludeUnapproved: all})
	return mapRows(rows, func(r queries.ListEditorTermsBySlugRow) slugRow { return slugRow{r.ID, r.Slug} })
}

func (c *catalogueReader) editorEventsBySlug(ctx context.Context, slugs []string, all bool) []slugRow {
	rows := fetch(ctx, c, "editor events", c.q.ListEditorEventsBySlug,
		queries.ListEditorEventsBySlugParams{Slugs: slugs, IncludeUnapproved: all})
	return mapRows(rows, func(r queries.ListEditorEventsBySlugRow) slugRow { return slugRow{r.ID, r.Slug} })
}

// mapRows returns f of each row.
func mapRows[R, T any](rows []R, f func(R) T) []T {
	out := make([]T, 0, len(rows))
	for _, row := range rows {
		out = append(out, f(row))
	}

	return out
}

// EditorReferences returns the entities refs references that the actor may see, with their slugs and labels.
func (r *Repository) EditorReferences(
	ctx context.Context, actor domain.Actor, refs domain.SnapshotReferences,
) ([]domain.EditorReference, error) {
	var labelled []domain.EditorReference
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		labelled = c.editorReferences(ctx, actor, refs, c.referencedPending(ctx, actor))
		return c.err
	})
	if err != nil {
		return nil, fmt.Errorf("labelling editor references: %w", err)
	}

	return labelled, nil
}

// labelRow is a referenced entity as the editor-reference queries return it.
type labelRow struct {
	id       uint64
	slug     string
	label    string
	isPublic bool
}

// editorReferences returns the entities refs references that the actor may see (public ones, and the pending new
// entities of pending), each once, by entity type and then ID: each public entity labelled with its public title or
// name, and each pending new entity with the name its pending document gives it.
func (c *catalogueReader) editorReferences(
	ctx context.Context, actor domain.Actor, refs domain.SnapshotReferences, pending []pendingNewEntity,
) []domain.EditorReference {
	all := actor.Role == domain.RoleModerator
	pendingByEntity := make(map[domain.EntityRef]pendingNewEntity, len(pending))
	for _, e := range pending {
		pendingByEntity[e.entity] = e
	}

	var out []domain.EditorReference
	add := func(entityType domain.EntityType, ids []int64, query func([]uint64) []labelRow) {
		unsigned := slices.Compact(slices.Sorted(slices.Values(unsignedIDs(ids))))
		if len(unsigned) == 0 {
			return
		}
		var typed []domain.EditorReference
		found := map[int64]bool{}
		for _, row := range query(unsigned) {
			id := c.signedID(row.id)
			ref := domain.EditorReference{EntityType: entityType, ID: id, Slug: row.slug, Label: row.label}
			if e, ok := pendingByEntity[domain.EntityRef{Type: entityType, ID: id}]; ok && !row.isPublic {
				ref.Label, ref.IsPending = documentTitle(e.doc), true
			}
			found[id] = true
			typed = append(typed, ref)
		}
		for _, id := range unsigned {
			e, ok := pendingByEntity[domain.EntityRef{Type: entityType, ID: c.signedID(id)}]
			if ok && !found[e.entity.ID] {
				typed = append(typed, domain.EditorReference{EntityType: entityType, ID: e.entity.ID, Slug: e.slug,
					Label: documentTitle(e.doc), IsPending: true})
			}
		}
		slices.SortFunc(typed, func(a, b domain.EditorReference) int { return cmp.Compare(a.ID, b.ID) })
		out = append(out, typed...)
	}
	add(domain.EntityTypePiece, refs.PieceIDs, func(ids []uint64) []labelRow {
		rows := fetch(ctx, c, "referenced pieces", c.q.ListEditorPiecesByID,
			queries.ListEditorPiecesByIDParams{Ids: ids, IncludeUnapproved: all})
		return mapRows(rows, func(r queries.ListEditorPiecesByIDRow) labelRow {
			return labelRow{r.ID, r.Slug, r.Title, r.IsPublic}
		})
	})
	add(domain.EntityTypePerson, refs.PersonIDs, func(ids []uint64) []labelRow {
		rows := fetch(ctx, c, "referenced persons", c.q.ListEditorPersonsByID,
			queries.ListEditorPersonsByIDParams{Ids: ids, IncludeUnapproved: all})
		return mapRows(rows, func(r queries.ListEditorPersonsByIDRow) labelRow {
			return labelRow{r.ID, r.Slug, r.Name, r.IsPublic}
		})
	})
	add(domain.EntityTypeTerm, refs.TermIDs, func(ids []uint64) []labelRow {
		rows := fetch(ctx, c, "referenced terms", c.q.ListEditorTermsByID,
			queries.ListEditorTermsByIDParams{Ids: ids, IncludeUnapproved: all})
		return mapRows(rows, func(r queries.ListEditorTermsByIDRow) labelRow {
			return labelRow{r.ID, r.Slug, r.DisplayName, r.IsPublic}
		})
	})
	add(domain.EntityTypeCollection, refs.CollectionIDs, func(ids []uint64) []labelRow {
		rows := fetch(ctx, c, "referenced collections", c.q.ListEditorCollectionsByID,
			queries.ListEditorCollectionsByIDParams{Ids: ids, IncludeUnapproved: all})
		return mapRows(rows, func(r queries.ListEditorCollectionsByIDRow) labelRow {
			return labelRow{id: r.ID, label: r.Name, isPublic: r.IsPublic}
		})
	})
	add(domain.EntityTypeEvent, refs.EventIDs, func(ids []uint64) []labelRow {
		rows := fetch(ctx, c, "referenced events", c.q.ListEditorEventsByID,
			queries.ListEditorEventsByIDParams{Ids: ids, IncludeUnapproved: all})
		return mapRows(rows, func(r queries.ListEditorEventsByIDRow) labelRow {
			return labelRow{r.ID, r.Slug, r.Name, r.IsPublic}
		})
	})

	return out
}

// documentTitle is the title or name a document gives its entity.
func documentTitle(doc domain.AnyDocument) string {
	switch d := doc.(type) {
	case domain.PieceDocument:
		return d.Title
	case domain.PersonDocument:
		return d.Name
	case domain.TermDocument:
		return d.DisplayName
	case domain.CollectionDocument:
		return d.Name
	case domain.EventDocument:
		return d.Name
	}

	return ""
}

// editorVisibility loads what decides what the actor sees of the documents of entity id (0 for a new entity) of
// kind (D5): the current visibility of what they reference, the hidden links among their links, the actor's own
// pending new terms and persons among own, and the actor's role.
func (c *catalogueReader) editorVisibility(
	ctx context.Context, actor domain.Actor, id int64, kind documentKind, own []pendingNewEntity,
	docs ...domain.AnyDocument,
) domain.EditorVisibility {
	var refs domain.SnapshotReferences
	for _, doc := range docs {
		kind.addReferences(id, doc, &refs)
	}
	hidden := map[string]bool{}
	for _, state := range c.linkStates(ctx, refs.LinkURLs) {
		if state.isHidden {
			hidden[state.normalizedURL] = true
		}
	}

	return domain.EditorVisibility{SnapshotVisibility: c.visibility(ctx, refs),
		OwnPending: ownPendingRefs(ownPendingFor(actor, own)), HiddenLinkURLs: hidden, Role: actor.Role}
}

// linkState is the state of the link with a normalized URL.
type linkState struct {
	id            int64
	normalizedURL string
	isHidden      bool
	liveness      domain.Liveness
}

// linkStates returns the states of the links whose normalized URLs are those of urls. A URL that does not normalize
// is no link's.
func (c *catalogueReader) linkStates(ctx context.Context, urls []string) map[string]linkState {
	normalized := make([]string, 0, len(urls))
	for _, url := range urls {
		if n, err := domain.NormalizeURL(url); err == nil {
			normalized = append(normalized, n)
		}
	}
	states := map[string]linkState{}
	if len(normalized) == 0 {
		return states
	}
	for _, row := range fetch(ctx, c, "link states", c.q.ListLinkStates, slices.Compact(slices.Sorted(
		slices.Values(normalized)))) {
		states[row.NormalizedUrl] = linkState{id: c.signedID(row.ID), normalizedURL: row.NormalizedUrl,
			isHidden: row.IsHidden, liveness: enumOf[domain.Liveness](c, row.Liveness)}
	}

	return states
}

// documentLinkStates returns the states of the links of doc, by the document's URLs: for an interactive moderator
// only (D8), and empty otherwise.
func (c *catalogueReader) documentLinkStates(
	ctx context.Context, actor domain.Actor, id int64, kind documentKind, doc domain.AnyDocument,
) []domain.LinkState {
	out := []domain.LinkState{}
	if actor.Role != domain.RoleModerator {
		return out
	}
	var refs domain.SnapshotReferences
	kind.addReferences(id, doc, &refs)
	states := c.linkStates(ctx, refs.LinkURLs)
	for _, url := range refs.LinkURLs {
		state := domain.LinkState{URL: url}
		normalized, err := domain.NormalizeURL(url)
		if s, ok := states[normalized]; err == nil && ok {
			state.LinkID, state.IsHidden, state.Liveness = &s.id, s.isHidden, &s.liveness
		}
		out = append(out, state)
	}

	return out
}

// errNotEditable is an entity the actor may not edit: neither public nor their own pending new entity.
var errNotEditable = fmt.Errorf("entity is not editable: %w", datasources.ErrNotFound)

// editTarget is an entity an actor may edit, and its current revision, nil for a pending new entity.
type editTarget struct {
	entity          domain.RevisionEntity
	currentRevision *int64
}

// editTarget returns the entity key addresses, if the actor may edit it: it is public, or one of the actor's own
// pending new entities among own.
func (c *catalogueReader) editTarget(ctx context.Context, key domain.EntityKey, own []pendingNewEntity) (
	editTarget, error,
) {
	for _, e := range own {
		if e.entity.Type == key.Type && (e.slug != "" && e.slug == key.Slug ||
			key.Type == domain.EntityTypeCollection && e.entity.ID == key.CollectionID) {
			return editTarget{entity: domain.RevisionEntity{Type: key.Type, ID: e.entity.ID, Slug: e.slug,
				IsNew: true}}, nil
		}
	}

	id, err := c.publicEntityKey(ctx, key)
	if errors.Is(err, datasources.ErrNotFound) {
		return editTarget{}, errNotEditable
	}
	if err != nil {
		return editTarget{}, err
	}

	entity := domain.RevisionEntity{Type: key.Type, ID: c.signedID(id), Slug: key.Slug}
	row, err := c.entityRow(ctx, key.Type, entity.ID, false)
	if err != nil {
		return editTarget{}, err
	}

	return editTarget{entity: entity, currentRevision: row.currentRevision}, nil
}

// publicEntityKey returns the ID of the public entity key addresses, or datasources.ErrNotFound.
func (c *catalogueReader) publicEntityKey(ctx context.Context, key domain.EntityKey) (uint64, error) {
	if key.Type != domain.EntityTypeCollection {
		slugType, ok := slugEntityTypes[key.Type]
		if !ok {
			return 0, fmt.Errorf("%w [%s] of entity type", domain.ErrUnknownEnumValue, key.Type)
		}
		return c.publicEntityID(ctx, slugType, key.Slug)
	}
	if key.CollectionID < 1 {
		return 0, datasources.ErrNotFound
	}
	id, err := c.q.GetPublicCollectionID(ctx, uint64(key.CollectionID))
	if err != nil {
		return 0, lookupError("collection", err)
	}

	return id, nil
}

// slugEntityTypes is the SlugEntityType of each entity type addressed by slug.
var slugEntityTypes = map[domain.EntityType]domain.SlugEntityType{
	domain.EntityTypePiece: domain.SlugEntityTypePiece, domain.EntityTypePerson: domain.SlugEntityTypePerson,
	domain.EntityTypeTerm: domain.SlugEntityTypeTerm, domain.EntityTypeEvent: domain.SlugEntityTypeEvent,
}
