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

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/audience"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/config"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/db"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/httpapi"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm/anthropicllm"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm/mockllm"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
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

	var model llm.Client = mockllm.Client{}
	if cfg.UseRealLLM() {
		client := anthropicllm.New(cfg.AnthropicAPIKey, cfg.AnthropicModel)
		model = client
		slog.Info("llm: anthropic", "model", client.Model())
	} else {
		slog.Info("llm: mock (no ANTHROPIC_API_KEY, or LLM_MODE=mock)")
	}

	// NewHTTPServer cancels request contexts on Shutdown, so open SSE streams
	// do not hold the process for the whole ShutdownTimeout.
	srv := httpapi.NewHTTPServer(cfg.HTTPAddr, httpapi.New(httpapi.Options{
		DB:       pool,
		Segments: segment.Generator{LLM: model},
		Audience: audience.Counter{DB: pool},
	}))

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
