package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
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

func TestInMemory_Register(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(*InMemory)
		arg     Workflow
		wantErr error
	}{
		{
			name: "nil workflow rejected",
			arg:  nil,
		},
		{
			name: "duplicate name rejected",
			setup: func(d *InMemory) {
				_ = d.Register(&counterWorkflow{name: "x"})
			},
			arg:     &counterWorkflow{name: "x"},
			wantErr: ErrDuplicateWorkflow,
		},
		{
			name: "fresh registration succeeds",
			arg:  &counterWorkflow{name: "x"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewInMemory()
			if tc.setup != nil {
				tc.setup(d)
			}
			err := d.Register(tc.arg)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			case tc.arg == nil:
				if err == nil {
					t.Fatal("expected error on nil workflow")
				}
			default:
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
			}
		})
	}
}

func TestInMemory_Submit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register Workflow
		tenant   tenancy.Context
		queue    QueueClass
		wfName   string
		wantErr  error
		wantRuns int64
	}{
		{
			name:     "happy path runs workflow",
			register: &counterWorkflow{name: "x"},
			tenant:   tenancy.Context{TenantID: "t1"},
			queue:    QueueDefault,
			wfName:   "x",
			wantRuns: 1,
		},
		{
			name:    "unknown workflow rejected",
			tenant:  tenancy.Context{TenantID: "t1"},
			queue:   QueueDefault,
			wfName:  "missing",
			wantErr: ErrUnknownWorkflow,
		},
		{
			name:     "invalid queue class rejected",
			register: &counterWorkflow{name: "x"},
			tenant:   tenancy.Context{TenantID: "t1"},
			queue:    QueueClass("bogus"),
			wfName:   "x",
		},
		{
			name:     "missing tenant rejected",
			register: &counterWorkflow{name: "x"},
			queue:    QueueDefault,
			wfName:   "x",
			wantErr:  tenancy.ErrNoTenant,
		},
		{
			name:     "propagates workflow error",
			register: &counterWorkflow{name: "x", err: errors.New("workflow boom")},
			tenant:   tenancy.Context{TenantID: "t1"},
			queue:    QueueDefault,
			wfName:   "x",
			wantRuns: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewInMemory()
			var runs int64
			if tc.register != nil {
				if cw, ok := tc.register.(*counterWorkflow); ok {
					cw.runs = &runs
				}
				if err := d.Register(tc.register); err != nil {
					t.Fatalf("register: %v", err)
				}
			}
			err := d.Submit(context.Background(), tc.tenant, tc.queue, tc.wfName, nil)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			case tc.name == "invalid queue class rejected":
				if err == nil {
					t.Fatal("expected error on bogus queue class")
				}
			case tc.name == "propagates workflow error":
				if err == nil || err.Error() != "workflow boom" {
					t.Fatalf("err = %v, want workflow boom", err)
				}
			default:
				if err != nil {
					t.Fatalf("submit: %v", err)
				}
			}
			if atomic.LoadInt64(&runs) != tc.wantRuns {
				t.Errorf("runs = %d, want %d", runs, tc.wantRuns)
			}
		})
	}
}

func TestInMemory_Drain(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, d *InMemory)
	}{
		{
			name: "refuses submit after drain",
			run: func(t *testing.T, d *InMemory) {
				t.Helper()
				if err := d.Register(&counterWorkflow{name: "x"}); err != nil {
					t.Fatal(err)
				}
				if err := d.Drain(context.Background()); err != nil {
					t.Fatal(err)
				}
				err := d.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, QueueDefault, "x", nil)
				if !errors.Is(err, ErrDraining) {
					t.Fatalf("expected ErrDraining, got %v", err)
				}
			},
		},
		{
			name: "succeeds with no work",
			run: func(t *testing.T, d *InMemory) {
				t.Helper()
				if err := d.Drain(context.Background()); err != nil {
					t.Fatalf("Drain with no work should succeed: %v", err)
				}
			},
		},
		{
			name: "is idempotent",
			run: func(t *testing.T, d *InMemory) {
				t.Helper()
				if err := d.Drain(context.Background()); err != nil {
					t.Fatalf("first Drain: %v", err)
				}
				if err := d.Drain(context.Background()); err != nil {
					t.Fatalf("second Drain: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, NewInMemory())
		})
	}
}

func TestInMemory_Workflows_DeterministicOrder(t *testing.T) {
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

func TestInMemory_Submit_RaceSafe(t *testing.T) {
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
