package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Memory is a fixed-window counter, safe for concurrent use. Eviction
// is lazy (no background goroutine to leak). Swap for the Redis limiter
// when running on more than one replica: in-memory state is per-pod, so
// N replicas multiplies the effective cap by N.
type Memory struct {
	mu        sync.Mutex
	now       func() time.Time
	limit     int
	window    time.Duration
	buckets   map[string]*bucket
	lastSwept time.Time
}

type bucket struct {
	count     int
	expiresAt time.Time
}

// NewMemory admits up to limit requests per window per key. Bad inputs
// panic — this is a programmer error, not a runtime condition.
func NewMemory(limit int, window time.Duration) *Memory {
	if limit <= 0 {
		panic("ratelimit: limit must be > 0")
	}
	if window <= 0 {
		panic("ratelimit: window must be > 0")
	}
	return &Memory{
		now:     time.Now,
		limit:   limit,
		window:  window,
		buckets: map[string]*bucket{},
	}
}

// WithClock injects a clock for tests. Must be monotonic at the
// window's scale; backwards jumps are undefined.
func (m *Memory) WithClock(now func() time.Time) *Memory {
	m.now = now
	return m
}

func (m *Memory) Allow(_ context.Context, key string) (bool, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	m.maybeSweep(now)

	b, ok := m.buckets[key]
	if !ok || now.After(b.expiresAt) {
		m.buckets[key] = &bucket{count: 1, expiresAt: now.Add(m.window)}
		return true, 0, nil
	}
	if b.count >= m.limit {
		return false, b.expiresAt.Sub(now), nil
	}
	b.count++
	return true, 0, nil
}

// maybeSweep runs at most once per window. Heavier traffic should
// swap to the Redis limiter rather than tune this loop.
func (m *Memory) maybeSweep(now time.Time) {
	if now.Sub(m.lastSwept) < m.window {
		return
	}
	for k, b := range m.buckets {
		if now.After(b.expiresAt) {
			delete(m.buckets, k)
		}
	}
	m.lastSwept = now
}

// Len is for tests; not part of the Limiter contract.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.buckets)
}

// Quota satisfies Quotaer so the middleware can emit X-RateLimit-*
// headers. Keys we have not seen yet return the full quota.
func (m *Memory) Quota(_ context.Context, key string) (limit, remaining int, resetAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.buckets[key]
	if !ok || m.now().After(b.expiresAt) {
		return m.limit, m.limit, m.now().Add(m.window)
	}
	rem := m.limit - b.count
	if rem < 0 {
		rem = 0
	}
	return m.limit, rem, b.expiresAt
}
