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
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/audience"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/campaign"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/config"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/db"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/delivery"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/httpapi"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm/anthropicllm"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm/mockllm"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/metrics"
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

	// The mock streams with a small delay so the UI visibly fills in.
	var model llm.Client = mockllm.Client{ChunkDelay: 25 * time.Millisecond}
	if cfg.UseRealLLM() {
		client := anthropicllm.New(cfg.AnthropicAPIKey, cfg.AnthropicModel)
		model = client
		slog.Info("llm: anthropic", "model", client.Model())
	} else {
		slog.Info("llm: mock (no ANTHROPIC_API_KEY, or LLM_MODE=mock)")
	}

	registry := metrics.New()

	// NewHTTPServer cancels request contexts on Shutdown, so open SSE streams
	// do not hold the process for the whole ShutdownTimeout.
	srv := httpapi.NewHTTPServer(cfg.HTTPAddr, httpapi.New(httpapi.Options{
		DB:         pool,
		Segments:   segment.Generator{LLM: model},
		Audience:   audience.Counter{DB: pool},
		Content:    content.Generator{LLM: model},
		VarLengths: &content.DBLengths{DB: pool},
		Campaigns:  campaign.Store{DB: pool},
		Metrics:    registry,
	}))

	// The delivery worker runs in this process. Its rate limiter is in-memory,
	// so exactly one process may send (DECISIONS 3); WORKER_ENABLED=false turns
	// it off for additional API replicas.
	workerDone := make(chan struct{})
	if cfg.WorkerEnabled {
		worker := &delivery.Worker{
			DB:       pool,
			Metrics:  registry,
			Provider: delivery.HTTPProvider{BaseURL: cfg.ProviderURL},
			Limiters: map[string]delivery.Waiter{
				campaign.ChannelSMS:   delivery.NewLimiter(cfg.SendRatePerSecond),
				campaign.ChannelEmail: delivery.NewLimiter(cfg.SendRatePerSecond),
			},
		}
		slog.Info("delivery worker started", "provider", cfg.ProviderURL, "rate_per_second", cfg.SendRatePerSecond)
		go func() {
			defer close(workerDone)
			worker.Run(ctx)
		}()
	} else {
		close(workerDone)
	}

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
	err = srv.Shutdown(shutdownCtx)
	stop() // also reached when the server failed on its own: stop the worker too
	select {
	case <-workerDone: // in-flight sends were recorded
	case <-shutdownCtx.Done():
		slog.Warn("delivery worker did not stop in time; leased messages will be retried")
	}
	return err
}
