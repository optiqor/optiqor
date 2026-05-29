package prom

import (
	"context"
	"sync"
	"time"
)

// InMemoryClient is a deterministic Client for unit + integration
// tests. Responses map promql → []Sample (instant) or []Series (range).
// Queries that don't match return empty results (mirrors Prometheus's
// no-data behaviour) so tests don't need to script every query path.
type InMemoryClient struct {
	mu        sync.Mutex
	Responses map[string][]Sample
	Ranges    map[string][]Series
	Calls     []string
	Err       error
}

func NewInMemoryClient() *InMemoryClient {
	return &InMemoryClient{
		Responses: map[string][]Sample{},
		Ranges:    map[string][]Series{},
	}
}

// Add registers a canned instant response. Repeat calls overwrite.
func (c *InMemoryClient) Add(promql string, samples []Sample) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Responses == nil {
		c.Responses = map[string][]Sample{}
	}
	c.Responses[promql] = samples
}

// AddRange registers a canned range response.
func (c *InMemoryClient) AddRange(promql string, series []Series) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Ranges == nil {
		c.Ranges = map[string][]Series{}
	}
	c.Ranges[promql] = series
}

func (c *InMemoryClient) Query(_ context.Context, promql string, _ time.Time) ([]Sample, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Calls = append(c.Calls, promql)
	if c.Err != nil {
		return nil, c.Err
	}
	return c.Responses[promql], nil
}

func (c *InMemoryClient) QueryRange(_ context.Context, promql string, _ Range) ([]Series, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Calls = append(c.Calls, promql)
	if c.Err != nil {
		return nil, c.Err
	}
	return c.Ranges[promql], nil
}
