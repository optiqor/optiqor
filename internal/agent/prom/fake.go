package prom

import (
	"context"
	"sync"
	"time"
)

// InMemoryClient is a deterministic Client for unit + integration
// tests. Responses map promql → []Sample; queries that don't match
// return an empty result (mirrors Prometheus's no-data behaviour) so
// tests don't need to script every query path.
type InMemoryClient struct {
	mu        sync.Mutex
	Responses map[string][]Sample
	Calls     []string
	Err       error
}

func NewInMemoryClient() *InMemoryClient {
	return &InMemoryClient{Responses: map[string][]Sample{}}
}

// Add registers a canned response for an exact promql match. Repeat
// calls overwrite — useful when a test wants to model a series that
// changes between scrape iterations.
func (c *InMemoryClient) Add(promql string, samples []Sample) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Responses == nil {
		c.Responses = map[string][]Sample{}
	}
	c.Responses[promql] = samples
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
