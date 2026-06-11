package config

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"currency-rate-aggregator/internal/domain"
	"github.com/stretchr/testify/require"
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
	t.Setenv("RATE_SOURCE", "")
	t.Setenv("RATE_SOURCES", "")
	t.Setenv("CBR_DAILY_URL", "")
	t.Setenv("FRANKFURTER_BASE_URL", "")
	t.Setenv("TBANK_RATES_URL", "")
	t.Setenv("TBANK_RATE_CATEGORY", "")

	cfg, err := Load()
	require.NoErrorf(t, err,
		"Load() error = %v", err)
	require.EqualValuesf(t, ":8080", cfg.HTTPAddr,
		"HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	require.EqualValuesf(t, 5*time.Second, cfg.ReadTimeout,
		"ReadTimeout = %v, want 5s", cfg.ReadTimeout)
	require.EqualValuesf(t, 10*time.Second, cfg.WriteTimeout,
		"WriteTimeout = %v, want 10s", cfg.WriteTimeout)
	require.EqualValuesf(t, 10*time.Second, cfg.ShutdownTimeout,
		"ShutdownTimeout = %v, want 10s", cfg.ShutdownTimeout)
	require.EqualValuesf(t, 30*time.Second, cfg.CacheTTL,
		"CacheTTL = %v, want 30s", cfg.CacheTTL)
	require.EqualValuesf(t, "", cfg.DatabaseURL,
		"DatabaseURL = %q, want empty", cfg.DatabaseURL)
	require.EqualValuesf(t, time.Minute, cfg.SchedulerInterval,
		"SchedulerInterval = %v, want 1m", cfg.SchedulerInterval)

	if got, want := cfg.SchedulerCurrencies, []string{"USD", "EUR"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		require.FailNowf(t, "test failed", "SchedulerCurrencies = %v, want %v", got, want)
	}
	require.EqualValuesf(t, 60, cfg.RateLimitRPM,
		"RateLimitRPM = %d, want 60", cfg.RateLimitRPM)

	if got, want := cfg.RateSources, []string{"cbr"}; len(got) != len(want) || got[0] != want[0] {
		require.FailNowf(t, "test failed", "RateSources = %v, want %v", got, want)
	}
	require.EqualValuesf(t, defaultCBRDailyURL, cfg.CBRDailyURL,
		"CBRDailyURL = %q, want default", cfg.CBRDailyURL)
	require.EqualValuesf(t, defaultFrankfurterURL, cfg.FrankfurterBaseURL,
		"FrankfurterBaseURL = %q, want default", cfg.FrankfurterBaseURL)
	require.EqualValuesf(t, defaultTBankRatesURL, cfg.TBankRatesURL,
		"TBankRatesURL = %q, want default", cfg.TBankRatesURL)
	require.EqualValuesf(t, defaultTBankCategory, cfg.TBankRateCategory,
		"TBankRateCategory = %q, want default", cfg.TBankRateCategory)

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
	t.Setenv("RATE_SOURCE", "")
	t.Setenv("RATE_SOURCES", " cbr, tbank, frankfurter, mock, cbr ")
	t.Setenv("CBR_DAILY_URL", " https://example.test/cbr.xml ")
	t.Setenv("FRANKFURTER_BASE_URL", " https://example.test/frankfurter ")
	t.Setenv("TBANK_RATES_URL", " https://example.test/tbank/rates ")
	t.Setenv("TBANK_RATE_CATEGORY", " C2CTransfers ")

	cfg, err := Load()
	require.NoErrorf(t, err,
		"Load() error = %v", err)
	require.EqualValuesf(t, ":9090", cfg.HTTPAddr,
		"HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	require.EqualValuesf(t, 2*time.Second, cfg.ReadTimeout,
		"ReadTimeout = %v, want 2s", cfg.ReadTimeout)
	require.EqualValuesf(t, 3*time.Second, cfg.WriteTimeout,
		"WriteTimeout = %v, want 3s", cfg.WriteTimeout)
	require.EqualValuesf(t, 4*time.Second, cfg.ShutdownTimeout,
		"ShutdownTimeout = %v, want 4s", cfg.ShutdownTimeout)
	require.EqualValuesf(t, 45*time.Second, cfg.CacheTTL,
		"CacheTTL = %v, want 45s", cfg.CacheTTL)
	require.EqualValuesf(t, "postgres://user:pass@localhost:5432/rates?sslmode=disable", cfg.DatabaseURL,
		"DatabaseURL = %q, want postgres URL", cfg.DatabaseURL)
	require.EqualValuesf(t, 2*time.Minute, cfg.SchedulerInterval,
		"SchedulerInterval = %v, want 2m", cfg.SchedulerInterval)

	if got, want := cfg.SchedulerCurrencies, []string{"USD", "EUR", "GBP"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		require.FailNowf(t, "test failed", "SchedulerCurrencies = %v, want %v", got, want)
	}
	require.EqualValuesf(t, 120, cfg.RateLimitRPM,
		"RateLimitRPM = %d, want 120", cfg.RateLimitRPM)

	if got, want := cfg.RateSources, []string{"cbr", "tbank", "frankfurter", "mock"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		require.FailNowf(t, "test failed", "RateSources = %v, want %v", got, want)
	}
	require.EqualValuesf(t, "https://example.test/cbr.xml", cfg.CBRDailyURL,
		"CBRDailyURL = %q, want custom URL", cfg.CBRDailyURL)
	require.EqualValuesf(t, "https://example.test/frankfurter", cfg.FrankfurterBaseURL,
		"FrankfurterBaseURL = %q, want custom URL", cfg.FrankfurterBaseURL)
	require.EqualValuesf(t, "https://example.test/tbank/rates", cfg.TBankRatesURL,
		"TBankRatesURL = %q, want custom URL", cfg.TBankRatesURL)
	require.EqualValuesf(t, "C2CTransfers", cfg.TBankRateCategory,
		"TBankRateCategory = %q, want custom category", cfg.TBankRateCategory)

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
	require.NoErrorf(t, err,
		"Load() error = %v", err)

	if got, want := cfg.SchedulerCurrencies, []string{"USD", "EUR"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		require.FailNowf(t, "test failed", "SchedulerCurrencies = %v, want %v", got, want)
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
	require.NoErrorf(t, err,
		"Load() error = %v", err)
	require.EqualValuesf(t, ":8080", cfg.HTTPAddr,
		"HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	require.EqualValuesf(t, "", cfg.DatabaseURL,
		"DatabaseURL = %q, want empty", cfg.DatabaseURL)
	require.EqualValuesf(t, 60, cfg.RateLimitRPM,
		"RateLimitRPM = %d, want 60", cfg.RateLimitRPM)

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
	require.NoErrorf(t, err,
		"Load() error = %v", err)
	require.EqualValuesf(t, ":8080", cfg.HTTPAddr,
		"HTTPAddr = %q, want :8080", cfg.HTTPAddr)

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
		require.FailNow(t, "test failed", "Load() error = nil, want error")
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
		require.FailNow(t, "test failed", "Load() error = nil, want error")
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
		require.FailNow(t, "test failed", "Load() error = nil, want error")
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
	require.Error(t, err,
		"Load() error = nil, want error")

	var numberErr *strconv.NumError
	require.Truef(t, errors.As(err, &numberErr),
		"Load() error = %v, want wrapped strconv.NumError", err)

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
		require.FailNow(t, "test failed", "Load() error = nil, want error")
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
		require.FailNow(t, "test failed", "Load() error = nil, want error")
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
	require.ErrorIsf(t, err, domain.ErrInvalidCurrencyCode,
		"Load() error = %v, want ErrInvalidCurrencyCode", err)

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
		require.FailNow(t, "test failed", "Load() error = nil, want error")
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
		require.FailNow(t, "test failed", "Load() error = nil, want error")
	}
}

func TestLoadRejectsInvalidRateSource(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "")
	t.Setenv("RATE_SOURCE", "")
	t.Setenv("RATE_SOURCES", "bank-web-scraping")

	if _, err := Load(); err == nil {
		require.FailNow(t, "test failed", "Load() error = nil, want error")
	}
}

func TestLoadSupportsLegacyRateSource(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("READ_TIMEOUT", "")
	t.Setenv("WRITE_TIMEOUT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SCHEDULER_INTERVAL", "")
	t.Setenv("SCHEDULER_CURRENCIES", "")
	t.Setenv("RATE_LIMIT_REQUESTS_PER_MINUTE", "")
	t.Setenv("RATE_SOURCE", "mock")
	t.Setenv("RATE_SOURCES", "")

	cfg, err := Load()
	require.NoErrorf(t, err,
		"Load() error = %v", err)

	if got, want := cfg.RateSources, []string{"mock"}; len(got) != len(want) || got[0] != want[0] {
		require.FailNowf(t, "test failed", "RateSources = %v, want %v", got, want)
	}
}
