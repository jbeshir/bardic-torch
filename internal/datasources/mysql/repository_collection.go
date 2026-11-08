package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ datasources.PublicCollectionGetter = (*Repository)(nil)

// GetPublicCollection retrieves a public collection by ID with its public parent, children and region and the
// first of its citations, returning datasources.ErrNotFound if no public collection has the ID.
func (r *Repository) GetPublicCollection(ctx context.Context, id int64) (domain.Collection, error) {
	// No collection has an ID below 1, and only positive IDs convert to the column's type.
	if id < 1 {
		return domain.Collection{}, fmt.Errorf("getting public collection [%d]: %w", id, datasources.ErrNotFound)
	}
	key := uint64(id)

	var collection domain.Collection
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		collection = c.collection(ctx, key)
		return c.err
	})
	if err != nil {
		return domain.Collection{}, fmt.Errorf("getting public collection [%d]: %w", id, err)
	}

	return collection, nil
}

func (c *catalogueReader) collection(ctx context.Context, id uint64) domain.Collection {
	row, err := c.q.GetPublicCollection(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		c.fail(datasources.ErrNotFound)
		return domain.Collection{}
	}
	if err != nil {
		c.fail(fmt.Errorf("fetching collection: %w", err))
		return domain.Collection{}
	}
	key := c.nullID(row.ID)

	collection := domain.Collection{
		CollectionSummary: c.collectionSummary(c.signedID(row.ID), row.Name, row.Kind, row.Url, row.IsBardicResource),
		DescriptionMD:     stringPtr(row.DescriptionMd),
		UpdatedAt:         row.UpdatedAt,
		Enumerable:        enumOf[domain.CollectionEnumerable](c, row.Enumerable),
		Children:          c.collectionChildren(fetch(ctx, c, "children", c.q.ListPublicCollectionChildren, key)),
		Citations: c.citations(ctx, fetch(ctx, c, "citations", c.q.ListPublicCollectionCitations,
			queries.ListPublicCollectionCitationsParams{CollectionID: key, Limit: domain.MaxNestedListItems})),
		CitationsTotal: fetch(ctx, c, "citation count", c.q.CountPublicCollectionCitations, key),
	}
	if row.RegionSlug.Valid {
		region := c.termSummary(row.RegionSlug.String, row.RegionFacet.TermsFacet, row.RegionDisplayName.String)
		collection.Region = &region
	}
	if row.ParentID.Valid {
		parent := c.collectionSummary(row.ParentID.Int64, row.ParentName.String, row.ParentKind.CollectionsKind,
			row.ParentUrl, row.ParentIsBardicResource.Bool)
		collection.Parent = &parent
	}

	return collection
}

func (c *catalogueReader) collectionChildren(
	rows []queries.ListPublicCollectionChildrenRow,
) []domain.CollectionSummary {
	children := make([]domain.CollectionSummary, 0, len(rows))
	for _, row := range rows {
		children = append(children, c.collectionSummary(c.signedID(row.ID), row.Name, row.Kind, row.Url,
			row.IsBardicResource))
	}

	return children
}

// attachmentKey identifies the attachments of one link to one piece or person, which make one citation.
type attachmentKey struct {
	linkID   uint64
	pieceID  sql.NullInt64
	personID sql.NullInt64
}

// citations returns a citation for each row, with the roles its link is attached to its piece or person with.
func (c *catalogueReader) citations(
	ctx context.Context, rows []queries.ListPublicCollectionCitationsRow,
) []domain.Citation {
	linkIDs := make([]uint64, 0, len(rows))
	var slugs []string
	for _, row := range rows {
		linkIDs = append(linkIDs, row.LinkID)
		if row.PieceSlug.Valid {
			slugs = append(slugs, row.PieceSlug.String)
		}
	}
	kinds := c.secondaryKinds(ctx, slugs)

	roles := make(map[attachmentKey][]domain.LinkRole)
	if len(linkIDs) > 0 {
		for _, row := range fetch(ctx, c, "citation roles", c.q.ListLinkAttachmentRoles, linkIDs) {
			key := attachmentKey{linkID: row.LinkID, pieceID: row.PieceID, personID: row.PersonID}
			roles[key] = append(roles[key], enumOf[domain.LinkRole](c, row.Role))
		}
	}

	citations := make([]domain.Citation, 0, len(rows))
	for _, row := range rows {
		citation := domain.Citation{
			URL:           row.Url,
			Title:         stringPtr(row.Title),
			Locator:       stringPtr(row.Locator),
			Liveness:      enumOf[domain.Liveness](c, row.Liveness),
			LastCheckedAt: timePtr(row.LastCheckedAt),
			ArchivedURL:   stringPtr(row.ArchivedUrl),
			Roles:         roles[attachmentKey{linkID: row.LinkID, pieceID: row.PieceID, personID: row.PersonID}],
		}
		if row.PieceSlug.Valid {
			piece := c.pieceSummary(kinds, row.PieceSlug.String, row.PieceTitle.String, row.PieceKind.PiecesKind,
				row.PieceConservationStatus.PiecesConservationStatus, row.PieceIsStub.Bool)
			citation.Piece = &piece
		} else {
			person := c.personSummary(row.PersonSlug.String, row.PersonName.String, row.PersonKind.PersonsKind,
				row.PersonIsStub.Bool)
			citation.Person = &person
		}
		citations = append(citations, citation)
	}

	return citations
}
