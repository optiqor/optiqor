package healthz

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRegistry_Run(t *testing.T) {
	slowCheck := func(ctx context.Context) error {
		select {
		case <-time.After(time.Second):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for _, tc := range []struct {
		name    string
		setup   func(*Registry)
		timeout time.Duration
		wantOK  bool
		check   func(t *testing.T, res []Result)
	}{
		{
			name: "all healthy",
			setup: func(r *Registry) {
				r.Register("postgres", AlwaysOK)
				r.Register("redis", AlwaysOK)
			},
			timeout: 50 * time.Millisecond,
			wantOK:  true,
			check: func(t *testing.T, res []Result) {
				t.Helper()
				if len(res) != 2 {
					t.Fatalf("expected 2 results, got %d", len(res))
				}
				if res[0].Name != "postgres" || res[1].Name != "redis" {
					t.Errorf("unexpected order: %+v", res)
				}
				for _, r := range res {
					if !r.OK || r.Error != "" {
						t.Errorf("expected ok with no error, got %+v", r)
					}
				}
			},
		},
		{
			name: "one failing surfaces error",
			setup: func(r *Registry) {
				r.Register("postgres", AlwaysOK)
				r.Register("redis", AlwaysFail("connection refused"))
			},
			timeout: 50 * time.Millisecond,
			check: func(t *testing.T, res []Result) {
				t.Helper()
				var redis Result
				for _, r := range res {
					if r.Name == "redis" {
						redis = r
					}
				}
				if redis.OK {
					t.Errorf("redis should be not-ok")
				}
				if !strings.Contains(redis.Error, "connection refused") {
					t.Errorf("redis error = %q", redis.Error)
				}
			},
		},
		{
			name: "timeout reports deadline error",
			setup: func(r *Registry) {
				r.Register("slow", slowCheck)
			},
			timeout: 5 * time.Millisecond,
			check: func(t *testing.T, res []Result) {
				t.Helper()
				if len(res) != 1 || res[0].OK {
					t.Fatalf("unexpected: %+v", res)
				}
				if !strings.Contains(res[0].Error, "deadline") && !strings.Contains(res[0].Error, "context") {
					t.Errorf("expected context-deadline error, got %q", res[0].Error)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry()
			tc.setup(r)
			res, ok := r.Run(context.Background(), tc.timeout)
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v", ok, tc.wantOK)
			}
			tc.check(t, res)
		})
	}
}

func TestRegistry_RegisterPanics(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func()
	}{
		{
			name: "duplicate name",
			run: func() {
				r := NewRegistry()
				r.Register("x", AlwaysOK)
				r.Register("x", AlwaysOK)
			},
		},
		{
			name: "empty name",
			run:  func() { NewRegistry().Register("", AlwaysOK) },
		},
		{
			name: "nil fn",
			run:  func() { NewRegistry().Register("x", nil) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			tc.run()
		})
	}
}

func TestRegistry_Names_DeterministicOrder(t *testing.T) {
	r := NewRegistry()
	r.Register("zeta", AlwaysOK)
	r.Register("alpha", AlwaysOK)
	r.Register("mike", AlwaysOK)

	got := r.Names()
	want := []string{"alpha", "mike", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: %v vs %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAlwaysFail(t *testing.T) {
	err := AlwaysFail("oops")(context.Background())
	if err == nil || err.Error() != "oops" {
		t.Fatalf("expected error %q, got %v", "oops", err)
	}
}
