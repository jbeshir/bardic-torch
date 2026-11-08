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

// The paged lists of what an entity's page caps at domain.MaxNestedListItems. Each runs the query of the page's
// own list with the page's limit and offset, so its first page of that size is the page's list.

var (
	_ datasources.PublicPersonPerformanceLister  = (*Repository)(nil)
	_ datasources.PublicCollectionCitationLister = (*Repository)(nil)
	_ datasources.PublicEventPerformanceLister   = (*Repository)(nil)
)

// ListPublicPersonPerformances lists one page of the performances of the public person with slug, as their
// profile lists them, with how many there are in all, returning datasources.ErrNotFound if no public person has
// the slug. A person who prefers no page has none, as their profile is not shown, and no query reads theirs.
func (r *Repository) ListPublicPersonPerformances(
	ctx context.Context, slug string, page domain.Page,
) (domain.PersonPerformanceList, error) {
	var list domain.PersonPerformanceList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		list = c.personPerformanceList(ctx, slug, page)
		return c.err
	})
	if err != nil {
		return domain.PersonPerformanceList{}, fmt.Errorf("listing performances of public person [%s]: %w", slug, err)
	}

	return list, nil
}

func (c *catalogueReader) personPerformanceList(
	ctx context.Context, slug string, page domain.Page,
) domain.PersonPerformanceList {
	row, err := c.q.GetPublicPersonStatus(ctx, slug)
	if errors.Is(err, sql.ErrNoRows) {
		c.fail(datasources.ErrNotFound)
		return domain.PersonPerformanceList{}
	}
	if err != nil {
		c.fail(fmt.Errorf("fetching person: %w", queryError(err)))
		return domain.PersonPerformanceList{}
	}
	if enumOf[domain.PersonStatus](c, row.Status) == domain.PersonStatusPrefersNoPage {
		return domain.PersonPerformanceList{Performances: []domain.PersonPerformance{}}
	}

	key := c.nullID(row.ID)
	rows := fetch(ctx, c, "performances", c.q.ListPublicPersonPerformances,
		queries.ListPublicPersonPerformancesParams{
			PerformerID: row.ID, AttachedPersonID: key, Limit: page.Limit, Offset: page.Offset,
		})
	slugs := make([]string, 0, len(rows))
	for _, performance := range rows {
		if performance.PieceSlug.Valid {
			slugs = append(slugs, performance.PieceSlug.String)
		}
	}

	return domain.PersonPerformanceList{
		Performances: c.personPerformances(c.pieceDetails(ctx, slugs), rows),
		Total: fetch(ctx, c, "performance count", c.q.CountPublicPersonPerformances,
			queries.CountPublicPersonPerformancesParams{PerformerID: row.ID, AttachedPersonID: key}),
	}
}

// ListPublicCollectionCitations lists one page of the citations of the public collection with id, as its page
// lists them, with how many there are in all, returning datasources.ErrNotFound if no public collection has the
// ID.
func (r *Repository) ListPublicCollectionCitations(
	ctx context.Context, id int64, page domain.Page,
) (domain.CitationList, error) {
	// No collection has an ID below 1, and only positive IDs convert to the column's type.
	if id < 1 {
		return domain.CitationList{}, fmt.Errorf("listing citations of public collection [%d]: %w", id,
			datasources.ErrNotFound)
	}
	key := uint64(id)

	var list domain.CitationList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		list = c.citationList(ctx, key, page)
		return c.err
	})
	if err != nil {
		return domain.CitationList{}, fmt.Errorf("listing citations of public collection [%d]: %w", id, err)
	}

	return list, nil
}

func (c *catalogueReader) citationList(ctx context.Context, id uint64, page domain.Page) domain.CitationList {
	found, err := c.q.GetPublicCollectionID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		c.fail(datasources.ErrNotFound)
		return domain.CitationList{}
	}
	if err != nil {
		c.fail(fmt.Errorf("fetching collection: %w", queryError(err)))
		return domain.CitationList{}
	}

	key := c.nullID(found)
	return domain.CitationList{
		Citations: c.citations(ctx, fetch(ctx, c, "citations", c.q.ListPublicCollectionCitations,
			queries.ListPublicCollectionCitationsParams{CollectionID: key, Limit: page.Limit, Offset: page.Offset})),
		Total: fetch(ctx, c, "citation count", c.q.CountPublicCollectionCitations, key),
	}
}

// ListPublicEventPerformances lists one page of the performances at the public event or venue with slug, as its
// page lists them, with how many there are in all, returning datasources.ErrNotFound if no public event has the
// slug.
func (r *Repository) ListPublicEventPerformances(
	ctx context.Context, slug string, page domain.Page,
) (domain.EventPerformanceList, error) {
	var list domain.EventPerformanceList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		list = c.eventPerformanceList(ctx, slug, page)
		return c.err
	})
	if err != nil {
		return domain.EventPerformanceList{}, fmt.Errorf("listing performances of public event [%s]: %w", slug, err)
	}

	return list, nil
}

func (c *catalogueReader) eventPerformanceList(
	ctx context.Context, slug string, page domain.Page,
) domain.EventPerformanceList {
	id, err := c.q.GetPublicEventID(ctx, slug)
	if errors.Is(err, sql.ErrNoRows) {
		c.fail(datasources.ErrNotFound)
		return domain.EventPerformanceList{}
	}
	if err != nil {
		c.fail(fmt.Errorf("fetching event: %w", queryError(err)))
		return domain.EventPerformanceList{}
	}

	key := c.nullID(id)
	return domain.EventPerformanceList{
		Performances: c.eventPerformances(ctx, fetch(ctx, c, "performances", c.q.ListPublicEventPerformances,
			queries.ListPublicEventPerformancesParams{EventID: key, Limit: page.Limit, Offset: page.Offset})),
		Total: fetch(ctx, c, "performance count", c.q.CountPublicEventPerformances, key),
	}
}
