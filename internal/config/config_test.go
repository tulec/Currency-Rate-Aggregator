package config

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"currency-rate-aggregator/internal/domain"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.ReadTimeout != 5*time.Second {
		t.Fatalf("ReadTimeout = %v, want 5s", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 10*time.Second {
		t.Fatalf("WriteTimeout = %v, want 10s", cfg.WriteTimeout)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("ShutdownTimeout = %v, want 10s", cfg.ShutdownTimeout)
	}
	if cfg.CacheTTL != 30*time.Second {
		t.Fatalf("CacheTTL = %v, want 30s", cfg.CacheTTL)
	}
	if cfg.DatabaseURL != "" {
		t.Fatalf("DatabaseURL = %q, want empty", cfg.DatabaseURL)
	}
	if cfg.SchedulerInterval != time.Minute {
		t.Fatalf("SchedulerInterval = %v, want 1m", cfg.SchedulerInterval)
	}
	if got, want := cfg.SchedulerCurrencies, []string{"USD", "EUR"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SchedulerCurrencies = %v, want %v", got, want)
	}
	if cfg.RateLimitRPM != 60 {
		t.Fatalf("RateLimitRPM = %d, want 60", cfg.RateLimitRPM)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("PORT", " 9090 ")
	t.Setenv("READ_TIMEOUT", " 2s ")
	t.Setenv("WRITE_TIMEOUT", "3s")
	t.Setenv("SHUTDOWN_TIMEOUT", "4")
	t.Setenv("CACHE_TTL", "45s")
	t.Setenv("DATABASE_URL", " postgres://user:pass@localhost:5432/rates?sslmode=disable ")
	t.Setenv("SCHEDULER_INTERVAL", "2m")
	t.Setenv("SCHEDULER_CURRENCIES", "usd, eur, gbp")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", " 120 ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.ReadTimeout != 2*time.Second {
		t.Fatalf("ReadTimeout = %v, want 2s", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 3*time.Second {
		t.Fatalf("WriteTimeout = %v, want 3s", cfg.WriteTimeout)
	}
	if cfg.ShutdownTimeout != 4*time.Second {
		t.Fatalf("ShutdownTimeout = %v, want 4s", cfg.ShutdownTimeout)
	}
	if cfg.CacheTTL != 45*time.Second {
		t.Fatalf("CacheTTL = %v, want 45s", cfg.CacheTTL)
	}
	if cfg.DatabaseURL != "postgres://user:pass@localhost:5432/rates?sslmode=disable" {
		t.Fatalf("DatabaseURL = %q, want postgres URL", cfg.DatabaseURL)
	}
	if cfg.SchedulerInterval != 2*time.Minute {
		t.Fatalf("SchedulerInterval = %v, want 2m", cfg.SchedulerInterval)
	}
	if got, want := cfg.SchedulerCurrencies, []string{"USD", "EUR", "GBP"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("SchedulerCurrencies = %v, want %v", got, want)
	}
	if cfg.RateLimitRPM != 120 {
		t.Fatalf("RateLimitRPM = %d, want 120", cfg.RateLimitRPM)
	}
}

func TestLoadDeduplicatesSchedulerCurrenciesAfterNormalization(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "usd, USD, eur, usd")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.SchedulerCurrencies, []string{"USD", "EUR"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SchedulerCurrencies = %v, want %v", got, want)
	}
}

func TestLoadTreatsWhitespaceOnlyScalarsAsDefaults(t *testing.T) {
	t.Setenv("PORT", " \t ")
	t.Setenv("READ_TIMEOUT", " ")
	t.Setenv("WRITE_TIMEOUT", " ")
	t.Setenv("SHUTDOWN_TIMEOUT", " ")
	t.Setenv("CACHE_TTL", " ")
	t.Setenv("DATABASE_URL", " ")
	t.Setenv("SCHEDULER_INTERVAL", " ")
	t.Setenv("SCHEDULER_CURRENCIES", "")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", " ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.DatabaseURL != "" {
		t.Fatalf("DatabaseURL = %q, want empty", cfg.DatabaseURL)
	}
	if cfg.RateLimitRPM != 60 {
		t.Fatalf("RateLimitRPM = %d, want 60", cfg.RateLimitRPM)
	}
}

func TestLoadCanonicalizesValidatedPort(t *testing.T) {
	t.Setenv("PORT", " 08080 ")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Setenv("PORT", "localhost:8080")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadRejectsOutOfRangePort(t *testing.T) {
	t.Setenv("PORT", "70000")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("READ_TIMEOUT", "later")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadInvalidDurationPreservesIntegerParseError(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("READ_TIMEOUT", "later")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}

	var numberErr *strconv.NumError
	if !errors.As(err, &numberErr) {
		t.Fatalf("Load() error = %v, want wrapped strconv.NumError", err)
	}
}

func TestLoadRejectsNonPositiveDuration(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "0s")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadRejectsEmptySchedulerCurrencies(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", ",,")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadRejectsInvalidSchedulerCurrency(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "USD,USDT")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "")

	_, err := Load()
	if !errors.Is(err, domain.ErrInvalidCurrencyCode) {
		t.Fatalf("Load() error = %v, want ErrInvalidCurrencyCode", err)
	}
}

func TestLoadRejectsInvalidRateLimit(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "none")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadRejectsNonPositiveRateLimit(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "0")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}
