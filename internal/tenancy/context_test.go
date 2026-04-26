package tenancy

import (
	"context"
	"errors"
	"testing"
)

func TestContext_Validate(t *testing.T) {
	tests := []struct {
		name    string
		c       Context
		wantErr error
	}{
		{"valid minimal", Context{TenantID: "t1"}, nil},
		{"valid full", Context{TenantID: "t1", WorkspaceID: "w1", ClusterID: "c1", Namespace: "n1"}, nil},
		{"missing tenant", Context{}, ErrNoTenant},
		{"workspace without tenant", Context{WorkspaceID: "w1"}, ErrNoTenant},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.c.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestWithFromContext_Roundtrip(t *testing.T) {
	in := Context{TenantID: "t1", WorkspaceID: "w1", ClusterID: "c1", Namespace: "ns1"}
	ctx := WithContext(context.Background(), in)
	out, err := FromContext(ctx)
	if err != nil {
		t.Fatalf("FromContext: %v", err)
	}
	if out != in {
		t.Fatalf("roundtrip: got %v, want %v", out, in)
	}
}

func TestFromContext_Empty(t *testing.T) {
	_, err := FromContext(context.Background())
	if !errors.Is(err, ErrNoTenant) {
		t.Fatalf("FromContext: got %v, want ErrNoTenant", err)
	}
}

func TestFromContext_InvalidStored(t *testing.T) {
	// A context value with an empty tenant must still report ErrNoTenant
	// even though the type-assertion succeeds — Validate() catches it.
	ctx := context.WithValue(context.Background(), ctxKey{}, Context{WorkspaceID: "w1"})
	_, err := FromContext(ctx)
	if !errors.Is(err, ErrNoTenant) {
		t.Fatalf("FromContext invalid: got %v, want ErrNoTenant", err)
	}
}

func TestMustFromContext_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic")
		}
	}()
	MustFromContext(context.Background())
}

func TestContext_String(t *testing.T) {
	c := Context{TenantID: "t1", WorkspaceID: "w1"}
	got := c.String()
	want := "tenant=t1 workspace=w1 cluster= ns="
	if got != want {
		t.Fatalf("String(): got %q, want %q", got, want)
	}
}
