package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/optiqor/backend/internal/tenancy"
)

func TestNewKeyspace_RequiresTenant(t *testing.T) {
	_, err := NewKeyspace(tenancy.Context{})
	if !errors.Is(err, ErrEmptyTenant) {
		t.Fatalf("expected ErrEmptyTenant, got %v", err)
	}
}

func TestKeyspace_Prefix(t *testing.T) {
	k, err := NewKeyspace(tenancy.Context{TenantID: "tenant-abc"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := k.Prefix(), "t:tenant-abc:"; got != want {
		t.Errorf("Prefix() = %q, want %q", got, want)
	}
}

func TestKeyspace_Key(t *testing.T) {
	k, _ := NewKeyspace(tenancy.Context{TenantID: "t1"})
	cases := []struct {
		parts []string
		want  string
	}{
		{[]string{}, "t:t1:"},
		{[]string{"foo"}, "t:t1:foo"},
		{[]string{"rate", "ip", "1.2.3.4"}, "t:t1:rate:ip:1.2.3.4"},
	}
	for _, tc := range cases {
		if got := k.Key(tc.parts...); got != tc.want {
			t.Errorf("Key(%v) = %q, want %q", tc.parts, got, tc.want)
		}
	}
}

func TestKeyspace_Pattern(t *testing.T) {
	k, _ := NewKeyspace(tenancy.Context{TenantID: "t1"})
	if got, want := k.Pattern("rate:*"), "t:t1:rate:*"; got != want {
		t.Errorf("Pattern(rate:*) = %q, want %q", got, want)
	}
}

func TestKeyspace_DifferentTenantsCannotCollide(t *testing.T) {
	a, _ := NewKeyspace(tenancy.Context{TenantID: "alice"})
	b, _ := NewKeyspace(tenancy.Context{TenantID: "bob"})
	if a.Key("x") == b.Key("x") {
		t.Fatal("two tenants must never produce the same key")
	}
	if !strings.HasPrefix(a.Key("x"), "t:alice:") || !strings.HasPrefix(b.Key("x"), "t:bob:") {
		t.Fatalf("prefix wrong: a=%q b=%q", a.Key("x"), b.Key("x"))
	}
}

func TestKeyspace_String(t *testing.T) {
	k, _ := NewKeyspace(tenancy.Context{TenantID: "t1"})
	if !strings.Contains(k.String(), "t:t1:") {
		t.Errorf("String() = %q", k.String())
	}
}

func TestKeyspace_TenantID(t *testing.T) {
	k, _ := NewKeyspace(tenancy.Context{TenantID: "t1"})
	if k.TenantID() != "t1" {
		t.Errorf("TenantID() = %q", k.TenantID())
	}
}
