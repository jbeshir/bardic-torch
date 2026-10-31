//go:build integration

package main

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	versionTwo  = "version: 2\n"
	versionNone = "version: none\n"
)

func TestMigrateRun_UpVersionDown(t *testing.T) {
	t.Parallel()

	databaseURL := mysqltest.NewDatabaseURL(t)

	steps := []struct {
		args []string
		want string
	}{
		// The database starts fully migrated, so up has nothing to apply and still succeeds.
		{args: []string{string(actionUp)}, want: versionTwo},
		{args: []string{string(actionVersion)}, want: versionTwo},
		{args: []string{string(actionDown), "1"}, want: "version: 1\n"},
		{args: []string{string(actionDown), "1"}, want: versionNone},
		{args: []string{string(actionVersion)}, want: versionNone},
		{args: []string{string(actionUp)}, want: versionTwo},
		{args: []string{string(actionDown), "2"}, want: versionNone},
		{args: []string{string(actionUp)}, want: versionTwo},
	}
	for _, step := range steps {
		var stdout bytes.Buffer
		require.NoError(t, run(t.Context(), step.args, databaseURL, &stdout), step.args)
		assert.Equal(t, step.want, stdout.String(), step.args)
	}

	var stdout bytes.Buffer
	err := run(t.Context(), []string{string(actionDown), "3"}, databaseURL, &stdout)
	require.ErrorContains(t, err, "rolling back 3 migrations")
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
	db, err := mysql.Connect(t.Context(), cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(t.Context(), "UPDATE schema_migrations SET version = ?, dirty = ?", version, dirty)
	require.NoError(t, err)
}
