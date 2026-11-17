package mysql

import (
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/migrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewestMigrationVersion(t *testing.T) {
	t.Parallel()

	names, err := fs.Glob(migrations.FS, "*.up.sql")
	require.NoError(t, err)
	require.NotEmpty(t, names)

	var want uint
	for _, name := range names {
		prefix, _, found := strings.Cut(name, "_")
		require.True(t, found, name)
		version, err := strconv.ParseUint(prefix, 10, 64)
		require.NoError(t, err, name)
		want = max(want, uint(version))
	}

	got, err := NewestMigrationVersion()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestMigratorConfig(t *testing.T) {
	t.Parallel()

	cfg, err := ConfigFromURL("mysql://user@localhost/" + testDBName)
	require.NoError(t, err)

	got := migratorConfig(cfg)
	assert.True(t, got.MultiStatements)
	// golang-migrate waits for its lock with GET_LOCK, which the application's SELECT time limit would cut short.
	assert.Equal(t, "0", got.Params["max_execution_time"])
	assert.Equal(t, "'+00:00'", got.Params["time_zone"])

	assert.False(t, cfg.MultiStatements, "the application's config is unchanged")
	assert.Equal(t, "2000", cfg.Params["max_execution_time"])
}
