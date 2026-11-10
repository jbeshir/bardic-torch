package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// fakeRunner is a statementRunner that records the statements it is given and answers the nth with results[n],
// each row a list of column values assigned to the scan destinations (through sql.Scanner when they have it).
type fakeRunner struct {
	statements []*statement
	results    [][][]any
}

func (f *fakeRunner) run(_ context.Context, s *statement, row rowFunc) error {
	n := len(f.statements)
	f.statements = append(f.statements, s)
	if n >= len(f.results) {
		return nil
	}

	for _, values := range f.results[n] {
		err := row(func(dest ...any) error {
			if len(dest) != len(values) {
				return fmt.Errorf("scanning %d columns into %d destinations", len(values), len(dest))
			}
			for i, d := range dest {
				if scanner, ok := d.(sql.Scanner); ok {
					if err := scanner.Scan(values[i]); err != nil {
						return err
					}
					continue
				}
				reflect.ValueOf(d).Elem().Set(reflect.ValueOf(values[i]))
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// reader returns a catalogueReader running statements with f.
func (f *fakeRunner) reader() *catalogueReader {
	return &catalogueReader{run: f.run}
}

func TestPieceSearch_RelevanceNeedsSearch(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	c := runner.reader()
	c.pieceList(t.Context(), datasources.PieceQuery{Sort: domain.PieceSortRelevance})
	require.ErrorIs(t, c.err, errRelevanceWithoutSearch)
	assert.Empty(t, runner.statements)
}

func TestPieceSearch_UnknownTerm(t *testing.T) {
	t.Parallel()

	// The resolution finds war only: swords is unknown, unapproved or not a piece term alike, and it is
	// reported as given; no other statement runs.
	runner := &fakeRunner{results: [][][]any{{{uint64(1), testWar, topic}}}}
	c := runner.reader()
	c.pieceList(t.Context(), datasources.PieceQuery{TermSlugs: []string{testWar, testSwords, "spears"},
		Sort: domain.PieceSortTitle})

	var unknown *datasources.UnknownTermError
	require.ErrorAs(t, c.err, &unknown)
	assert.Equal(t, testSwords, unknown.Slug)
	assert.Len(t, runner.statements, 1)
}

func TestPersonSearch_UnknownAffiliation(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	c := runner.reader()
	slug := "westmarch"
	c.personList(t.Context(), datasources.PersonQuery{AffiliationSlug: &slug, Sort: domain.PersonSortName})

	var unknown *datasources.UnknownTermError
	require.ErrorAs(t, c.err, &unknown)
	assert.Equal(t, slug, unknown.Slug)
	assert.Len(t, runner.statements, 1)
}

func TestPersonSearch_RelevanceNeedsSearch(t *testing.T) {
	t.Parallel()

	c := (&fakeRunner{}).reader()
	c.personList(t.Context(), datasources.PersonQuery{Sort: domain.PersonSortRelevance})
	require.ErrorIs(t, c.err, errRelevanceWithoutSearch)
}

func TestQueryError(t *testing.T) {
	t.Parallel()

	timeout := &mysqldriver.MySQLError{Number: erQueryTimeout, Message: "maximum statement execution time exceeded"}
	require.ErrorIs(t, queryError(fmt.Errorf("wrapped: %w", timeout)), datasources.ErrQueryTimeout)
	require.ErrorIs(t, queryError(timeout), timeout, "the server's error is kept")

	other := &mysqldriver.MySQLError{Number: 1146, Message: "table does not exist"}
	require.NotErrorIs(t, queryError(other), datasources.ErrQueryTimeout)
	require.NotErrorIs(t, queryError(errors.New("connection refused")), datasources.ErrQueryTimeout)
	assert.NoError(t, queryError(nil))
}

func TestFetch_QueryTimeout(t *testing.T) {
	t.Parallel()

	// A sqlc read the server stops, under the session limit every connection has, is a timeout as a search's is.
	c := &catalogueReader{}
	fetch(t.Context(), c, "terms", func(context.Context, int) (int, error) {
		return 0, &mysqldriver.MySQLError{Number: erQueryTimeout, Message: "maximum statement execution time exceeded"}
	}, 0)
	require.ErrorIs(t, c.err, datasources.ErrQueryTimeout)

	c = &catalogueReader{}
	fetch(t.Context(), c, "terms", func(context.Context, int) (int, error) {
		return 0, errors.New("connection refused")
	}, 0)
	require.Error(t, c.err)
	require.NotErrorIs(t, c.err, datasources.ErrQueryTimeout)
}

func TestPrefixPattern(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "the%", prefixPattern("the"))
	assert.Equal(t, "!!!%!_x%", prefixPattern("!%_x"))
	assert.Equal(t, `back\slash%`, prefixPattern(`back\slash`), "backslashes are ordinary with ESCAPE '!'")
}

func TestBooleanQuery(t *testing.T) {
	t.Parallel()

	assert.Equal(t, wildQuery, booleanQuery([]string{"wild", "heron"}))
}

// Test strings used more than once.
const (
	testSwords = "swords"
	testWar    = "war"
	topic      = string(domain.FacetTopic)
	wildQuery  = "+wild* +heron*"
)
