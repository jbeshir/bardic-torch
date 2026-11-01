package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The catalogue's search statements filter on whichever parameters a request sets, which sqlc's static queries
// cannot express, so they are built here. The rules that keep them safe:
//   - SQL text is written only as sqlFragment values. The type has no conversion from a string variable, so
//     every fragment is a constant, or chosen from constants by a switch over a typed enum.
//   - Every value is a ? placeholder with its argument, and an IN list has one placeholder per value.
//   - Every statement's top-level SELECT carries executionTimeHint, so a pathological search is stopped by the
//     server and reported as datasources.ErrQueryTimeout.

// sqlFragment is a piece of SQL text. Only constants can be passed where one is expected.
type sqlFragment string

// executionTimeHint is the optimizer hint, placed right after a statement's top-level SELECT, that has MySQL stop
// the statement after maxExecutionTimeMillis (error 3024). The session limit every connection has (ConfigFromURL)
// is the same, so the hint changes nothing there; it keeps searches, the costliest statements and the ones whose
// shape a request chooses, capped on a connection that has lifted the session limit, such as a writer's.
const executionTimeHint sqlFragment = "/*+ MAX_EXECUTION_TIME(" + maxExecutionTimeMillis + ") */"

// erQueryTimeout is MySQL's ER_QUERY_TIMEOUT: a statement exceeded its maximum execution time.
const erQueryTimeout = 3024

// statement is a SQL statement under construction and its arguments, one per placeholder.
type statement struct {
	text strings.Builder
	args []any
}

// write appends fragments to the statement's text.
func (s *statement) write(fragments ...sqlFragment) {
	for _, f := range fragments {
		s.text.WriteString(string(f))
	}
}

// arg appends a placeholder for value.
func (s *statement) arg(value any) {
	s.text.WriteString("?")
	s.args = append(s.args, value)
}

// list appends a comma-separated placeholder for each of values, for an IN list. values must not be empty.
func list[T any](s *statement, values []T) {
	for i, value := range values {
		if i > 0 {
			s.write(", ")
		}
		s.arg(value)
	}
}

// String returns the statement's SQL text.
func (s *statement) String() string {
	return s.text.String()
}

// rowFunc reads one row of a statement's result with scan, which is sql.Rows.Scan.
type rowFunc func(scan func(dest ...any) error) error

// statementRunner runs a statement and calls row for each row of its result, in order.
type statementRunner func(ctx context.Context, s *statement, row rowFunc) error

