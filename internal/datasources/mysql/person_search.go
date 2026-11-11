package mysql

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// ListPublicPersons searches the public persons, returning one page of them with the total and the letter
// counts. Persons who prefer no page are listed as stubs, match a query by name only, and match no role or
// affiliation filter. An affiliation slug that no public affiliation term has gives a
// datasources.UnknownTermError.
//
// It runs at most three statements in one read-only transaction: the affiliation's resolution (only if the
// search has one), the page of persons, and the counts. See personSearch.
func (r *Repository) ListPublicPersons(
	ctx context.Context, query datasources.PersonQuery,
) (domain.PersonList, error) {
	var list domain.PersonList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		list = c.personList(ctx, query)
		return c.err
	})
	if err != nil {
		return domain.PersonList{}, fmt.Errorf("listing public persons: %w", err)
	}

	return list, nil
}

func (c *catalogueReader) personList(ctx context.Context, query datasources.PersonQuery) domain.PersonList {
	if query.Sort == domain.PersonSortRelevance && query.Search == nil {
		c.fail(errRelevanceWithoutSearch)
		return domain.PersonList{}
	}

	search := personSearch{query: query}
	if query.AffiliationSlug != nil {
		groups := c.termGroups(ctx, []string{*query.AffiliationSlug}, affiliationTermFacets)
		if c.err != nil {
			return domain.PersonList{}
		}
		// The slug names one term, so it resolves to one group of one ID.
		search.affiliationID = &groups[0].ids[0]
	}

	list := domain.PersonList{Persons: c.personSearchItems(ctx, search.itemsStatement())}
	counts := c.letterListCounts(ctx, "person counts", search.countsStatement())
	list.Total, list.Letters = counts.total, counts.letters

	return list
}

// personSearch builds the statements of a person search whose affiliation, if any, is resolved to
// affiliationID.
//
// Both statements start with the same WITH clause. If the search has a query, matched(person_id, score) holds
// every person whose name, or an alias, matches it, with the best score of those matches; aliases of
// persons who prefer no page are not searched. Alias matches score by MATCH over the alias names, like name
// matches. candidates holds every public person matching every filter
// but the letter, with a letter_match column if the search has a letter.
type personSearch struct {
	query         datasources.PersonQuery
	affiliationID *uint64
}

// Fragments of the person search statements.
const (
	personMatched sqlFragment = "matched AS (SELECT hits.person_id, MAX(hits.score) AS score FROM (" +
		"SELECT p.id AS person_id, "
	personAliasMatched sqlFragment = " UNION ALL SELECT a.person_id, "
	personAliasFrom    sqlFragment = " FROM person_aliases a JOIN listed_persons ap ON ap.id = a.person_id " +
		"WHERE "
	personMatchedEnd sqlFragment = ") hits GROUP BY hits.person_id), "
	personCandidates sqlFragment = "candidates AS (SELECT p.id, p.slug, p.name, p.sort_name, p.sort_letter, " +
		"p.kind, p.status, p.is_stub"
	personHasPage sqlFragment = " AND EXISTS (SELECT 1 FROM listed_persons lp WHERE lp.id = p.id)"
	personHasRole sqlFragment = " AND EXISTS (SELECT 1 FROM person_roles r " +
		"WHERE r.person_id = p.id AND r.role IN ("
	personAffiliated sqlFragment = " AND EXISTS (SELECT 1 FROM person_affiliations pa " +
		"JOIN term_ancestors ta ON ta.descendant_id = pa.term_id" + publicDescendantJoin +
		" WHERE pa.person_id = p.id AND ta.ancestor_id = "
	personItemsSelect sqlFragment = " SELECT " + executionTimeHint +
		" c.slug, c.name, c.kind, c.status, c.is_stub FROM candidates c WHERE TRUE"
)

// writeWith writes the WITH clause defining matched, if the search has a query, and candidates.
func (ps personSearch) writeWith(s *statement) {
	q := ps.query
	s.write("WITH ")
	if q.Search != nil {
		s.write(personMatched)
		writeMatch(s, *q.Search, "p.name", "p.name", " FROM persons p WHERE ")
		s.write(personAliasMatched)
		writeMatch(s, *q.Search, "a.name", "a.name", personAliasFrom)
		s.write(personMatchedEnd)
	}

	s.write(personCandidates)
	if q.Search != nil {
		s.write(", m.score")
	}
	writeLetterMatch(s, "p.sort_letter", q.Letter)
	s.write(" FROM persons p")
	if q.Search != nil {
		s.write(" JOIN matched m ON m.person_id = p.id")
	}
	s.write(" WHERE p.current_revision_id IS NOT NULL")
	if len(q.Kinds) > 0 {
		s.write(" AND p.kind IN (")
		list(s, q.Kinds)
		s.write(")")
	}
	if len(q.Roles) > 0 {
		s.write(personHasPage, personHasRole)
		list(s, q.Roles)
		s.write("))")
	}
	if ps.affiliationID != nil {
		s.write(personHasPage, personAffiliated)
		s.arg(*ps.affiliationID)
		s.write(")")
	}
	s.write(")")
}

// itemsStatement selects the page of matching persons: (slug, name, kind, status, is_stub), ordered by score if
// by relevance, then sort name and ID.
func (ps personSearch) itemsStatement() *statement {
	s := &statement{}
	ps.writeWith(s)
	s.write(personItemsSelect)
	if ps.query.Letter != nil {
		s.write(" AND c.", letterMatch)
	}
	s.write(" ORDER BY ")
	switch ps.query.Sort {
	case domain.PersonSortRelevance:
		s.write("c.score DESC, ")
	case domain.PersonSortName:
	}
	s.write("c.sort_name, c.id")
	writePage(s, ps.query.Page)

	return s
}

// countsStatement selects the total and the letter counts of the search.
func (ps personSearch) countsStatement() *statement {
	s := &statement{}
	ps.writeWith(s)
	writeLetterListCounts(s, ps.query.Letter != nil)

	return s
}

func (c *catalogueReader) personSearchItems(ctx context.Context, s *statement) []domain.PersonSummary {
	persons := []domain.PersonSummary{}
	c.query(ctx, "persons", s, func(scan func(dest ...any) error) error {
		var slug, name string
		var kind queries.PersonsKind
		var status queries.PersonsStatus
		var isStub bool
		if err := scan(&slug, &name, &kind, &status, &isStub); err != nil {
			return err
		}
		persons = append(persons, c.personSummary(slug, name, kind,
			domain.PersonIsStub(isStub, enumOf[domain.PersonStatus](c, status))))
		return nil
	})

	return persons
}
