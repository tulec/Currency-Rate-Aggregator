package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"currency-rate-aggregator/internal/bankclient"
	"currency-rate-aggregator/internal/domain"
)

func TestAggregatorFetchRatesSelectsBestRates(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 18, 10, 1, 0, 0, time.UTC)
	aggregator := NewAggregator(testClients(
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1, FetchedAt: now}},
		testClient{name: "Bank B", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank B", Buy: 91.0, Sell: 91.8, FetchedAt: now}},
	))
	aggregator.now = func() time.Time { return updatedAt }

	result, err := aggregator.FetchRates(context.Background(), " usd ")
	if err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	if result.Currency != "USD" {
		t.Fatalf("Currency = %q, want USD", result.Currency)
	}
	if result.BestBuy.Bank != "Bank A" {
		t.Fatalf("BestBuy.Bank = %q, want Bank A", result.BestBuy.Bank)
	}
	if result.BestSell.Bank != "Bank B" {
		t.Fatalf("BestSell.Bank = %q, want Bank B", result.BestSell.Bank)
	}
	if len(result.Sources) != 2 {
		t.Fatalf("len(Sources) = %d, want 2", len(result.Sources))
	}
	if !result.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("UpdatedAt = %v, want %v", result.UpdatedAt, updatedAt)
	}
}

func TestAggregatorFetchRatesReturnsPartialDataWhenBankFails(t *testing.T) {
	aggregator := NewAggregator(testClients(
		testClient{name: "Offline Bank", err: domain.ErrBankUnavailable},
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "EUR", Bank: "Bank A", Buy: 99.4, Sell: 100.3}},
	))

	result, err := aggregator.FetchRates(context.Background(), "EUR")
	if err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	if result.BestBuy.Bank != "Bank A" {
		t.Fatalf("BestBuy.Bank = %q, want Bank A", result.BestBuy.Bank)
	}
	if len(result.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(result.Sources))
	}
}

