package wikidumptest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// forbiddenInResearchTests are the ways a test could print a value; a test that reads the research crawl may
// report only through Equal and NoError.
var forbiddenInResearchTests = regexp.MustCompile(`"github\.com/stretchr/testify|\bt\.(Log|Logf|Error|Errorf|` +
	`Fatal|Fatalf|Skipf)\(|\bfmt\.|\bprint(ln)?\(|\bos\.(Stdout|Stderr)\b|\blog\.|\bslog\.`)

func TestResearchTestsReportOnlyThroughEqual(t *testing.T) {
	repo := os.DirFS(repositoryRoot(t))
	var researchFiles int
	err := fs.WalkDir(repo, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") || strings.HasPrefix(path, "internal/testutil/wikidumptest/") {
			return nil
		}
		src, err := fs.ReadFile(repo, path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(src), "wikidumptest.Research(") {
			return nil
		}
		researchFiles++
		if loc := forbiddenInResearchTests.FindIndex(src); loc != nil {
			t.Errorf("%s reads the research crawl but reports with %q; use wikidumptest.Equal or NoError", path,
				src[loc[0]:loc[1]])
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if researchFiles == 0 {
		t.Error("no test reads the research crawl, so this check would pass vacuously")
	}
}

func TestResearch_SkipsWhenUnsetOrEmpty(t *testing.T) {
	t.Setenv(ResearchDirEnv, "")
	skipped := runSkips(t, func(tb testing.TB) { Research(tb) })
	if !skipped {
		t.Error("Research did not skip with an empty " + ResearchDirEnv)
	}
}

func runSkips(t *testing.T, f func(testing.TB)) bool {
	t.Helper()
	var skipped bool
	t.Run("inner", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		f(t)
	})

	return skipped
}

func TestEqual_ReportsDigestsNotValues(t *testing.T) {
	rec := &recordingTB{TB: t}
	if Equal(rec, Location{PageID: 7, Field: "sections.heading", Offset: 2}, "Wren Placeholder", "Ash Placeholder") {
		t.Fatal("Equal reported different values as equal")
	}
	if len(rec.messages) != 1 {
		t.Fatalf("Equal reported %d messages, want 1", len(rec.messages))
	}
	msg := rec.messages[0]
	if strings.Contains(msg, "Placeholder") || !strings.Contains(msg, "page 7, field sections.heading, offset 2") {
		t.Errorf("Equal's message %q shows a value or lacks the location", msg)
	}
	if !Equal(rec, Location{}, []string{"a"}, []string{"a"}) || len(rec.messages) != 1 {
		t.Error("Equal reported equal values as different")
	}
}

func TestNoError_ReportsTypeNotMessage(t *testing.T) {
	rec := &recordingTB{TB: t}
	NoError(rec, "dump", nil)
	if len(rec.messages) != 0 {
		t.Fatal("NoError reported a nil error")
	}
	NoError(rec, "dump", errors.New("Wren Placeholder"))
	if len(rec.messages) != 1 || strings.Contains(rec.messages[0], "Placeholder") {
		t.Errorf("NoError's messages %q show the error's text", rec.messages)
	}
}

// recordingTB records failures instead of failing.
type recordingTB struct {
	testing.TB
	messages []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's directory")
		}
		dir = parent
	}
}
