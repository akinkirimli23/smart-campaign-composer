// Command mockprovider runs the fake SMS / email provider the worker sends to.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/mockprovider"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg := mockprovider.Config{
		ErrorRate:     rate("MOCK_ERROR_RATE", 0.05),
		RateLimitRate: rate("MOCK_RATE_LIMIT_RATE", 0.01),
		LostReplyRate: rate("MOCK_LOST_REPLY_RATE", 0.01),
		MinLatency:    millis("MOCK_MIN_LATENCY_MS", 20),
		MaxLatency:    millis("MOCK_MAX_LATENCY_MS", 250),
		RetryAfter:    millis("MOCK_RETRY_AFTER_MS", 1000),
		Seed:          42,
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	srv := &http.Server{Addr: addr, Handler: mockprovider.New(cfg), ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("mock provider listening", "addr", addr,
		"error_rate", cfg.ErrorRate, "rate_limit_rate", cfg.RateLimitRate, "lost_reply_rate", cfg.LostReplyRate)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("mock provider exited", "err", err)
		os.Exit(1)
	}
}

func rate(key string, def float64) float64 {
	v, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil || v < 0 || v > 1 {
		return def
	}
	return v
}

func millis(key string, def int) time.Duration {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil || v < 0 {
		v = def
	}
	return time.Duration(v) * time.Millisecond
}
