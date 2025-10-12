package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/skobkin/amdgputop-web/internal/config"
	"github.com/skobkin/amdgputop-web/internal/gpu"
	"github.com/skobkin/amdgputop-web/internal/httpserver"
)

const shutdownTimeout = 10 * time.Second

// Run bootstraps the application lifecycle.
func Run(ctx context.Context, baseLogger *slog.Logger, cfg config.Config) error {
	appLogger := baseLogger.With("component", "app")

	gpus, err := gpu.Discover(cfg.SysfsRoot, baseLogger.With("component", "gpu_discovery"))
	if err != nil {
		return fmt.Errorf("discover gpus: %w", err)
	}
	appLogger.Info("discovered GPUs", "count", len(gpus))

	srv := httpserver.New(cfg, baseLogger.With("component", "http"), gpus)

	appLogger.Info("starting HTTP server", "listen_addr", cfg.ListenAddr)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		appLogger.Info("shutdown initiated", "reason", ctx.Err())

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("http shutdown: %w", err)
		}

		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}

		appLogger.Info("shutdown complete")
		return nil
	}
}
