// Package worker is the dispatcher abstraction cmd/worker mounts.
// Per-tenant task queues are the workflow-layer isolation primitive
// per CLAUDE.md "Multi-tenancy is non-negotiable".
package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/optiqor/optiqor/internal/tenancy"
)

type QueueClass string

const (
	QueueDefault  QueueClass = "default"
	QueuePriority QueueClass = "priority"
)

var SupportedQueues = []QueueClass{QueueDefault, QueuePriority}

// QueueName is the canonical Temporal task-queue name for a (tenant,
// class) pair. Stable wire format — logs, dashboards, and the Temporal
// admin tool read it.
func QueueName(tenantID string, class QueueClass) string {
	return fmt.Sprintf("tenant-%s-%s", tenantID, class)
}

// Workflow is the shape every registered workflow satisfies. The Phase
// 3 Temporal adapter wraps a temporal.WorkflowFunc behind this.
type Workflow interface {
	Name() string
	Execute(ctx context.Context, t tenancy.Context, payload []byte) error
}

// Dispatcher routes work to registered workflows. Implementations must
// enforce tenant isolation (no work crosses tenant queues) and bound
// goroutine growth per tenant.
type Dispatcher interface {
	Register(w Workflow) error
	Submit(ctx context.Context, t tenancy.Context, class QueueClass, workflowName string, payload []byte) error
	Drain(ctx context.Context) error
}

var ErrUnknownWorkflow = errors.New("worker: unknown workflow")
var ErrDuplicateWorkflow = errors.New("worker: duplicate workflow")
var ErrDraining = errors.New("worker: dispatcher draining")

// InMemory runs Submit inline on the calling goroutine inside a
// tenant-scoped context. Used by tests and `make dev`; gives
// deterministic ordering and natural backpressure.
type InMemory struct {
	mu        sync.RWMutex
	workflows map[string]Workflow
	draining  bool
	wg        sync.WaitGroup
}

func NewInMemory() *InMemory {
	return &InMemory{workflows: map[string]Workflow{}}
}

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

// Submit runs the workflow inline and returns its error verbatim; the
// caller decides whether to retry.
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

// Workflows returns the registered names in deterministic order;
// consumed by tests and the worker boot logger.
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
