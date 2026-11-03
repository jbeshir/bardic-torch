package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ datasources.PublicPieceLister = (*Repository)(nil)

// ListPublicPieces searches the public pieces, returning one page of them with the total and the facet counts.
// A term slug that no public piece term has gives a datasources.UnknownTermError.
//
// It runs at most three statements in one read-only transaction: the term slugs' resolution (only if the
// search has terms), the page of pieces, and every count in one statement. See pieceSearch.
func (r *Repository) ListPublicPieces(ctx context.Context, query datasources.PieceQuery) (domain.PieceList, error) {
	var list domain.PieceList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		list = c.pieceList(ctx, query)
		return c.err
	})
	if err != nil {
		return domain.PieceList{}, fmt.Errorf("listing public pieces: %w", err)
	}

	return list, nil
}

func (c *catalogueReader) pieceList(ctx context.Context, query datasources.PieceQuery) domain.PieceList {
	if query.Sort == domain.PieceSortRelevance && query.Search == nil {
		c.fail(errRelevanceWithoutSearch)
		return domain.PieceList{}
	}

	search := pieceSearch{query: query, groups: c.termGroups(ctx, query.TermSlugs, pieceTermFacets)}
	if c.err != nil {
		return domain.PieceList{}
	}
	list := domain.PieceList{Pieces: c.pieceSearchItems(ctx, search.itemsStatement())}
	list.Total, list.Facets = c.pieceSearchCounts(ctx, search.countsStatement())

	return list
}

// pieceSearch builds the statements of a piece search whose terms are resolved to groups.
//
// Both statements start with the same WITH clause. If the search has a query, matched(piece_id, score) holds
// every piece whose title and notes, or an alias, match it, with the best score of those matches. candidates
// holds every public piece (matched, if there is a query, and with the stub filter applied), with a boolean
// column per other filter set: facet_<facet> for each facet's terms, kind_match, status_match and
// letter_match. Each count then needs only a different set of those columns.
type pieceSearch struct {
	query  datasources.PieceQuery
	groups []termGroup
}

// Fragments of the piece search statements.
const (
	pieceMatched sqlFragment = "matched AS (SELECT hits.piece_id, MAX(hits.score) AS score FROM (" +
		"SELECT p.id AS piece_id, "
	pieceAliasMatched sqlFragment = " UNION ALL SELECT a.piece_id, "
	pieceMatchedEnd   sqlFragment = ") hits GROUP BY hits.piece_id), "
	pieceCandidates   sqlFragment = "candidates AS (SELECT p.id, p.slug, p.title, p.sort_title, p.sort_letter, " +
		"p.kind, p.conservation_status, p.is_stub"
	pieceTagged sqlFragment = ", EXISTS (SELECT 1 FROM piece_terms pt " +
		"JOIN term_ancestors ta ON ta.descendant_id = pt.term_id" + publicDescendantJoin +
		" WHERE pt.piece_id = p.id AND ta.ancestor_id IN ("
	pieceKindMatch   sqlFragment = "kind_match"
	pieceStatusMatch sqlFragment = "status_match"
	pieceItemsSelect sqlFragment = " SELECT " + executionTimeHint +
		" c.slug, c.title, c.kind, c.conservation_status, c.is_stub, " +
		"(SELECT GROUP_CONCAT(sk.kind ORDER BY sk.kind SEPARATOR ',') FROM piece_secondary_kinds sk " +
		"WHERE sk.piece_id = c.id) FROM candidates c WHERE TRUE"
	pieceTermCounts sqlFragment = " UNION ALL SELECT 'term', t.slug, 0, COUNT(DISTINCT c.id) FROM candidates c " +
		"JOIN piece_terms pt ON pt.piece_id = c.id JOIN term_ancestors ta ON ta.descendant_id = pt.term_id" +
		publicDescendantJoin + " JOIN terms t ON t.id = ta.ancestor_id WHERE TRUE"
	pieceTermGroup  sqlFragment = " GROUP BY t.id, t.slug"
	pieceKindCounts sqlFragment = " UNION ALL SELECT 'kind', k.kind, k.ordinal, COUNT(DISTINCT c.id) " +
		"FROM candidates c JOIN (SELECT id AS piece_id, kind, kind + 0 AS ordinal FROM pieces " +
		"UNION ALL SELECT piece_id, kind, kind + 0 FROM piece_secondary_kinds) k ON k.piece_id = c.id WHERE TRUE"
	pieceKindGroup    sqlFragment = " GROUP BY k.kind, k.ordinal"
	pieceStatusCounts sqlFragment = " UNION ALL SELECT 'conservation_status', c.conservation_status, " +
		"c.conservation_status + 0, COUNT(*) FROM candidates c WHERE TRUE"
	pieceStatusGroup sqlFragment = " GROUP BY c.conservation_status"
)

