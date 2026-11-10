// Command scrub-dump replaces the inline lyrics of a crawl of the legacy wiki with placeholder lines and writes the
// deny list of the lyric n-grams it dropped (see internal/scrub). It prints only counts.
//
// Usage: scrub-dump --in DIR --out DIR
//
// It is lyric-only: names, email addresses and wiki users are copied as they are, so its output from a real crawl is
// private and must never be committed. The scrubbed copy committed under testdata/wiki-dump is frozen; it was made
// by an earlier scrubber that also replaced names, and it is never regenerated.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/scrub"
)

// ErrUsage is returned for a command line without --in or --out.
var ErrUsage = errors.New("usage: scrub-dump --in DIR --out DIR")

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "scrub-dump:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("scrub-dump", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	in := flags.String("in", "", "directory of the crawl")
	out := flags.String("out", "", "directory to write the scrubbed copy to")
	if err := flags.Parse(args); err != nil || *in == "" || *out == "" || flags.NArg() > 0 {
		return ErrUsage
	}
	d, err := mediawiki.LoadDump(os.DirFS(*in))
	if err != nil {
		return err
	}
	res := scrub.Scrub(d)
	files, err := mediawiki.EncodeDump(res.Dump)
	if err != nil {
		return err
	}
	files[scrub.DenylistFile] = res.Denylist.Encode()
	if err := os.MkdirAll(*out, 0o750); err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if err := os.WriteFile(filepath.Join(*out, name), files[name], 0o600); err != nil {
			return err
		}
	}
	s := res.Stats
	_, err = fmt.Fprintf(stdout, "lyrics blocks: poem %d, prose %d, comment %d; deny list digests %d\n", s.PoemBlocks,
		s.ProseBlocks, s.CommentBlocks, res.Denylist.Len())

	return err
}
