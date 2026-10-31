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

// Command names accepted on the command line.
const (
	commandUp      = "up"
	commandDown    = "down"
	commandVersion = "version"
)

type action int

const (
	actionUp action = iota
	actionDown
	actionVersion
)

// migrateArgs is a parsed command line. Steps is the number of migrations to roll back, and is only set for
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
	case len(args) == 1 && args[0] == commandUp:
		return migrateArgs{action: actionUp}, nil
	case len(args) == 1 && args[0] == commandVersion:
		return migrateArgs{action: actionVersion}, nil
	case len(args) == 2 && args[0] == commandDown:
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
// Cancelling ctx stops after the migration in progress. Applying up when the schema is already current
// succeeds.
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
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("applying migrations: %w", err)
		}
	case actionDown:
		if err := m.Steps(-parsed.steps); err != nil {
			return fmt.Errorf("rolling back %d migrations: %w", parsed.steps, err)
		}
	case actionVersion:
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
