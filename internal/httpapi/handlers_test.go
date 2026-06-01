package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"currency-rate-aggregator/internal/domain"
	appmetrics "currency-rate-aggregator/internal/metrics"
	"currency-rate-aggregator/internal/ratelimit"
	"currency-rate-aggregator/internal/service"
	"currency-rate-aggregator/internal/storage"
)

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("status body = %q, want ok", body.Status)
	}
}

func TestHealthHandlerRejectsUnsupportedMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != allowedReadMethods {
		t.Fatalf("Allow header = %q, want %q", got, allowedReadMethods)
	}
	assertErrorResponse(t, rec, "method not allowed")
}

func TestPprofRejectsUnsupportedMethodWithAllowHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/debug/pprof/goroutine", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != allowedReadMethods {
		t.Fatalf("Allow header = %q, want %q", got, allowedReadMethods)
	}
	assertErrorResponse(t, rec, "method not allowed")
}

func TestRouterReturnsJSONForUnknownRoute(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	assertErrorResponse(t, rec, "route not found")
}

func TestRatesHandlerRequiresCurrency(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates", nil)
	rec := httptest.NewRecorder()

	NewRouter(&fakeRateFetcher{}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, rec, "currency query parameter is required")
}

func TestRatesHandlerRequiresNonBlankCurrency(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates?currency=%20%09", nil)
	rec := httptest.NewRecorder()

	NewRouter(&fakeRateFetcher{}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, rec, "currency query parameter is required")
}

