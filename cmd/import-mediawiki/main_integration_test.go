//go:build integration

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
)

func TestRun_ApplyThenApplyAgainRefuses(t *testing.T) {
	t.Parallel()

	dbURL := mysqltest.NewDatabaseURL(t)
	databaseURL := func() string { return dbURL }
	args := func(review string) []string {
		return []string{flagDump, scrubbedDumpDir(t), flagApply, flagReview, review}
	}

	var stdout bytes.Buffer
	require.NoError(t, run(t.Context(), args(filepath.Join(t.TempDir(), "import-review.md")), databaseURL, &stdout))
	assert.Contains(t, stdout.String(), "pieces 142")
	assert.Contains(t, stdout.String(), "applied")

	// The review is written before the transaction, so a refused import still leaves it for the moderators.
	again := filepath.Join(t.TempDir(), "import-review.md")
	err := run(t.Context(), args(again), databaseURL, &bytes.Buffer{})
	require.ErrorIs(t, err, datasources.ErrCatalogueNotEmpty)
	info, err := os.Stat(again)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
