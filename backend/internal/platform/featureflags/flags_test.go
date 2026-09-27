package featureflags

import (
	"context"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestClient_Provider(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		client    func() *Client
		boolKey   string
		boolDef   bool
		wantBool  bool
		stringKey string
		stringDef string
		wantStr   string
		numberKey string
		numberDef float64
		wantNum   float64
	}{
		{
			name:    "noop returns defaults",
			client:  func() *Client { return NewClient(NoopProvider()) },
			boolKey: "x", boolDef: true, wantBool: true,
			stringKey: "y", stringDef: "fallback", wantStr: "fallback",
			numberKey: "z", numberDef: 1.5, wantNum: 1.5,
		},
		{
			name: "static overrides defaults",
			client: func() *Client {
				sp := NewStaticProvider()
				sp.Bools["beta"] = true
				sp.Strings["mode"] = "canary"
				sp.Numbers["budget"] = 0.4
				return NewClient(sp)
			},
			boolKey: "beta", boolDef: false, wantBool: true,
			stringKey: "mode", stringDef: "stable", wantStr: "canary",
			numberKey: "budget", numberDef: 0.2, wantNum: 0.4,
		},
		{
			name:    "static absent falls back to default",
			client:  func() *Client { return NewClient(NewStaticProvider()) },
			boolKey: "absent", boolDef: true, wantBool: true,
		},
		{
			name:    "nil provider falls back to default",
			client:  func() *Client { return &Client{} },
			boolKey: "anything", boolDef: true, wantBool: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.client()
			if got := c.Bool(ctx, tc.boolKey, tc.boolDef, EvalContext{}); got != tc.wantBool {
				t.Errorf("Bool = %v, want %v", got, tc.wantBool)
			}
			if tc.stringKey != "" {
				if got := c.String(ctx, tc.stringKey, tc.stringDef, EvalContext{}); got != tc.wantStr {
					t.Errorf("String = %q, want %q", got, tc.wantStr)
				}
			}
			if tc.numberKey != "" {
				if got := c.Number(ctx, tc.numberKey, tc.numberDef, EvalContext{}); got != tc.wantNum {
					t.Errorf("Number = %v, want %v", got, tc.wantNum)
				}
			}
		})
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
