package config

import (
	"log/slog"
	"strings"
	"testing"
)

// clearEnv blanks every variable Load reads; getenv treats "" as unset.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"DATABASE_URL", "HTTP_ADDR", "ANTHROPIC_API_KEY", "LLM_MODE", "LOG_LEVEL", "SEED_USERS", "SEED_RANDOM"} {
		t.Setenv(k, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.DatabaseURL, "localhost:5433/") {
		t.Errorf("default DATABASE_URL = %q, want the compose Postgres on host port 5433", c.DatabaseURL)
	}
	if c.LogLevel != slog.LevelInfo {
		t.Errorf("default LogLevel = %v, want INFO", c.LogLevel)
	}
	if c.LLMMode != "auto" || c.UseRealLLM() {
		t.Errorf("default LLM: mode=%q real=%v, want auto without a real LLM", c.LLMMode, c.UseRealLLM())
	}
	if c.SeedUsers != 5000 || c.SeedRandom != 42 {
		t.Errorf("seed defaults = %d/%d, want 5000/42", c.SeedUsers, c.SeedRandom)
	}
}

func TestLoadLogLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"debug", slog.LevelDebug, false},
		{"WARN", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"loud", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("LOG_LEVEL", tt.in)
			c, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("LOG_LEVEL=%q: want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.LogLevel != tt.want {
				t.Fatalf("LogLevel = %v, want %v", c.LogLevel, tt.want)
			}
		})
	}
}

func TestLoadLLMMode(t *testing.T) {
	clearEnv(t)
	t.Setenv("LLM_MODE", "anthropic")
	if _, err := Load(); err == nil {
		t.Fatal("LLM_MODE=anthropic without a key must fail at startup")
	}

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("LLM_MODE", "auto")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.UseRealLLM() {
		t.Fatal("auto with a key must use the real LLM")
	}

	t.Setenv("LLM_MODE", "gpt")
	if _, err := Load(); err == nil {
		t.Fatal("unknown LLM_MODE must be rejected")
	}
}
