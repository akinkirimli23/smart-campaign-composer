// Command api serves the Smart Campaign Composer HTTP API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/config"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/db"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/httpapi"
)

func main() {
	// Default logger until the configured level is known, so a config error is
	// still logged as JSON.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("api exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Migrations are idempotent and lock-protected, so running them on every
	// start is safe even if the init container already did.
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}

	// NewHTTPServer cancels request contexts on Shutdown, so open SSE streams
	// do not hold the process for the whole ShutdownTimeout.
	srv := httpapi.NewHTTPServer(cfg.HTTPAddr, httpapi.New(pool))

	errCh := make(chan error, 1)
	go func() {
		slog.Info("api listening", "addr", cfg.HTTPAddr, "real_llm", cfg.UseRealLLM())
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
