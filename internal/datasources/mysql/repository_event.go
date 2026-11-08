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

var (
	_ datasources.PublicEventLister = (*Repository)(nil)
	_ datasources.PublicEventGetter = (*Repository)(nil)
)

// ListPublicEvents lists page of the public events and venues of kind, or of both kinds if it is nil, by name
// and ID, with how many there are in all.
func (r *Repository) ListPublicEvents(
	ctx context.Context, kind *domain.EventKind, page domain.Page,
) (domain.EventList, error) {
	var filter queries.NullEventsKind
	if kind != nil {
		filter = queries.NullEventsKind{EventsKind: queries.EventsKind(*kind), Valid: true}
	}

	var list domain.EventList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		rows := fetch(ctx, c, "events", c.q.ListPublicEvents,
			queries.ListPublicEventsParams{Kind: filter, Limit: page.Limit, Offset: page.Offset})
		events := make([]domain.EventSummary, 0, len(rows))
		for _, row := range rows {
			events = append(events, c.eventSummary(row.Slug, row.Name, row.Kind))
		}
		list = domain.EventList{
			Events: events,
			Total:  fetch(ctx, c, "event count", c.q.CountPublicEvents, queries.CountPublicEventsParams{Kind: filter}),
		}
		return c.err
	})
	if err != nil {
		return domain.EventList{}, fmt.Errorf("listing public events: %w", err)
	}

	return list, nil
}

// GetPublicEvent retrieves a public event or venue by slug with the first of its performances, returning
// datasources.ErrNotFound if no public event has the slug.
func (r *Repository) GetPublicEvent(ctx context.Context, slug string) (domain.Event, error) {
	var event domain.Event
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		event = c.event(ctx, slug)
		return c.err
	})
	if err != nil {
		return domain.Event{}, fmt.Errorf("getting public event [%s]: %w", slug, err)
	}

	return event, nil
}

func (c *catalogueReader) event(ctx context.Context, slug string) domain.Event {
	row, err := c.q.GetPublicEvent(ctx, slug)
	if errors.Is(err, sql.ErrNoRows) {
		c.fail(datasources.ErrNotFound)
		return domain.Event{}
	}
	if err != nil {
		c.fail(fmt.Errorf("fetching event: %w", err))
		return domain.Event{}
	}
	key := c.nullID(row.ID)

	return domain.Event{
		EventSummary:  c.eventSummary(row.Slug, row.Name, row.Kind),
		DescriptionMD: stringPtr(row.DescriptionMd),
		UpdatedAt:     row.UpdatedAt,
		Performances: c.eventPerformances(ctx, fetch(ctx, c, "performances", c.q.ListPublicEventPerformances,
			queries.ListPublicEventPerformancesParams{EventID: key, Limit: domain.MaxNestedListItems})),
		PerformancesTotal: fetch(ctx, c, "performance count", c.q.CountPublicEventPerformances, key),
	}
}

func (c *catalogueReader) eventPerformances(
	ctx context.Context, rows []queries.ListPublicEventPerformancesRow,
) []domain.EventPerformance {
	ids := make([]uint64, 0, len(rows))
	var slugs []string
	for _, row := range rows {
		ids = append(ids, row.ID)
		if row.PieceSlug.Valid {
			slugs = append(slugs, row.PieceSlug.String)
		}
	}
	kinds := c.secondaryKinds(ctx, slugs)
	performers := c.performers(ctx, ids)

	performances := make([]domain.EventPerformance, 0, len(rows))
	for _, row := range rows {
		performance := domain.EventPerformance{
			Link: c.performanceLink(row.Url, row.Title, row.Platform, row.Liveness, row.LastCheckedAt,
				row.ArchivedUrl),
			Year:                int16Ptr(row.Year),
			Performers:          performers[row.ID],
			DescriptionVerbatim: row.DescriptionVerbatim,
		}
		if row.PieceSlug.Valid {
			piece := c.pieceSummary(kinds, row.PieceSlug.String, row.PieceTitle.String, row.PieceKind.PiecesKind,
				row.PieceConservationStatus.PiecesConservationStatus, row.PieceIsStub.Bool)
			performance.Piece = &piece
		}
		performances = append(performances, performance)
	}

	return performances
}
