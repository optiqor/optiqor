package featureflags

import (
	"context"
	"testing"

	"github.com/optiqor/backend/internal/tenancy"
)

func TestNoopProvider_ReturnsDefaults(t *testing.T) {
	c := NewClient(NoopProvider())
	if got := c.Bool(context.Background(), "x", true, EvalContext{}); got != true {
		t.Errorf("bool default lost: %v", got)
	}
	if got := c.String(context.Background(), "y", "fallback", EvalContext{}); got != "fallback" {
		t.Errorf("string default lost: %v", got)
	}
	if got := c.Number(context.Background(), "z", 1.5, EvalContext{}); got != 1.5 {
		t.Errorf("number default lost: %v", got)
	}
}

func TestStaticProvider_OverridesDefaults(t *testing.T) {
	sp := NewStaticProvider()
	sp.Bools["beta"] = true
	sp.Strings["mode"] = "canary"
	sp.Numbers["budget"] = 0.4

	c := NewClient(sp)
	if !c.Bool(context.Background(), "beta", false, EvalContext{}) {
		t.Error("beta should return true")
	}
	if got := c.String(context.Background(), "mode", "stable", EvalContext{}); got != "canary" {
		t.Errorf("mode = %q", got)
	}
	if got := c.Number(context.Background(), "budget", 0.2, EvalContext{}); got != 0.4 {
		t.Errorf("budget = %v", got)
	}
}

func TestStaticProvider_FallsBackWhenAbsent(t *testing.T) {
	c := NewClient(NewStaticProvider())
	if got := c.Bool(context.Background(), "absent", true, EvalContext{}); got != true {
		t.Errorf("absent flag should fall back to default; got %v", got)
	}
}

func TestClient_NilProviderUsesDefault(t *testing.T) {
	c := &Client{} // intentionally no provider set
	if got := c.Bool(context.Background(), "anything", true, EvalContext{}); got != true {
		t.Error("nil provider should fall back to default")
	}
}

func TestClient_SetSwapsProvider(t *testing.T) {
	c := NewClient(NoopProvider())
	if c.Bool(context.Background(), "x", false, EvalContext{}) != false {
		t.Fatal("noop should return default")
	}
	sp := NewStaticProvider()
	sp.Bools["x"] = true
	c.Set(sp)
	if !c.Bool(context.Background(), "x", false, EvalContext{}) {
		t.Error("after Set, provider should override")
	}
}

func TestEvalFromTenant(t *testing.T) {
	in := tenancy.Context{TenantID: "t1", WorkspaceID: "w1", ClusterID: "c1"}
	got := EvalFromTenant(in)
	if got.TenantID != "t1" || got.WorkspaceID != "w1" {
		t.Errorf("EvalFromTenant lost values: %+v", got)
	}
}
