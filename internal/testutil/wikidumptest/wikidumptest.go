// Package wikidumptest gives tests the real research crawl of the legacy wiki, which is private (it holds legal
// names and lyrics) and is never committed, and the one way research tests may report a difference: by page ID,
// field and offset, with digests in place of values, so that no research value reaches test output or logs.
package wikidumptest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ScrubbedDir is the scrubbed copy of the crawl (written by cmd/scrub-dump) relative to the repository root.
const ScrubbedDir = "testdata/wiki-dump"

// ResearchDirEnv names the directory holding the research output: its dump/ and analysis/ directories. Unset or
// empty, research tests skip.
const ResearchDirEnv = "BARDIC_RESEARCH_DIR"

// Research returns the research crawl (dump) and the reference analysis of it (analysis), or skips the test when
// ResearchDirEnv is unset or empty.
func Research(t testing.TB) (dump, analysis fs.FS) {
	t.Helper()
	dir := researchDir(t)

	return os.DirFS(filepath.Join(dir, "dump")), os.DirFS(filepath.Join(dir, "analysis"))
}

// ResearchDumpDir returns the directory of the research crawl, for a command that reads a directory, or skips the
// test when ResearchDirEnv is unset or empty.
func ResearchDumpDir(t testing.TB) string {
	t.Helper()

	return filepath.Join(researchDir(t), "dump")
}

func researchDir(t testing.TB) string {
	t.Helper()
	dir := os.Getenv(ResearchDirEnv)
	if dir == "" {
		t.Skip(ResearchDirEnv + " is not set")
	}

	return dir
}

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

// Location names a research value without showing it: the page, the field and an offset (a byte offset or an
// index into a list, whichever the field has; -1 for none).
type Location struct {
	PageID int64
	Field  string
	Offset int
}

// Equal reports whether want and got have the same JSON encoding. If not, it fails the test with the location
// and short digests of both encodings, never the values themselves. It is the only way research tests report.
func Equal(t testing.TB, at Location, want, got any) bool {
	t.Helper()
	wantDigest, gotDigest := digest(t, want), digest(t, got)
	if wantDigest == gotDigest {
		return true
	}
	t.Errorf("research mismatch: page %d, field %s, offset %d (want digest %s, got digest %s)",
		at.PageID, at.Field, at.Offset, wantDigest, gotDigest)

	return false
}

func digest(t testing.TB, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("research value of type %T does not encode as JSON", v)
	}
	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:6])
}

// NoError fails the test if err is not nil, naming what failed and the error's type but not its message, which
// may quote research data.
func NoError(t testing.TB, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("research %s failed: %T", what, err)
	}
}