// secondaryKindSeparator separates the secondary kinds the items statement aggregates with GROUP_CONCAT.
const secondaryKindSeparator = ","

// facetMatch returns the name of the candidates column saying whether a candidate matches the terms of facet.
func facetMatch(facet domain.Facet) sqlFragment {
	switch facet {
	case domain.FacetMood:
		return "facet_mood"
	case domain.FacetTopic:
		return "facet_topic"
	case domain.FacetCulture:
		return "facet_culture"
	case domain.FacetLanguage:
		return "facet_language"
	case domain.FacetLegendaria:
		return "facet_legendaria"
	case domain.FacetForm:
		return "facet_form"
	case domain.FacetOrigin:
		return "facet_origin"
	case domain.FacetOther:
		return "facet_other"
	case domain.FacetAffiliation:
		return "facet_affiliation"
	}

	return "facet_unknown"
}

// matchColumns returns the names of the candidates' filter columns, one per filter the search sets.
func (ps pieceSearch) matchColumns() []sqlFragment {
	var columns []sqlFragment
	for _, group := range ps.groups {
		columns = append(columns, facetMatch(group.facet))
	}
	if len(ps.query.Kinds) > 0 {
		columns = append(columns, pieceKindMatch)
	}
	if len(ps.query.ConservationStatuses) > 0 {
		columns = append(columns, pieceStatusMatch)
	}
	if ps.query.Letter != nil {
		columns = append(columns, letterMatch)
	}

	return columns
}

// writeMatches writes, after a "WHERE TRUE", the conditions that a candidate matches every filter except the
// one whose column is except (nothing to except if empty).
func (ps pieceSearch) writeMatches(s *statement, except sqlFragment) {
	for _, column := range ps.matchColumns() {
		if column != except {
			s.write(" AND c.", column)
		}
	}
}

// writeWith writes the WITH clause defining matched, if the search has a query, and candidates.
func (ps pieceSearch) writeWith(s *statement) {
	q := ps.query
	s.write("WITH ")
	if q.Search != nil {
		s.write(pieceMatched)
		writeMatch(s, *q.Search, "p.title, p.notes_md", "p.title", " FROM pieces p WHERE ", true)
		s.write(pieceAliasMatched)
		writeMatch(s, *q.Search, "a.text", "a.text", " FROM piece_aliases a WHERE ", true)
		s.write(pieceMatchedEnd)
	}

	s.write(pieceCandidates)
	if q.Search != nil {
		s.write(", m.score")
	}
	for _, group := range ps.groups {
		s.write(pieceTagged)
		list(s, group.ids)
		s.write(")) AS ", facetMatch(group.facet))
	}
	if len(q.Kinds) > 0 {
		s.write(", (p.kind IN (")
		list(s, q.Kinds)
		s.write(") OR EXISTS (SELECT 1 FROM piece_secondary_kinds sk WHERE sk.piece_id = p.id AND sk.kind IN (")
		list(s, q.Kinds)
		s.write("))) AS ", pieceKindMatch)
	}
	if len(q.ConservationStatuses) > 0 {
		s.write(", p.conservation_status IN (")
		list(s, q.ConservationStatuses)
		s.write(") AS ", pieceStatusMatch)
	}
	writeLetterMatch(s, "p.sort_letter", q.Letter)

	s.write(" FROM pieces p")
	if q.Search != nil {
		s.write(" JOIN matched m ON m.piece_id = p.id")
	}
	s.write(" WHERE p.current_revision_id IS NOT NULL")
	if q.Stub != nil {
		s.write(" AND p.is_stub = ")
		s.arg(*q.Stub)
	}
	s.write(")")
}

