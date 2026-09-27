package coalesce

import (
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestCoalescer_Observe(t *testing.T) {
	t0 := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	now := t0
	clock := func() time.Time { return now }

	c := New(24*time.Hour, clock)
	k := Key{TenantID: "t-1", RepoOwner: "acme", RepoName: "api", ChartPath: "charts/api"}
	tnt := tenancy.Context{TenantID: k.TenantID}

	for _, tc := range []struct {
		name          string
		advance       time.Duration
		k             Key
		prN           int
		prURL         string
		wantDisp      bool
		wantCoalToURL string
		wantErr       bool
	}{
		{name: "first PR dispatches", k: k, prN: 100, prURL: "https://x/100", wantDisp: true},
		{name: "second PR within TTL coalesces", k: k, prN: 101, prURL: "https://x/101", wantDisp: false, wantCoalToURL: "https://x/100"},
		{name: "different chart-path dispatches", k: Key{TenantID: k.TenantID, RepoOwner: k.RepoOwner, RepoName: k.RepoName, ChartPath: "charts/worker"}, prN: 102, prURL: "https://x/102", wantDisp: true},
		{name: "after TTL same chart dispatches fresh", advance: 25 * time.Hour, k: k, prN: 200, prURL: "https://x/200", wantDisp: true},
		{name: "incomplete key errors", k: Key{TenantID: "t-1"}, prN: 0, prURL: "", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now = now.Add(tc.advance)
			dec, err := c.Observe(tnt, tc.k, tc.prN, tc.prURL)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Observe: %v", err)
			}
			if dec.Dispatch != tc.wantDisp {
				t.Errorf("Dispatch = %v, want %v", dec.Dispatch, tc.wantDisp)
			}
			if tc.wantCoalToURL != "" && dec.CoalesceTo != tc.wantCoalToURL {
				t.Errorf("CoalesceTo = %q, want %q", dec.CoalesceTo, tc.wantCoalToURL)
			}
		})
	}
}

func TestCoalescer_Sweep(t *testing.T) {
	t0 := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	now := t0
	c := New(1*time.Hour, func() time.Time { return now })

	tnt := tenancy.Context{TenantID: "t-1"}
	for i := 0; i < 3; i++ {
		k := Key{TenantID: "t-1", RepoOwner: "a", RepoName: "b", ChartPath: "c-"}
		k.ChartPath += string(rune('a' + i))
		if _, err := c.Observe(tnt, k, i, ""); err != nil {
			t.Fatal(err)
		}
	}
	if c.Len() != 3 {
		t.Fatalf("Len = %d, want 3", c.Len())
	}
	now = now.Add(2 * time.Hour) // all expired
	if got := c.Sweep(); got != 3 {
		t.Errorf("Sweep dropped %d, want 3", got)
	}
	if c.Len() != 0 {
		t.Errorf("Len after sweep = %d, want 0", c.Len())
	}
}
