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

// ListPublicTerms lists the public terms of facet, or of every facet if it is nil, in tree order: by facet, each
// parent before its children, and siblings by position, display name and ID. A term whose parent is not listed,
// being of another facet, is listed as a root.
func (r *Repository) ListPublicTerms(ctx context.Context, facet *domain.Facet) ([]domain.TermListItem, error) {
	var params queries.ListPublicTermsParams
	if facet != nil {
		params.Facet = queries.NullTermsFacet{TermsFacet: queries.TermsFacet(*facet), Valid: true}
	}

	var terms []domain.TermListItem
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		rows := fetch(ctx, c, "terms", c.q.ListPublicTerms, params)
		ids := make([]uint64, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		terms = c.termTree(rows, c.termCounts(ctx, ids))
		return c.err
	})
	if err != nil {
		return nil, fmt.Errorf("listing public terms: %w", err)
	}

	return terms, nil
}

// GetPublicTerm retrieves a public term by slug with one page, members, of its pieces or persons, returning
// datasources.ErrNotFound if no public term has the slug.
func (r *Repository) GetPublicTerm(ctx context.Context, slug string, members domain.Page) (domain.Term, error) {
	var term domain.Term
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		term = c.term(ctx, slug, members)
		return c.err
	})
	if err != nil {
		return domain.Term{}, fmt.Errorf("getting public term [%s]: %w", slug, err)
	}

	return term, nil
}

// termTree orders rows, which are ordered by facet, position, display name and ID, so that each term comes
// right after its parent and its earlier siblings' subtrees, with its counts.
func (c *catalogueReader) termTree(
	rows []queries.ListPublicTermsRow, counts map[uint64]domain.TermCounts,
) []domain.TermListItem {
	listed := make(map[string]bool, len(rows))
	for _, row := range rows {
		listed[row.Slug] = true
	}

	var roots []int
	children := make(map[string][]int)
	for i, row := range rows {
		if row.ParentSlug.Valid && listed[row.ParentSlug.String] {
			children[row.ParentSlug.String] = append(children[row.ParentSlug.String], i)
		} else {
			roots = append(roots, i)
		}
	}

	terms := make([]domain.TermListItem, 0, len(rows))
	var visit func(i int)
	visit = func(i int) {
		row := rows[i]
		terms = append(terms, domain.TermListItem{
			TermSummary: c.termSummary(row.Slug, row.Facet, row.DisplayName),
			Name:        row.Name,
			ParentSlug:  stringPtr(row.ParentSlug),
			TermCounts:  counts[row.ID],
		})
		for _, child := range children[row.Slug] {
			visit(child)
		}
	}
	for _, root := range roots {
		visit(root)
	}

	return terms
}

func (c *catalogueReader) term(ctx context.Context, slug string, members domain.Page) domain.Term {
	row, err := c.q.GetPublicTerm(ctx, slug)
	if errors.Is(err, sql.ErrNoRows) {
		c.fail(datasources.ErrNotFound)
		return domain.Term{}
	}
	if err != nil {
		c.fail(fmt.Errorf("fetching term: %w", err))
		return domain.Term{}
	}

	term := domain.Term{
		TermSummary:   c.termSummary(row.Slug, row.Facet, row.DisplayName),
		UpdatedAt:     row.UpdatedAt,
		Name:          row.Name,
		DescriptionMD: stringPtr(row.DescriptionMd),
		Ancestors:     c.termAncestors(fetch(ctx, c, "ancestors", c.q.ListTermAncestors, row.ID)),
	}
	children := fetch(ctx, c, "children", c.q.ListPublicTermChildren, c.nullID(row.ID))
	ids := []uint64{row.ID}
	for _, child := range children {
		ids = append(ids, child.ID)
	}
	counts := c.termCounts(ctx, ids)
	term.TermCounts = counts[row.ID]
	term.Children = c.termChildren(children, counts)

	if row.RelatedSlug.Valid {
		related := c.termSummary(row.RelatedSlug.String, row.RelatedFacet.TermsFacet, row.RelatedDisplayName.String)
		term.RelatedAffiliation = &related
	}

	if term.Facet == domain.FacetAffiliation {
		term.Persons = c.termPersons(fetch(ctx, c, "persons", c.q.ListPublicTermPersons,
			queries.ListPublicTermPersonsParams{TermID: row.ID, Limit: members.Limit, Offset: members.Offset}))
	} else {
		term.Pieces = c.termPieces(ctx, fetch(ctx, c, "pieces", c.q.ListPublicTermPieces,
			queries.ListPublicTermPiecesParams{TermID: row.ID, Limit: members.Limit, Offset: members.Offset}))
	}

	return term
}

func (c *catalogueReader) termAncestors(rows []queries.ListTermAncestorsRow) []domain.TermSummary {
	ancestors := make([]domain.TermSummary, 0, len(rows))
	for _, row := range rows {
		ancestors = append(ancestors, c.termSummary(row.Slug, row.Facet, row.DisplayName))
	}

	return ancestors
}

func (c *catalogueReader) termChildren(
	rows []queries.ListPublicTermChildrenRow, counts map[uint64]domain.TermCounts,
) []domain.TermChild {
	children := make([]domain.TermChild, 0, len(rows))
	for _, row := range rows {
		children = append(children, domain.TermChild{
			Term:       c.termSummary(row.Slug, row.Facet, row.DisplayName),
			TermCounts: counts[row.ID],
		})
	}

	return children
}

func (c *catalogueReader) termPieces(
	ctx context.Context, rows []queries.ListPublicTermPiecesRow,
) []domain.PieceSummary {
	slugs := make([]string, 0, len(rows))
	for _, row := range rows {
		slugs = append(slugs, row.Slug)
	}
	creditNames := c.creditNames(ctx, slugs)

	pieces := make([]domain.PieceSummary, 0, len(rows))
	for _, row := range rows {
		pieces = append(pieces, c.pieceSummary(creditNames, row.Slug, row.Title, row.Kind, row.ConservationStatus,
			row.IsStub))
	}

	return pieces
}

func (c *catalogueReader) termPersons(rows []queries.ListPublicTermPersonsRow) []domain.PersonSummary {
	persons := make([]domain.PersonSummary, 0, len(rows))
	for _, row := range rows {
		persons = append(persons, domain.PersonSummary{
			Slug: row.Slug, Name: row.Name, Kind: enumOf[domain.PersonKind](c, row.Kind), IsStub: row.IsStub,
		})
	}

	return persons
}

// Members a term count row counts, as ListPublicTermCounts labels them.
const (
	termCountPieces  = "piece"
	termCountPersons = "person"
)

// termCounts returns the counts of the public terms with ids, by ID. A term with no members has no row, and so
// counts of 0. It runs no statement for no IDs.
func (c *catalogueReader) termCounts(ctx context.Context, ids []uint64) map[uint64]domain.TermCounts {
	counts := make(map[uint64]domain.TermCounts, len(ids))
	if len(ids) == 0 {
		return counts
	}

	for _, row := range fetch(ctx, c, "term counts", c.q.ListPublicTermCounts,
		queries.ListPublicTermCountsParams{PieceTermIds: ids, PersonTermIds: ids}) {
		count := counts[row.TermID]
		switch row.Member {
		case termCountPieces:
			count.PieceCount = row.N
		case termCountPersons:
			count.PersonCount = row.N
		default:
			c.fail(fmt.Errorf("unexpected term count member [%s]", row.Member))
		}
		counts[row.TermID] = count
	}

	return counts
}