func TestRatesHandlerReturnsAggregatedRates(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 18, 10, 1, 0, 0, time.UTC)
	fetcher := &fakeRateFetcher{
		result: domain.RateResult{
			Currency: "USD",
			BestBuy: domain.CurrencyRate{
				Currency:  "USD",
				Bank:      "Bank A",
				Buy:       91.2,
				Sell:      92.1,
				FetchedAt: fetchedAt,
			},
			BestSell: domain.CurrencyRate{
				Currency:  "USD",
				Bank:      "Bank B",
				Buy:       91.0,
				Sell:      91.8,
				FetchedAt: fetchedAt,
			},
			Sources: []domain.CurrencyRate{
				{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1, FetchedAt: fetchedAt},
				{Currency: "USD", Bank: "Bank B", Buy: 91.0, Sell: 91.8, FetchedAt: fetchedAt},
			},
			UpdatedAt: updatedAt,
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/rates?currency=usd", nil)
	rec := httptest.NewRecorder()

	NewRouter(fetcher, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if fetcher.currency != "USD" {
		t.Fatalf("currency passed to fetcher = %q, want USD", fetcher.currency)
	}

	var body domain.RateResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Currency != "USD" {
		t.Fatalf("Currency = %q, want USD", body.Currency)
	}
	if body.BestBuy.Bank != "Bank A" {
		t.Fatalf("BestBuy.Bank = %q, want Bank A", body.BestBuy.Bank)
	}
	if body.BestSell.Bank != "Bank B" {
		t.Fatalf("BestSell.Bank = %q, want Bank B", body.BestSell.Bank)
	}
	if len(body.Sources) != 2 {
		t.Fatalf("len(Sources) = %d, want 2", len(body.Sources))
	}
}

func TestRatesHandlerReturnsBadRequestForInvalidCurrency(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates?currency=USDT", nil)
	rec := httptest.NewRecorder()

	NewRouter(&fakeRateFetcher{err: domain.ErrInvalidCurrencyCode}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, rec, "currency must be a 3-letter code")
}

func TestRatesHandlerValidatesCurrencyBeforeRatesService(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates?currency=USDT", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, rec, "currency must be a 3-letter code")
}

func TestRatesHandlerReportsTypedNilRatesService(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates?currency=USD", nil)
	rec := httptest.NewRecorder()

	var fetcher *fakeRateFetcher
	NewRouter(fetcher, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	assertErrorResponse(t, rec, "rates service is not configured")
}

func TestRatesHandlerReturnsUnavailableWhenNoRatesExist(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates?currency=USD", nil)
	rec := httptest.NewRecorder()

	NewRouter(&fakeRateFetcher{err: service.ErrNoRatesAvailable}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	assertErrorResponse(t, rec, "no rates available")
}

func TestRatesHandlerReturnsGatewayTimeoutOnDeadline(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates?currency=USD", nil)
	rec := httptest.NewRecorder()

	NewRouter(&fakeRateFetcher{err: context.DeadlineExceeded}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusGatewayTimeout)
	}
	assertErrorResponse(t, rec, "request timed out")
}

func TestRatesHandlerReturnsClientClosedRequestOnCancellation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates?currency=USD", nil)
	rec := httptest.NewRecorder()

	NewRouter(&fakeRateFetcher{err: context.Canceled}, nil).ServeHTTP(rec, req)

	if rec.Code != statusClientClosedRequest {
		t.Fatalf("status = %d, want %d", rec.Code, statusClientClosedRequest)
	}
	assertErrorResponse(t, rec, "request canceled")
}

func TestRatesHistoryHandlerReturnsHistory(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	history := &fakeRateHistoryReader{
		rates: []domain.CurrencyRate{
			{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1, FetchedAt: fetchedAt},
			{Currency: "USD", Bank: "Bank B", Buy: 91.0, Sell: 91.8, FetchedAt: fetchedAt.Add(-time.Minute)},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=usd&limit=2", nil)
	rec := httptest.NewRecorder()

	NewRouterWithHistory(&fakeRateFetcher{}, history, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if history.currency != "USD" {
		t.Fatalf("currency passed to history = %q, want USD", history.currency)
	}
	if history.limit != 2 {
		t.Fatalf("limit passed to history = %d, want 2", history.limit)
	}

	var body []domain.CurrencyRate
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("history rows = %d, want 2", len(body))
	}
	if body[0].Bank != "Bank A" {
		t.Fatalf("first bank = %q, want Bank A", body[0].Bank)
	}
}

func TestRatesHistoryHandlerReturnsEmptyArrayWhenNoRows(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=USD", nil)
	rec := httptest.NewRecorder()

	NewRouterWithHistory(&fakeRateFetcher{}, &fakeRateHistoryReader{}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("body = %q, want []", got)
	}
}

func TestRatesHistoryHandlerRequiresCurrency(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates/history", nil)
	rec := httptest.NewRecorder()

	NewRouterWithHistory(&fakeRateFetcher{}, &fakeRateHistoryReader{}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, rec, "currency query parameter is required")
}

func TestRatesHistoryHandlerRequiresNonBlankCurrency(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=%20%09", nil)
	rec := httptest.NewRecorder()

	NewRouterWithHistory(&fakeRateFetcher{}, &fakeRateHistoryReader{}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, rec, "currency query parameter is required")
}

func TestRatesHistoryHandlerRejectsInvalidLimit(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=USD&limit=many", nil)
	rec := httptest.NewRecorder()

	NewRouterWithHistory(&fakeRateFetcher{}, &fakeRateHistoryReader{}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, rec, "limit must be a positive integer")
}

func TestRatesHistoryHandlerCapsLimit(t *testing.T) {
	history := &fakeRateHistoryReader{}
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=USD&limit=999999", nil)
	rec := httptest.NewRecorder()

	NewRouterWithHistory(&fakeRateFetcher{}, history, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if history.limit != storage.MaxHistoryLimit {
		t.Fatalf("limit passed to history = %d, want %d", history.limit, storage.MaxHistoryLimit)
	}
}

func TestRatesHistoryHandlerTrimsLimitWhitespace(t *testing.T) {
	history := &fakeRateHistoryReader{}
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=USD&limit=%202%20", nil)
	rec := httptest.NewRecorder()

	NewRouterWithHistory(&fakeRateFetcher{}, history, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if history.limit != 2 {
		t.Fatalf("limit passed to history = %d, want 2", history.limit)
	}
}

func TestRatesHistoryHandlerReturnsBadRequestForInvalidCurrency(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=USDT", nil)
	rec := httptest.NewRecorder()

	NewRouterWithHistory(&fakeRateFetcher{}, &fakeRateHistoryReader{err: domain.ErrInvalidCurrencyCode}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, rec, "currency must be a 3-letter code")
}

func TestRatesHistoryHandlerValidatesCurrencyBeforeHistoryStore(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=USDT", nil)
	rec := httptest.NewRecorder()

	NewRouterWithHistory(&fakeRateFetcher{}, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, rec, "currency must be a 3-letter code")
}

func TestRatesHistoryHandlerReportsTypedNilHistoryStore(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=USD", nil)
	rec := httptest.NewRecorder()

	var history *fakeRateHistoryReader
	NewRouterWithHistory(&fakeRateFetcher{}, history, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	assertErrorResponse(t, rec, "rate history store is not configured")
}

func TestRatesHistoryHandlerReturnsGatewayTimeoutOnDeadline(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=USD", nil)
	rec := httptest.NewRecorder()

	NewRouterWithHistory(&fakeRateFetcher{}, &fakeRateHistoryReader{err: context.DeadlineExceeded}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusGatewayTimeout)
	}
	assertErrorResponse(t, rec, "request timed out")
}

func TestRatesHistoryHandlerReturnsClientClosedRequestOnCancellation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=USD", nil)
	rec := httptest.NewRecorder()

	NewRouterWithHistory(&fakeRateFetcher{}, &fakeRateHistoryReader{err: context.Canceled}, nil).ServeHTTP(rec, req)

	if rec.Code != statusClientClosedRequest {
		t.Fatalf("status = %d, want %d", rec.Code, statusClientClosedRequest)
	}
	assertErrorResponse(t, rec, "request canceled")
}

func TestRatesHistoryHandlerReportsUnconfiguredStore(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=USD", nil)
	rec := httptest.NewRecorder()

	NewRouter(&fakeRateFetcher{}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	assertErrorResponse(t, rec, "rate history store is not configured")
}

func TestRatesHistoryHandlerReportsStoreConfigurationError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rates/history?currency=USD", nil)
	rec := httptest.NewRecorder()

	NewRouterWithHistory(&fakeRateFetcher{}, &fakeRateHistoryReader{err: storage.ErrStoreNotConfigured}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	assertErrorResponse(t, rec, "rate history store is not configured")
}

func TestRecovererReturnsJSONError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := recoverer(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	assertErrorResponse(t, rec, "internal server error")
}

func TestRecovererHandlesNilLogger(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	handler := recoverer(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	assertErrorResponse(t, rec, "internal server error")
}

func TestRecovererDoesNotWriteErrorAfterResponseStarted(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/panic-after-write", nil)
	rec := httptest.NewRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := recoverer(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("started"))
		panic("boom")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if rec.Body.String() != "started" {
		t.Fatalf("body = %q, want started", rec.Body.String())
	}
}

func TestRequestLoggerHandlesNilLogger(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler := requestLogger(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRateLimitMiddlewareReturnsTooManyRequests(t *testing.T) {
	limiter := ratelimit.New(1)
	handler := NewRouterWithHistoryAndRateLimit(&fakeRateFetcher{}, &fakeRateHistoryReader{}, nil, limiter)

	firstReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	firstReq.RemoteAddr = "192.0.2.1:1234"
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, firstReq)

	if firstRec.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", firstRec.Code, http.StatusOK)
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	secondReq.RemoteAddr = "192.0.2.1:5678"
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", secondRec.Code, http.StatusTooManyRequests)
	}
	if got := secondRec.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}
	assertErrorResponse(t, secondRec, "rate limit exceeded")
}

func TestRateLimitMiddlewareUsesClientIP(t *testing.T) {
	limiter := ratelimit.New(1)
	handler := NewRouterWithHistoryAndRateLimit(&fakeRateFetcher{}, &fakeRateHistoryReader{}, nil, limiter)

	firstReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	firstReq.RemoteAddr = "192.0.2.1:1234"
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, firstReq)

	secondReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	secondReq.RemoteAddr = "192.0.2.2:1234"
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d", secondRec.Code, http.StatusOK)
	}
}

func TestClientIPReturnsHostFromRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "192.0.2.1:1234"

	if got := clientIP(req); got != "192.0.2.1" {
		t.Fatalf("clientIP() = %q, want 192.0.2.1", got)
	}
}

func TestClientIPFallsBackToRemoteAddrWithoutPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "192.0.2.1"

	if got := clientIP(req); got != "192.0.2.1" {
		t.Fatalf("clientIP() = %q, want 192.0.2.1", got)
	}
}

func TestClientIPTrimsRemoteAddrFallback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = " 192.0.2.1 "

	if got := clientIP(req); got != "192.0.2.1" {
		t.Fatalf("clientIP() = %q, want 192.0.2.1", got)
	}
}

func TestClientIPUsesExplicitFallbackForEmptyRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = ""

	if got := clientIP(req); got != unknownClientIP {
		t.Fatalf("clientIP() = %q, want %q", got, unknownClientIP)
	}
}

