// Package worker contains the dispatcher abstraction that the
// `cmd/worker` binary mounts. Phase 1 ships an in-memory implementation
// suitable for unit tests and local dev; the production Temporal SDK
// adapter swaps in at Phase 3 (cmd/worker registers a Dispatcher,
// nothing else changes).
//
// Per-tenant task queues are how Sevro enforces tenant isolation at
// the workflow layer — see CLAUDE.md "Multi-tenancy is non-negotiable"
// and todo.md production-readiness gap #3 (multi-cluster + team
// hierarchy).
package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lowplane/backend/internal/tenancy"
)

// QueueClass differentiates workflow priorities. Two classes are
// supported in Year 1; we can add more without breaking the schema
// because queue names are derived from (tenant, class) pairs.
type QueueClass string

const (
	QueueDefault  QueueClass = "default"
	QueuePriority QueueClass = "priority"
)

// SupportedQueues is the closed set Phase 1 understands.
var SupportedQueues = []QueueClass{QueueDefault, QueuePriority}

// QueueName returns the canonical Temporal task-queue name for a
// (tenant, class) pair. Stable wire format — used in logs, dashboards,
// and the Temporal admin tool.
//
//	QueueName("tenant-abc", QueueDefault)  // -> "tenant-tenant-abc-default"
func QueueName(tenantID string, class QueueClass) string {
	return fmt.Sprintf("tenant-%s-%s", tenantID, class)
}

// Workflow is the shape every registered workflow satisfies.
//
// Real Temporal workflows have a richer type signature; the Phase 1
// shape is deliberately small so the dispatcher can be exercised
// without the SDK. The Phase 3 adapter wraps a temporal.WorkflowFunc
// behind this interface.
type Workflow interface {
	Name() string
	Execute(ctx context.Context, t tenancy.Context, payload []byte) error
}

// Dispatcher routes work to registered workflows. Implementations
// must enforce tenant isolation (no work crosses tenant queues) and
// concurrency limits (no unbounded goroutine growth per tenant).
type Dispatcher interface {
	// Register adds a workflow to the dispatcher. Re-registering the
	// same name returns an error.
	Register(w Workflow) error

	// Submit enqueues a payload on the (tenant, class) queue and
	// returns once the workflow has been scheduled. The actual
	// workflow execution happens asynchronously; callers wishing to
	// wait on completion should query an out-of-band status surface.
	Submit(ctx context.Context, t tenancy.Context, class QueueClass, workflowName string, payload []byte) error

	// Drain completes any in-flight work and stops accepting new
	// submissions. Returns when every queue is empty or ctx is done.
	Drain(ctx context.Context) error
}

// ErrUnknownWorkflow is returned by Submit when no workflow with the
// given name has been registered.
var ErrUnknownWorkflow = errors.New("worker: unknown workflow")

// ErrDuplicateWorkflow is returned by Register when the same name is
// registered twice.
var ErrDuplicateWorkflow = errors.New("worker: duplicate workflow")

// ErrDraining is returned by Submit after Drain has been called.
var ErrDraining = errors.New("worker: dispatcher draining")

// InMemory is a synchronous, in-process Dispatcher used by tests and
// `make dev`. Each Submit runs the workflow on the calling goroutine
// inside a tenant-scoped context; this keeps the test surface
// deterministic and provides natural backpressure.
type InMemory struct {
	mu        sync.RWMutex
	workflows map[string]Workflow
	draining  bool
	wg        sync.WaitGroup
}

// NewInMemory returns an empty Dispatcher.
func NewInMemory() *InMemory {
	return &InMemory{workflows: map[string]Workflow{}}
}

// Register adds a workflow.
func (d *InMemory) Register(w Workflow) error {
	if w == nil || w.Name() == "" {
		return errors.New("worker: nil workflow or empty name")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, dup := d.workflows[w.Name()]; dup {
		return fmt.Errorf("%w: %s", ErrDuplicateWorkflow, w.Name())
	}
	d.workflows[w.Name()] = w
	return nil
}

// Submit runs the workflow inline. Returns the workflow's error
// verbatim; the caller decides whether to retry.
func (d *InMemory) Submit(ctx context.Context, t tenancy.Context, class QueueClass, name string, payload []byte) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if !validClass(class) {
		return fmt.Errorf("worker: unknown queue class %q", class)
	}
	d.mu.RLock()
	if d.draining {
		d.mu.RUnlock()
		return ErrDraining
	}
	wf, ok := d.workflows[name]
	d.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownWorkflow, name)
	}

	d.wg.Add(1)
	defer d.wg.Done()

	scoped := tenancy.WithContext(ctx, t)
	return wf.Execute(scoped, t, payload)
}

// Drain waits for in-flight Submit calls to finish, then refuses
// further submissions. Idempotent.
func (d *InMemory) Drain(ctx context.Context) error {
	d.mu.Lock()
	d.draining = true
	d.mu.Unlock()

	done := make(chan struct{})
	go func() { d.wg.Wait(); close(done) }()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Workflows returns the registered workflow names in deterministic
// order; used by tests and the worker boot logger.
func (d *InMemory) Workflows() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]string, 0, len(d.workflows))
	for n := range d.workflows {
		out = append(out, n)
	}
	sortStrings(out)
	return out
}

func validClass(c QueueClass) bool {
	for _, k := range SupportedQueues {
		if c == k {
			return true
		}
	}
	return false
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
