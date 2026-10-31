package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMigrateArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		args    []string
		want    migrateArgs
		wantErr bool
	}{
		{name: commandUp, args: []string{commandUp}, want: migrateArgs{action: actionUp}},
		{name: commandVersion, args: []string{commandVersion}, want: migrateArgs{action: actionVersion}},
		{name: "down_one", args: []string{commandDown, "1"}, want: migrateArgs{action: actionDown, steps: 1}},
		{name: "down_many", args: []string{commandDown, "12"}, want: migrateArgs{action: actionDown, steps: 12}},
		{name: "no_args", args: nil, wantErr: true},
		{name: "unknown_command", args: []string{"drop"}, wantErr: true},
		{name: "down_without_count", args: []string{commandDown}, wantErr: true},
		{name: "down_zero", args: []string{commandDown, "0"}, wantErr: true},
		{name: "down_negative", args: []string{commandDown, "-1"}, wantErr: true},
		{name: "down_not_a_number", args: []string{commandDown, "all"}, wantErr: true},
		{name: "up_with_count", args: []string{commandUp, "1"}, wantErr: true},
		{name: "version_with_extra", args: []string{commandVersion, "now"}, wantErr: true},
		{name: "down_with_extra", args: []string{commandDown, "1", "2"}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseMigrateArgs(tc.args)
			if tc.wantErr {
				assert.ErrorContains(t, err, usage)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRun_InvalidArgsFailBeforeConnecting(t *testing.T) {
	t.Parallel()

	err := run(t.Context(), []string{"sideways"}, "not a database URL", nil)
	assert.ErrorContains(t, err, usage)
}

func TestRun_InvalidDatabaseURL(t *testing.T) {
	t.Parallel()

	err := run(t.Context(), []string{commandVersion}, "postgres://user@localhost/db", nil)
	assert.ErrorContains(t, err, "parsing DATABASE_URL")
}
