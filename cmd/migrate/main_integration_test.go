//go:build integration

package main

import (
	"bytes"
	"testing"

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
		{args: []string{commandUp}, want: versionTwo},
		{args: []string{commandVersion}, want: versionTwo},
		{args: []string{commandDown, "1"}, want: "version: 1\n"},
		{args: []string{commandDown, "1"}, want: versionNone},
		{args: []string{commandVersion}, want: versionNone},
		{args: []string{commandUp}, want: versionTwo},
		{args: []string{commandDown, "2"}, want: versionNone},
		{args: []string{commandUp}, want: versionTwo},
	}
	for _, step := range steps {
		var stdout bytes.Buffer
		require.NoError(t, run(t.Context(), step.args, databaseURL, &stdout), step.args)
		assert.Equal(t, step.want, stdout.String(), step.args)
	}

	var stdout bytes.Buffer
	err := run(t.Context(), []string{commandDown, "3"}, databaseURL, &stdout)
	require.ErrorContains(t, err, "rolling back 3 migrations")
}
