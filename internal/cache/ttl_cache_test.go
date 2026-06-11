package cache

import (
	"sync"
	"testing"
	"time"

	"currency-rate-aggregator/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTTLCacheReturnsValueWithinTTL(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	cache := NewTTLCache(time.Minute)
	cache.now = func() time.Time { return now }

	cache.Set("USD", domain.RateResult{
		Currency: "USD",
		Sources:  []domain.CurrencyRate{{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
	})

	got, ok := cache.Get("USD")
	require.True(t, ok,
		"Get() ok = false, want true")
	require.EqualValuesf(t, "USD", got.Currency,
		"Currency = %q, want USD", got.Currency)
	require.Lenf(t, got.Sources, 1,
		"len(Sources) = %d, want 1", len(got.Sources))

}

func TestTTLCacheExpiresValueAfterTTL(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	cache := NewTTLCache(time.Minute)
	cache.now = func() time.Time { return now }

	cache.Set("USD", domain.RateResult{Currency: "USD"})

	now = now.Add(time.Minute)

	if _, ok := cache.Get("USD"); ok {
		require.FailNow(t, "test failed", "Get() ok = true, want false")
	}
}

func TestTTLCacheCopiesSources(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	cache := NewTTLCache(time.Minute)
	cache.now = func() time.Time { return now }

	source := domain.RateResult{
		Currency: "USD",
		Sources:  []domain.CurrencyRate{{Currency: "USD", Bank: "Bank A", Buy: 91.2, Sell: 92.1}},
	}
	cache.Set("USD", source)
	source.Sources[0].Bank = "Changed"

	got, ok := cache.Get("USD")
	require.True(t, ok,
		"Get() ok = false, want true")

	got.Sources[0].Bank = "Mutated"

	gotAgain, ok := cache.Get("USD")
	require.True(t, ok,
		"Get() second ok = false, want true")
	require.EqualValuesf(t, "Bank A", gotAgain.Sources[0].Bank,
		"cached source bank = %q, want Bank A", gotAgain.Sources[0].Bank)

}

func TestTTLCacheIsConcurrentSafe(t *testing.T) {
	cache := NewTTLCache(time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			cache.Set("USD", domain.RateResult{Currency: "USD"})
		}()
		go func() {
			defer wg.Done()
			_, _ = cache.Get("USD")
		}()
	}
	wg.Wait()
}
