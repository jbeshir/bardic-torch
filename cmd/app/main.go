package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jbeshir/bardic-torch/internal/app"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/joho/godotenv"
	"golang.org/x/sync/errgroup"
)

func main() {
	_ = godotenv.Load()
	ctx := context.Background()

	var logLevel slog.Level
	logLevelStr := app.MustGetEnvAsString(ctx, "LOG_LEVEL")
	if err := logLevel.UnmarshalText([]byte(logLevelStr)); err != nil {
		panic(fmt.Sprintf("unable to setup logger, LOG_LEVEL not recognised [%s]", logLevelStr))
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)
	ctx = domain.ContextWithLogger(ctx, logger)

	if err := run(ctx); err != nil {
		logger.ErrorContext(ctx, "shutting down due to error", "error", err)
		os.Exit(1)
	}
}

// run sets up and runs every component until one fails or the process receives SIGINT or SIGTERM, which
// shuts the components down gracefully.
func run(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	components, err := app.Setup(ctx)
	if err != nil {
		return fmt.Errorf("setting up components: %w", err)
	}

	grp, grpCtx := errgroup.WithContext(ctx)
	for _, c := range components {
		grp.Go(func() error {
			return c.Run(grpCtx)
		})
	}

	return grp.Wait()
}
