package scrub

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
	"github.com/stretchr/testify/require"
)

// repositoryFile is a text file of the repository: its path from the root, with slashes, and its content.
type repositoryFile struct {
	path string
	text string
}

// repositoryTextFiles returns every text file of the repository, skipping .git, what the root .gitignore ignores
// and binary files (a NUL byte or invalid UTF-8). File names are included as text of their own, so a name in a
// path is found too.
func repositoryTextFiles(t *testing.T) []repositoryFile {
	t.Helper()
	root := wikidumptest.RepositoryRoot(t)
	ignore := loadGitignore(t, filepath.Join(root, ".gitignore"))
	var out []repositoryFile
	err := fs.WalkDir(os.DirFS(root), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		if d.Name() == ".git" || ignore.ignored(p, d.IsDir()) {
			if d.IsDir() {
				return fs.SkipDir
			}

			return nil
		}
		out = append(out, repositoryFile{path: p + " (path)", text: p})
		if d.IsDir() {
			return nil
		}
		content, err := os.ReadFile(filepath.Clean(filepath.Join(root, filepath.FromSlash(p))))
		if err != nil {
			return err
		}
		if bytes.IndexByte(content, 0) < 0 && utf8.Valid(content) {
			out = append(out, repositoryFile{path: p, text: string(content)})
		}

		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, out)

	return out
}

// gitignore is the subset of .gitignore patterns this repository uses: base-name globs, optionally only for
// directories (a trailing slash). loadGitignore fails on any other pattern, so the walk cannot silently scan less
// or more than git does.
type gitignore struct {
	patterns []gitignorePattern
}

type gitignorePattern struct {
	glob    string
	dirOnly bool
}

func loadGitignore(t *testing.T, file string) gitignore {
	t.Helper()
	content, err := os.ReadFile(filepath.Clean(file))
	require.NoError(t, err)
	var g gitignore
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := gitignorePattern{glob: strings.TrimSuffix(line, "/"), dirOnly: strings.HasSuffix(line, "/")}
		require.False(t, strings.ContainsAny(p.glob, "/!\\") || strings.Contains(p.glob, "**"),
			".gitignore pattern %q is not a base-name glob; extend loadGitignore", line)
		_, err := path.Match(p.glob, "")
		require.NoError(t, err)
		g.patterns = append(g.patterns, p)
	}

	return g
}

func (g gitignore) ignored(p string, dir bool) bool {
	base := path.Base(p)
	for _, pattern := range g.patterns {
		if pattern.dirOnly && !dir {
			continue
		}
		if ok, _ := path.Match(pattern.glob, base); ok {
			return true
		}
	}

	return false
}

// TestRepositoryHasNoDenylistedText scans every text file of the repository, the scrubbed copy, tests, golden
// files and docs included, for the n-grams of the lyrics dropped from the scrubbed copy.
func TestRepositoryHasNoDenylistedText(t *testing.T) {
	file, err := os.ReadFile(filepath.Join(wikidumptest.RepositoryRoot(t), wikidumptest.ScrubbedDir, DenylistFile))
	require.NoError(t, err)
	d, err := ParseDenylist(file)
	require.NoError(t, err)
	require.Positive(t, d.Len())

	for _, f := range repositoryTextFiles(t) {
		for _, hit := range d.Scan(f.text) {
			t.Errorf("%s: deny-listed lyric n-gram at byte %d", f.path, hit.Start)
		}
	}
}
