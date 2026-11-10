// Package wikidumptest gives tests the committed scrubbed copy of the legacy wiki crawl (testdata/wiki-dump), which
// is frozen: it holds no private data and is never regenerated.
package wikidumptest

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ScrubbedDir is the frozen scrubbed copy of the crawl, relative to the repository root.
const ScrubbedDir = "testdata/wiki-dump"

// RepositoryRoot returns the repository root, found from this file.
func RepositoryRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller information")
	}

	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// Scrubbed returns the committed scrubbed copy of the crawl, which every test may read: it holds no private data.
func Scrubbed(t testing.TB) fs.FS {
	t.Helper()

	return os.DirFS(filepath.Join(RepositoryRoot(t), ScrubbedDir))
}
