package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/scrub"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

// The command line flags.
const (
	flagDump   = "--dump"
	flagReview = "--review"
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

	review := filepath.Join(t.TempDir(), "import-review.md")
	cases := []struct {
		name    string
		args    []string
		want    options
		wantErr error
	}{
		{name: "dry_run", args: []string{flagDump, "d", flagDryRun, flagReview, review},
			want: options{dump: "d", review: review}},
		{name: "apply", args: []string{flagApply, flagDump, "d", flagReview, review},
			want: options{dump: "d", review: review, apply: true}},
		{name: "no_mode", args: []string{flagDump, "d", flagReview, review}, wantErr: ErrUsage},
		{name: "both_modes", args: []string{flagDump, "d", flagDryRun, flagApply, flagReview, review},
			wantErr: ErrUsage},
		{name: "no_dump", args: []string{flagDryRun, flagReview, review}, wantErr: ErrUsage},
		{name: "no_review_dry_run", args: []string{flagDump, "d", flagDryRun}, wantErr: ErrUsage},
		{name: "no_review_apply", args: []string{flagDump, "d", flagApply}, wantErr: ErrUsage},
		{name: "extra_argument", args: []string{flagDump, "d", flagDryRun, flagReview, review, "now"},
			wantErr: ErrUsage},
		{name: "unknown_flag", args: []string{flagDump, "d", flagDryRun, flagReview, review, "--force"},
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

func TestRun_RefusesReviewInsideRepository(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{flagDryRun, flagApply} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			review := filepath.Join(wikidumptest.RepositoryRoot(t), "cmd", "import-mediawiki", "import-review"+mode+".md")
			err := run(t.Context(), []string{flagDump, scrubbedDumpDir(t), mode, flagReview, review}, noDatabase(t),
				&bytes.Buffer{})

			require.ErrorIs(t, err, ErrInsideRepository)
			assert.NoFileExists(t, review)
		})
	}
}

func TestRun_DryRunNeedsNoDatabase(t *testing.T) {
	t.Parallel()

	review := filepath.Join(t.TempDir(), "import-review.md")
	var stdout bytes.Buffer

	err := run(t.Context(), []string{flagDump, scrubbedDumpDir(t), flagDryRun, flagReview, review}, noDatabase(t),
		&stdout)

	require.NoError(t, err)
	info, err := os.Stat(review)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the review is private")
	content, err := os.ReadFile(filepath.Clean(review))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(content), "# Import review\n"))
	assert.NotContains(t, string(content), lyricMarker)
	assert.NotContains(t, stdout.String(), "applied")
}

func TestRun_DryRunScrubbedCounts(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	review := filepath.Join(t.TempDir(), "import-review.md")
	err := run(t.Context(), []string{flagDump, scrubbedDumpDir(t), flagDryRun, flagReview, review}, noDatabase(t),
		&stdout)

	require.NoError(t, err)
	// The research counts (PLAN §7, "Tests assert"): 142 pieces by kind, 48 bards and 61 collections, of which the
	// scrubbed copy's 4 named by a placeholder (each counts as a legal name) are withheld and listed.
	assert.Contains(t, stdout.String(), "pieces 142 (dance_music 1, poem 4, song 135, story 2)")
	assert.Contains(t, stdout.String(), "bards 48")
	assert.Contains(t, stdout.String(), "collections 57")
	content, err := os.ReadFile(filepath.Clean(review))
	require.NoError(t, err)
	assert.Equal(t, 4, strings.Count(string(content), "is not imported: its name or URL holds"))
	assert.NotContains(t, stdout.String(), lyricMarker)
}

// TestRun_RealDumpDryRun runs the dry run on the research crawl (BARDIC_RESEARCH_DIR): it succeeds, prints counts
// only, and its review holds no lyric n-gram of the deny list.
func TestRun_RealDumpDryRun(t *testing.T) {
	t.Parallel()

	dump := wikidumptest.ResearchDumpDir(t)
	review := filepath.Join(t.TempDir(), "import-review.md")
	var stdout bytes.Buffer

	wikidumptest.NoError(t, "dry run", run(t.Context(), []string{flagDump, dump, flagDryRun, flagReview, review},
		noDatabase(t), &stdout))

	denylist, err := os.ReadFile(filepath.Join(scrubbedDumpDir(t), scrub.DenylistFile))
	require.NoError(t, err)
	d, err := scrub.ParseDenylist(denylist)
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Clean(review))
	require.NoError(t, err)
	for _, text := range map[string]string{"review": string(content), "stdout": stdout.String()} {
		for _, hit := range d.Scan(text) {
			t.Errorf("deny-listed lyric n-gram at byte %d", hit.Start)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if !strings.ContainsAny(line, "0123456789") {
			t.Errorf("stdout has a line without a count")
		}
	}
	crawl, err := mediawiki.LoadDump(os.DirFS(dump))
	wikidumptest.NoError(t, "loading the crawl", err)
	names := mediawiki.NewPrivateNameMatcher(mediawiki.ParseCorpus(crawl).PrivateNames)
	for _, m := range names.Find(stdout.String()) {
		wikidumptest.Equal(t, wikidumptest.Location{Field: "standard output", Offset: m.Start}, "no private name", "name")
	}
	for _, m := range mediawiki.EmailAddress.FindAllStringIndex(stdout.String(), -1) {
		wikidumptest.Equal(t, wikidumptest.Location{Field: "standard output", Offset: m[0]}, "no email", "email")
	}
}
