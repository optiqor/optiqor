// Package oomkilled scrapes Prometheus for the OOMKilled history per
// workload over a fixed 7-day window. Feeds the validator pipeline's
// `oom-recent` check + the OOMRecent flag on validator.ClusterSignals.
//
// Production wiring uses the Prometheus HTTP API behind a
// QueryClient interface; in-tree InMemoryClient is the test fake.
package oomkilled

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// WorkloadRef is intentionally narrow; we don't reach across to
// internal/agent/k8s because this package may run from the SaaS side
// over a Prometheus scrape rather than from the in-cluster agent.
type WorkloadRef struct {
	ClusterID string
	Namespace string
	Workload  string
}

// QueryClient is the seam to a Prometheus instant-query endpoint.
// Production wires github.com/prometheus/client_golang/api/v1; tests
// use InMemoryClient.
type QueryClient interface {
	Count(ctx context.Context, query string, atTime time.Time) (int, error)
}

// Reader returns the OOMKilled count for a workload over the window.
type Reader struct {
	Client  QueryClient
	Window  time.Duration
	NowFunc func() time.Time
}

func NewReader(c QueryClient, window time.Duration) (*Reader, error) {
	if c == nil {
		return nil, errors.New("oomkilled: nil QueryClient")
	}
	if window <= 0 {
		window = 7 * 24 * time.Hour
	}
	return &Reader{Client: c, Window: window, NowFunc: time.Now}, nil
}

// Count returns the OOMKilled increase over r.Window for the workload.
// Zero means "no OOMKilled in the window" — safe to propose memory
// cuts. Errors wrap the QueryClient's error so callers can branch on
// transport failures via errors.Is.
func (r *Reader) Count(ctx context.Context, _ tenancy.Context, w WorkloadRef) (int, error) {
	if w.ClusterID == "" || w.Namespace == "" || w.Workload == "" {
		return 0, errors.New("oomkilled: incomplete WorkloadRef")
	}
	now := r.NowFunc().UTC()
	q := buildQuery(w, r.Window)
	return r.Client.Count(ctx, q, now)
}

// Recent returns true iff the workload OOMKilled at least once in the
// window. Convenience wrapper around Count for the validator path.
func (r *Reader) Recent(ctx context.Context, t tenancy.Context, w WorkloadRef) (bool, error) {
	n, err := r.Count(ctx, t, w)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// buildQuery is the PromQL we send. Standard
// kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}
// counter per kube-state-metrics; sum over the window.
func buildQuery(w WorkloadRef, window time.Duration) string {
	return `sum(increase(kube_pod_container_status_last_terminated_reason{cluster="` +
		w.ClusterID + `",namespace="` + w.Namespace +
		`",pod=~"` + w.Workload + `-.*",reason="OOMKilled"}[` +
		windowExpr(window) + `]))`
}

// windowExpr renders a duration as Prometheus's `[7d]` style. Whole
// days when divisible; otherwise hours.
func windowExpr(d time.Duration) string {
	if d%(24*time.Hour) == 0 {
		return itoa(int(d/(24*time.Hour))) + "d"
	}
	return itoa(int(d/time.Hour)) + "h"
}

// InMemoryClient is the deterministic test double. Tests set a value
// per (query, atTime) and the Reader rounds-trip it.
type InMemoryClient struct {
	mu      sync.RWMutex
	results map[string]int
}

func NewInMemoryClient() *InMemoryClient {
	return &InMemoryClient{results: map[string]int{}}
}

// Set seeds the result for a query; subsequent Count calls return it.
func (c *InMemoryClient) Set(query string, count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results[query] = count
}

func (c *InMemoryClient) Count(_ context.Context, query string, _ time.Time) (int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.results[query], nil
}

// itoa avoids importing strconv for a 1-call need.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [16]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
