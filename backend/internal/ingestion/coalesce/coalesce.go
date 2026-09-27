// Package coalesce collapses two PRs against the same chart within a
// short window into one analysis. Removes a known friction point with
// platform teams that have noisy PRs; the dedup window is per (tenant,
// repo, chart-path) and defaults to 24h.
package coalesce

import (
	"errors"
	"sync"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Key is the dedup grouping. (tenant, repo, chart-path) means two PRs
// editing the same chart in the same repo for the same tenant.
type Key struct {
	TenantID  string
	RepoOwner string
	RepoName  string
	ChartPath string
}

func (k Key) valid() bool {
	return k.TenantID != "" && k.RepoOwner != "" && k.RepoName != "" && k.ChartPath != ""
}

// Decision tells the caller whether to dispatch a fresh analysis or
// short-circuit and reference the existing one. ExistingPRNumber is
// set when CoalesceTo is non-empty.
type Decision struct {
	Dispatch         bool
	CoalesceTo       string
	ExistingPRNumber int
}

// Coalescer remembers the first PR per Key within a TTL window. Safe
// for concurrent use.
type Coalescer struct {
	now func() time.Time
	ttl time.Duration

	mu   sync.Mutex
	seen map[Key]entry
}

type entry struct {
	PRNumber int
	PRURL    string
	At       time.Time
}

// New takes the dedup window (24h is the playbook default) and a
// time source (inject the clock so tests don't sleep).
func New(ttl time.Duration, now func() time.Time) *Coalescer {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if now == nil {
		now = time.Now
	}
	return &Coalescer{now: now, ttl: ttl, seen: map[Key]entry{}}
}

// Observe records the first PR observed for a Key. Subsequent
// observations within the TTL return Dispatch=false + the existing
// PR ref. Expired entries are replaced.
func (c *Coalescer) Observe(_ tenancy.Context, k Key, prNumber int, prURL string) (Decision, error) {
	if !k.valid() {
		return Decision{}, errors.New("coalesce: incomplete key")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if e, ok := c.seen[k]; ok {
		if now.Sub(e.At) < c.ttl {
			return Decision{
				Dispatch:         false,
				CoalesceTo:       e.PRURL,
				ExistingPRNumber: e.PRNumber,
			}, nil
		}
	}
	c.seen[k] = entry{PRNumber: prNumber, PRURL: prURL, At: now}
	return Decision{Dispatch: true}, nil
}

// Sweep drops expired entries; callers run it on a slow tick to keep
// the in-memory map bounded.
func (c *Coalescer) Sweep() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	var dropped int
	for k, e := range c.seen {
		if now.Sub(e.At) >= c.ttl {
			delete(c.seen, k)
			dropped++
		}
	}
	return dropped
}

// Len is for tests + observability.
func (c *Coalescer) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}