// itemsStatement selects the page of matching pieces: (slug, title, kind, conservation_status, is_stub,
// secondary kinds as a comma-separated list in the ENUM's order, or NULL), ordered by score if by relevance,
// then sort title and ID.
func (ps pieceSearch) itemsStatement() *statement {
	s := &statement{}
	ps.writeWith(s)
	s.write(pieceItemsSelect)
	ps.writeMatches(s, "")
	s.write(" ORDER BY ")
	switch ps.query.Sort {
	case domain.PieceSortRelevance:
		s.write("c.score DESC, ")
	case domain.PieceSortTitle:
	}
	s.write("c.sort_title, c.id")
	writePage(s, ps.query.Page)

	return s
}

// countsStatement selects every count of the search as rows (dimension, value, n): the total; for each public
// term of a piece facet, the matching pieces tagged with it or a descendant, ignoring the search's terms of that
// facet; and likewise by kind (primary or secondary), conservation status and sort letter, each ignoring its own
// filter. Each dimension's rows are in order: terms by slug, kinds and statuses in their ENUMs' order, letters
// alphabetically after "0".
func (ps pieceSearch) countsStatement() *statement {
	s := &statement{}
	ps.writeWith(s)
	s.write(totalCountSelect)
	ps.writeMatches(s, "")

	// Terms of the facets the search does not filter on, which every filter applies to.
	s.write(pieceTermCounts)
	ps.writeMatches(s, "")
	s.write(" AND t.facet NOT IN (")
	s.arg(string(domain.FacetAffiliation))
	for _, group := range ps.groups {
		s.write(", ")
		s.arg(string(group.facet))
	}
	s.write(")", pieceTermGroup)
	// Terms of each facet the search filters on, ignoring that facet's terms.
	for _, group := range ps.groups {
		s.write(pieceTermCounts)
		ps.writeMatches(s, facetMatch(group.facet))
		s.write(" AND t.facet = ")
		s.arg(string(group.facet))
		s.write(pieceTermGroup)
	}

	s.write(pieceKindCounts)
	ps.writeMatches(s, pieceKindMatch)
	s.write(pieceKindGroup, pieceStatusCounts)
	ps.writeMatches(s, pieceStatusMatch)
	s.write(pieceStatusGroup, letterCountSelect)
	ps.writeMatches(s, letterMatch)
	s.write(letterCountGroup, countsOrder)

	return s
}

func (c *catalogueReader) pieceSearchItems(ctx context.Context, s *statement) []domain.PieceSummary {
	pieces := []domain.PieceSummary{}
	c.query(ctx, "pieces", s, func(scan func(dest ...any) error) error {
		var slug, title string
		var kind queries.PiecesKind
		var status queries.PiecesConservationStatus
		var isStub bool
		var secondary sql.NullString
		if err := scan(&slug, &title, &kind, &status, &isStub, &secondary); err != nil {
			return err
		}

		var kinds []domain.PieceKind
		if secondary.Valid {
			for _, k := range strings.Split(secondary.String, secondaryKindSeparator) {
				kinds = append(kinds, enumOf[domain.PieceKind](c, queries.PiecesKind(k)))
			}
		}
		pieces = append(pieces, c.pieceSummary(map[string][]domain.PieceKind{slug: kinds}, slug, title, kind, status,
			isStub))
		return nil
	})

	return pieces
}

func (c *catalogueReader) pieceSearchCounts(ctx context.Context, s *statement) (int64, domain.PieceFacets) {
	var total int64
	facets := domain.PieceFacets{
		Terms:                []domain.TermCount{},
		Kinds:                []domain.PieceKindCount{},
		ConservationStatuses: []domain.ConservationStatusCount{},
		Letters:              []domain.LetterCount{},
	}
	for _, row := range c.counts(ctx, "piece counts", s) {
		switch row.dimension {
		case dimensionTotal:
			total = row.n
		case dimensionTerm:
			facets.Terms = append(facets.Terms, domain.TermCount{Slug: row.value, Count: row.n})
		case dimensionKind:
			facets.Kinds = append(facets.Kinds, domain.PieceKindCount{
				Kind: enumOf[domain.PieceKind](c, queries.PiecesKind(row.value)), Count: row.n,
			})
		case dimensionConservationStatus:
			facets.ConservationStatuses = append(facets.ConservationStatuses, domain.ConservationStatusCount{
				ConservationStatus: enumOf[domain.ConservationStatus](c, queries.PiecesConservationStatus(row.value)),
				Count:              row.n,
			})
		case dimensionLetter:
			facets.Letters = append(facets.Letters, c.letterCount(row))
		default:
			c.fail(fmt.Errorf("unexpected count dimension [%s]", row.dimension))
		}
	}

	return total, facets
}
