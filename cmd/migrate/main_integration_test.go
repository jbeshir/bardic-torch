//go:build integration

package main

import (
	"bytes"
	"fmt"
	"strconv"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const versionNone = "version: none\n"

func TestMigrateRun_UpVersionDown(t *testing.T) {
	t.Parallel()

	databaseURL := mysqltest.NewDatabaseURL(t)
	newest, err := mysql.NewestMigrationVersion()
	require.NoError(t, err)
	require.Greater(t, newest, uint(1), "the steps below need at least two migrations")
	versionNewest := fmt.Sprintf("version: %d\n", newest)
	allSteps := strconv.FormatUint(uint64(newest), 10)

	steps := []struct {
		args []string
		want string
	}{
		// The database starts fully migrated, so up has nothing to apply and still succeeds.
		{args: []string{string(actionUp)}, want: versionNewest},
		{args: []string{string(actionVersion)}, want: versionNewest},
		{args: []string{string(actionDown), "1"}, want: fmt.Sprintf("version: %d\n", newest-1)},
		{args: []string{string(actionDown), strconv.FormatUint(uint64(newest-1), 10)}, want: versionNone},
		{args: []string{string(actionVersion)}, want: versionNone},
		{args: []string{string(actionUp)}, want: versionNewest},
		{args: []string{string(actionDown), allSteps}, want: versionNone},
		{args: []string{string(actionUp)}, want: versionNewest},
	}
	for _, step := range steps {
		var stdout bytes.Buffer
		require.NoError(t, run(t.Context(), step.args, databaseURL, &stdout), step.args)
		assert.Equal(t, step.want, stdout.String(), step.args)
	}

	var stdout bytes.Buffer
	err = run(t.Context(), []string{string(actionDown), strconv.FormatUint(uint64(newest+1), 10)}, databaseURL,
		&stdout)
	require.ErrorContains(t, err, fmt.Sprintf("rolling back %d migrations", newest+1))
}

// An older release's predeploy runs its own up against a schema a newer release migrated further, which must
// succeed so the older release can be deployed again.
func TestMigrateRun_UpWithNewerSchemaIsNoChange(t *testing.T) {
	t.Parallel()

	databaseURL := mysqltest.NewDatabaseURL(t)
	newest, err := mysql.NewestMigrationVersion()
	require.NoError(t, err)
	newer := newest + 1
	setSchemaVersion(t, databaseURL, newer, false)

	var stdout bytes.Buffer
	require.NoError(t, run(t.Context(), []string{string(actionUp)}, databaseURL, &stdout))
	assert.Equal(t,
		fmt.Sprintf("version: %d (newer than this release's migrations, which end at %d; nothing applied)\n",
			newer, newest),
		stdout.String())

	stdout.Reset()
	require.NoError(t, run(t.Context(), []string{string(actionVersion)}, databaseURL, &stdout))
	assert.Equal(t, fmt.Sprintf("version: %d\n", newer), stdout.String())
}

func TestMigrateRun_UpWithDirtySchemaFails(t *testing.T) {
	t.Parallel()

	newest, err := mysql.NewestMigrationVersion()
	require.NoError(t, err)

	for _, version := range []uint{newest, newest + 1} {
		databaseURL := mysqltest.NewDatabaseURL(t)
		setSchemaVersion(t, databaseURL, version, true)

		var stdout bytes.Buffer
		err := run(t.Context(), []string{string(actionUp)}, databaseURL, &stdout)
		var dirtyErr migrate.ErrDirty
		require.ErrorAs(t, err, &dirtyErr, version)
		assert.EqualValues(t, version, dirtyErr.Version, version)
	}
}

// setSchemaVersion records version and dirty in golang-migrate's schema_migrations table, as a newer
// release's migrations or a failed migration would leave it.
func setSchemaVersion(t *testing.T, databaseURL string, version uint, dirty bool) {
	t.Helper()

	cfg, err := mysql.ConfigFromURL(databaseURL)
	require.NoError(t, err)
	db, err := mysql.Connect(t.Context(), cfg, mysqltest.Pool())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(t.Context(), "UPDATE schema_migrations SET version = ?, dirty = ?", version, dirty)
	require.NoError(t, err)
}
