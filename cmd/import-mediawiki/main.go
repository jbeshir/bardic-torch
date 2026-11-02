// Command import-mediawiki imports the crawl of the legacy MediaWiki site (internal/mediawiki) into the catalogue
// (PLAN §7).
//
// Usage: import-mediawiki --dump DIR (--dry-run | --apply) --review PATH
//
// Both modes parse the crawl in DIR, build the import (internal/importer), validate it and write the review for
// the moderators to PATH, then print the counts. The review names hidden aliases and other possible legal names,
// so it is written with mode 0600 and refused inside this repository, which is public. --dry-run stops there: it
// never reads DATABASE_URL or opens a database. --apply then writes the import into the empty catalogue at
// DATABASE_URL in one transaction, and refuses a catalogue that already holds rows.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/importer"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/repopath"
)

var (
	// ErrUsage is returned for a command line without --dump or --review, or without exactly one mode.
	ErrUsage = errors.New("usage: import-mediawiki --dump DIR (--dry-run | --apply) --review PATH")
	// ErrInsideRepository is returned when --review names a file inside this repository, which is public.
	ErrInsideRepository = errors.New("the review names hidden aliases and must be written outside the repository")
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], databaseURL, os.Stdout)
	stop()

	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "import-mediawiki:", err)
		os.Exit(1)
	}
}

// databaseURL returns DATABASE_URL, loading .env first as the other commands do.
func databaseURL() string {
	_ = godotenv.Load()

	return os.Getenv("DATABASE_URL")
}

// options is a parsed command line.
type options struct {
	dump, review string
	// apply is whether to write the import (--apply) rather than only build it (--dry-run).
	apply bool
}

func parseOptions(args []string) (options, error) {
	flags := flag.NewFlagSet("import-mediawiki", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var o options
	var dryRun bool
	flags.StringVar(&o.dump, "dump", "", "directory of the crawl")
	flags.StringVar(&o.review, "review", "", "file to write the review to, outside the repository")
	flags.BoolVar(&dryRun, "dry-run", false, "build the import and the review without a database")
	flags.BoolVar(&o.apply, "apply", false, "build the import and write it into the empty catalogue")
	if err := flags.Parse(args); err != nil || o.dump == "" || o.review == "" || dryRun == o.apply ||
		flags.NArg() > 0 {
		return options{}, ErrUsage
	}
	inside, err := repopath.Inside(o.review)
	if err != nil {
		return options{}, err
	}
	if inside {
		return options{}, ErrInsideRepository
	}

	return o, nil
}

// run executes the command line args, reporting counts to stdout. databaseURL is called for --apply only.
func run(ctx context.Context, args []string, databaseURL func() string, stdout io.Writer) error {
	o, err := parseOptions(args)
	if err != nil {
		return err
	}
	d, err := mediawiki.LoadDump(os.DirFS(o.dump))
	if err != nil {
		return err
	}
	imp, review := importer.Build(mediawiki.ParseCorpus(d), importer.BuildOptions{Now: time.Now().UTC()})
	if err := imp.Validate(); err != nil {
		return err
	}
	if err := writeReview(o.review, importer.RenderReview(review)); err != nil {
		return err
	}
	if err := printCounts(stdout, review); err != nil {
		return err
	}
	if !o.apply {
		return nil
	}

	cfg, err := mysql.ConfigFromURL(databaseURL())
	if err != nil {
		return fmt.Errorf("parsing DATABASE_URL: %w", err)
	}
	db, err := mysql.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if _, err := (&command.ApplyCatalogueImport{Importer: mysql.New(db)}).Execute(ctx, imp); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, "applied: the catalogue holds the import")

	return err
}

// writeReview writes the review to path with mode 0600, also when the file exists with another mode.
func writeReview(path string, review []byte) error {
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("writing the review: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing the review: %w", err)
	}
	if _, err := f.Write(review); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing the review: %w", err)
	}

	return f.Close()
}

// printCounts prints what the import holds and how many review items there are, as counts only.
func printCounts(w io.Writer, r importer.Review) error {
	c := r.Counts
	kinds := make([]string, 0, len(c.PiecesByKind))
	for kind := range c.PiecesByKind {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	pieces, byKind := 0, make([]string, 0, len(kinds))
	for _, kind := range kinds {
		pieces += c.PiecesByKind[kind]
		byKind = append(byKind, fmt.Sprintf("%s %d", kind, c.PiecesByKind[kind]))
	}
	lines := []string{
		fmt.Sprintf("pieces %d (%s); stub pieces %d; with inline lyrics %d", pieces, strings.Join(byKind, ", "),
			c.StubPieces, c.InlineLyricsPieces),
		fmt.Sprintf("persons %d (bards %d, stubs %d)", c.Persons, c.Bards, c.StubPersons),
		fmt.Sprintf("terms %d; collections %d (of %d entries); events %d", c.Terms, c.Collections,
			c.CollectionEntries, c.Events),
		fmt.Sprintf("links %d (hidden %d); revisions %d", c.Links, c.HiddenLinks, c.Revisions),
		fmt.Sprintf("review items %d", len(r.Items())),
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))

	return err
}