func TestMetricsEndpointExposesObservedRequests(t *testing.T) {
	handler := NewRouter(&fakeRateFetcher{}, nil)

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)

	ratesReq := httptest.NewRequest(http.MethodGet, "/rates", nil)
	ratesRec := httptest.NewRecorder()
	handler.ServeHTTP(ratesRec, ratesReq)

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	handler.ServeHTTP(metricsRec, metricsReq)

	if metricsRec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", metricsRec.Code, http.StatusOK)
	}
	if got := metricsRec.Header().Get("Content-Type"); got != "text/plain; version=0.0.4; charset=utf-8" {
		t.Fatalf("metrics Content-Type = %q, want Prometheus text", got)
	}

	body := metricsRec.Body.String()
	if !strings.Contains(body, `http_requests_total{method="GET",path="/health",status="200"} 1`) {
		t.Fatalf("metrics body missing health counter:\n%s", body)
	}
	if !strings.Contains(body, `http_requests_total{method="GET",path="/rates",status="400"} 1`) {
		t.Fatalf("metrics body missing rates counter:\n%s", body)
	}
}

func TestMetricsEndpointCollapsesUnmatchedPaths(t *testing.T) {
	handler := NewRouter(&fakeRateFetcher{}, nil)

	for _, path := range []string{"/missing-one", "/missing-two"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want %d", path, rec.Code, http.StatusNotFound)
		}
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	handler.ServeHTTP(metricsRec, metricsReq)

	body := metricsRec.Body.String()
	if !strings.Contains(body, `http_requests_total{method="GET",path="unmatched",status="404"} 2`) {
		t.Fatalf("metrics body missing collapsed unmatched counter:\n%s", body)
	}
	if strings.Contains(body, `path="/missing-one"`) || strings.Contains(body, `path="/missing-two"`) {
		t.Fatalf("metrics body included raw unmatched paths:\n%s", body)
	}
}