func TestAggregatorFetchRatesAggregatesConfiguredMultipleSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cbr.xml":
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ValCurs Date="05.06.2026"><Valute><CharCode>USD</CharCode><Nominal>1</Nominal><Value>70,0000</Value></Valute></ValCurs>`))
		case "/v2/rate/USD/RUB":
			_, _ = w.Write([]byte(`{"base":"USD","quote":"RUB","date":"2026-06-05","rate":80}`))
		case "/tbank/rates":
			_, _ = w.Write([]byte(`{"resultCode":"OK","payload":{"lastUpdate":{"milliseconds":1780685158794},"rates":[{"category":"DebitCardsTransfers","fromCurrency":{"name":"USD"},"toCurrency":{"name":"RUB"},"buy":72.3,"sell":78.95}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	clients, err := bankclient.NewClients([]string{"cbr", "frankfurter", "tbank", "mock"}, bankclient.SourceOptions{
		CBRDailyURL:        server.URL + "/cbr.xml",
		FrankfurterBaseURL: server.URL + "/v2",
		TBankRatesURL:      server.URL + "/tbank/rates",
	})
	if err != nil {
		t.Fatalf("NewClients() error = %v", err)
	}
	aggregator := NewAggregator(clients)

	result, err := aggregator.FetchRates(context.Background(), "USD")
	if err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	if len(result.Sources) != 5 {
		t.Fatalf("sources = %d, want 5 successful sources", len(result.Sources))
	}
	if result.BestBuy.Bank != "North Bank" {
		t.Fatalf("BestBuy.Bank = %q, want North Bank", result.BestBuy.Bank)
	}
	if result.BestSell.Bank != "Bank of Russia" {
		t.Fatalf("BestSell.Bank = %q, want Bank of Russia", result.BestSell.Bank)
	}
}

func TestAggregatorFetchRatesWaitsForSuccessfulBankAfterFailure(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	aggregator := NewAggregator(testClients(
		testClient{name: "Offline Bank", err: domain.ErrBankUnavailable},
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "EUR", Bank: "Bank A", Buy: 99.4, Sell: 100.3}, started: started, release: release},
	))

	done := make(chan error, 1)
	go func() {
		result, err := aggregator.FetchRates(context.Background(), "EUR")
		if err == nil && result.BestBuy.Bank != "Bank A" {
			err = errors.New("expected Bank A result")
		}
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for successful bank to start")
	}

	select {
	case err := <-done:
		t.Fatalf("FetchRates() finished before successful bank responded: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("FetchRates() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for FetchRates")
	}
}

func TestAggregatorFetchRatesSkipsUnexpectedSourceCurrency(t *testing.T) {
	metrics := newFakeAggregatorMetrics()
	aggregator := NewAggregator([]bankclient.BankClient{
		rawRateClient{name: "Wrong Bank", rate: domain.CurrencyRate{Currency: "EUR", Bank: "Wrong Bank", Buy: 99.4, Sell: 100.3}},
		rawRateClient{name: "Bank A", rate: domain.CurrencyRate{Currency: " usd ", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
	}).WithMetrics(metrics)

	result, err := aggregator.FetchRates(context.Background(), "USD")
	if err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	if len(result.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(result.Sources))
	}
	if result.Sources[0].Bank != "Bank A" {
		t.Fatalf("source bank = %q, want Bank A", result.Sources[0].Bank)
	}
	if result.Sources[0].Currency != "USD" {
		t.Fatalf("source currency = %q, want USD", result.Sources[0].Currency)
	}
	if metrics.bankErrors["Wrong Bank"] != 1 {
		t.Fatalf("bank error metrics = %d, want 1", metrics.bankErrors["Wrong Bank"])
	}
}

func TestAggregatorFetchRatesNormalizesSourceTimestampsToUTC(t *testing.T) {
	offset := time.FixedZone("MSK", 3*60*60)
	fetchedAt := time.Date(2026, 5, 18, 13, 0, 0, 0, offset)
	aggregator := NewAggregator([]bankclient.BankClient{
		rawRateClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1, FetchedAt: fetchedAt}},
	})

	result, err := aggregator.FetchRates(context.Background(), "USD")
	if err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	if result.Sources[0].FetchedAt.Location() != time.UTC {
		t.Fatalf("source FetchedAt location = %v, want UTC", result.Sources[0].FetchedAt.Location())
	}
	if !result.Sources[0].FetchedAt.Equal(fetchedAt) {
		t.Fatalf("source FetchedAt = %v, want same instant as %v", result.Sources[0].FetchedAt, fetchedAt)
	}
	if result.BestBuy.FetchedAt.Location() != time.UTC {
		t.Fatalf("best buy FetchedAt location = %v, want UTC", result.BestBuy.FetchedAt.Location())
	}
	if result.BestSell.FetchedAt.Location() != time.UTC {
		t.Fatalf("best sell FetchedAt location = %v, want UTC", result.BestSell.FetchedAt.Location())
	}
}

func TestAggregatorFetchRatesReturnsNoRatesWhenSourcesUseWrongCurrency(t *testing.T) {
	aggregator := NewAggregator([]bankclient.BankClient{
		rawRateClient{name: "Wrong Bank", rate: domain.CurrencyRate{Currency: "EUR", Bank: "Wrong Bank", Buy: 99.4, Sell: 100.3}},
	})

	_, err := aggregator.FetchRates(context.Background(), "USD")
	if !errors.Is(err, ErrNoRatesAvailable) {
		t.Fatalf("FetchRates() error = %v, want ErrNoRatesAvailable", err)
	}
	if !errors.Is(err, errUnexpectedCurrency) {
		t.Fatalf("FetchRates() error = %v, want errUnexpectedCurrency", err)
	}
}

func TestAggregatorFetchRatesIgnoresNilClients(t *testing.T) {
	clients := []bankclient.BankClient{
		nil,
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
	}
	aggregator := NewAggregator(clients)

	result, err := aggregator.FetchRates(context.Background(), "USD")
	if err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	if len(result.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(result.Sources))
	}
	if result.BestBuy.Bank != "Bank A" {
		t.Fatalf("BestBuy.Bank = %q, want Bank A", result.BestBuy.Bank)
	}
}

func TestAggregatorFetchRatesReturnsNoRatesWhenOnlyNilClientsExist(t *testing.T) {
	aggregator := NewAggregator([]bankclient.BankClient{nil})

	_, err := aggregator.FetchRates(context.Background(), "USD")
	if !errors.Is(err, ErrNoRatesAvailable) {
		t.Fatalf("FetchRates() error = %v, want ErrNoRatesAvailable", err)
	}
}

