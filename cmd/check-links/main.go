// Command check-links checks whether the catalogue's visible links still work and looks up archived copies of them
// in the Wayback Machine (docs/PLAN.md §5.5). dokku runs it monthly from app.json's cron entry. It stops after 60
// minutes. Logs are JSON lines naming links by ID only, never by URL.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources/wayback"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/linkcheck"
)

// maxDuration is how long a run may take.
const maxDuration = 60 * time.Minute

func main() {
	_ = godotenv.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(domain.ContextWithLogger(context.Background(), logger), os.Interrupt,
		syscall.SIGTERM)
	err := run(ctx, os.Getenv("DATABASE_URL"))
	stop()

	if err != nil {
		logger.ErrorContext(ctx, "link check failed", "error", err)
		os.Exit(1)
	}
}

// run checks links once against databaseURL.
func run(ctx context.Context, databaseURL string) error {
	cfg, err := mysql.ConfigFromURL(databaseURL)
	if err != nil {
		return fmt.Errorf("parsing DATABASE_URL: %w", err)
	}
	db, err := mysql.Connect(ctx, cfg, mysql.ProductionPool())
	if err != nil {
		return fmt.Errorf("connecting to the database: %w", err)
	}
	defer func() { _ = db.Close() }()

	checker := &command.CheckLinks{
		Links: mysql.New(db), Prober: linkcheck.New(), Archive: wayback.New(linkcheck.UserAgent), Now: time.Now,
	}
	_, err = checker.Execute(ctx, command.CheckLinksRequest{MaxDuration: maxDuration})

	return err
}
