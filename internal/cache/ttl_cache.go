package cache

import (
	"sync"
	"time"

	"currency-rate-aggregator/internal/domain"
)

type TTLCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]entry
}

type entry struct {
	value     domain.RateResult
	expiresAt time.Time
}

func NewTTLCache(ttl time.Duration) *TTLCache {
	return &TTLCache{
		ttl:     ttl,
		now:     func() time.Time { return time.Now().UTC() },
		entries: make(map[string]entry),
	}
}

func (c *TTLCache) Get(currency string) (domain.RateResult, bool) {
	if c == nil || c.ttl <= 0 {
		return domain.RateResult{}, false
	}

	now := c.now()

	c.mu.RLock()
	item, ok := c.entries[currency]
	c.mu.RUnlock()

	if !ok || !now.Before(item.expiresAt) {
		if ok {
			c.mu.Lock()
			if current, exists := c.entries[currency]; exists && !now.Before(current.expiresAt) {
				delete(c.entries, currency)
			}
			c.mu.Unlock()
		}
		return domain.RateResult{}, false
	}

	return cloneRateResult(item.value), true
}

func (c *TTLCache) Set(currency string, value domain.RateResult) {
	if c == nil || c.ttl <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[currency] = entry{
		value:     cloneRateResult(value),
		expiresAt: c.now().Add(c.ttl),
	}
}

func cloneRateResult(value domain.RateResult) domain.RateResult {
	value.Sources = append([]domain.CurrencyRate(nil), value.Sources...)
	return value
}