func TestAggregatorFetchRatesReturnsNoRatesWhenOnlyTypedNilClientsExist(t *testing.T) {
	var typedNil *bankclient.MockBank
	aggregator := NewAggregator([]bankclient.BankClient{typedNil})

	_, err := aggregator.FetchRates(context.Background(), "USD")
	if !errors.Is(err, ErrNoRatesAvailable) {
		t.Fatalf("FetchRates() error = %v, want ErrNoRatesAvailable", err)
	}
}

func TestAggregatorFetchRatesIgnoresTypedNilCacheAndStore(t *testing.T) {
	var typedNilCache *fakeRateCache
	var typedNilStore *fakeRateStore

	aggregator := NewAggregatorWithCacheAndStore(testClients(
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
	), typedNilCache, typedNilStore)

	result, err := aggregator.FetchRates(context.Background(), "USD")
	if err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}
	if result.BestBuy.Bank != "Bank A" {
		t.Fatalf("BestBuy.Bank = %q, want Bank A", result.BestBuy.Bank)
	}
}

func TestAggregatorFetchRatesIgnoresTypedNilMetrics(t *testing.T) {
	var typedNilMetrics *fakeAggregatorMetrics
	aggregator := NewAggregatorWithCache(testClients(
		testClient{name: "Offline Bank", err: domain.ErrBankUnavailable},
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
	), &fakeRateCache{}).WithMetrics(typedNilMetrics)

	result, err := aggregator.FetchRates(context.Background(), "USD")
	if err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}
	if len(result.Sources) != 1 {
		t.Fatalf("len(Sources) = %d, want 1", len(result.Sources))
	}
}

func TestAggregatorFetchRatesReturnsErrorWhenAllBanksFail(t *testing.T) {
	aggregator := NewAggregator(testClients(
		testClient{name: "Offline A", err: domain.ErrBankUnavailable},
		testClient{name: "Offline B", err: domain.ErrCurrencyNotFound},
	))

	_, err := aggregator.FetchRates(context.Background(), "USD")
	if !errors.Is(err, ErrNoRatesAvailable) {
		t.Fatalf("FetchRates() error = %v, want ErrNoRatesAvailable", err)
	}
	if !errors.Is(err, domain.ErrBankUnavailable) {
		t.Fatalf("FetchRates() error = %v, want ErrBankUnavailable in chain", err)
	}
}

func TestAggregatorFetchRatesRunsClientsInParallel(t *testing.T) {
	var inFlight int32
	var maxInFlight int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})

	aggregator := NewAggregator(testClients(
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}, started: started, release: release, inFlight: &inFlight, maxInFlight: &maxInFlight},
		testClient{name: "Bank B", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank B", Buy: 91.0, Sell: 91.8}, started: started, release: release, inFlight: &inFlight, maxInFlight: &maxInFlight},
	))

	done := make(chan error, 1)
	go func() {
		_, err := aggregator.FetchRates(context.Background(), "USD")
		done <- err
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for clients to start")
		}
	}

	if got := atomic.LoadInt32(&maxInFlight); got != 2 {
		t.Fatalf("max in-flight clients = %d, want 2", got)
	}

	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("FetchRates() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for FetchRates")
	}
}

func TestAggregatorFetchRatesStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	client := testClient{
		name:       "Slow Bank",
		rate:       domain.CurrencyRate{Currency: "USD", Bank: "Slow Bank", Buy: 91.2, Sell: 92.1},
		started:    started,
		waitForCtx: true,
	}
	aggregator := NewAggregator(testClients(client))

	done := make(chan error, 1)
	go func() {
		_, err := aggregator.FetchRates(ctx, "USD")
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for client to start")
	}

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("FetchRates() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for FetchRates cancellation")
	}
}

func TestAggregatorFetchRatesReturnsContextErrorFromClient(t *testing.T) {
	metrics := newFakeAggregatorMetrics()
	aggregator := NewAggregator(testClients(
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
		testClient{name: "Canceled Bank", err: context.Canceled},
	)).WithMetrics(metrics)

	_, err := aggregator.FetchRates(context.Background(), "USD")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchRates() error = %v, want context.Canceled", err)
	}
	if metrics.bankErrors["Canceled Bank"] != 0 {
		t.Fatalf("bank error metrics = %d, want 0 for context cancellation", metrics.bankErrors["Canceled Bank"])
	}
}

