package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"currency-rate-aggregator/internal/bankclient"
	"currency-rate-aggregator/internal/cache"
	"currency-rate-aggregator/internal/domain"
	appmetrics "currency-rate-aggregator/internal/metrics"
	"currency-rate-aggregator/internal/service"
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

	if ratesRec.Code != http.StatusOK {
		t.Fatalf("rates status = %d, want %d; body: %s", ratesRec.Code, http.StatusOK, ratesRec.Body.String())
	}

	result := decodeResponseData[domain.RateResult](t, ratesRec)
	if result.Currency != "USD" {
		t.Fatalf("result currency = %q, want USD", result.Currency)
	}
	if result.BestBuy.Bank != "North Bank" {
		t.Fatalf("best buy bank = %q, want North Bank", result.BestBuy.Bank)
	}
	if result.BestSell.Bank != "Metro Bank" {
		t.Fatalf("best sell bank = %q, want Metro Bank", result.BestSell.Bank)
	}
	if len(result.Sources) != 2 {
		t.Fatalf("sources = %d, want 2 successful mock banks", len(result.Sources))
	}

	cachedRatesReq := httptest.NewRequest(http.MethodGet, "/rates?currency=usd", nil)
	cachedRatesRec := httptest.NewRecorder()
	handler.ServeHTTP(cachedRatesRec, cachedRatesReq)

	if cachedRatesRec.Code != http.StatusOK {
		t.Fatalf("cached rates status = %d, want %d; body: %s", cachedRatesRec.Code, http.StatusOK, cachedRatesRec.Body.String())
	}

	cachedResult := decodeResponseData[domain.RateResult](t, cachedRatesRec)
	if cachedResult.BestBuy.Bank != result.BestBuy.Bank {
		t.Fatalf("cached best buy bank = %q, want %q", cachedResult.BestBuy.Bank, result.BestBuy.Bank)
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/rates/history?currency=usd&limit=10", nil)
	historyRec := httptest.NewRecorder()
	handler.ServeHTTP(historyRec, historyReq)

	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status = %d, want %d; body: %s", historyRec.Code, http.StatusOK, historyRec.Body.String())
	}

	history := decodeResponseData[[]domain.CurrencyRate](t, historyRec)
	if len(history) != 2 {
		t.Fatalf("history rows = %d, want 2 saved source rates without duplicating cached response", len(history))
	}
	for _, rate := range history {
		if rate.Currency != "USD" {
			t.Fatalf("history currency = %q, want USD", rate.Currency)
		}
	}

	byDateReq := httptest.NewRequest(http.MethodGet, "/rates/history/by-date?currency=usd&from=2026-05-18&to=2026-05-18&limit=10", nil)
	byDateRec := httptest.NewRecorder()
	handler.ServeHTTP(byDateRec, byDateReq)

	if byDateRec.Code != http.StatusOK {
		t.Fatalf("history by date status = %d, want %d; body: %s", byDateRec.Code, http.StatusOK, byDateRec.Body.String())
	}

	byDateHistory := decodeResponseData[[]domain.CurrencyRate](t, byDateRec)
	if len(byDateHistory) != 2 {
		t.Fatalf("history by date rows = %d, want 2 saved source rates", len(byDateHistory))
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	handler.ServeHTTP(metricsRec, metricsReq)

	if metricsRec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", metricsRec.Code, http.StatusOK)
	}
	metricsBody := metricsRec.Body.String()
	if !strings.Contains(metricsBody, `http_requests_total{method="GET",path="/rates",status="200"} 2`) {
		t.Fatalf("metrics missing rates counter:\n%s", metricsBody)
	}
	if !strings.Contains(metricsBody, `rate_cache_hits_total{currency="USD"} 1`) {
		t.Fatalf("metrics missing cache hit counter:\n%s", metricsBody)
	}
	if !strings.Contains(metricsBody, `rate_cache_misses_total{currency="USD"} 1`) {
		t.Fatalf("metrics missing cache miss counter:\n%s", metricsBody)
	}
	if !strings.Contains(metricsBody, `bank_request_errors_total{bank="Offline Bank"} 1`) {
		t.Fatalf("metrics missing offline bank error counter:\n%s", metricsBody)
	}
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
