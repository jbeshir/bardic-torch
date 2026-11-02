package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/scrub"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The command line flags.
const (
	flagIn         = "--in"
	flagOut        = "--out"
	flagCandidates = "--candidates"
)

// legalAlias is a synthetic bard's legal name, which the scrubber replaces.
const legalAlias = "Quill Fakename"

// writeDump writes a synthetic crawl to a temporary directory.
func writeDump(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	page := `{"ns":0,"title":"Wren Placeholder","pageid":1,"categories":["Bards"],"hidden_categories":[],` +
		`"wikitext":"= Wren Placeholder (` + legalAlias + `) =\nMail quill@example.com\n== Lyrics ==\n` +
		`<poem>\nline one\nline two\n</poem>\n","url":"https://wiki.example/wiki/Wren_Placeholder",` +
		`"revid":5,"last_rev_timestamp":"2025-01-02T03:04:05Z","last_rev_user":"Fern Handle"}` + "\n" +
		`{"ns":2,"title":"User:Fern Handle","pageid":2,"categories":[],"wikitext":"me"}` + "\n"
	files := map[string]string{
		mediawiki.PagesFile:      page,
		mediawiki.CategoriesFile: `{"categories":{}}`,
		mediawiki.RedirectsFile:  `{"redirect_pages":[{"from":"` + legalAlias + `","ns":0,"to":"Wren Placeholder"}]}`,
		mediawiki.ActivityFile:   `{"per_page":[{"title":"Wren Placeholder","ns":0,"revisions":1,"users":{"Fern Handle":1}}]}`,
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	return dir
}

func TestRun_WritesScrubbedFiles(t *testing.T) {
	in, out := writeDump(t), filepath.Join(t.TempDir(), "wiki-dump")
	candidates := filepath.Join(t.TempDir(), "candidates.md")
	require.NoError(t, os.WriteFile(candidates, nil, 0o600))
	info, err := os.Stat(candidates)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(candidates, info.Mode()|0o044), "readable by everyone, as a file left there could be")
	var stdout bytes.Buffer

	require.NoError(t, run([]string{flagIn, in, flagOut, out, flagCandidates, candidates}, &stdout))

	d, err := mediawiki.LoadDump(os.DirFS(out))
	require.NoError(t, err)
	require.Len(t, d.Pages, 1)
	assert.Equal(t, "= Wren Placeholder (Legal Name 001) =\nMail email-001@example.invalid\n== Lyrics ==\n<poem>\n"+
		scrub.LyricPlaceholderLine+"\n"+scrub.LyricPlaceholderLine+"\n</poem>\n", d.Pages[0].Wikitext)
	assert.Equal(t, []mediawiki.Redirect{{From: "Legal Name 001", To: "Wren Placeholder"}}, d.Redirects)
	assert.Equal(t, map[string]int{"Wiki User 01": 1}, d.Activity[0].Users)
	denylist, err := os.ReadFile(filepath.Clean(filepath.Join(out, scrub.DenylistFile)))
	require.NoError(t, err)
	parsed, err := scrub.ParseDenylist(denylist)
	require.NoError(t, err)
	assert.Zero(t, parsed.Len(), "two words are no 8-word window")

	list, err := os.ReadFile(filepath.Clean(candidates))
	require.NoError(t, err)
	assert.Contains(t, string(list), "| 1 | h1_parenthetical | "+legalAlias+" | legal | hidden_alias | Legal Name 001 |")
	info, err = os.Stat(candidates)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "private, also over a 0644 file")
}

func TestRun_PrintsNoReplacedStrings(t *testing.T) {
	var stdout bytes.Buffer

	require.NoError(t, run([]string{flagIn, writeDump(t), flagOut, t.TempDir()}, &stdout))

	out := stdout.String()
	assert.Contains(t, out, "pages kept 1, dropped 1")
	assert.Contains(t, out, "emails 1; phones 0; wiki users 1")
	for _, private := range []string{"Quill", "Fakename", "quill@", "line one", "Fern"} {
		assert.NotContains(t, out, private)
	}
}

func TestRun_RefusesCandidatesInsideRepository(t *testing.T) {
	inside := filepath.Join(wikidumptest.RepositoryRoot(t), "docs", "candidates.md")
	out := t.TempDir()

	err := run([]string{flagIn, writeDump(t), flagOut, out, flagCandidates, inside}, &bytes.Buffer{})

	require.ErrorIs(t, err, ErrInsideRepository)
	_, statErr := os.Stat(inside)
	assert.True(t, os.IsNotExist(statErr))
	entries, err := os.ReadDir(out)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing is written")
}

// TestRun_RefusesCandidatesIntoRepositoryThroughSymlink covers a --candidates outside the repository that is a
// symbolic link to a file in it, which does not exist yet.
func TestRun_RefusesCandidatesIntoRepositoryThroughSymlink(t *testing.T) {
	target := filepath.Join(wikidumptest.RepositoryRoot(t), "docs", "leaked-candidates.md")
	link := filepath.Join(t.TempDir(), "candidates.md")
	require.NoError(t, os.Symlink(target, link))

	err := run([]string{flagIn, writeDump(t), flagOut, t.TempDir(), flagCandidates, link}, &bytes.Buffer{})

	require.ErrorIs(t, err, ErrInsideRepository)
	assert.NoFileExists(t, target)
}

func TestRun_Usage(t *testing.T) {
	for _, args := range [][]string{nil, {flagIn, "x"}, {flagOut, "y"}, {flagIn, "x", flagOut, "y", "extra"},
		{"--unknown"}} {
		require.ErrorIs(t, run(args, &bytes.Buffer{}), ErrUsage, strings.Join(args, " "))
	}
}

func TestRun_MalformedDump(t *testing.T) {
	err := run([]string{flagIn, t.TempDir(), flagOut, t.TempDir()}, &bytes.Buffer{})

	require.ErrorIs(t, err, mediawiki.ErrMalformedDump)
}
