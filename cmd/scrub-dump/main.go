// Command scrub-dump writes the scrubbed copy of the research crawl of the legacy wiki that is committed under
// testdata/wiki-dump (see internal/scrub). It prints only counts, never a replaced string.
//
// Usage: scrub-dump --in DIR --out DIR [--candidates FILE]
//
// --candidates writes every possible person name of the crawl with the decision taken on it, for the privacy
// review before the copy is committed. It names real people, so it is written with mode 0600 and refused inside
// this repository.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/repopath"
	"github.com/jbeshir/bardic-torch/internal/scrub"
)

var (
	// ErrUsage is returned for a command line without --in or --out.
	ErrUsage = errors.New("usage: scrub-dump --in DIR --out DIR [--candidates FILE]")
	// ErrInsideRepository is returned when --candidates names a file inside this repository, which is public.
	ErrInsideRepository = errors.New("the candidates list names real people and must be written outside the repository")
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "scrub-dump:", err)
		os.Exit(1)
	}
}

// options is a parsed command line.
type options struct {
	in, out, candidates string
}

func parseOptions(args []string) (options, error) {
	flags := flag.NewFlagSet("scrub-dump", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var o options
	flags.StringVar(&o.in, "in", "", "directory of the crawl")
	flags.StringVar(&o.out, "out", "", "directory to write the scrubbed copy to")
	flags.StringVar(&o.candidates, "candidates", "", "file to write the private candidates list to")
	if err := flags.Parse(args); err != nil || o.in == "" || o.out == "" || flags.NArg() > 0 {
		return options{}, ErrUsage
	}
	if o.candidates != "" {
		inside, err := repopath.Inside(o.candidates)
		if err != nil {
			return options{}, err
		}
		if inside {
			return options{}, ErrInsideRepository
		}
	}

	return o, nil
}

func run(args []string, stdout io.Writer) error {
	o, err := parseOptions(args)
	if err != nil {
		return err
	}
	d, err := mediawiki.LoadDump(os.DirFS(o.in))
	if err != nil {
		return err
	}
	res, err := scrub.Scrub(d)
	if err != nil {
		return err
	}
	files, err := mediawiki.EncodeDump(res.Dump)
	if err != nil {
		return err
	}
	files[scrub.DenylistFile] = res.Denylist.Encode()
	if err := writeFiles(o.out, files); err != nil {
		return err
	}
	if o.candidates != "" {
		if err := os.WriteFile(o.candidates, candidatesList(res.Candidates), 0o600); err != nil {
			return err
		}
	}

	return printCounts(stdout, res)
}

func writeFiles(dir string, files map[string][]byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	for _, name := range sortedNames(files) {
		if err := os.WriteFile(filepath.Join(dir, name), files[name], 0o600); err != nil {
			return err
		}
	}

	return nil
}

func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// printCounts prints what was scrubbed, as counts only.
func printCounts(w io.Writer, res scrub.Result) error {
	phrases := map[string]bool{}
	reasons := map[string]int{}
	for _, c := range res.Candidates {
		reasons[c.Rule+" "+string(c.Reason)]++
		if c.Private {
			phrases[c.Placeholder] = true
		}
	}
	s := res.Stats
	lines := []string{
		fmt.Sprintf("pages kept %d, dropped %d", s.PagesKept, s.PagesDropped),
		fmt.Sprintf("deny phrases %d; names replaced %d; honorific names %d", len(phrases), s.Names, s.Honorifics),
		fmt.Sprintf("emails %d; phones %d; wiki users %d", s.Emails, s.Phones, s.WikiUsers),
		fmt.Sprintf("lyrics blocks: poem %d, prose %d, comment %d; deny list digests %d", s.PoemBlocks, s.ProseBlocks,
			s.CommentBlocks, res.Denylist.Len()),
	}
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("candidates %s: %d", k, reasons[k]))
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))

	return err
}

// candidatesList is the private candidates list: one table row per decision.
func candidatesList(decisions []scrub.CandidateDecision) []byte {
	var b strings.Builder
	b.WriteString("# Scrubber name candidates (PRIVATE: names real people; never commit)\n\n")
	b.WriteString("| page | rule | candidate | decision | reason | placeholder |\n|---|---|---|---|---|---|\n")
	cell := strings.NewReplacer("|", `\|`, "\n", " ")
	for _, c := range decisions {
		decision := "keep"
		if c.Private {
			decision = "legal"
		}
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s |\n", c.PageID, c.Rule, cell.Replace(c.Text), decision, c.Reason,
			c.Placeholder)
	}

	return []byte(b.String())
}
