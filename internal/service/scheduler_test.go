package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"currency-rate-aggregator/internal/domain"
)

func TestNewSchedulerValidatesInputs(t *testing.T) {
	fetcher := &schedulerFetcher{}
	var typedNilFetcher *schedulerFetcher

	tests := []struct {
		name       string
		fetcher    scheduledRateFetcher
		currencies []string
		interval   time.Duration
	}{
		{name: "missing fetcher", fetcher: nil, currencies: []string{"USD"}, interval: time.Second},
		{name: "typed nil fetcher", fetcher: typedNilFetcher, currencies: []string{"USD"}, interval: time.Second},
		{name: "missing currencies", fetcher: fetcher, currencies: nil, interval: time.Second},
		{name: "invalid currency", fetcher: fetcher, currencies: []string{"USDT"}, interval: time.Second},
		{name: "non-positive interval", fetcher: fetcher, currencies: []string{"USD"}, interval: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewScheduler(tt.fetcher, tt.currencies, tt.interval, nil); err == nil {
				t.Fatal("NewScheduler() error = nil, want error")
			}
		})
	}
}

func TestSchedulerRefreshesCurrenciesImmediatelyAndStops(t *testing.T) {
	fetcher := &schedulerFetcher{
		results: map[string]domain.RateResult{
			"USD": {Currency: "USD", Sources: []domain.CurrencyRate{{Currency: "USD", Bank: "Bank A"}}},
			"EUR": {Currency: "EUR", Sources: []domain.CurrencyRate{{Currency: "EUR", Bank: "Bank A"}}},
		},
	}
	scheduler := mustScheduler(t, fetcher, []string{" usd ", "EUR", "USD"}, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()

	fetcher.waitForCalls(t, 2)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for scheduler to stop")
	}

	calls := fetcher.callsSnapshot()
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want two unique currencies", calls)
	}
	if calls[0] != "USD" || calls[1] != "EUR" {
		t.Fatalf("calls = %v, want [USD EUR]", calls)
	}
}

func TestSchedulerContinuesAfterRefreshError(t *testing.T) {
	fetcher := &schedulerFetcher{
		results: map[string]domain.RateResult{
			"EUR": {Currency: "EUR", Sources: []domain.CurrencyRate{{Currency: "EUR", Bank: "Bank A"}}},
		},
		errs: map[string]error{
			"USD": errors.New("bank failed"),
		},
	}
	scheduler := mustScheduler(t, fetcher, []string{"USD", "EUR"}, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()

	fetcher.waitForCalls(t, 2)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for scheduler to stop")
	}

	calls := fetcher.callsSnapshot()
	if len(calls) != 2 || calls[0] != "USD" || calls[1] != "EUR" {
		t.Fatalf("calls = %v, want scheduler to continue after USD error", calls)
	}
}

func TestSchedulerRefreshesOnTicker(t *testing.T) {
	fetcher := &schedulerFetcher{
		results: map[string]domain.RateResult{
			"USD": {Currency: "USD", Sources: []domain.CurrencyRate{{Currency: "USD", Bank: "Bank A"}}},
		},
	}
	scheduler := mustScheduler(t, fetcher, []string{"USD"}, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()

	fetcher.waitForCalls(t, 2)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for scheduler to stop")
	}
}

func TestSchedulerUsesFreshRefreshWhenAvailable(t *testing.T) {
	fetcher := &schedulerRefreshFetcher{
		schedulerFetcher: schedulerFetcher{
			results: map[string]domain.RateResult{
				"USD": {Currency: "USD", Sources: []domain.CurrencyRate{{Currency: "USD", Bank: "Bank A"}}},
			},
		},
	}
	scheduler := mustScheduler(t, fetcher, []string{"USD"}, time.Hour)

	scheduler.refreshAll(context.Background())

	calls := fetcher.callsSnapshot()
	if len(calls) != 1 || calls[0] != "USD" {
		t.Fatalf("refresh calls = %v, want [USD]", calls)
	}
	if got := fetcher.cachedFetchCalls(); got != 0 {
		t.Fatalf("cached FetchRates calls = %d, want 0", got)
	}
}

func mustScheduler(t *testing.T, fetcher scheduledRateFetcher, currencies []string, interval time.Duration) *Scheduler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	scheduler, err := NewScheduler(fetcher, currencies, interval, logger)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	return scheduler
}

type schedulerFetcher struct {
	mu      sync.Mutex
	changed chan struct{}
	calls   []string
	results map[string]domain.RateResult
	errs    map[string]error
}

func (f *schedulerFetcher) FetchRates(ctx context.Context, currency string) (domain.RateResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.RateResult{}, err
	}

	f.mu.Lock()
	f.calls = append(f.calls, currency)
	f.notifyChangedLocked()
	err := f.errs[currency]
	result := f.results[currency]
	f.mu.Unlock()

	if err != nil {
		return domain.RateResult{}, err
	}
	if result.Currency == "" {
		result.Currency = currency
	}
	return result, nil
}

func (f *schedulerFetcher) waitForCalls(t *testing.T, count int) {
	t.Helper()

	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()

	for {
		f.mu.Lock()
		if len(f.calls) >= count {
			f.mu.Unlock()
			return
		}
		if f.changed == nil {
			f.changed = make(chan struct{})
		}
		changed := f.changed
		calls := append([]string(nil), f.calls...)
		f.mu.Unlock()

		select {
		case <-changed:
		case <-timeout.C:
			t.Fatalf("timed out waiting for %d calls; got %v", count, calls)
		}
	}
}

func (f *schedulerFetcher) notifyChangedLocked() {
	if f.changed == nil {
		return
	}
	close(f.changed)
	f.changed = nil
}

func (f *schedulerFetcher) callsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.calls...)
}

type schedulerRefreshFetcher struct {
	schedulerFetcher
	fetchCalls int
}

func (f *schedulerRefreshFetcher) FetchRates(_ context.Context, _ string) (domain.RateResult, error) {
	f.mu.Lock()
	f.fetchCalls++
	f.mu.Unlock()

	return domain.RateResult{}, errors.New("cached fetch should not be used")
}

func (f *schedulerRefreshFetcher) RefreshRates(ctx context.Context, currency string) (domain.RateResult, error) {
	return f.schedulerFetcher.FetchRates(ctx, currency)
}

func (f *schedulerRefreshFetcher) cachedFetchCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.fetchCalls
}
