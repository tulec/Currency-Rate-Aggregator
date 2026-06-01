package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"currency-rate-aggregator/internal/domain"
)

const (
	defaultPort            = "8080"
	defaultReadTimeout     = 5 * time.Second
	defaultWriteTimeout    = 10 * time.Second
	defaultShutdownTimeout = 10 * time.Second
	defaultCacheTTL        = 30 * time.Second
	defaultDatabaseURL     = ""
	defaultSchedulerEvery  = time.Minute
	defaultCurrencies      = "USD,EUR"
	defaultRateLimitRPM    = 60
)

type Config struct {
	HTTPAddr            string
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	ShutdownTimeout     time.Duration
	CacheTTL            time.Duration
	DatabaseURL         string
	SchedulerInterval   time.Duration
	SchedulerCurrencies []string
	RateLimitRPM        int
}

func Load() (Config, error) {
	port, err := envPort("PORT", defaultPort)
	if err != nil {
		return Config{}, err
	}

	readTimeout, err := envDuration("READ_TIMEOUT", defaultReadTimeout)
	if err != nil {
		return Config{}, err
	}

	writeTimeout, err := envDuration("WRITE_TIMEOUT", defaultWriteTimeout)
	if err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := envDuration("SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}

	cacheTTL, err := envDuration("CACHE_TTL", defaultCacheTTL)
	if err != nil {
		return Config{}, err
	}

	databaseURL := envString("DATABASE_URL", defaultDatabaseURL)

	schedulerInterval, err := envDuration("SCHEDULER_INTERVAL", defaultSchedulerEvery)
	if err != nil {
		return Config{}, err
	}

	schedulerCurrencies, err := envStringList("SCHEDULER_CURRENCIES", defaultCurrencies)
	if err != nil {
		return Config{}, err
	}

	rateLimitRPM, err := envInt("RATE_LIMIT_REQUESTS_PER_MINUTE", defaultRateLimitRPM)
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPAddr:            ":" + port,
		ReadTimeout:         readTimeout,
		WriteTimeout:        writeTimeout,
		ShutdownTimeout:     shutdownTimeout,
		CacheTTL:            cacheTTL,
		DatabaseURL:         databaseURL,
		SchedulerInterval:   schedulerInterval,
		SchedulerCurrencies: schedulerCurrencies,
		RateLimitRPM:        rateLimitRPM,
	}, nil
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envPort(key, fallback string) (string, error) {
	value := envString(key, fallback)
	port, err := strconv.Atoi(value)
	if err != nil {
		return "", fmt.Errorf("%s must be a TCP port number: %w", key, err)
	}
	if port <= 0 || port > 65535 {
		return "", fmt.Errorf("%s must be between 1 and 65535", key)
	}
	return strconv.Itoa(port), nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err == nil {
		if duration <= 0 {
			return 0, fmt.Errorf("%s must be positive", key)
		}
		return duration, nil
	}

	seconds, convErr := strconv.Atoi(value)
	if convErr != nil {
		return 0, fmt.Errorf("%s must be a duration like 5s or seconds as integer: %w", key, errors.Join(err, convErr))
	}
	if seconds <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}

	return time.Duration(seconds) * time.Second, nil
}

func envStringList(key, fallback string) ([]string, error) {
	value := envString(key, fallback)
	parts := strings.Split(value, ",")
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		normalized, err := domain.NormalizeCurrency(item)
		if err != nil {
			return nil, fmt.Errorf("%s contains invalid currency %q: %w", key, item, err)
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%s must contain at least one value", key)
	}
	return result, nil
}

func envInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}

	return parsed, nil
}