func TestMetricsEndpointUsesKnownPathForRateLimitedRoutes(t *testing.T) {
	metrics := appmetrics.NewHTTPMetrics()
	handler := NewRouterWithHistoryRateLimitAndMetrics(&fakeRateFetcher{}, &fakeRateHistoryReader{}, nil, ratelimit.New(1), metrics)

	firstReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	firstReq.RemoteAddr = "192.0.2.10:1234"
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, firstReq)

	secondReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	secondReq.RemoteAddr = "192.0.2.10:5678"
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", secondRec.Code, http.StatusTooManyRequests)
	}

	var body strings.Builder
	if err := metrics.WritePrometheus(&body); err != nil {
		t.Fatalf("WritePrometheus() error = %v", err)
	}
	if !strings.Contains(body.String(), `http_requests_total{method="GET",path="/health",status="429"} 1`) {
		t.Fatalf("metrics body missing rate-limited health counter:\n%s", body.String())
	}
}

func TestMetricsEndpointCollapsesRateLimitedUnknownPaths(t *testing.T) {
	metrics := appmetrics.NewHTTPMetrics()
	handler := NewRouterWithHistoryRateLimitAndMetrics(&fakeRateFetcher{}, &fakeRateHistoryReader{}, nil, ratelimit.New(1), metrics)

	firstReq := httptest.NewRequest(http.MethodGet, "/missing-one", nil)
	firstReq.RemoteAddr = "192.0.2.20:1234"
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, firstReq)

	secondReq := httptest.NewRequest(http.MethodGet, "/missing-two", nil)
	secondReq.RemoteAddr = "192.0.2.20:5678"
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", secondRec.Code, http.StatusTooManyRequests)
	}

	var body strings.Builder
	if err := metrics.WritePrometheus(&body); err != nil {
		t.Fatalf("WritePrometheus() error = %v", err)
	}
	if !strings.Contains(body.String(), `http_requests_total{method="GET",path="unmatched",status="429"} 1`) {
		t.Fatalf("metrics body missing collapsed rate-limited unmatched counter:\n%s", body.String())
	}
	if strings.Contains(body.String(), `path="/missing-two"`) {
		t.Fatalf("metrics body included raw rate-limited path:\n%s", body.String())
	}
}

