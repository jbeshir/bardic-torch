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

	waitForShutdownSignal(ctx)
}

// waitForShutdownSignal blocks until the process receives SIGINT or SIGTERM.
func waitForShutdownSignal(ctx context.Context) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
}
