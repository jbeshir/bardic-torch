// Command migrate applies or rolls back the embedded schema migrations against DATABASE_URL.
//
// Usage: migrate up | migrate down N | migrate version
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/joho/godotenv"
)

const usage = "usage: migrate up | migrate down N | migrate version"

// action is a command named on the command line.
type action string

const (
	actionUp      action = "up"
	actionDown    action = "down"
	actionVersion action = "version"
)

// migrateArgs is a parsed command line. steps is the number of migrations to roll back, and is only set for
// actionDown.
type migrateArgs struct {
	action action
	steps  int
}

func main() {
	_ = godotenv.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Getenv("DATABASE_URL"), os.Stdout)
	stop()

	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

// parseMigrateArgs parses the command line arguments, excluding the program name.
func parseMigrateArgs(args []string) (migrateArgs, error) {
	switch {
	case len(args) == 1 && action(args[0]) == actionUp:
		return migrateArgs{action: actionUp}, nil
	case len(args) == 1 && action(args[0]) == actionVersion:
		return migrateArgs{action: actionVersion}, nil
	case len(args) == 2 && action(args[0]) == actionDown:
		steps, err := strconv.Atoi(args[1])
		if err != nil || steps < 1 {
			return migrateArgs{}, fmt.Errorf("down needs a positive number of migrations, got [%s]\n%s", args[1], usage)
		}
		return migrateArgs{action: actionDown, steps: steps}, nil
	default:
		return migrateArgs{}, errors.New(usage)
	}
}

// run executes the command in args against databaseURL and reports the resulting schema version to stdout.
// Cancelling ctx stops after the migration in progress. Applying up succeeds without change when the schema
// is already current, and also when it is clean and newer than every embedded migration, so an older release
// can be deployed again after a newer one added migrations. A dirty schema still fails up.
func run(ctx context.Context, args []string, databaseURL string, stdout io.Writer) error {
	parsed, err := parseMigrateArgs(args)
	if err != nil {
		return err
	}

	cfg, err := mysql.ConfigFromURL(databaseURL)
	if err != nil {
		return fmt.Errorf("parsing DATABASE_URL: %w", err)
	}

	m, err := mysql.NewMigrator(cfg)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()

	stopOnCancel := context.AfterFunc(ctx, func() { m.GracefulStop <- true })
	defer stopOnCancel()

	switch parsed.action {
	case actionUp:
		return up(m, stdout)
	case actionDown:
		if err := m.Steps(-parsed.steps); err != nil {
			return fmt.Errorf("rolling back %d migrations: %w", parsed.steps, err)
		}
	case actionVersion:
	}

	return printVersion(m, stdout)
}

// up applies every pending migration, unless the schema is clean and newer than every embedded migration, as
// it is after a newer release applied migrations this binary does not have; then it applies nothing.
func up(m *migrate.Migrate, stdout io.Writer) error {
	version, dirty, versionErr := m.Version()
	if versionErr != nil && !errors.Is(versionErr, migrate.ErrNilVersion) {
		return fmt.Errorf("reading schema version: %w", versionErr)
	}
	if versionErr == nil && !dirty {
		newest, err := mysql.NewestMigrationVersion()
		if err != nil {
			return err
		}
		if version > newest {
			_, err = fmt.Fprintf(stdout,
				"version: %d (newer than this release's migrations, which end at %d; nothing applied)\n",
				version, newest)
			return err
		}
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("applying migrations: %w", err)
	}

	return printVersion(m, stdout)
}

func printVersion(m *migrate.Migrate, stdout io.Writer) error {
	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		_, err = fmt.Fprintln(stdout, "version: none")
		return err
	}
	if err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	state := ""
	if dirty {
		state = " (dirty)"
	}
	_, err = fmt.Fprintf(stdout, "version: %d%s\n", version, state)
	return err
}
