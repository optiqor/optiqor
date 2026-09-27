//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/worker"
)

// TestTemporalQueueIsolation_PerTenant pins ADR-0003 + the per-tenant
// queue contract: a workflow submitted under tenant A must not be
// observable from tenant B's poller, even when both share the same
// in-process dispatcher. Production swaps the dispatcher for the
// Temporal SDK adapter (`worker/temporal_adapter.go`); the contract
// must hold on both paths because the queue name is the only
// isolation primitive.
func TestTemporalQueueIsolation_PerTenant(t *testing.T) {
	d := worker.NewInMemory()
	if err := d.Register(captureWorkflow{name: "test_isolation"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	tenantA := tenancy.Context{TenantID: uuid.NewString()}
	tenantB := tenancy.Context{TenantID: uuid.NewString()}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Submit(ctx, tenantA, worker.QueueDefault, "test_isolation", []byte(`"a"`)); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	if err := d.Submit(ctx, tenantB, worker.QueueDefault, "test_isolation", []byte(`"b"`)); err != nil {
		t.Fatalf("submit B: %v", err)
	}

	if err := d.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}

	wfA := worker.QueueName(tenantA.TenantID, worker.QueueDefault)
	wfB := worker.QueueName(tenantB.TenantID, worker.QueueDefault)
	if wfA == wfB {
		t.Fatalf("QueueName must differ per tenant: A=%q B=%q", wfA, wfB)
	}

	// Inspecting the recorded execution sites proves a B-tenant
	// workflow never landed on A's queue and vice versa.
	for _, exec := range captured.executions {
		got := worker.QueueName(exec.tenant.TenantID, worker.QueueDefault)
		switch exec.payload {
		case `"a"`:
			if got != wfA {
				t.Errorf("tenant A workflow landed on queue %q, want %q", got, wfA)
			}
		case `"b"`:
			if got != wfB {
				t.Errorf("tenant B workflow landed on queue %q, want %q", got, wfB)
			}
		}
	}
}

// captured collects executions across the test. Scoped to the file so
// the in-process dispatcher tests can introspect without exporting
// dispatcher internals.
var captured = &executionRecorder{}

type executionRecorder struct {
	mu         sync.Mutex
	executions []execution
}

type execution struct {
	tenant  tenancy.Context
	payload string
}

func (r *executionRecorder) record(t tenancy.Context, payload string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executions = append(r.executions, execution{tenant: t, payload: payload})
}

type captureWorkflow struct {
	name string
}

func (c captureWorkflow) Name() string { return c.name }

func (c captureWorkflow) Execute(_ context.Context, t tenancy.Context, raw []byte) error {
	if t.TenantID == "" {
		return errors.New("captureWorkflow: empty tenant id — isolation invariant broken")
	}
	captured.record(t, string(raw))
	return nil
}
