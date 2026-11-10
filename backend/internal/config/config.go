// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL     string
	HTTPAddr        string
	AnthropicAPIKey string
	// LLMMode: "mock" always uses the mock LLM, "anthropic" requires a key,
	// "auto" uses Anthropic when a key is present and falls back to mock otherwise.
	LLMMode string
	// LogLevel comes from LOG_LEVEL (debug, info, warn, error); default info.
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	SeedUsers       int
	SeedRandom      int64
}

func Load() (Config, error) {
	c := Config{
		// The default targets the compose Postgres, which is published on host
		// port 5433 so it does not clash with a locally installed Postgres.
		DatabaseURL:     getenv("DATABASE_URL", "postgres://scc:scc@localhost:5433/scc?sslmode=disable"),
		HTTPAddr:        getenv("HTTP_ADDR", ":8080"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		LLMMode:         getenv("LLM_MODE", "auto"),
		ShutdownTimeout: 10 * time.Second,
	}

	// UnmarshalText accepts debug/info/warn/error in any case.
	if err := c.LogLevel.UnmarshalText([]byte(getenv("LOG_LEVEL", "info"))); err != nil {
		return c, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	var err error
	if c.SeedUsers, err = getint("SEED_USERS", 5000); err != nil {
		return c, err
	}
	seed, err := getint("SEED_RANDOM", 42)
	if err != nil {
		return c, err
	}
	c.SeedRandom = int64(seed)

	switch c.LLMMode {
	case "mock", "auto":
	case "anthropic":
		if c.AnthropicAPIKey == "" {
			return c, fmt.Errorf("LLM_MODE=anthropic requires ANTHROPIC_API_KEY")
		}
	default:
		return c, fmt.Errorf("invalid LLM_MODE %q (want mock, anthropic or auto)", c.LLMMode)
	}
	return c, nil
}

// UseRealLLM reports whether the Anthropic API should be called.
func (c Config) UseRealLLM() bool {
	return c.LLMMode == "anthropic" || (c.LLMMode == "auto" && c.AnthropicAPIKey != "")
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getint(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}
