package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimiterAllowsRequestsWithinLimit(t *testing.T) {
	limiter := New(2)

	if !limiter.Allow("127.0.0.1") {
		t.Fatal("first request rejected")
	}
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("second request rejected")
	}
}

func TestLimiterRejectsRequestsOverLimitForSameKey(t *testing.T) {
	limiter := New(1)

	if !limiter.Allow("127.0.0.1") {
		t.Fatal("first request rejected")
	}
	if limiter.Allow("127.0.0.1") {
		t.Fatal("second request allowed, want rejected")
	}
}

func TestLimiterTracksKeysIndependently(t *testing.T) {
	limiter := New(1)

	if !limiter.Allow("127.0.0.1") {
		t.Fatal("first key rejected")
	}
	if !limiter.Allow("127.0.0.2") {
		t.Fatal("second key rejected")
	}
}

func TestLimiterResetsAfterWindow(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	limiter := New(1)
	limiter.now = func() time.Time { return now }

	if !limiter.Allow("127.0.0.1") {
		t.Fatal("first request rejected")
	}
	if limiter.Allow("127.0.0.1") {
		t.Fatal("second request allowed before reset")
	}

	now = now.Add(Window)
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("request rejected after reset")
	}
}

func TestLimiterRemovesExpiredKeys(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	limiter := New(1)
	limiter.now = func() time.Time { return now }

	if !limiter.Allow("127.0.0.1") {
		t.Fatal("first key rejected")
	}
	if !limiter.Allow("127.0.0.2") {
		t.Fatal("second key rejected")
	}

	now = now.Add(Window)
	if !limiter.Allow("127.0.0.3") {
		t.Fatal("third key rejected after previous windows expired")
	}

	if len(limiter.clients) != 1 {
		t.Fatalf("tracked clients = %d, want 1", len(limiter.clients))
	}
	if _, ok := limiter.clients["127.0.0.3"]; !ok {
		t.Fatal("current key was not retained")
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
		t.Fatalf("allowed concurrent requests = %d, want %d", got, limit)
	}
}