func TestAggregatorFetchRatesReturnsDeadlineErrorFromClient(t *testing.T) {
	aggregator := NewAggregator(testClients(
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
		testClient{name: "Slow Bank", err: context.DeadlineExceeded},
	))

	_, err := aggregator.FetchRates(context.Background(), "USD")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("FetchRates() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestAggregatorFetchRatesValidatesCurrency(t *testing.T) {
	aggregator := NewAggregator(testClients(
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
	))

	_, err := aggregator.FetchRates(context.Background(), "USDT")
	if !errors.Is(err, domain.ErrInvalidCurrencyCode) {
		t.Fatalf("FetchRates() error = %v, want ErrInvalidCurrencyCode", err)
	}
}

func TestAggregatorFetchRatesUsesCachedResult(t *testing.T) {
	cached := domain.RateResult{
		Currency: "USD",
		BestBuy:  domain.CurrencyRate{Currency: "USD", Bank: "Cached Bank", Buy: 91.2, Sell: 92.1},
		Sources:  []domain.CurrencyRate{{Currency: "USD", Bank: "Cached Bank", Buy: 91.2, Sell: 92.1}},
	}
	cache := &fakeRateCache{result: cached, ok: true}
	aggregator := NewAggregatorWithCache(testClients(
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 90, Sell: 91}},
	), cache)

	result, err := aggregator.FetchRates(context.Background(), " usd ")
	if err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	if result.BestBuy.Bank != "Cached Bank" {
		t.Fatalf("BestBuy.Bank = %q, want Cached Bank", result.BestBuy.Bank)
	}
	if cache.getCurrency != "USD" {
		t.Fatalf("cache get currency = %q, want USD", cache.getCurrency)
	}
	if cache.setCurrency != "" {
		t.Fatalf("cache set currency = %q, want empty", cache.setCurrency)
	}
}

func TestAggregatorRefreshRatesBypassesCachedResult(t *testing.T) {
	cached := domain.RateResult{
		Currency: "USD",
		BestBuy:  domain.CurrencyRate{Currency: "USD", Bank: "Cached Bank", Buy: 91.2, Sell: 92.1},
		Sources:  []domain.CurrencyRate{{Currency: "USD", Bank: "Cached Bank", Buy: 91.2, Sell: 92.1}},
	}
	cache := &fakeRateCache{result: cached, ok: true}
	aggregator := NewAggregatorWithCache(testClients(
		testClient{name: "Fresh Bank", rate: domain.CurrencyRate{Currency: "USD", Bank: "Fresh Bank", Buy: 92, Sell: 93}},
	), cache)

	result, err := aggregator.RefreshRates(context.Background(), " usd ")
	if err != nil {
		t.Fatalf("RefreshRates() error = %v", err)
	}

	if result.BestBuy.Bank != "Fresh Bank" {
		t.Fatalf("BestBuy.Bank = %q, want Fresh Bank", result.BestBuy.Bank)
	}
	if cache.getCurrency != "" {
		t.Fatalf("cache get currency = %q, want empty", cache.getCurrency)
	}
	if cache.setCurrency != "USD" {
		t.Fatalf("cache set currency = %q, want USD", cache.setCurrency)
	}
	if cache.stored.BestBuy.Bank != "Fresh Bank" {
		t.Fatalf("cached best buy bank = %q, want Fresh Bank", cache.stored.BestBuy.Bank)
	}
}

func TestAggregatorFetchRatesObservesCacheHit(t *testing.T) {
	cached := domain.RateResult{
		Currency: "USD",
		BestBuy:  domain.CurrencyRate{Currency: "USD", Bank: "Cached Bank", Buy: 91.2, Sell: 92.1},
		Sources:  []domain.CurrencyRate{{Currency: "USD", Bank: "Cached Bank", Buy: 91.2, Sell: 92.1}},
	}
	metrics := newFakeAggregatorMetrics()
	aggregator := NewAggregatorWithCache(testClients(
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 90, Sell: 91}},
	), &fakeRateCache{result: cached, ok: true}).WithMetrics(metrics)

	if _, err := aggregator.FetchRates(context.Background(), "usd"); err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	if metrics.cacheHits["USD"] != 1 {
		t.Fatalf("cache hit metrics = %d, want 1", metrics.cacheHits["USD"])
	}
	if metrics.cacheMisses["USD"] != 0 {
		t.Fatalf("cache miss metrics = %d, want 0", metrics.cacheMisses["USD"])
	}
}

