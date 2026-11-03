package mysql

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queryStatements returns the named statements of queries.sql, by name.
func queryStatements(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile("queries.sql")
	require.NoError(t, err)
	statements := map[string]string{}
	for _, part := range strings.Split(string(data), "-- name: ")[1:] {
		name, _, _ := strings.Cut(part, " ")
		statements[name] = part
	}
	require.NotEmpty(t, statements)

	return statements
}

// TestQueries_LockingReadsCarryTheHint checks that every locking read of queries.sql carries the locking reads'
// execution time limit (lockingReadMillis), which overrides the session's, so that a lock wait ends with InnoDB's
// lock wait timeout rather than the session's execution time limit.
func TestQueries_LockingReadsCarryTheHint(t *testing.T) {
	t.Parallel()
	hint := fmt.Sprintf("/*+ MAX_EXECUTION_TIME(%d) */", lockingReadMillis)
	locking := 0
	for name, statement := range queryStatements(t) {
		if strings.Contains(statement, "FOR UPDATE") || strings.Contains(statement, "FOR SHARE") {
			locking++
			assert.Contains(t, statement, hint, name)
		}
	}
	assert.Positive(t, locking)
	assert.Contains(t, string(lockingReadHint), hint)
}

// TestQueries_DuplicateNameLimit checks that the duplicate check's name queries read one row more than the names it
// expects, so that reaching the limit is noticed.
func TestQueries_DuplicateNameLimit(t *testing.T) {
	t.Parallel()
	limit := fmt.Sprintf("LIMIT %d;", maxDuplicateNames+1)
	statements := queryStatements(t)
	for _, name := range []string{"ListPieceTitles", "ListPieceAliasNames", "ListPersonNames", "ListPersonAliasNames",
		"ListFacetTermNames", "ListEventNames", "ListCollectionNames"} {
		assert.Contains(t, statements[name], limit, name)
	}
}