// txRunner returns a statementRunner running statements in tx.
func txRunner(tx *sql.Tx) statementRunner {
	return func(ctx context.Context, s *statement, row rowFunc) error {
		rows, err := tx.QueryContext(ctx, s.String(), s.args...)
		if err != nil {
			return queryError(err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			if err := row(rows.Scan); err != nil {
				return err
			}
		}

		return queryError(rows.Err())
	}
}

// queryError returns err, marked as datasources.ErrQueryTimeout if the server stopped the statement for running
// too long.
func queryError(err error) error {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == erQueryTimeout {
		return fmt.Errorf("%w: %w", datasources.ErrQueryTimeout, err)
	}

	return err
}

// query runs s with the reader's runner, calling row for each row, unless the reader has already failed. A
// failure fails the reader.
func (c *catalogueReader) query(ctx context.Context, what string, s *statement, row rowFunc) {
	if c.err != nil {
		return
	}
	if err := c.run(ctx, s, row); err != nil {
		c.fail(fmt.Errorf("querying %s: %w", what, err))
	}
}

// errRelevanceWithoutSearch is returned for a list ordered by relevance with no search to be relevant to.
var errRelevanceWithoutSearch = errors.New("relevance order needs a search")

// Fragments shared by the search statements.
const (
	// publicDescendantJoin follows a join of term_ancestors ta, keeping the rows whose tagged term
	// ta.descendant_id, and so each of its ancestors, is public.
	publicDescendantJoin sqlFragment = " JOIN public_terms dt ON dt.id = ta.descendant_id"
	// letterMatch is the name of a candidates column saying whether the candidate matches the letter filter.
	letterMatch sqlFragment = "letter_match"
	// totalCountSelect starts, after a WITH clause defining the candidates, the SELECT of a counts statement:
	// rows (dimension, value, n), the first counting the candidates matching every filter, which the rest of its
	// WHERE clause follows. Further counts are UNION ALL branches, and countsOrder ends the statement.
	totalCountSelect sqlFragment = " SELECT " + executionTimeHint + " counted.dimension, counted.value, counted.n " +
		"FROM (SELECT 'total' AS dimension, '' AS value, 0 AS ordinal, COUNT(*) AS n FROM candidates c WHERE TRUE"
	// letterCountSelect counts the candidates by sort letter: its WHERE clause leaves out the letter filter.
	letterCountSelect sqlFragment = " UNION ALL SELECT 'letter', c.sort_letter, 0, COUNT(*) FROM candidates c " +
		"WHERE TRUE"
	letterCountGroup sqlFragment = " GROUP BY c.sort_letter"
	countsOrder      sqlFragment = ") counted ORDER BY counted.dimension, counted.ordinal, counted.value"
)

// Count dimensions, as the counts statements label their rows.
const (
	dimensionTotal              = "total"
	dimensionLetter             = "letter"
	dimensionTerm               = "term"
	dimensionKind               = "kind"
	dimensionConservationStatus = "conservation_status"
)

// count is one row of a counts statement: n candidates have value in dimension.
type count struct {
	dimension string
	value     string
	n         int64
}

// counts runs a counts statement and returns its rows in order.
func (c *catalogueReader) counts(ctx context.Context, what string, s *statement) []count {
	var counts []count
	c.query(ctx, what, s, func(scan func(dest ...any) error) error {
		var row count
		if err := scan(&row.dimension, &row.value, &row.n); err != nil {
			return err
		}
		counts = append(counts, row)
		return nil
	})

	return counts
}

// letterCount converts a letter count row, failing the reader if the stored letter is not one of the index's.
func (c *catalogueReader) letterCount(row count) domain.LetterCount {
	letter, err := domain.ParseLetter(row.value)
	if err != nil {
		c.fail(fmt.Errorf("sort letter [%s]: %w", row.value, err))
	}

	return domain.LetterCount{Letter: letter, Count: row.n}
}

// letterListCounts are the counts of a list whose only facet is the letter.
type letterListCounts struct {
	total   int64
	letters []domain.LetterCount
}

// letterListCounts runs a counts statement built by writeLetterListCounts.
func (c *catalogueReader) letterListCounts(ctx context.Context, what string, s *statement) letterListCounts {
	counts := letterListCounts{letters: []domain.LetterCount{}}
	for _, row := range c.counts(ctx, what, s) {
		switch row.dimension {
		case dimensionTotal:
			counts.total = row.n
		case dimensionLetter:
			counts.letters = append(counts.letters, c.letterCount(row))
		default:
			c.fail(fmt.Errorf("unexpected count dimension [%s]", row.dimension))
		}
	}

	return counts
}

// writeLetterListCounts writes, after the WITH clause defining the candidates, the SELECT counting them in all and
// by letter. hasLetter says whether the candidates have a letterMatch column.
func writeLetterListCounts(s *statement, hasLetter bool) {
	s.write(totalCountSelect)
	if hasLetter {
		s.write(" AND c.", letterMatch)
	}
	s.write(letterCountSelect, letterCountGroup, countsOrder)
}

// writeLetterMatch writes, in a candidates column list, the letterMatch column for letter, if it is set, comparing
// column with it.
func writeLetterMatch(s *statement, column sqlFragment, letter *domain.Letter) {
	if letter == nil {
		return
	}
	s.write(", ", column, " = ")
	s.arg(string(*letter))
	s.write(" AS ", letterMatch)
}

// writePage writes the LIMIT and OFFSET clauses of page.
func writePage(s *statement, page domain.Page) {
	s.write(" LIMIT ")
	s.arg(page.Limit)
	s.write(" OFFSET ")
	s.arg(page.Offset)
}

// termFacets says which facets a term filter accepts.
type termFacets int

const (
	// pieceTermFacets are the facets that classify pieces: every one except affiliation.
	pieceTermFacets termFacets = iota
	// affiliationTermFacets is the affiliation facet, which classifies persons.
	affiliationTermFacets
)

// termGroup is the public terms of one facet a search filters on.
type termGroup struct {
	facet domain.Facet
	ids   []uint64
}

// publicTermsStatement selects the ID, slug and facet of each public term of facets with one of slugs, by facet
// in the ENUM's order, then ID.
func publicTermsStatement(slugs []string, facets termFacets) *statement {
	s := &statement{}
	s.write("SELECT ", executionTimeHint, " t.id, t.slug, t.facet FROM public_terms t WHERE t.slug IN (")
	list(s, slugs)
	s.write(") AND t.facet ")
	switch facets {
	case pieceTermFacets:
		s.write("<> ")
	case affiliationTermFacets:
		s.write("= ")
	}
	s.arg(string(domain.FacetAffiliation))
	s.write(" ORDER BY t.facet, t.id")

	return s
}

// termGroups resolves slugs to the public terms of facets they name, grouped by facet in the ENUM's order. If a
// slug names no such term, it fails the reader with an UnknownTermError for the first such slug, in slugs' order.
// It runs no statement for no slugs.
func (c *catalogueReader) termGroups(ctx context.Context, slugs []string, facets termFacets) []termGroup {
	if len(slugs) == 0 {
		return nil
	}

	var groups []termGroup
	found := make(map[string]bool, len(slugs))
	c.query(ctx, "terms", publicTermsStatement(slugs, facets), func(scan func(dest ...any) error) error {
		var id uint64
		var slug, facet string
		if err := scan(&id, &slug, &facet); err != nil {
			return err
		}
		found[slug] = true
		f := enumOf[domain.Facet](c, queries.TermsFacet(facet))
		if len(groups) == 0 || groups[len(groups)-1].facet != f {
			groups = append(groups, termGroup{facet: f})
		}
		groups[len(groups)-1].ids = append(groups[len(groups)-1].ids, id)
		return nil
	})
	if c.err != nil {
		return nil
	}

	for _, slug := range slugs {
		if !found[slug] {
			c.fail(&datasources.UnknownTermError{Slug: slug})
			return nil
		}
	}

	return groups
}

// booleanQuery returns the FULLTEXT boolean-mode query matching text with a word starting with each of tokens.
// Tokens are letters, digits and marks only (domain.SearchQuery), so they hold no boolean-mode operator.
func booleanQuery(tokens []string) string {
	required := make([]string, 0, len(tokens))
	for _, token := range tokens {
		required = append(required, "+"+token+"*")
	}

	return strings.Join(required, " ")
}

// likeEscaper escapes the LIKE wildcards and the escape character itself, which statements declare with
// ESCAPE '!' so that the pattern does not depend on the NO_BACKSLASH_ESCAPES SQL mode.
var likeEscaper = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

// prefixPattern returns the LIKE pattern, with escape character !, matching text that starts with prefix.
func prefixPattern(prefix string) string {
	return likeEscaper.Replace(prefix) + "%"
}

// writeMatch writes the condition matching search with the FULLTEXT index on columns, or, if search has no
// tokens, the condition matching prefixColumn by prefix; and before it the score column's expression and FROM
// clause from.
//
// The two forms are: "MATCH (columns) AGAINST (? IN BOOLEAN MODE) AS score FROM ... WHERE ... MATCH (columns)
// AGAINST (? IN BOOLEAN MODE)", and "0 AS score FROM ... WHERE ... prefixColumn LIKE ? ESCAPE '!'". Prefix
// matches all score 0, so they are ordered by sort key. Unless scored, a FULLTEXT match scores 0 too: a MATCH
// score depends on every row of the index, so an index holding rows a search must not reveal, such as hidden
// aliases, must not be scored. Whether a row matches a boolean-mode query of required tokens does not depend on
// the other rows.
func writeMatch(s *statement, search domain.SearchQuery, columns, prefixColumn, from sqlFragment, scored bool) {
	if len(search.Tokens) == 0 || !scored {
		s.write("0 AS score", from)
		writeMatchCondition(s, search, columns, prefixColumn)
		return
	}

	query := booleanQuery(search.Tokens)
	s.write("MATCH (", columns, ") AGAINST (")
	s.arg(query)
	s.write(" IN BOOLEAN MODE) AS score", from)
	writeMatchCondition(s, search, columns, prefixColumn)
}

// writeMatchCondition writes the condition of writeMatch.
func writeMatchCondition(s *statement, search domain.SearchQuery, columns, prefixColumn sqlFragment) {
	if len(search.Tokens) == 0 {
		s.write(prefixColumn, " LIKE ")
		s.arg(prefixPattern(search.Text))
		s.write(" ESCAPE '!'")
		return
	}

	s.write("MATCH (", columns, ") AGAINST (")
	s.arg(booleanQuery(search.Tokens))
	s.write(" IN BOOLEAN MODE)")
}
