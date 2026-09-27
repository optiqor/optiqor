//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"

	"github.com/optiqor/optiqor/internal/sandbox"
)

func TestPgStore_PutGetRoundtrip(t *testing.T) {
	h := New(t)
	ctx := context.Background()
	store := sandbox.NewPgStore(PgxExec{Pool: h.PG})

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	body := []byte(`{"workloads_analyzed":3,"source":"cli","findings":[]}`)
	in := sandbox.SharedAnalysis{
		Hash:      hashOf(body),
		Body:      body,
		MediaType: "application/json",
		Source:    "cli",
		Workloads: 3,
		Findings: []rules.Finding{
			{DetectorID: "cpu-overprovisioned", Severity: rules.SeverityMed, Workload: "api"},
		},
		CreatedAt: now,
		ExpiresAt: now.Add(30 * 24 * time.Hour),
	}

	if err := store.Put(ctx, in); err != nil {
		t.Fatalf("Put: %v", err)
	}
	out, err := store.Get(ctx, in.Hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !bytes.Equal(out.Body, in.Body) {
		t.Errorf("Body drift:\n got %q\nwant %q", out.Body, in.Body)
	}
	if out.MediaType != in.MediaType || out.Source != in.Source || out.Workloads != in.Workloads {
		t.Errorf("metadata drift: got %+v want %+v", out, in)
	}
	if len(out.Findings) != 1 || out.Findings[0].DetectorID != "cpu-overprovisioned" {
		t.Errorf("findings drift: %+v", out.Findings)
	}
}

// Re-Put on the same hash must preserve the original payload (only
// expires_at refreshes). Keeps share URLs stable across reanalysis.
func TestPgStore_HashDedupOnRepeatPut(t *testing.T) {
	h := New(t)
	ctx := context.Background()
	store := sandbox.NewPgStore(PgxExec{Pool: h.PG})

	t0 := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return t0 }
	body := []byte(`{"k":"v"}`)
	hash := hashOf(body)
	first := sandbox.SharedAnalysis{
		Hash: hash, Body: body, MediaType: "application/json",
		Source: "cli", Workloads: 1,
		CreatedAt: t0, ExpiresAt: t0.Add(7 * 24 * time.Hour),
	}
	if err := store.Put(ctx, first); err != nil {
		t.Fatalf("first Put: %v", err)
	}
	repeat := first
	repeat.Body = []byte(`{"k":"different"}`)
	repeat.ExpiresAt = t0.Add(30 * 24 * time.Hour)
	if err := store.Put(ctx, repeat); err != nil {
		t.Fatalf("repeat Put: %v", err)
	}

	out, err := store.Get(ctx, hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(out.Body, first.Body) {
		t.Errorf("re-Put mutated payload: got %q, want %q", out.Body, first.Body)
	}
	if !out.ExpiresAt.Equal(repeat.ExpiresAt) {
		t.Errorf("ExpiresAt not refreshed: got %v, want %v", out.ExpiresAt, repeat.ExpiresAt)
	}
}

func TestPgStore_ExpiredReturnsNotFound(t *testing.T) {
	h := New(t)
	ctx := context.Background()
	store := sandbox.NewPgStore(PgxExec{Pool: h.PG})

	t0 := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return t0 }
	body := []byte(`{"expired":true}`)
	hash := hashOf(body)
	if err := store.Put(ctx, sandbox.SharedAnalysis{
		Hash: hash, Body: body, MediaType: "application/json",
		Source: "cli", CreatedAt: t0, ExpiresAt: t0.Add(1 * time.Hour),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	store.Now = func() time.Time { return t0.Add(2 * time.Hour) }

	if _, err := store.Get(ctx, hash); !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("Get expired hash: err = %v, want ErrNotFound", err)
	}
}

func TestPgStore_ViewCountIncrements(t *testing.T) {
	h := New(t)
	ctx := context.Background()
	store := sandbox.NewPgStore(PgxExec{Pool: h.PG})

	t0 := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return t0 }
	body := []byte(`{"v":1}`)
	hash := hashOf(body)
	if err := store.Put(ctx, sandbox.SharedAnalysis{
		Hash: hash, Body: body, MediaType: "application/json",
		Source: "sandbox", CreatedAt: t0, ExpiresAt: t0.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := store.Get(ctx, hash); err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}
	}
	var n int
	if err := h.PG.QueryRow(ctx,
		`SELECT view_count FROM shared_analyses WHERE hash = $1`, hash).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("view_count = %d, want 3", n)
	}
}

func hashOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:6])
}
