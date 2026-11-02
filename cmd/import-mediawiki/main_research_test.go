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
)

// fieldStdout is the field the research mismatches on the command's standard output name.
const fieldStdout = "standard output"

// TestRun_RealDumpDryRun runs the dry run on the research crawl (BARDIC_RESEARCH_DIR): it succeeds, prints counts
// only, and its review holds no lyric n-gram of the deny list. It reports only through wikidumptest, as every test
// reading the crawl must.
func TestRun_RealDumpDryRun(t *testing.T) {
	t.Parallel()

	dump := wikidumptest.ResearchDumpDir(t)
	review := filepath.Join(t.TempDir(), "import-review.md")
	var stdout bytes.Buffer

	wikidumptest.NoError(t, "dry run", run(t.Context(), []string{flagDump, dump, flagDryRun, flagReview, review},
		noDatabase(t), &stdout))

	denylist, err := os.ReadFile(filepath.Join(scrubbedDumpDir(t), scrub.DenylistFile))
	wikidumptest.NoError(t, "reading the deny list", err)
	d, err := scrub.ParseDenylist(denylist)
	wikidumptest.NoError(t, "parsing the deny list", err)
	content, err := os.ReadFile(filepath.Clean(review))
	wikidumptest.NoError(t, "reading the review", err)
	for field, text := range map[string]string{"review": string(content), fieldStdout: stdout.String()} {
		for _, hit := range d.Scan(text) {
			wikidumptest.Equal(t, wikidumptest.Location{Field: field, Offset: hit.Start}, "no lyric", "lyric n-gram")
		}
	}
	for k, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		wikidumptest.Equal(t, wikidumptest.Location{Field: "standard output line", Offset: k}, true,
			strings.ContainsAny(line, "0123456789"))
	}
	crawl, err := mediawiki.LoadDump(os.DirFS(dump))
	wikidumptest.NoError(t, "loading the crawl", err)
	names := mediawiki.NewPrivateNameMatcher(mediawiki.ParseCorpus(crawl).PrivateNames)
	for _, m := range names.Find(stdout.String()) {
		wikidumptest.Equal(t, wikidumptest.Location{Field: fieldStdout, Offset: m.Start}, "no private name", "name")
	}
	for _, m := range mediawiki.EmailAddress.FindAllStringIndex(stdout.String(), -1) {
		wikidumptest.Equal(t, wikidumptest.Location{Field: fieldStdout, Offset: m[0]}, "no email", "email")
	}
}
