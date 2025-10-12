package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/skobkin/amdgputop-web/internal/app"
	"github.com/skobkin/amdgputop-web/internal/config"
	"github.com/skobkin/amdgputop-web/internal/version"
)

var (
	buildVersion = "dev"
	buildCommit  = ""
	buildTime    = ""
)

func main() {
	version.Set(version.Info{
		Version:   buildVersion,
		Commit:    buildCommit,
		BuildTime: buildTime,
	})

	cfg, err := config.Load()
	if err != nil {
		handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})
		slog.New(handler).Error("failed to load configuration", "err", err)
		os.Exit(1)
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel})
	logger := slog.New(handler)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, logger, cfg); err != nil {
		logger.Error("application error", "err", err)
		os.Exit(1)
	}
}
