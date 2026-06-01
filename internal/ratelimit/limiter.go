package ratelimit

import (
	"sync"
	"time"
)

// Window is the fixed counting window used by the in-memory limiter.
const Window = time.Minute

type Limiter struct {
	mu      sync.Mutex
	limit   int
	now     func() time.Time
	clients map[string]entry
}

type entry struct {
	count   int
	resetAt time.Time
}

func New(requestsPerMinute int) *Limiter {
	if requestsPerMinute <= 0 {
		requestsPerMinute = 1
	}
	return &Limiter{
		limit:   requestsPerMinute,
		now:     time.Now,
		clients: make(map[string]entry),
	}
}

func (l *Limiter) Allow(key string) bool {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.deleteExpiredLocked(now)

	current := l.clients[key]
	if current.resetAt.IsZero() || !now.Before(current.resetAt) {
		l.clients[key] = entry{count: 1, resetAt: now.Add(Window)}
		return true
	}

	if current.count >= l.limit {
		return false
	}

	current.count++
	l.clients[key] = current
	return true
}

func (l *Limiter) deleteExpiredLocked(now time.Time) {
	for key, current := range l.clients {
		if !current.resetAt.IsZero() && !now.Before(current.resetAt) {
			delete(l.clients, key)
		}
	}
}
