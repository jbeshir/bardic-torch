package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// ListPublicCollections lists one page of the public collections matching query, by sort name and ID, with
// the total and the letter counts, in two statements in one read-only transaction.
func (r *Repository) ListPublicCollections(
	ctx context.Context, query datasources.CollectionQuery,
) (domain.CollectionList, error) {
	var list domain.CollectionList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		list = domain.CollectionList{Collections: c.collectionListItems(ctx, collectionListItemsStatement(query))}
		counts := c.letterListCounts(ctx, "collection counts", collectionListCountsStatement(query))
		list.Total, list.Letters = counts.total, counts.letters
		return c.err
	})
	if err != nil {
		return domain.CollectionList{}, fmt.Errorf("listing public collections: %w", err)
	}

	return list, nil
}

// writeCollectionListWith writes the WITH clause defining candidates: every public collection of the query's
// kinds, with a letter_match column if the query has a letter.
func writeCollectionListWith(s *statement, query datasources.CollectionQuery) {
	s.write("WITH candidates AS (SELECT col.id, col.name, col.sort_name, col.sort_letter, col.kind, col.url, " +
		"col.is_bardic_resource")
	writeLetterMatch(s, "col.sort_letter", query.Letter)
	s.write(" FROM collections col WHERE col.current_revision_id IS NOT NULL")
	if len(query.Kinds) > 0 {
		s.write(" AND col.kind IN (")
		list(s, query.Kinds)
		s.write(")")
	}
	s.write(")")
}

// collectionListItemsStatement selects the page of matching collections: (id, name, kind, url,
// is_bardic_resource), by sort name and ID.
func collectionListItemsStatement(query datasources.CollectionQuery) *statement {
	s := &statement{}
	writeCollectionListWith(s, query)
	s.write(" SELECT ", executionTimeHint, " c.id, c.name, c.kind, c.url, c.is_bardic_resource "+
		"FROM candidates c WHERE TRUE")
	if query.Letter != nil {
		s.write(" AND c.", letterMatch)
	}
	s.write(" ORDER BY c.sort_name, c.id")
	writePage(s, query.Page)

	return s
}

// collectionListCountsStatement selects the total and the letter counts of the matching collections.
func collectionListCountsStatement(query datasources.CollectionQuery) *statement {
	s := &statement{}
	writeCollectionListWith(s, query)
	writeLetterListCounts(s, query.Letter != nil)

	return s
}

func (c *catalogueReader) collectionListItems(ctx context.Context, s *statement) []domain.CollectionSummary {
	collections := []domain.CollectionSummary{}
	c.query(ctx, "collections", s, func(scan func(dest ...any) error) error {
		var id uint64
		var name string
		var kind queries.CollectionsKind
		var url sql.NullString
		var isBardicResource bool
		if err := scan(&id, &name, &kind, &url, &isBardicResource); err != nil {
			return err
		}
		collections = append(collections, c.collectionSummary(c.signedID(id), name, kind, url, isBardicResource))
		return nil
	})

	return collections
}