func TestAggregatorFetchRatesStoresFetchedResultInCache(t *testing.T) {
	cache := &fakeRateCache{}
	aggregator := NewAggregatorWithCache(testClients(
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
	), cache)

	result, err := aggregator.FetchRates(context.Background(), "USD")
	if err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	if cache.setCurrency != "USD" {
		t.Fatalf("cache set currency = %q, want USD", cache.setCurrency)
	}
	if cache.stored.BestBuy.Bank != result.BestBuy.Bank {
		t.Fatalf("cached best buy bank = %q, want %q", cache.stored.BestBuy.Bank, result.BestBuy.Bank)
	}
}

func TestAggregatorFetchRatesObservesCacheMissAndBankErrors(t *testing.T) {
	metrics := newFakeAggregatorMetrics()
	aggregator := NewAggregatorWithCache(testClients(
		testClient{name: "Offline Bank", err: domain.ErrBankUnavailable},
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
	), &fakeRateCache{}).WithMetrics(metrics)

	if _, err := aggregator.FetchRates(context.Background(), "usd"); err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	if metrics.cacheMisses["USD"] != 1 {
		t.Fatalf("cache miss metrics = %d, want 1", metrics.cacheMisses["USD"])
	}
	if metrics.bankErrors["Offline Bank"] != 1 {
		t.Fatalf("bank error metrics = %d, want 1", metrics.bankErrors["Offline Bank"])
	}
}

