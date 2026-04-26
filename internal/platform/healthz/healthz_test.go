package healthz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRegistry_AllOK(t *testing.T) {
	r := NewRegistry()
	r.Register("postgres", AlwaysOK)
	r.Register("redis", AlwaysOK)

	res, ok := r.Run(context.Background(), 50*time.Millisecond)
	if !ok {
		t.Fatalf("expected allOK, results: %+v", res)
	}
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
}

func TestRegistry_OneFailing(t *testing.T) {
	r := NewRegistry()
	r.Register("postgres", AlwaysOK)
	r.Register("redis", AlwaysFail("connection refused"))

	res, ok := r.Run(context.Background(), 50*time.Millisecond)
	if ok {
		t.Fatalf("expected not ok, got ok")
	}
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
}

func TestRegistry_Timeout(t *testing.T) {
	r := NewRegistry()
	r.Register("slow", func(ctx context.Context) error {
		select {
		case <-time.After(time.Second):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	res, ok := r.Run(context.Background(), 5*time.Millisecond)
	if ok {
		t.Fatal("expected not-ok due to timeout")
	}
	if len(res) != 1 || res[0].OK {
		t.Fatalf("unexpected: %+v", res)
	}
	if !strings.Contains(res[0].Error, "deadline") && !strings.Contains(res[0].Error, "context") {
		t.Errorf("expected context-deadline error, got %q", res[0].Error)
	}
}

func TestRegistry_DuplicatePanics(t *testing.T) {
	r := NewRegistry()
	r.Register("x", AlwaysOK)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate")
		}
	}()
	r.Register("x", AlwaysOK)
}

func TestRegistry_EmptyNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty name")
		}
	}()
	NewRegistry().Register("", AlwaysOK)
}

func TestRegistry_NilFnPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil fn")
		}
	}()
	NewRegistry().Register("x", nil)
}

func TestRegistry_DeterministicOrder(t *testing.T) {
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
	if err == nil || !errors.Is(err, err) || err.Error() != "oops" {
		t.Fatalf("expected error %q, got %v", "oops", err)
	}
}
