package k8s

import (
	"context"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

var (
	ctx = context.Background()
	tnt = tenancy.Context{TenantID: "tenant-k"}
	w   = WorkloadRef{ClusterID: "c-1", Namespace: "default", Kind: "Deployment", Name: "api"}
)

func TestInMemoryEvents_Recent(t *testing.T) {
	e := NewInMemoryEvents()
	t0 := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	e.Record(Event{Workload: w, Reason: "OOMKilled", Type: "Warning", LastSeen: t0})
	e.Record(Event{Workload: w, Reason: "FailedScheduling", Type: "Warning", LastSeen: t0.Add(-2 * time.Hour)})

	for _, tc := range []struct {
		name  string
		since time.Time
		want  int
	}{
		{name: "window includes both", since: t0.Add(-3 * time.Hour), want: 2},
		{name: "window excludes old", since: t0.Add(-time.Hour), want: 1},
		{name: "window excludes all", since: t0.Add(time.Hour), want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.Recent(ctx, tnt, w, tc.since)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Errorf("len = %d, want %d", len(got), tc.want)
			}
		})
	}
}

func TestInMemoryHPA_GetMiss(t *testing.T) {
	h := NewInMemoryHPA()
	got, err := h.Get(ctx, tnt, w)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("Get on empty returns %+v, want nil", got)
	}
}

func TestInMemoryHPA_GetHit(t *testing.T) {
	h := NewInMemoryHPA()
	h.Set(w, HPAState{MinReplicas: 2, MaxReplicas: 10, ConditionsHealthy: true})
	got, err := h.Get(ctx, tnt, w)
	if err != nil || got == nil {
		t.Fatalf("Get: %v / %+v", err, got)
	}
	if got.MinReplicas != 2 || got.MaxReplicas != 10 {
		t.Errorf("HPAState = %+v", got)
	}
}

func TestInMemoryPolicy_Snapshot(t *testing.T) {
	p := NewInMemoryPolicy()
	p.Set(w, PolicySnapshot{PDB: &PDB{MinAvailable: 2}})
	snap, err := p.Snapshot(ctx, tnt, w)
	if err != nil {
		t.Fatal(err)
	}
	if snap.PDB == nil || snap.PDB.MinAvailable != 2 {
		t.Errorf("PDB = %+v", snap.PDB)
	}
}

func TestInMemoryVPA(t *testing.T) {
	v := NewInMemoryVPA()
	v.Set(w, VPARecommendation{Mode: "Auto", RecommendCPUm: 1500, RecommendMemB: 1 << 30})
	got, err := v.Get(ctx, tnt, w)
	if err != nil || got == nil {
		t.Fatalf("Get: %v / %+v", err, got)
	}
	if got.Mode != "Auto" {
		t.Errorf("Mode = %q", got.Mode)
	}
}

func TestInMemoryKarpenter(t *testing.T) {
	k := NewInMemoryKarpenter()
	k.Set([]KarpenterNodePool{{Name: "default", NodeCountCurrent: 5}})
	got, err := k.List(ctx, tnt)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "default" {
		t.Errorf("List = %+v", got)
	}
}
