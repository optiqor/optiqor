package oomkilled

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestReader_CountAndRecent(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	client := NewInMemoryClient()
	r, err := NewReader(client, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r.NowFunc = func() time.Time { return now }

	w := WorkloadRef{ClusterID: "prod-1", Namespace: "default", Workload: "api"}
	query := buildQuery(w, 7*24*time.Hour)
	client.Set(query, 3)

	got, err := r.Count(context.Background(), tenancy.Context{TenantID: "t"}, w)
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("Count = %d, want 3", got)
	}

	recent, err := r.Recent(context.Background(), tenancy.Context{TenantID: "t"}, w)
	if err != nil {
		t.Fatal(err)
	}
	if !recent {
		t.Errorf("Recent = false, want true with count=3")
	}
}

func TestReader_EmptyWindowZeroOOMs(t *testing.T) {
	r, _ := NewReader(NewInMemoryClient(), 7*24*time.Hour)
	r.NowFunc = func() time.Time { return time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC) }
	recent, err := r.Recent(context.Background(), tenancy.Context{TenantID: "t"}, WorkloadRef{
		ClusterID: "c", Namespace: "default", Workload: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if recent {
		t.Error("Recent should be false on empty client")
	}
}

func TestReader_RejectsIncompleteRef(t *testing.T) {
	r, _ := NewReader(NewInMemoryClient(), time.Hour)
	_, err := r.Count(context.Background(), tenancy.Context{TenantID: "t"}, WorkloadRef{})
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("err = %v, want substring 'incomplete'", err)
	}
}

func TestNewReader_RejectsNilClient(t *testing.T) {
	_, err := NewReader(nil, time.Hour)
	if err == nil {
		t.Error("expected nil-client rejection")
	}
}

func TestBuildQuery(t *testing.T) {
	q := buildQuery(WorkloadRef{ClusterID: "prod-1", Namespace: "default", Workload: "api"}, 7*24*time.Hour)
	for _, want := range []string{"cluster=\"prod-1\"", "namespace=\"default\"", "pod=~\"api-.*\"", "OOMKilled", "[7d]"} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q:\n%s", want, q)
		}
	}
}

func TestWindowExpr(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{7 * 24 * time.Hour, "7d"},
		{24 * time.Hour, "1d"},
		{36 * time.Hour, "36h"},
		{2 * time.Hour, "2h"},
	} {
		if got := windowExpr(tc.in); got != tc.want {
			t.Errorf("windowExpr(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
