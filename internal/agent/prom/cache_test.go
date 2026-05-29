package prom

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingClient tracks how many real calls reached the inner client.
// snapshot copies under the mutex so assertions don't race with later
// goroutines fanned out by the singleflight test.
type countingClient struct {
	mu          sync.Mutex
	queryCalls  int
	rangeCalls  int
	samples     []Sample
	series      []Series
	err         error
	releaseGate chan struct{}
}

func (c *countingClient) Query(_ context.Context, _ string, _ time.Time) ([]Sample, error) {
	c.mu.Lock()
	c.queryCalls++
	c.mu.Unlock()
	if c.releaseGate != nil {
		<-c.releaseGate
	}
	return c.samples, c.err
}

func (c *countingClient) QueryRange(_ context.Context, _ string, _ Range) ([]Series, error) {
	c.mu.Lock()
	c.rangeCalls++
	c.mu.Unlock()
	if c.releaseGate != nil {
		<-c.releaseGate
	}
	return c.series, c.err
}

func (c *countingClient) snapshotCalls() (query, queryRange int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.queryCalls, c.rangeCalls
}

func TestCachingClient_QueryCachesWithinTTL(t *testing.T) {
	inner := &countingClient{samples: []Sample{{Value: 42}}}
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	cur := now
	c := NewCachingClient(inner, time.Minute, time.Hour)
	c.NowFunc = func() time.Time { return cur }

	at := now
	if _, err := c.Query(context.Background(), "up", at); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Bump 10s — same TTL bucket. Should be a cache hit.
	cur = cur.Add(10 * time.Second)
	if _, err := c.Query(context.Background(), "up", at); err != nil {
		t.Fatalf("second call: %v", err)
	}
	q, _ := inner.snapshotCalls()
	if q != 1 {
		t.Errorf("inner.queryCalls = %d, want 1 (cache hit)", q)
	}

	// Past the TTL. Should be a fresh call.
	cur = cur.Add(2 * time.Minute)
	if _, err := c.Query(context.Background(), "up", at); err != nil {
		t.Fatalf("third call: %v", err)
	}
	q, _ = inner.snapshotCalls()
	if q != 2 {
		t.Errorf("inner.queryCalls after TTL = %d, want 2", q)
	}
}

func TestCachingClient_QueryRangeCachesPerRange(t *testing.T) {
	inner := &countingClient{series: []Series{{Labels: map[string]string{"foo": "bar"}}}}
	c := NewCachingClient(inner, 0, 24*time.Hour)
	c.NowFunc = func() time.Time { return time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC) }

	r1 := Range{Start: time.Unix(1, 0), End: time.Unix(2, 0), Step: time.Hour}
	r2 := Range{Start: time.Unix(3, 0), End: time.Unix(4, 0), Step: time.Hour}

	if _, err := c.QueryRange(context.Background(), "x", r1); err != nil {
		t.Fatalf("r1: %v", err)
	}
	if _, err := c.QueryRange(context.Background(), "x", r1); err != nil {
		t.Fatalf("r1 again: %v", err)
	}
	if _, err := c.QueryRange(context.Background(), "x", r2); err != nil {
		t.Fatalf("r2: %v", err)
	}
	_, rc := inner.snapshotCalls()
	if rc != 2 {
		t.Errorf("range calls = %d, want 2 (r1 cache hit; r2 miss)", rc)
	}
}

func TestCachingClient_SingleflightCollapsesParallelMisses(t *testing.T) {
	inner := &countingClient{
		samples:     []Sample{{Value: 1}},
		releaseGate: make(chan struct{}),
	}
	c := NewCachingClient(inner, time.Minute, time.Hour)
	c.NowFunc = func() time.Time { return time.Unix(1700000000, 0) }

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, _ = c.Query(context.Background(), "up", time.Unix(1700000000, 0))
		}()
	}
	// Give the goroutines a chance to all line up under the inflight map.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		n := len(c.inflight)
		c.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(inner.releaseGate)
	wg.Wait()

	q, _ := inner.snapshotCalls()
	if q != 1 {
		t.Errorf("inner.queryCalls under singleflight = %d, want 1", q)
	}
}

func TestCachingClient_NilTTLsApplyDefaults(t *testing.T) {
	c := NewCachingClient(&countingClient{}, 0, 0)
	if c.InstantTTL != time.Minute {
		t.Errorf("InstantTTL default = %v, want 1m", c.InstantTTL)
	}
	if c.RangeTTL != 24*time.Hour {
		t.Errorf("RangeTTL default = %v, want 24h", c.RangeTTL)
	}
}

func TestCachingClient_MaxEntriesEvictsOldest(t *testing.T) {
	inner := &countingClient{samples: []Sample{{Value: 1}}}
	c := NewCachingClient(inner, time.Hour, time.Hour)
	c.MaxEntries = 2
	cur := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	c.NowFunc = func() time.Time { return cur }

	// Fill two distinct entries.
	_, _ = c.Query(context.Background(), "a", time.Unix(1700, 0))
	cur = cur.Add(time.Second)
	_, _ = c.Query(context.Background(), "b", time.Unix(1700, 0))
	// Third entry forces eviction of the oldest ("a").
	cur = cur.Add(time.Second)
	_, _ = c.Query(context.Background(), "c", time.Unix(1700, 0))

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) != 2 {
		t.Errorf("entries = %d, want 2 (MaxEntries cap)", len(c.entries))
	}
}

func TestCachingClient_QueryErrorIsCached(t *testing.T) {
	// Errors cache too — otherwise a Prom outage produces a thundering
	// herd as every cache-miss retries. TTL is the same as the success
	// path; cache pressure is the same.
	inner := &countingClient{err: errors.New("prom down")}
	c := NewCachingClient(inner, time.Minute, time.Hour)
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	c.NowFunc = func() time.Time { return now }

	for i := 0; i < 5; i++ {
		_, err := c.Query(context.Background(), "x", time.Unix(1700, 0))
		if err == nil {
			t.Fatalf("iter %d: want error", i)
		}
	}
	q, _ := inner.snapshotCalls()
	if q != 1 {
		t.Errorf("inner.queryCalls under cached error = %d, want 1", q)
	}
}

// Concurrent reads + writes on the cache must be race-clean.
func TestCachingClient_RaceFreeUnderConcurrentLoad(t *testing.T) {
	inner := &countingClient{samples: []Sample{{Value: 1}}}
	c := NewCachingClient(inner, time.Minute, time.Hour)
	c.MaxEntries = 16
	var wg sync.WaitGroup
	var ops atomic.Int32
	stop := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = c.Query(context.Background(), "k", time.Unix(int64(id)*1000, 0))
					ops.Add(1)
				}
			}
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
	if ops.Load() == 0 {
		t.Error("no ops ran")
	}
}
