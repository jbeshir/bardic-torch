package app

import (
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/transport/web/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoredTime_UTCWholeSeconds(t *testing.T) {
	t.Parallel()

	// Half a second or more past the second would round up if stored as is; storedTime truncates instead.
	fractional := time.Date(2026, 10, 31, 13, 0, 0, 734512345, time.FixedZone("CET", 60*60))

	got := storedTime(fractional)

	assert.Equal(t, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), got)
	assert.Equal(t, time.UTC, got.Location())
}

func TestSetup_InvalidDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/bardic_torch")

	components, err := Setup(t.Context())

	require.ErrorContains(t, err, "parsing DATABASE_URL: database URL scheme must be mysql, got [postgres]")
	assert.Nil(t, components)
}

// TestWriteTxBudget requires a write transaction, retries included, to end before the server stops writing the
// response, so that a client sees a 503 and not a dropped connection while the transaction may still commit: the
// request's budget is under the server's WriteTimeout with a margin for writing the response, and a fresh request's
// write transaction gets its whole deadline and still leaves the post-commit reserve. Work before the transaction,
// such as a Turnstile verification, shortens the transaction, not the request.
func TestWriteTxBudget(t *testing.T) {
	t.Parallel()

	assert.LessOrEqual(t, server.RequestBudget+time.Second, server.WriteTimeout)
	assert.LessOrEqual(t, mysql.WriteTxDeadline+mysql.PostCommitReserve, server.RequestBudget)
	assert.Less(t, mysql.MinWriteTx, mysql.WriteTxDeadline)
}
