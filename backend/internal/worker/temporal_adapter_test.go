package worker

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
	"go.temporal.io/sdk/client"
)

// fakeTemporalClient records every ExecuteWorkflow call so tests can
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

func TestTemporal_Register(t *testing.T) {
	for _, tc := range []struct {
		name    string
		first   trivialWorkflow
		second  *trivialWorkflow
		wantErr error
	}{
		{
			name:    "duplicate rejected",
			first:   trivialWorkflow{name: "x"},
			second:  &trivialWorkflow{name: "x"},
			wantErr: ErrDuplicateWorkflow,
		},
		{
			name:  "empty name rejected",
			first: trivialWorkflow{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewTemporal(&fakeTemporalClient{})
			err := a.Register(tc.first)
			if tc.second == nil {
				if err == nil {
					t.Error("empty name should fail")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			err = a.Register(*tc.second)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestTemporal_Submit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register *trivialWorkflow
		tenant   tenancy.Context
		queue    QueueClass
		wfName   string
		client   *fakeTemporalClient
		drain    bool
		wantErr  error
		check    func(t *testing.T, c *fakeTemporalClient)
	}{
		{
			name:    "unknown workflow rejected",
			tenant:  tenancy.Context{TenantID: "t1"},
			queue:   QueueDefault,
			wfName:  "missing",
			wantErr: ErrUnknownWorkflow,
		},
		{
			name:     "derives task queue and workflow id",
			register: &trivialWorkflow{name: "apply_fix"},
			tenant:   tenancy.Context{TenantID: "tenant-abc", WorkspaceID: "ws-1"},
			queue:    QueuePriority,
			wfName:   "apply_fix",
			check: func(t *testing.T, c *fakeTemporalClient) {
				t.Helper()
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
			},
		},
		{
			name:     "missing tenant rejected",
			register: &trivialWorkflow{name: "x"},
			queue:    QueueDefault,
			wfName:   "x",
			wantErr:  tenancy.ErrNoTenant,
		},
		{
			name:     "bad queue class rejected",
			register: &trivialWorkflow{name: "x"},
			tenant:   tenancy.Context{TenantID: "t1"},
			queue:    QueueClass("bogus"),
			wfName:   "x",
		},
		{
			name:     "rejected after drain",
			register: &trivialWorkflow{name: "x"},
			tenant:   tenancy.Context{TenantID: "t1"},
			queue:    QueueDefault,
			wfName:   "x",
			drain:    true,
			wantErr:  ErrDraining,
		},
		{
			name:     "upstream error wrapped",
			register: &trivialWorkflow{name: "x"},
			tenant:   tenancy.Context{TenantID: "t1"},
			queue:    QueueDefault,
			wfName:   "x",
			client:   &fakeTemporalClient{err: errors.New("temporal unavailable")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.client
			if c == nil {
				c = &fakeTemporalClient{}
			}
			a := NewTemporal(c)
			if tc.register != nil {
				if err := a.Register(*tc.register); err != nil {
					t.Fatalf("register: %v", err)
				}
			}
			if tc.drain {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if err := a.Drain(ctx); err != nil && !errors.Is(err, context.Canceled) {
					t.Fatalf("Drain: %v", err)
				}
			}
			err := a.Submit(context.Background(), tc.tenant, tc.queue, tc.wfName, []byte("{}"))
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
			case tc.name == "bad queue class rejected":
				if err == nil {
					t.Error("want error for unsupported queue class")
				}
			case tc.name == "upstream error wrapped":
				if err == nil {
					t.Fatal("want error")
				}
			default:
				if err != nil {
					t.Fatalf("Submit: %v", err)
				}
			}
			if tc.check != nil {
				tc.check(t, c)
			}
		})
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
	var _ Dispatcher = NewTemporal(&fakeTemporalClient{})
}
