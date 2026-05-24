// Temporal SDK adapter for Dispatcher. Submit becomes
// client.ExecuteWorkflow; each registered Workflow is wrapped in a
// Temporal workflow function via MakeTemporalWorkflowFn.
//
// Per CLAUDE.md, per-tenant task queues are the isolation boundary.
// QueueName produces the same name on the in-memory and Temporal paths
// so a workflow's queue identity is stable across backends.
package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"
)

// TemporalClient is the narrow subset of client.Client this adapter
// uses so tests can supply a hand-rolled fake instead of mocking the
// full ~40-method SDK interface. The real *client.Client satisfies it.
type TemporalClient interface {
	ExecuteWorkflow(
		ctx context.Context,
		options client.StartWorkflowOptions,
		workflow any,
		args ...any,
	) (client.WorkflowRun, error)
}

// Temporal is the production Dispatcher backed by go.temporal.io/sdk.
// One per process. cmd/worker owns the client's connection lifecycle.
type Temporal struct {
	client TemporalClient

	mu        sync.RWMutex
	workflows map[string]Workflow
	draining  bool
}

func NewTemporal(c TemporalClient) *Temporal {
	if c == nil {
		// Panic in main is preferable to a nil-deref later.
		panic("worker: NewTemporal: nil client")
	}
	return &Temporal{client: c, workflows: map[string]Workflow{}}
}

// Register tracks the workflow name so Submit can reject unknowns
// before hitting the network. cmd/worker binds the SDK-side function
// separately via MakeTemporalWorkflowFn.
func (a *Temporal) Register(w Workflow) error {
	if w == nil || w.Name() == "" {
		return errors.New("worker: nil workflow or empty name")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, dup := a.workflows[w.Name()]; dup {
		return fmt.Errorf("%w: %s", ErrDuplicateWorkflow, w.Name())
	}
	a.workflows[w.Name()] = w
	return nil
}

// Submit derives a deterministic workflow ID from (workflowName,
// tenant) so retries are idempotent — Temporal rejects the duplicate
// while the original run is alive.
func (a *Temporal) Submit(ctx context.Context, t tenancy.Context, class QueueClass, name string, payload []byte) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if !validClass(class) {
		return fmt.Errorf("worker: unknown queue class %q", class)
	}
	a.mu.RLock()
	if a.draining {
		a.mu.RUnlock()
		return ErrDraining
	}
	if _, ok := a.workflows[name]; !ok {
		a.mu.RUnlock()
		return fmt.Errorf("%w: %s", ErrUnknownWorkflow, name)
	}
	a.mu.RUnlock()

	opts := client.StartWorkflowOptions{
		ID:                       workflowID(name, t),
		TaskQueue:                QueueName(t.TenantID, class),
		WorkflowExecutionTimeout: 24 * time.Hour, // hard upper bound; per-workflow overrides later
	}
	_, err := a.client.ExecuteWorkflow(ctx, opts, name, TemporalEnvelope{
		Tenant:  t,
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("worker: temporal start: %w", err)
	}
	return nil
}

// Drain only flips the local registration table. Temporal owns
// workflow lifetimes; we honour ctx so callers with their own deadline
// aren't stuck waiting on the cluster.
func (a *Temporal) Drain(ctx context.Context) error {
	a.mu.Lock()
	a.draining = true
	a.mu.Unlock()
	<-ctx.Done()
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func (a *Temporal) Workflows() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, 0, len(a.workflows))
	for n := range a.workflows {
		out = append(out, n)
	}
	sortStrings(out)
	return out
}

// TemporalEnvelope carries the tenant context next to the payload so
// the Temporal-side workflow can reconstruct scope without a
// session/state lookup.
type TemporalEnvelope struct {
	Tenant  tenancy.Context
	Payload []byte
}

// MakeTemporalWorkflowFn returns a Temporal-shaped function that
// delegates to w. Kept at module level (not a closure) so Temporal's
// deterministic-replay constraints hold.
func MakeTemporalWorkflowFn(w Workflow) any {
	return func(ctx workflow.Context, env TemporalEnvelope) error {
		// Temporal's workflow.Context does not satisfy context.Context.
		// Temporal manages cancellation on its side.
		stdCtx := context.Background()
		return w.Execute(stdCtx, env.Tenant, env.Payload)
	}
}

func workflowID(name string, t tenancy.Context) string {
	return fmt.Sprintf("%s::%s::%s", name, t.TenantID, t.WorkspaceID)
}

var _ Dispatcher = (*Temporal)(nil)
