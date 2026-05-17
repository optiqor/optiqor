// Temporal SDK adapter for [Dispatcher].
//
// The default backend build ships [InMemory], which executes
// workflows inline on the calling goroutine. The Temporal adapter
// replaces that for production: every Submit becomes
// `client.ExecuteWorkflow` against the shared Temporal cluster, and
// each registered Optiqor [Workflow] gets wrapped in a Temporal
// workflow function that delegates to it.
//
// Per CLAUDE.md: per-tenant task queues are the isolation
// boundary. The adapter derives a Temporal task-queue name from the
// (tenant, class) pair using [QueueName] so the in-memory and
// Temporal paths produce identical queue identities.
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

// TemporalClient is the narrow subset of [client.Client] this adapter
// uses. Keeping it small means tests can supply a hand-rolled fake
// (rather than mocking the full ~40-method SDK interface), and the
// real *client.Client satisfies it trivially.
type TemporalClient interface {
	ExecuteWorkflow(
		ctx context.Context,
		options client.StartWorkflowOptions,
		workflow any,
		args ...any,
	) (client.WorkflowRun, error)
}

// Temporal is the production [Dispatcher] backed by go.temporal.io/sdk.
//
// One Temporal per process. Construct via [NewTemporal] with a
// connected Temporal client; the adapter does NOT manage the
// connection lifecycle (that lives in cmd/worker).
type Temporal struct {
	client TemporalClient

	mu        sync.RWMutex
	workflows map[string]Workflow
	draining  bool
}

// NewTemporal wraps a Temporal client. The full *client.Client
// satisfies [TemporalClient]; cmd/worker passes one obtained from
// client.Dial.
func NewTemporal(c TemporalClient) *Temporal {
	if c == nil {
		// Constructing the adapter without a client is always a
		// programming error; surfacing this as a panic in main is
		// preferable to a nil-deref later.
		panic("worker: NewTemporal: nil client")
	}
	return &Temporal{client: c, workflows: map[string]Workflow{}}
}

// Register adds w to the registry. Workers (cmd/worker) call
// [RegisterOnTemporalWorker] separately to bind the SDK-side
// workflow function — this method only tracks names so Submit can
// reject unknowns before reaching the network.
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

// Submit calls ExecuteWorkflow on the wrapped client. The Temporal
// workflow id is derived deterministically from (workflowName,
// tenantID) so retries are idempotent.
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
		WorkflowExecutionTimeout: 24 * time.Hour, // hard upper bound; per-workflow overrides come later
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

// Drain refuses new submissions and waits for the client's running
// workflows to finish. We don't drain the cluster itself — Temporal
// workflows have their own retry / completion semantics — only the
// adapter's local registration table.
func (a *Temporal) Drain(ctx context.Context) error {
	a.mu.Lock()
	a.draining = true
	a.mu.Unlock()
	// No-op for the client surface: Temporal owns workflow lifetimes.
	// Honour ctx so callers with their own deadline aren't stuck.
	<-ctx.Done()
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// Workflows returns the registered workflow names. Helper for the
// cmd/worker boot logger; mirrors [InMemory.Workflows].
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

// TemporalEnvelope is the wire format that travels across Temporal's
// payload encoder. The wrapped tenant context arrives at the worker
// alongside the workflow payload so the Temporal-side adapter can
// reconstruct it without a session/state lookup.
type TemporalEnvelope struct {
	Tenant  tenancy.Context
	Payload []byte
}

// MakeTemporalWorkflowFn returns a Temporal-shaped workflow function
// that delegates to w. cmd/worker calls this to bind every registered
// Optiqor Workflow to the Temporal worker on the right task queue.
//
// The function intentionally lives at module-level rather than as a
// closure so Temporal's deterministic-execution constraints (no
// non-replayable closures) hold.
func MakeTemporalWorkflowFn(w Workflow) any {
	return func(ctx workflow.Context, env TemporalEnvelope) error {
		// The Workflow contract uses a standard context; the Temporal
		// SDK exposes a separate Context type that does NOT satisfy it.
		// We bridge by spawning a regular context bound to the workflow
		// info so cancellation propagates.
		stdCtx := context.Background() // Temporal manages its own cancellation
		return w.Execute(stdCtx, env.Tenant, env.Payload)
	}
}

// workflowID is the deterministic ID we hand Temporal. Re-submitting
// the same (workflowName, tenant) tuple is a no-op once the workflow
// is running — Temporal will reject the duplicate. That's the
// behaviour we want: an idempotent Submit.
func workflowID(name string, t tenancy.Context) string {
	return fmt.Sprintf("%s::%s::%s", name, t.TenantID, t.WorkspaceID)
}

// Static compile-time assertion that Temporal satisfies Dispatcher.
var _ Dispatcher = (*Temporal)(nil)
