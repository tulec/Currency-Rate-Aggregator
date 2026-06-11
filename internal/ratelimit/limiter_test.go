package ratelimit

import (
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimiterAllowsRequestsWithinLimit(t *testing.T) {
	limiter := New(2)
	require.True(t, limiter.Allow("127.0.0.1"),
		"first request rejected")
	require.True(t, limiter.Allow("127.0.0.1"),
		"second request rejected")

}

func TestLimiterRejectsRequestsOverLimitForSameKey(t *testing.T) {
	limiter := New(1)
	require.True(t, limiter.Allow("127.0.0.1"),
		"first request rejected")
	require.False(t, limiter.Allow("127.0.0.1"),
		"second request allowed, want rejected")

}

func TestLimiterTracksKeysIndependently(t *testing.T) {
	limiter := New(1)
	require.True(t, limiter.Allow("127.0.0.1"),
		"first key rejected")
	require.True(t, limiter.Allow("127.0.0.2"),
		"second key rejected")

}

func TestLimiterResetsAfterWindow(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	limiter := New(1)
	limiter.now = func() time.Time { return now }
	require.True(t, limiter.Allow("127.0.0.1"),
		"first request rejected")
	require.False(t, limiter.Allow("127.0.0.1"),
		"second request allowed before reset")

	now = now.Add(Window)
	require.True(t, limiter.Allow("127.0.0.1"),
		"request rejected after reset")

}

func TestLimiterRemovesExpiredKeys(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	limiter := New(1)
	limiter.now = func() time.Time { return now }
	require.True(t, limiter.Allow("127.0.0.1"),
		"first key rejected")
	require.True(t, limiter.Allow("127.0.0.2"),
		"second key rejected")

	now = now.Add(Window)
	require.True(t, limiter.Allow("127.0.0.3"),
		"third key rejected after previous windows expired")
	require.Lenf(t, limiter.clients, 1,
		"tracked clients = %d, want 1", len(limiter.clients))

	if _, ok := limiter.clients["127.0.0.3"]; !ok {
		require.FailNow(t, "test failed", "current key was not retained")
	}
}

func TestLimiterAllowsOnlyLimitUnderConcurrentRequests(t *testing.T) {
	const limit = 25

	limiter := New(limit)
	start := make(chan struct{})
	var allowed int32
	var wg sync.WaitGroup

	for i := 0; i < limit*2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if limiter.Allow("127.0.0.1") {
				atomic.AddInt32(&allowed, 1)
			}
		}()
	}

	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&allowed); got != limit {
		require.FailNowf(t, "test failed", "allowed concurrent requests = %d, want %d", got, limit)
	}
}
