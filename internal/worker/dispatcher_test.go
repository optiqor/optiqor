package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/optiqor/backend/internal/tenancy"
)

type counterWorkflow struct {
	name string
	runs *int64
	err  error
}

func (c *counterWorkflow) Name() string { return c.name }
func (c *counterWorkflow) Execute(ctx context.Context, t tenancy.Context, _ []byte) error {
	if c.runs != nil {
		atomic.AddInt64(c.runs, 1)
	}
	if _, err := tenancy.FromContext(ctx); err != nil {
		return errors.New("workflow: context lacks tenant")
	}
	if t.TenantID == "" {
		return errors.New("workflow: arg tenancy.Context empty")
	}
	return c.err
}

func TestQueueName_Stable(t *testing.T) {
	if got, want := QueueName("tenant-abc", QueueDefault), "tenant-tenant-abc-default"; got != want {
		t.Errorf("QueueName = %q, want %q", got, want)
	}
}

func TestRegister_AndExecute(t *testing.T) {
	var runs int64
	d := NewInMemory()
	wf := &counterWorkflow{name: "test", runs: &runs}
	if err := d.Register(wf); err != nil {
		t.Fatal(err)
	}
	err := d.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, QueueDefault, "test", nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if runs != 1 {
		t.Errorf("runs = %d, want 1", runs)
	}
}

func TestRegister_NilWorkflow(t *testing.T) {
	if err := NewInMemory().Register(nil); err == nil {
		t.Fatal("expected error on nil workflow")
	}
}

func TestRegister_DuplicateName(t *testing.T) {
	d := NewInMemory()
	wf := &counterWorkflow{name: "x"}
	_ = d.Register(wf)
	err := d.Register(wf)
	if !errors.Is(err, ErrDuplicateWorkflow) {
		t.Fatalf("expected ErrDuplicateWorkflow, got %v", err)
	}
}

func TestSubmit_UnknownWorkflow(t *testing.T) {
	d := NewInMemory()
	err := d.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, QueueDefault, "missing", nil)
	if !errors.Is(err, ErrUnknownWorkflow) {
		t.Fatalf("expected ErrUnknownWorkflow, got %v", err)
	}
}

func TestSubmit_InvalidQueue(t *testing.T) {
	d := NewInMemory()
	_ = d.Register(&counterWorkflow{name: "x"})
	err := d.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, QueueClass("bogus"), "x", nil)
	if err == nil {
		t.Fatal("expected error on bogus queue class")
	}
}

func TestSubmit_RequiresTenant(t *testing.T) {
	d := NewInMemory()
	_ = d.Register(&counterWorkflow{name: "x"})
	err := d.Submit(context.Background(), tenancy.Context{}, QueueDefault, "x", nil)
	if !errors.Is(err, tenancy.ErrNoTenant) {
		t.Fatalf("expected ErrNoTenant, got %v", err)
	}
}

func TestSubmit_PassesTenantToWorkflowAndContext(t *testing.T) {
	d := NewInMemory()
	wf := &counterWorkflow{name: "x"}
	_ = d.Register(wf)
	if err := d.Submit(context.Background(), tenancy.Context{TenantID: "tenant-99"}, QueueDefault, "x", nil); err != nil {
		t.Fatalf("Submit: %v", err)
	}
}

func TestDrain_RefusesAfterDrain(t *testing.T) {
	d := NewInMemory()
	_ = d.Register(&counterWorkflow{name: "x"})
	if err := d.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := d.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, QueueDefault, "x", nil)
	if !errors.Is(err, ErrDraining) {
		t.Fatalf("expected ErrDraining, got %v", err)
	}
}

func TestDrain_NoWorkSucceeds(t *testing.T) {
	d := NewInMemory()
	if err := d.Drain(context.Background()); err != nil {
		t.Fatalf("Drain with no work should succeed: %v", err)
	}
}

func TestDrain_Idempotent(t *testing.T) {
	d := NewInMemory()
	if err := d.Drain(context.Background()); err != nil {
		t.Fatalf("first Drain: %v", err)
	}
	// Second Drain must also succeed; the dispatcher is already in
	// the draining state but no in-flight work means immediate return.
	if err := d.Drain(context.Background()); err != nil {
		t.Fatalf("second Drain: %v", err)
	}
}

func TestWorkflows_DeterministicOrder(t *testing.T) {
	d := NewInMemory()
	for _, n := range []string{"zeta", "alpha", "mike"} {
		_ = d.Register(&counterWorkflow{name: n})
	}
	got := d.Workflows()
	want := []string{"alpha", "mike", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Workflows()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSubmit_PropagatesWorkflowError(t *testing.T) {
	d := NewInMemory()
	_ = d.Register(&counterWorkflow{name: "x", err: errors.New("workflow boom")})
	err := d.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, QueueDefault, "x", nil)
	if err == nil || err.Error() != "workflow boom" {
		t.Fatalf("err = %v, want workflow boom", err)
	}
}

// Concurrent Submits should not race on the workflow registry.
func TestSubmit_RaceSafe(t *testing.T) {
	d := NewInMemory()
	var runs int64
	_ = d.Register(&counterWorkflow{name: "x", runs: &runs})

	const N = 200
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			errs <- d.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, QueueDefault, "x", nil)
		}()
	}
	deadline := time.After(2 * time.Second)
	for i := 0; i < N; i++ {
		select {
		case err := <-errs:
			if err != nil {
				t.Errorf("submit: %v", err)
			}
		case <-deadline:
			t.Fatal("timed out")
		}
	}
	if atomic.LoadInt64(&runs) != N {
		t.Errorf("runs = %d, want %d", runs, N)
	}
}
