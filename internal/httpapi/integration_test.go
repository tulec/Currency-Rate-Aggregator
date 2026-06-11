package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"currency-rate-aggregator/internal/bankclient"
	"currency-rate-aggregator/internal/cache"
	"currency-rate-aggregator/internal/domain"
	appmetrics "currency-rate-aggregator/internal/metrics"
	"currency-rate-aggregator/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRouterIntegrationAggregatesPersistsAndReportsMetrics(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	store := &integrationHistoryStore{}
	metrics := appmetrics.NewHTTPMetrics()
	aggregator := service.NewAggregatorWithCacheAndStore(
		bankclient.DefaultMockBanks(fetchedAt),
		cache.NewTTLCache(time.Minute),
		store,
	).WithMetrics(metrics)
	handler := NewRouterWithHistoryRateLimitAndMetrics(aggregator, store, nil, nil, metrics)

	ratesReq := httptest.NewRequest(http.MethodGet, "/rates?currency=usd", nil)
	ratesRec := httptest.NewRecorder()
	handler.ServeHTTP(ratesRec, ratesReq)
	require.EqualValuesf(t, http.StatusOK, ratesRec.Code,
		"rates status = %d, want %d; body: %s", ratesRec.Code, http.StatusOK, ratesRec.Body.String())

	result := decodeResponseData[domain.RateResult](t, ratesRec)
	require.EqualValuesf(t, "USD", result.Currency,
		"result currency = %q, want USD", result.Currency)
	require.EqualValuesf(t, "North Bank", result.BestBuy.Bank,
		"best buy bank = %q, want North Bank", result.BestBuy.Bank)
	require.EqualValuesf(t, "Metro Bank", result.BestSell.Bank,
		"best sell bank = %q, want Metro Bank", result.BestSell.Bank)
	require.Lenf(t, result.Sources, 2,
		"sources = %d, want 2 successful mock banks", len(result.Sources))

	cachedRatesReq := httptest.NewRequest(http.MethodGet, "/rates?currency=usd", nil)
	cachedRatesRec := httptest.NewRecorder()
	handler.ServeHTTP(cachedRatesRec, cachedRatesReq)
	require.EqualValuesf(t, http.StatusOK, cachedRatesRec.Code,
		"cached rates status = %d, want %d; body: %s", cachedRatesRec.Code, http.StatusOK, cachedRatesRec.Body.String())

	cachedResult := decodeResponseData[domain.RateResult](t, cachedRatesRec)
	require.EqualValuesf(t, result.BestBuy.Bank, cachedResult.BestBuy.Bank,
		"cached best buy bank = %q, want %q", cachedResult.BestBuy.Bank, result.BestBuy.Bank)

	historyReq := httptest.NewRequest(http.MethodGet, "/rates/history?currency=usd&limit=10", nil)
	historyRec := httptest.NewRecorder()
	handler.ServeHTTP(historyRec, historyReq)
	require.EqualValuesf(t, http.StatusOK, historyRec.Code,
		"history status = %d, want %d; body: %s", historyRec.Code, http.StatusOK, historyRec.Body.String())

	history := decodeResponseData[[]domain.CurrencyRate](t, historyRec)
	require.Lenf(t, history, 2,
		"history rows = %d, want 2 saved source rates without duplicating cached response", len(history))

	for _, rate := range history {
		require.EqualValuesf(t, "USD", rate.Currency,
			"history currency = %q, want USD", rate.Currency)

	}

	byDateReq := httptest.NewRequest(http.MethodGet, "/rates/history/by-date?currency=usd&from=2026-05-18&to=2026-05-18&limit=10", nil)
	byDateRec := httptest.NewRecorder()
	handler.ServeHTTP(byDateRec, byDateReq)
	require.EqualValuesf(t, http.StatusOK, byDateRec.Code,
		"history by date status = %d, want %d; body: %s", byDateRec.Code, http.StatusOK, byDateRec.Body.String())

	byDateHistory := decodeResponseData[[]domain.CurrencyRate](t, byDateRec)
	require.Lenf(t, byDateHistory, 2,
		"history by date rows = %d, want 2 saved source rates", len(byDateHistory))

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	handler.ServeHTTP(metricsRec, metricsReq)
	require.EqualValuesf(t, http.StatusOK, metricsRec.Code,
		"metrics status = %d, want %d", metricsRec.Code, http.StatusOK)

	metricsBody := metricsRec.Body.String()
	require.Containsf(t, metricsBody, `http_requests_total{method="GET",path="/rates",status="200"} 2`,
		"metrics missing rates counter:\n%s", metricsBody)
	require.Containsf(t, metricsBody, `rate_cache_hits_total{currency="USD"} 1`,
		"metrics missing cache hit counter:\n%s", metricsBody)
	require.Containsf(t, metricsBody, `rate_cache_misses_total{currency="USD"} 1`,
		"metrics missing cache miss counter:\n%s", metricsBody)
	require.Containsf(t, metricsBody, `bank_request_errors_total{bank="Offline Bank"} 1`,
		"metrics missing offline bank error counter:\n%s", metricsBody)

}

type integrationHistoryStore struct {
	mu    sync.Mutex
	rates []domain.CurrencyRate
}

func (s *integrationHistoryStore) SaveRates(ctx context.Context, rates []domain.CurrencyRate) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	copied := append([]domain.CurrencyRate(nil), rates...)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.rates = append(s.rates, copied...)
	return nil
}

func (s *integrationHistoryStore) History(ctx context.Context, currency string, limit int) ([]domain.CurrencyRate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	normalized, err := domain.NormalizeCurrency(currency)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	matches := make([]domain.CurrencyRate, 0, len(s.rates))
	for i := len(s.rates) - 1; i >= 0; i-- {
		if s.rates[i].Currency.String() != normalized {
			continue
		}
		matches = append(matches, s.rates[i])
		if len(matches) == limit {
			break
		}
	}

	return matches, nil
}

func (s *integrationHistoryStore) HistoryByDate(ctx context.Context, currency string, from, to time.Time, limit int) ([]domain.CurrencyRate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	normalized, err := domain.NormalizeCurrency(currency)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	matches := make([]domain.CurrencyRate, 0, len(s.rates))
	for i := len(s.rates) - 1; i >= 0; i-- {
		rate := s.rates[i]
		if rate.Currency.String() != normalized {
			continue
		}
		fetchedAt := rate.FetchedAt.UTC()
		if fetchedAt.Before(from) || !fetchedAt.Before(to) {
			continue
		}
		matches = append(matches, rate)
		if len(matches) == limit {
			break
		}
	}

	return matches, nil
}
