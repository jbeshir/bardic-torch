//go:build integration

package main

import (
	"bytes"
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
	args := []string{flagDump, scrubbedDumpDir(t), flagApply, flagReview, filepath.Join(t.TempDir(), "import-review.md")}

	var stdout bytes.Buffer
	require.NoError(t, run(t.Context(), args, databaseURL, &stdout))
	assert.Contains(t, stdout.String(), "pieces 142")
	assert.Contains(t, stdout.String(), "applied")

	err := run(t.Context(), args, databaseURL, &bytes.Buffer{})
	require.ErrorIs(t, err, datasources.ErrCatalogueNotEmpty)
}
