package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestNewKeyspace_RequiresTenant(t *testing.T) {
	_, err := NewKeyspace(tenancy.Context{})
	if !errors.Is(err, ErrEmptyTenant) {
		t.Fatalf("expected ErrEmptyTenant, got %v", err)
	}
}

func TestKeyspace_Key(t *testing.T) {
	k, err := NewKeyspace(tenancy.Context{TenantID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		parts []string
		want  string
	}{
		{"no parts is prefix", []string{}, "t:t1:"},
		{"single part", []string{"foo"}, "t:t1:foo"},
		{"multiple parts joined", []string{"rate", "ip", "1.2.3.4"}, "t:t1:rate:ip:1.2.3.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := k.Key(tc.parts...); got != tc.want {
				t.Errorf("Key(%v) = %q, want %q", tc.parts, got, tc.want)
			}
		})
	}
}

func TestKeyspace_PrefixAndPattern(t *testing.T) {
	k, _ := NewKeyspace(tenancy.Context{TenantID: "tenant-abc"})
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"prefix", k.Prefix(), "t:tenant-abc:"},
		{"pattern", k.Pattern("rate:*"), "t:tenant-abc:rate:*"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
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

func TestKeyspace_Accessors(t *testing.T) {
	k, _ := NewKeyspace(tenancy.Context{TenantID: "t1"})
	for _, tc := range []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "string contains prefix",
			check: func(t *testing.T) {
				t.Helper()
				if !strings.Contains(k.String(), "t:t1:") {
					t.Errorf("String() = %q", k.String())
				}
			},
		},
		{
			name: "tenant id round trip",
			check: func(t *testing.T) {
				t.Helper()
				if k.TenantID() != "t1" {
					t.Errorf("TenantID() = %q", k.TenantID())
				}
			},
		},
	} {
		t.Run(tc.name, tc.check)
	}
}
