package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"currency-rate-aggregator/internal/domain"
)

type Scheduler struct {
	rates      scheduledRateFetcher
	currencies []string
	interval   time.Duration
	logger     *slog.Logger
}

func NewScheduler(rates scheduledRateFetcher, currencies []string, interval time.Duration, logger *slog.Logger) (*Scheduler, error) {
	if isNilInterface(rates) {
		return nil, errors.New("rates service is required")
	}
	if interval <= 0 {
		return nil, errors.New("scheduler interval must be positive")
	}

	normalized := make([]string, 0, len(currencies))
	seen := make(map[string]struct{}, len(currencies))
	for _, currency := range currencies {
		code, err := domain.NormalizeCurrency(currency)
		if err != nil {
			return nil, fmt.Errorf("invalid scheduler currency %q: %w", currency, err)
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		normalized = append(normalized, code)
	}
	if len(normalized) == 0 {
		return nil, errors.New("scheduler currencies are required")
	}

	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &Scheduler{
		rates:      rates,
		currencies: normalized,
		interval:   interval,
		logger:     logger,
	}, nil
}

func (s *Scheduler) Run(ctx context.Context) {
	s.refreshAll(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler stopped")
			return
		case <-ticker.C:
			s.refreshAll(ctx)
		}
	}
}

func (s *Scheduler) refreshAll(ctx context.Context) {
	for _, currency := range s.currencies {
		if err := ctx.Err(); err != nil {
			return
		}

		result, err := s.fetchFresh(ctx, currency)
		if err != nil {
			s.logger.Error("scheduler refresh failed",
				slog.String("currency", currency),
				slog.String("error", err.Error()),
			)
			continue
		}

		s.logger.Info("scheduler refresh succeeded",
			slog.String("currency", result.Currency),
			slog.Int("sources", len(result.Sources)),
			slog.Time("updated_at", result.UpdatedAt),
		)
	}
}

func (s *Scheduler) fetchFresh(ctx context.Context, currency string) (domain.RateResult, error) {
	if refresher, ok := s.rates.(freshRateFetcher); ok {
		return refresher.RefreshRates(ctx, currency)
	}
	return s.rates.FetchRates(ctx, currency)
}

type scheduledRateFetcher interface {
	FetchRates(ctx context.Context, currency string) (domain.RateResult, error)
}

type freshRateFetcher interface {
	RefreshRates(ctx context.Context, currency string) (domain.RateResult, error)
}
