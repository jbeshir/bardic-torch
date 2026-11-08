// Command check-links checks whether the catalogue's visible links still work and looks up archived copies of them
// in the Wayback Machine (docs/PLAN.md §3, "cmd/check-links"). dokku runs it daily from app.json's cron entry.
//
// Usage: check-links [--limit N] [--archive-limit N] [--max-duration D]
//
// One run at a time holds the database's link check lock; a run that finds it held logs that and exits 0. A run
// whose first checks mostly fail transiently (the network is down) records none of them and exits 1, as does one
// whose first archive lookups mostly fail. Logs are JSON lines naming links by ID only, never by URL.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources/wayback"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/linkcheck"
)

// waybackAvailabilityURL is the Wayback Machine's availability API.
const waybackAvailabilityURL = "https://archive.org/wayback/available"

// The fixed settings of a run.
const (
	// workers is how many links are checked at once.
	workers = 4
	// archiveSpacing is the least time between two archive lookups.
	archiveSpacing = time.Second
	// lockPingInterval is how often the run checks that it still holds the lock.
	lockPingInterval = time.Minute
	// waybackTimeout bounds one archive lookup.
	waybackTimeout = 10 * time.Second
)

// ErrUsage is returned for a command line that does not parse.
var ErrUsage = errors.New("usage: check-links [--limit N] [--archive-limit N] [--max-duration D]")

func main() {
	_ = godotenv.Load()

	logger, err := newLogger(os.Getenv("LOG_LEVEL"), os.Stdout)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "check-links:", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(domain.ContextWithLogger(context.Background(), logger), os.Interrupt,
		syscall.SIGTERM)
	err = run(ctx, os.Args[1:], os.Getenv("DATABASE_URL"))
	stop()

	if err != nil {
		logger.ErrorContext(ctx, "link check failed", "error", err)
		os.Exit(1)
	}
}

// newLogger returns a JSON logger at level, INFO if level is empty.
func newLogger(level string, w io.Writer) (*slog.Logger, error) {
	var logLevel slog.Level
	if level != "" {
		if err := logLevel.UnmarshalText([]byte(level)); err != nil {
			return nil, fmt.Errorf("LOG_LEVEL not recognised [%s]", level)
		}
	}

	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: logLevel})), nil
}

// options is a parsed command line.
type options struct {
	limit, archiveLimit int32
	maxDuration         time.Duration
}

func parseOptions(args []string) (options, error) {
	flags := flag.NewFlagSet("check-links", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	o := options{limit: 400, archiveLimit: 200}
	flags.Func("limit", "most links to check (400)", int32Flag(&o.limit))
	flags.Func("archive-limit", "most archive lookups to make (200)", int32Flag(&o.archiveLimit))
	flags.DurationVar(&o.maxDuration, "max-duration", 20*time.Minute, "how long the run may check and look up")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return options{}, ErrUsage
	}
	if o.limit < 1 || o.limit > 10000 || o.archiveLimit < 0 || o.archiveLimit > 10000 ||
		o.maxDuration < time.Minute || o.maxDuration > 6*time.Hour {
		return options{}, fmt.Errorf("%w: --limit 1 to 10000, --archive-limit 0 to 10000, --max-duration 1m to 6h",
			ErrUsage)
	}

	return o, nil
}

// int32Flag returns a flag.Func parser setting *v to a decimal int32.
func int32Flag(v *int32) func(string) error {
	return func(s string) error {
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return ErrUsage
		}
		*v = int32(n)
		return nil
	}
}

// request returns the command's request for o.
func (o options) request() command.CheckLinksRequest {
	return command.CheckLinksRequest{
		Limit: o.limit, ArchiveLimit: o.archiveLimit, Workers: workers,
		MaxDuration: o.maxDuration, ArchiveSpacing: archiveSpacing, LockPingInterval: lockPingInterval,
	}
}

// run checks links once against databaseURL.
func run(ctx context.Context, args []string, databaseURL string) error {
	o, err := parseOptions(args)
	if err != nil {
		return err
	}
	cfg, err := mysql.ConfigFromURL(databaseURL)
	if err != nil {
		return fmt.Errorf("parsing DATABASE_URL: %w", err)
	}
	db, err := mysql.Connect(ctx, cfg, mysql.LinkCheckPool(o.maxDuration))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	result, err := newCheckLinks(mysql.New(db)).Execute(ctx, o.request())
	logResult(ctx, result)

	return err
}

// newCheckLinks returns the production link checker over repository.
func newCheckLinks(repository *mysql.Repository) *command.CheckLinks {
	return &command.CheckLinks{
		Locker: repository,
		Links:  repository,
		Prober: linkcheck.New(linkcheck.Config{
			UserAgent: linkcheck.UserAgent, AllowAddress: linkcheck.PublicAddress, Resolver: net.DefaultResolver,
			Dial: linkcheck.GuardedDial(linkcheck.PublicAddress), Deadline: linkcheck.LinkDeadline,
			HostInterval: linkcheck.HostInterval,
		}),
		Archive: wayback.New(wayback.Config{BaseURL: waybackAvailabilityURL, UserAgent: linkcheck.UserAgent},
			&http.Client{Timeout: waybackTimeout, Transport: &http.Transport{Proxy: nil}}),
		Now: time.Now,
	}
}

// logResult logs a run's counts.
func logResult(ctx context.Context, r command.CheckLinksResult) {
	if r.LockHeld {
		return
	}
	domain.LoggerFromContext(ctx).InfoContext(ctx, "link check finished",
		"checked", r.Outcomes, "not_recorded", r.NotRecorded, "archive_lookups", r.ArchiveLookups,
		"archives_found", r.ArchivesFound, "stopped_at_max_duration", r.StoppedAtMaxDuration)
}
