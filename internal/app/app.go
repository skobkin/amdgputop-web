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
	"github.com/skobkin/amdgputop-web/internal/sampler"
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

	readers := make(map[string]*sampler.Reader, len(gpus))
	for _, info := range gpus {
		readerLogger := baseLogger.With("component", "sampler_reader", "gpu_id", info.ID)
		reader, err := sampler.NewReader(info.ID, cfg.SysfsRoot, cfg.DebugfsRoot, readerLogger)
		if err != nil {
			appLogger.Warn("failed to initialise metrics reader", "gpu_id", info.ID, "err", err)
			continue
		}
		readers[info.ID] = reader
	}

	if len(gpus) > 0 && len(readers) == 0 {
		appLogger.Warn("no metrics readers initialised", "reason", "sysfs access failed")
	}

	samplerManager, err := sampler.NewManager(cfg.SampleInterval, readers, baseLogger.With("component", "sampler"))
	if err != nil {
		return fmt.Errorf("init sampler manager: %w", err)
	}

	samplerCtx, samplerCancel := context.WithCancel(ctx)
	defer samplerCancel()

	samplerErrCh := make(chan error, 1)
	go func() {
		samplerErrCh <- samplerManager.Run(samplerCtx)
	}()

	srv := httpserver.New(cfg, baseLogger.With("component", "http"), gpus, samplerManager)

	appLogger.Info("starting HTTP server", "listen_addr", cfg.ListenAddr)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	for {
		select {
		case err := <-errCh:
			samplerCancel()
			if err != nil {
				return err
			}
			if samplerErrCh != nil {
				if samplerErr := <-samplerErrCh; samplerErr != nil && !errors.Is(samplerErr, context.Canceled) {
					return samplerErr
				}
			}
			return nil
		case err := <-samplerErrCh:
			samplerErrCh = nil
			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
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

			samplerCancel()
			if samplerErrCh != nil {
				if samplerErr := <-samplerErrCh; samplerErr != nil && !errors.Is(samplerErr, context.Canceled) {
					return samplerErr
				}
			}

			appLogger.Info("shutdown complete")
			return nil
		}
	}
}