func TestAggregatorFetchRatesLogsBankErrors(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	aggregator := NewAggregator(testClients(
		testClient{name: "Offline Bank", err: domain.ErrBankUnavailable},
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
	)).WithLogger(logger)

	if _, err := aggregator.FetchRates(context.Background(), "usd"); err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	output := logs.String()
	for _, want := range []string{
		`level=WARN`,
		`msg="bank request failed"`,
		`bank="Offline Bank"`,
		`currency=USD`,
		`error="bank unavailable"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("log output missing %q:\n%s", want, output)
		}
	}
}

func TestAggregatorFetchRatesSavesFetchedSources(t *testing.T) {
	store := &fakeRateStore{}
	aggregator := NewAggregatorWithCacheAndStore(testClients(
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
		testClient{name: "Bank B", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank B", Buy: 91.0, Sell: 91.8}},
	), nil, store)

	result, err := aggregator.FetchRates(context.Background(), " usd ")
	if err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	if store.calls != 1 {
		t.Fatalf("SaveRates calls = %d, want 1", store.calls)
	}
	if len(store.rates) != len(result.Sources) {
		t.Fatalf("saved rates = %d, want %d", len(store.rates), len(result.Sources))
	}
	for _, rate := range store.rates {
		if rate.Currency != "USD" {
			t.Fatalf("saved currency = %q, want USD", rate.Currency)
		}
	}
}

func TestAggregatorFetchRatesReturnsErrorWhenSavingFails(t *testing.T) {
	saveErr := errors.New("save failed")
	store := &fakeRateStore{err: saveErr}
	cache := &fakeRateCache{}
	aggregator := NewAggregatorWithCacheAndStore(testClients(
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
	), cache, store)

	_, err := aggregator.FetchRates(context.Background(), "USD")
	if !errors.Is(err, saveErr) {
		t.Fatalf("FetchRates() error = %v, want save error", err)
	}
	if cache.setCurrency != "" {
		t.Fatalf("cache set currency = %q, want empty after save failure", cache.setCurrency)
	}
}

func TestAggregatorFetchRatesDoesNotSaveCachedResult(t *testing.T) {
	cache := &fakeRateCache{
		result: domain.RateResult{
			Currency: "USD",
			BestBuy:  domain.CurrencyRate{Currency: "USD", Bank: "Cached Bank", Buy: 91.2, Sell: 92.1},
			Sources:  []domain.CurrencyRate{{Currency: "USD", Bank: "Cached Bank", Buy: 91.2, Sell: 92.1}},
		},
		ok: true,
	}
	store := &fakeRateStore{}
	aggregator := NewAggregatorWithCacheAndStore(testClients(
		testClient{name: "Bank A", rate: domain.CurrencyRate{Currency: "USD", Bank: "Bank A", Buy: 90, Sell: 91}},
	), cache, store)

	if _, err := aggregator.FetchRates(context.Background(), "USD"); err != nil {
		t.Fatalf("FetchRates() error = %v", err)
	}

	if store.calls != 0 {
		t.Fatalf("SaveRates calls = %d, want 0 for cached result", store.calls)
	}
}

type testClient struct {
	name        string
	rate        domain.CurrencyRate
	err         error
	started     chan<- struct{}
	release     <-chan struct{}
	waitForCtx  bool
	inFlight    *int32
	maxInFlight *int32
}

func testClients(clients ...testClient) []bankclient.BankClient {
	result := make([]bankclient.BankClient, 0, len(clients))
	for _, client := range clients {
		result = append(result, client)
	}
	return result
}

func (c testClient) Name() string {
	return c.name
}

func (c testClient) FetchRate(ctx context.Context, currency string) (domain.CurrencyRate, error) {
	if c.inFlight != nil && c.maxInFlight != nil {
		current := atomic.AddInt32(c.inFlight, 1)
		defer atomic.AddInt32(c.inFlight, -1)
		for {
			maximum := atomic.LoadInt32(c.maxInFlight)
			if current <= maximum || atomic.CompareAndSwapInt32(c.maxInFlight, maximum, current) {
				break
			}
		}
	}

	if c.started != nil {
		c.started <- struct{}{}
	}

	if c.waitForCtx {
		<-ctx.Done()
		return domain.CurrencyRate{}, ctx.Err()
	}

	if c.release != nil {
		select {
		case <-ctx.Done():
			return domain.CurrencyRate{}, ctx.Err()
		case <-c.release:
		}
	}

	if err := ctx.Err(); err != nil {
		return domain.CurrencyRate{}, err
	}
	if c.err != nil {
		return domain.CurrencyRate{}, c.err
	}

	rate := c.rate
	rate.Currency = domain.CurrencyCode(currency)
	rate.Bank = c.name
	return rate, nil
}

type rawRateClient struct {
	name string
	rate domain.CurrencyRate
}

func (c rawRateClient) Name() string {
	return c.name
}

func (c rawRateClient) FetchRate(ctx context.Context, _ string) (domain.CurrencyRate, error) {
	if err := ctx.Err(); err != nil {
		return domain.CurrencyRate{}, err
	}
	return c.rate, nil
}

type fakeRateCache struct {
	result      domain.RateResult
	stored      domain.RateResult
	ok          bool
	getCurrency string
	setCurrency string
}

func (c *fakeRateCache) Get(currency string) (domain.RateResult, bool) {
	c.getCurrency = currency
	return c.result, c.ok
}

func (c *fakeRateCache) Set(currency string, result domain.RateResult) {
	c.setCurrency = currency
	c.stored = result
}

type fakeRateStore struct {
	rates []domain.CurrencyRate
	err   error
	calls int
}

func (s *fakeRateStore) SaveRates(ctx context.Context, rates []domain.CurrencyRate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.calls++
	s.rates = append([]domain.CurrencyRate(nil), rates...)
	return s.err
}

type fakeAggregatorMetrics struct {
	cacheHits   map[string]int
	cacheMisses map[string]int
	bankErrors  map[string]int
}

func newFakeAggregatorMetrics() *fakeAggregatorMetrics {
	return &fakeAggregatorMetrics{
		cacheHits:   make(map[string]int),
		cacheMisses: make(map[string]int),
		bankErrors:  make(map[string]int),
	}
}

func (m *fakeAggregatorMetrics) ObserveCacheHit(currency string) {
	m.cacheHits[currency]++
}

func (m *fakeAggregatorMetrics) ObserveCacheMiss(currency string) {
	m.cacheMisses[currency]++
}

func (m *fakeAggregatorMetrics) ObserveBankRequestError(bank string) {
	m.bankErrors[bank]++
}
