// Package healthz provides a small registry of named readiness checks.
//
// Liveness ("is the process alive?") is trivial — if the HTTP server can
// answer at all, the process is alive. Readiness ("can this instance
// serve traffic right now?") is a composite of dependent-service pings:
// Postgres, Redis, Temporal, etc. Each domain registers its own checks
// here at boot; the API server iterates them on /readyz.
package healthz

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CheckFunc returns nil when the dependency is reachable. Any error is
// surfaced verbatim to the /readyz response.
type CheckFunc func(context.Context) error

// Result is the outcome of one named check.
type Result struct {
	Name    string        `json:"name"`
	OK      bool          `json:"ok"`
	Error   string        `json:"error,omitempty"`
	Latency time.Duration `json:"latency_ns"`
}

// Registry is a thread-safe set of named checks. Construct one per
// process; share by pointer.
type Registry struct {
	mu     sync.RWMutex
	checks map[string]CheckFunc
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry { return &Registry{checks: map[string]CheckFunc{}} }

// Register adds a check. The name must be unique; duplicates panic to
// catch programming errors at boot.
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

// Run executes every registered check with the given timeout. Returns
// (results, allOK). Results are deterministic-ordered by name.
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

// Names returns the registered names in deterministic order. Useful for
// tests and operator inspection.
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

// AlwaysOK is a placeholder check that succeeds. Used during local dev
// before real driver wiring lands; remove from production registry.
func AlwaysOK(_ context.Context) error { return nil }

// AlwaysFail is the inverse, useful in tests.
func AlwaysFail(reason string) CheckFunc {
	return func(_ context.Context) error { return fmt.Errorf("%s", reason) }
}

func sortStrings(s []string) {
	// insertion sort; n is tiny (≤ a dozen checks)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
