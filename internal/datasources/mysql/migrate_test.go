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
