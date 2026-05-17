package worker

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
	"go.temporal.io/sdk/client"
)

// fakeTemporalClient implements the narrow [TemporalClient] interface
// we depend on. It records every ExecuteWorkflow call so tests can
// assert on task-queue derivation, workflow IDs, and payload routing.
type fakeTemporalClient struct {
	mu   sync.Mutex
	runs []startCall
	err  error
}

type startCall struct {
	opts client.StartWorkflowOptions
	name string
	args []any
}

func (f *fakeTemporalClient) ExecuteWorkflow(
	_ context.Context,
	opts client.StartWorkflowOptions,
	wf any,
	args ...any,
) (client.WorkflowRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name, _ := wf.(string)
	f.runs = append(f.runs, startCall{opts: opts, name: name, args: args})
	if f.err != nil {
		return nil, f.err
	}
	return fakeRun{}, nil
}

type fakeRun struct{}

func (fakeRun) GetID() string                      { return "wf-1" }
func (fakeRun) GetRunID() string                   { return "run-1" }
func (fakeRun) Get(_ context.Context, _ any) error { return nil }
func (fakeRun) GetWithOptions(_ context.Context, _ any, _ client.WorkflowRunGetOptions) error {
	return nil
}

// trivialWorkflow is a registrable Workflow stub.
type trivialWorkflow struct {
	name string
	run  func(ctx context.Context, t tenancy.Context, payload []byte) error
}

func (w trivialWorkflow) Name() string { return w.name }
func (w trivialWorkflow) Execute(ctx context.Context, t tenancy.Context, payload []byte) error {
	if w.run == nil {
		return nil
	}
	return w.run(ctx, t, payload)
}

// ---- tests -------------------------------------------------------

func TestTemporal_Register_Duplicate(t *testing.T) {
	a := NewTemporal(&fakeTemporalClient{})
	if err := a.Register(trivialWorkflow{name: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Register(trivialWorkflow{name: "x"}); !errors.Is(err, ErrDuplicateWorkflow) {
		t.Errorf("want ErrDuplicateWorkflow, got %v", err)
	}
}

func TestTemporal_Register_EmptyName(t *testing.T) {
	a := NewTemporal(&fakeTemporalClient{})
	if err := a.Register(trivialWorkflow{}); err == nil {
		t.Error("empty name should fail")
	}
}

func TestTemporal_Submit_UnknownWorkflow(t *testing.T) {
	a := NewTemporal(&fakeTemporalClient{})
	err := a.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, QueueDefault, "missing", []byte("{}"))
	if !errors.Is(err, ErrUnknownWorkflow) {
		t.Errorf("want ErrUnknownWorkflow, got %v", err)
	}
}

func TestTemporal_Submit_DerivesTaskQueueAndID(t *testing.T) {
	c := &fakeTemporalClient{}
	a := NewTemporal(c)
	_ = a.Register(trivialWorkflow{name: "apply_fix"})

	err := a.Submit(
		context.Background(),
		tenancy.Context{TenantID: "tenant-abc", WorkspaceID: "ws-1"},
		QueuePriority,
		"apply_fix",
		[]byte("{}"),
	)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(c.runs) != 1 {
		t.Fatalf("got %d runs", len(c.runs))
	}
	got := c.runs[0]
	if got.opts.TaskQueue != "tenant-tenant-abc-priority" {
		t.Errorf("task queue = %q", got.opts.TaskQueue)
	}
	if got.opts.ID != "apply_fix::tenant-abc::ws-1" {
		t.Errorf("workflow id = %q", got.opts.ID)
	}
	if got.name != "apply_fix" {
		t.Errorf("workflow name = %q", got.name)
	}
}

func TestTemporal_Submit_RejectsNoTenant(t *testing.T) {
	a := NewTemporal(&fakeTemporalClient{})
	_ = a.Register(trivialWorkflow{name: "x"})
	err := a.Submit(context.Background(), tenancy.Context{}, QueueDefault, "x", []byte("{}"))
	if !errors.Is(err, tenancy.ErrNoTenant) {
		t.Errorf("want ErrNoTenant, got %v", err)
	}
}

func TestTemporal_Submit_RejectsBadQueueClass(t *testing.T) {
	a := NewTemporal(&fakeTemporalClient{})
	_ = a.Register(trivialWorkflow{name: "x"})
	err := a.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, "bogus", "x", []byte("{}"))
	if err == nil {
		t.Errorf("want error for unsupported queue class")
	}
}

func TestTemporal_Submit_RejectsAfterDrain(t *testing.T) {
	a := NewTemporal(&fakeTemporalClient{})
	_ = a.Register(trivialWorkflow{name: "x"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Drain(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Drain: %v", err)
	}
	err := a.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, QueueDefault, "x", []byte("{}"))
	if !errors.Is(err, ErrDraining) {
		t.Errorf("want ErrDraining, got %v", err)
	}
}

func TestTemporal_Submit_UpstreamErrorWrapped(t *testing.T) {
	c := &fakeTemporalClient{err: errors.New("temporal unavailable")}
	a := NewTemporal(c)
	_ = a.Register(trivialWorkflow{name: "x"})
	err := a.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, QueueDefault, "x", []byte("{}"))
	if err == nil {
		t.Fatal("want error")
	}
}

func TestTemporal_Workflows_StableOrder(t *testing.T) {
	a := NewTemporal(&fakeTemporalClient{})
	_ = a.Register(trivialWorkflow{name: "b"})
	_ = a.Register(trivialWorkflow{name: "a"})
	got := a.Workflows()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("workflows = %v, want [a b]", got)
	}
}

func TestNewTemporal_PanicsOnNilClient(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on nil client")
		}
	}()
	_ = NewTemporal(nil)
}

func TestTemporal_SatisfiesDispatcher(t *testing.T) {
	// Compile-time check, expressed as a runtime test for visibility.
	var _ Dispatcher = NewTemporal(&fakeTemporalClient{})
}
