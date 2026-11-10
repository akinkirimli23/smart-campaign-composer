// Command seed applies migrations and loads the fake recipient population.
// It is idempotent: if users already exist it does nothing.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/config"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/db"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/seed"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("seed failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	_, err = seed.Load(ctx, pool, cfg.SeedUsers, cfg.SeedRandom, time.Now())
	return err
}
