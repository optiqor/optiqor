package prom

import (
	"context"
	"sync"
	"time"
)

// CachingClient wraps a Client with TTL caching. Instant queries TTL
// short (60s default) — the snapshot loop fires once per minute and
// repeat hits within that window are wasted round-trips. Range queries
// TTL long (24h default) — the 30-day quantile_over_time shape costs
// the customer's Prometheus minutes of CPU at most installs, so we
// align with the doc's "run them once daily during quiet hours".
//
// Keys hash the PromQL string + range shape; the timestamp is bucketed
// to the cache TTL so two calls one second apart hit the same entry.
// Concurrent in-flight queries collapse via singleflight so 200 ticks
// firing the same query in parallel still cost one round-trip.
type CachingClient struct {
	Client     Client
	InstantTTL time.Duration
	RangeTTL   time.Duration
	NowFunc    func() time.Time
	MaxEntries int

	mu       sync.Mutex
	entries  map[string]*cacheEntry
	inflight map[string]*inflightCall
}

type cacheEntry struct {
	expiresAt time.Time
	samples   []Sample
	series    []Series
	err       error
}

type inflightCall struct {
	wg      sync.WaitGroup
	samples []Sample
	series  []Series
	err     error
}

// NewCachingClient applies safe defaults. Pass zero TTLs to fall through
// to the doc-recommended values (60s instant, 24h range). MaxEntries=0
// caps at 1024 — the canonical query set is ~12 queries × ~200 workloads
// ≈ 2400 unique keys in the worst case; 1024 catches the common case
// without unbounded growth on a label-churn pathology.
func NewCachingClient(inner Client, instantTTL, rangeTTL time.Duration) *CachingClient {
	if instantTTL <= 0 {
		instantTTL = time.Minute
	}
	if rangeTTL <= 0 {
		rangeTTL = 24 * time.Hour
	}
	return &CachingClient{
		Client:     inner,
		InstantTTL: instantTTL,
		RangeTTL:   rangeTTL,
		NowFunc:    time.Now,
		MaxEntries: 1024,
		entries:    map[string]*cacheEntry{},
		inflight:   map[string]*inflightCall{},
	}
}

func (c *CachingClient) Query(ctx context.Context, promql string, at time.Time) ([]Sample, error) {
	key := "q:" + promql + "@" + bucketKey(at, c.InstantTTL)
	call, leader := c.acquire(key)
	if !leader {
		call.wg.Wait()
		return call.samples, call.err
	}
	samples, err := c.Client.Query(ctx, promql, at)
	call.samples = samples
	call.err = err
	c.release(key, call, &cacheEntry{
		expiresAt: c.now().Add(c.InstantTTL),
		samples:   samples,
		err:       err,
	})
	return samples, err
}

func (c *CachingClient) QueryRange(ctx context.Context, promql string, r Range) ([]Series, error) {
	key := "qr:" + promql + "|" + r.Start.UTC().Format(time.RFC3339) +
		"|" + r.End.UTC().Format(time.RFC3339) + "|" + r.Step.String()
	call, leader := c.acquire(key)
	if !leader {
		call.wg.Wait()
		return call.series, call.err
	}
	series, err := c.Client.QueryRange(ctx, promql, r)
	call.series = series
	call.err = err
	c.release(key, call, &cacheEntry{
		expiresAt: c.now().Add(c.RangeTTL),
		series:    series,
		err:       err,
	})
	return series, err
}

// acquire is the atomic "lookup-or-create" the singleflight pattern
// needs. Returns (call, leader) where leader=true means "you do the
// real work and then call release", leader=false means "wait on
// call.wg and read the result." Cache hits embed the cached value in
// the returned call so the non-leader path returns the cached samples
// without a second lookup.
func (c *CachingClient) acquire(key string) (*inflightCall, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		if !c.now().After(e.expiresAt) {
			done := &inflightCall{samples: e.samples, series: e.series, err: e.err}
			// Pre-completed; caller won't actually wait.
			return done, false
		}
		delete(c.entries, key)
	}
	if existing, ok := c.inflight[key]; ok {
		return existing, false
	}
	call := &inflightCall{}
	call.wg.Add(1)
	c.inflight[key] = call
	return call, true
}

func (c *CachingClient) release(key string, call *inflightCall, e *cacheEntry) {
	c.mu.Lock()
	delete(c.inflight, key)
	c.storeLocked(key, e)
	c.mu.Unlock()
	call.wg.Done()
}

func (c *CachingClient) storeLocked(key string, e *cacheEntry) {
	if len(c.entries) >= c.MaxEntries {
		// Drop the oldest-expiry entry. Linear scan is fine at this
		// scale — MaxEntries is ~1024 and writes are at most once per
		// snapshot interval.
		var oldestKey string
		var oldest time.Time
		for k, v := range c.entries {
			if oldestKey == "" || v.expiresAt.Before(oldest) {
				oldestKey, oldest = k, v.expiresAt
			}
		}
		if oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}
	c.entries[key] = e
}

func (c *CachingClient) now() time.Time {
	if c.NowFunc != nil {
		return c.NowFunc()
	}
	return time.Now()
}

func bucketKey(at time.Time, ttl time.Duration) string {
	if ttl <= 0 {
		return at.UTC().Format(time.RFC3339)
	}
	return time.Unix(at.Unix()-(at.Unix()%int64(ttl.Seconds())), 0).UTC().Format(time.RFC3339)
}