func TestRequestMetricsKeepsFirstWrittenStatus(t *testing.T) {
	metrics := appmetrics.NewHTTPMetrics()
	handler := requestMetrics(metrics)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodPost, "/double-header", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var body strings.Builder
	if err := metrics.WritePrometheus(&body); err != nil {
		t.Fatalf("WritePrometheus() error = %v", err)
	}
	if !strings.Contains(body.String(), `http_requests_total{method="POST",path="/double-header",status="201"} 1`) {
		t.Fatalf("metrics body missing first status counter:\n%s", body.String())
	}
	if strings.Contains(body.String(), `status="500"`) {
		t.Fatalf("metrics body recorded later status:\n%s", body.String())
	}
}

func TestStatusRecorderWriteRecordsImplicitOK(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := &statusRecorder{ResponseWriter: rec}

	if _, err := wrapped.Write([]byte("ok")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if wrapped.status != http.StatusOK {
		t.Fatalf("recorded status = %d, want %d", wrapped.status, http.StatusOK)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want ok", rec.Body.String())
	}
}

func TestStatusRecorderSupportsResponseControllerFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	if err := http.NewResponseController(wrapped).Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if !rec.Flushed {
		t.Fatal("wrapped response writer did not flush underlying recorder")
	}
}

func TestMetricsEndpointExposesSharedCollectorCounters(t *testing.T) {
	metrics := appmetrics.NewHTTPMetrics()
	metrics.ObserveCacheHit("USD")
	metrics.ObserveBankRequestError("Offline Bank")
	handler := NewRouterWithHistoryRateLimitAndMetrics(&fakeRateFetcher{}, nil, nil, nil, metrics)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `rate_cache_hits_total{currency="USD"} 1`) {
		t.Fatalf("metrics body missing cache hit counter:\n%s", body)
	}
	if !strings.Contains(body, `bank_request_errors_total{bank="Offline Bank"} 1`) {
		t.Fatalf("metrics body missing bank error counter:\n%s", body)
	}
}

func TestPprofIndexIsMounted(t *testing.T) {
	handler := NewRouter(&fakeRateFetcher{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("pprof status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "goroutine") {
		t.Fatalf("pprof body missing goroutine profile link:\n%s", rec.Body.String())
	}
}

type fakeRateFetcher struct {
	result   domain.RateResult
	err      error
	currency string
}

func (f *fakeRateFetcher) FetchRates(ctx context.Context, currency string) (domain.RateResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.RateResult{}, err
	}
	f.currency = currency
	if f.err != nil {
		return domain.RateResult{}, f.err
	}
	return f.result, nil
}

type fakeRateHistoryReader struct {
	rates    []domain.CurrencyRate
	err      error
	currency string
	limit    int
}

func (f *fakeRateHistoryReader) History(ctx context.Context, currency string, limit int) ([]domain.CurrencyRate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.currency = currency
	f.limit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.rates, nil
}

func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()

	var body errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error != want {
		t.Fatalf("error body = %q, want %q", body.Error, want)
	}
}
