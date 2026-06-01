package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"time"

	"currency-rate-aggregator/internal/bankclient"
	"currency-rate-aggregator/internal/domain"
)

var (
	ErrNoRatesAvailable   = errors.New("no rates available")
	errBankClientPanicked = errors.New("bank client panicked")
	errUnexpectedCurrency = errors.New("bank returned unexpected currency")
)

type Aggregator struct {
	clients []bankclient.BankClient
	cache   rateCache
	store   rateStore
	metrics aggregatorMetrics
	logger  *slog.Logger
	now     func() time.Time
}

func NewAggregator(clients []bankclient.BankClient) *Aggregator {
	copied := make([]bankclient.BankClient, 0, len(clients))
	for _, client := range clients {
		if isNilInterface(client) {
			continue
		}
		copied = append(copied, client)
	}

	return &Aggregator{
		clients: copied,
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func NewAggregatorWithCache(clients []bankclient.BankClient, cache rateCache) *Aggregator {
	aggregator := NewAggregator(clients)
	if !isNilInterface(cache) {
		aggregator.cache = cache
	}
	return aggregator
}

func NewAggregatorWithCacheAndStore(clients []bankclient.BankClient, cache rateCache, store rateStore) *Aggregator {
	aggregator := NewAggregatorWithCache(clients, cache)
	if !isNilInterface(store) {
		aggregator.store = store
	}
	return aggregator
}

func (a *Aggregator) WithMetrics(metrics aggregatorMetrics) *Aggregator {
	if isNilInterface(metrics) {
		a.metrics = nil
		return a
	}
	a.metrics = metrics
	return a
}

func (a *Aggregator) WithLogger(logger *slog.Logger) *Aggregator {
	if logger == nil {
		a.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
		return a
	}
	a.logger = logger
	return a
}

func (a *Aggregator) FetchRates(ctx context.Context, currency string) (domain.RateResult, error) {
	normalized, err := domain.NormalizeCurrency(currency)
	if err != nil {
		return domain.RateResult{}, err
	}

	if err := ctx.Err(); err != nil {
		return domain.RateResult{}, err
	}

	if a.cache != nil {
		if result, ok := a.cache.Get(normalized); ok {
			a.observeCacheHit(normalized)
			return result, nil
		}
		a.observeCacheMiss(normalized)
	}

	if len(a.clients) == 0 {
		return domain.RateResult{}, ErrNoRatesAvailable
	}

	return a.fetchFreshRates(ctx, normalized)
}

func (a *Aggregator) RefreshRates(ctx context.Context, currency string) (domain.RateResult, error) {
	normalized, err := domain.NormalizeCurrency(currency)
	if err != nil {
		return domain.RateResult{}, err
	}

	if err := ctx.Err(); err != nil {
		return domain.RateResult{}, err
	}

	if len(a.clients) == 0 {
		return domain.RateResult{}, ErrNoRatesAvailable
	}

	return a.fetchFreshRates(ctx, normalized)
}

func (a *Aggregator) fetchFreshRates(ctx context.Context, normalized string) (domain.RateResult, error) {
	results := make(chan fetchResult, len(a.clients))
	for _, client := range a.clients {
		go func(client bankclient.BankClient) {
			results <- fetchClientRate(ctx, client, normalized)
		}(client)
	}

	sources := make([]domain.CurrencyRate, 0, len(a.clients))
	var errs []error
	for range a.clients {
		var result fetchResult
		select {
		case result = <-results:
		case <-ctx.Done():
			return domain.RateResult{}, ctx.Err()
		}

		if result.err != nil {
			if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
				return domain.RateResult{}, result.err
			}
			a.recordBankRequestError(result.bank, normalized, result.err)
			errs = append(errs, result.err)
			continue
		}
		rate, err := normalizeFetchedRate(result.rate, normalized)
		if err != nil {
			a.recordBankRequestError(result.bank, normalized, err)
			errs = append(errs, err)
			continue
		}
		sources = append(sources, rate)
	}

	if err := ctx.Err(); err != nil {
		return domain.RateResult{}, err
	}

	if len(sources) == 0 {
		if len(errs) > 0 {
			return domain.RateResult{}, fmt.Errorf("%w: %w", ErrNoRatesAvailable, errors.Join(errs...))
		}
		return domain.RateResult{}, ErrNoRatesAvailable
	}

	bestBuy := sources[0]
	bestSell := sources[0]
	for _, rate := range sources[1:] {
		if rate.Buy > bestBuy.Buy {
			bestBuy = rate
		}
		if rate.Sell < bestSell.Sell {
			bestSell = rate
		}
	}

	result := domain.RateResult{
		Currency:  normalized,
		BestBuy:   bestBuy,
		BestSell:  bestSell,
		Sources:   sources,
		UpdatedAt: a.now(),
	}

	if a.store != nil {
		if err := a.store.SaveRates(ctx, sources); err != nil {
			return domain.RateResult{}, fmt.Errorf("save fetched rates: %w", err)
		}
	}

	if a.cache != nil {
		a.cache.Set(normalized, result)
	}

	return result, nil
}

func normalizeFetchedRate(rate domain.CurrencyRate, expectedCurrency string) (domain.CurrencyRate, error) {
	normalized, err := domain.NormalizeCurrency(rate.Currency)
	if err != nil {
		return domain.CurrencyRate{}, fmt.Errorf("normalize fetched rate currency %q from %s: %w", rate.Currency, rate.Bank, err)
	}
	if normalized != expectedCurrency {
		return domain.CurrencyRate{}, fmt.Errorf("%w: got %s from %s for %s", errUnexpectedCurrency, normalized, rate.Bank, expectedCurrency)
	}
	rate.Currency = normalized
	rate.FetchedAt = rate.FetchedAt.UTC()
	return rate, nil
}

type fetchResult struct {
	bank string
	rate domain.CurrencyRate
	err  error
}

func fetchClientRate(ctx context.Context, client bankclient.BankClient, currency string) (result fetchResult) {
	result.bank = "unknown"
	defer func() {
		if recovered := recover(); recovered != nil {
			result.err = fmt.Errorf("%w: %v", errBankClientPanicked, recovered)
		}
	}()

	result.bank = client.Name()
	result.rate, result.err = client.FetchRate(ctx, currency)
	return result
}

type rateCache interface {
	Get(currency string) (domain.RateResult, bool)
	Set(currency string, result domain.RateResult)
}

type rateStore interface {
	SaveRates(ctx context.Context, rates []domain.CurrencyRate) error
}

type aggregatorMetrics interface {
	ObserveCacheHit(currency string)
	ObserveCacheMiss(currency string)
	ObserveBankRequestError(bank string)
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (a *Aggregator) observeCacheHit(currency string) {
	if a.metrics != nil {
		a.metrics.ObserveCacheHit(currency)
	}
}

func (a *Aggregator) observeCacheMiss(currency string) {
	if a.metrics != nil {
		a.metrics.ObserveCacheMiss(currency)
	}
}

func (a *Aggregator) recordBankRequestError(bank, currency string, err error) {
	if a.metrics != nil {
		a.metrics.ObserveBankRequestError(bank)
	}
	if a.logger != nil {
		a.logger.Warn("bank request failed",
			slog.String("bank", bank),
			slog.String("currency", currency),
			slog.String("error", err.Error()),
		)
	}
}
