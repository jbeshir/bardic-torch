package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

// The command line flags.
const (
	flagDump   = "--dump"
	flagDryRun = "--dry-run"
	flagApply  = "--apply"
)

// lyricMarker marks the placeholder lines that stand in for lyrics in the scrubbed copy.
const lyricMarker = "scrubbedlyric"

// scrubbedDumpDir is the committed scrubbed copy of the crawl.
func scrubbedDumpDir(t *testing.T) string {
	t.Helper()

	return filepath.Join(wikidumptest.RepositoryRoot(t), wikidumptest.ScrubbedDir)
}

// noDatabase is the databaseURL of a run that must not read DATABASE_URL.
func noDatabase(t *testing.T) func() string {
	return func() string {
		t.Error("the run read DATABASE_URL")
		return "mysql://user:secret@localhost/bardic"
	}
}

func TestParseOptions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		args    []string
		want    options
		wantErr error
	}{
		{name: "dry_run", args: []string{flagDump, "d", flagDryRun},
			want: options{dump: "d"}},
		{name: "apply", args: []string{flagApply, flagDump, "d"},
			want: options{dump: "d", apply: true}},
		{name: "no_mode", args: []string{flagDump, "d"}, wantErr: ErrUsage},
		{name: "both_modes", args: []string{flagDump, "d", flagDryRun, flagApply},
			wantErr: ErrUsage},
		{name: "no_dump", args: []string{flagDryRun}, wantErr: ErrUsage},
		{name: "extra_argument", args: []string{flagDump, "d", flagDryRun, "now"},
			wantErr: ErrUsage},
		{name: "unknown_flag", args: []string{flagDump, "d", flagDryRun, "--force"},
			wantErr: ErrUsage},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseOptions(tc.args)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRun_DryRunNeedsNoDatabase(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	err := run(t.Context(), []string{flagDump, scrubbedDumpDir(t), flagDryRun}, noDatabase(t), &stdout)

	require.NoError(t, err)
	assert.NotContains(t, stdout.String(), "applied")
}

func TestRun_DryRunScrubbedCounts(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	err := run(t.Context(), []string{flagDump, scrubbedDumpDir(t), flagDryRun}, noDatabase(t),
		&stdout)

	require.NoError(t, err)
	// The research counts (PLAN §7, "Tests assert"): 142 pieces by kind, 11 of them with inline lyrics, 48 bards,
	// and 61 collections of the 62 entries.
	assert.Contains(t, stdout.String(), "pieces 142 (dance_music 1, poem 4, song 135, story 2)")
	assert.Contains(t, stdout.String(), "with inline lyrics 11")
	assert.Contains(t, stdout.String(), "bards 48")
	assert.Contains(t, stdout.String(), "collections 61 (of 62 entries)")
	assert.NotContains(t, stdout.String(), lyricMarker)
}
