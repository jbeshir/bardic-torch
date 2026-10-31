package migrations

import (
	"io/fs"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var migrationFileName = regexp.MustCompile(`^(\d{3}_[a-z0-9_]+)\.(up|down)\.sql$`)

func TestFS_EveryMigrationHasUpAndDown(t *testing.T) {
	t.Parallel()

	names, err := fs.Glob(FS, "*")
	require.NoError(t, err)
	require.NotEmpty(t, names)

	directions := map[string][]string{}
	for _, name := range names {
		match := migrationFileName.FindStringSubmatch(name)
		require.NotNil(t, match, "unexpected file name [%s]", name)
		directions[match[1]] = append(directions[match[1]], match[2])
	}

	for migration, got := range directions {
		assert.ElementsMatch(t, []string{"up", "down"}, got, "migration [%s]", migration)
	}
}
