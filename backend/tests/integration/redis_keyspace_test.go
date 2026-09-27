//go:build integration

package integration

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/platform/db"
	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestRedis_KeyspacePrefixIsolation(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	ksA, err := db.NewKeyspace(tenancy.Context{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("keyspace A: %v", err)
	}
	ksB, err := db.NewKeyspace(tenancy.Context{TenantID: "tenant-b"})
	if err != nil {
		t.Fatalf("keyspace B: %v", err)
	}

	if err := h.Redis.Set(ctx, ksA.Prefix()+"session:abc", "alice", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := h.Redis.Set(ctx, ksB.Prefix()+"session:abc", "bob", 0).Err(); err != nil {
		t.Fatal(err)
	}

	if a, _ := h.Redis.Get(ctx, ksA.Prefix()+"session:abc").Result(); a != "alice" {
		t.Errorf("A read = %q, want alice", a)
	}
	if b, _ := h.Redis.Get(ctx, ksB.Prefix()+"session:abc").Result(); b != "bob" {
		t.Errorf("B read = %q, want bob", b)
	}

	// SCAN audit: any key without the t: prefix is a tenancy bypass.
	keys, _, err := h.Redis.Scan(ctx, 0, "*", 1000).Result()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !strings.HasPrefix(k, "t:") {
			t.Errorf("found Redis key without t: prefix — tenancy bypass: %q", k)
		}
	}
}

// Empty tenant id must NOT yield a Keyspace; the resulting "t::"
// prefix would alias every caller that forgot to bind a tenant.
func TestRedis_EmptyTenantRejected(t *testing.T) {
	if _, err := db.NewKeyspace(tenancy.Context{}); err == nil {
		t.Error("NewKeyspace({}) returned nil error — would build prefix 't::'")
	}
}

func TestRedis_TTLBehaviour(t *testing.T) {
	h := New(t)
	ctx := context.Background()
	ks, _ := db.NewKeyspace(tenancy.Context{TenantID: "ttl-test"})

	key := ks.Prefix() + "ratelimit:ip:1.2.3.4"
	if err := h.Redis.Set(ctx, key, "1", 2*time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	ttl, err := h.Redis.TTL(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= 0 || ttl > 2*time.Second {
		t.Errorf("ttl = %v, want 0 < ttl ≤ 2s", ttl)
	}
}
