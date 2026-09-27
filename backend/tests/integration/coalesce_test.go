//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/ingestion/coalesce"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// TestCoalesce_TwoPRsSameChart confirms the documented contract:
// the second PR against the same chart within 24h short-circuits to
// the first, but a fresh PR after the TTL goes through again.
func TestCoalesce_TwoPRsSameChart(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	c := coalesce.New(24*time.Hour, clock)
	tnt := tenancy.Context{TenantID: "tenant-c"}
	k := coalesce.Key{TenantID: tnt.TenantID, RepoOwner: "acme", RepoName: "api", ChartPath: "charts/api"}

	first, err := c.Observe(tnt, k, 100, "https://github.com/acme/api/pull/100")
	if err != nil {
		t.Fatal(err)
	}
	if !first.Dispatch {
		t.Errorf("first PR must dispatch")
	}

	now = now.Add(2 * time.Hour)
	second, err := c.Observe(tnt, k, 101, "https://github.com/acme/api/pull/101")
	if err != nil {
		t.Fatal(err)
	}
	if second.Dispatch {
		t.Errorf("second PR within TTL must coalesce")
	}
	if second.CoalesceTo == "" {
		t.Errorf("CoalesceTo should be set to the existing PR URL")
	}

	now = now.Add(48 * time.Hour) // both entries now stale
	third, err := c.Observe(tnt, k, 200, "https://github.com/acme/api/pull/200")
	if err != nil {
		t.Fatal(err)
	}
	if !third.Dispatch {
		t.Errorf("after TTL, fresh PR must dispatch again")
	}
}
