// Package healthz is a registry of named readiness checks. Liveness is
// trivial (server answers => process alive); readiness is the composite
// of dependent pings (Postgres, Redis, Temporal). Domains register at
// boot; /readyz iterates.
package healthz

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CheckFunc returns nil when the dependency is reachable; any error is
// surfaced verbatim to /readyz.
type CheckFunc func(context.Context) error

type Result struct {
	Name    string        `json:"name"`
	OK      bool          `json:"ok"`
	Error   string        `json:"error,omitempty"`
	Latency time.Duration `json:"latency_ns"`
}

// Registry is concurrent-safe; one per process, shared by pointer.
type Registry struct {
	mu     sync.RWMutex
	checks map[string]CheckFunc
}

func NewRegistry() *Registry { return &Registry{checks: map[string]CheckFunc{}} }

// Register adds a check. Duplicates panic — programmer error at boot.
func (r *Registry) Register(name string, fn CheckFunc) {
	if name == "" || fn == nil {
		panic("healthz: empty name or nil func")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.checks[name]; dup {
		panic("healthz: duplicate check " + name)
	}
	r.checks[name] = fn
}

// Run executes every check with per-check timeout. Results are sorted
// by name so /readyz output is deterministic.
func (r *Registry) Run(ctx context.Context, perCheckTimeout time.Duration) ([]Result, bool) {
	r.mu.RLock()
	names := make([]string, 0, len(r.checks))
	for n := range r.checks {
		names = append(names, n)
	}
	r.mu.RUnlock()

	sortStrings(names)

	results := make([]Result, 0, len(names))
	allOK := true
	for _, n := range names {
		r.mu.RLock()
		fn := r.checks[n]
		r.mu.RUnlock()

		cctx, cancel := context.WithTimeout(ctx, perCheckTimeout)
		start := time.Now()
		err := fn(cctx)
		cancel()

		res := Result{Name: n, OK: err == nil, Latency: time.Since(start)}
		if err != nil {
			res.Error = err.Error()
			allOK = false
		}
		results = append(results, res)
	}
	return results, allOK
}

// Names returns registered names sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.checks))
	for n := range r.checks {
		names = append(names, n)
	}
	sortStrings(names)
	return names
}

// AlwaysOK is a placeholder for local dev; never register in prod.
func AlwaysOK(_ context.Context) error { return nil }

// AlwaysFail is the test-only inverse.
func AlwaysFail(reason string) CheckFunc {
	return func(_ context.Context) error { return fmt.Errorf("%s", reason) }
}

func sortStrings(s []string) {
	// insertion sort — n is a dozen at most
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
