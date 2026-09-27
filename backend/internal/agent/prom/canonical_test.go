package prom

import (
	"strings"
	"testing"
	"time"
)

// Every live + historical CPU/memory query must filter container!="POD"
// and container!="" so cAdvisor's pause-container metric doesn't get
// counted as application usage. The doc's "20-40% over-recommendation"
// trap; this test makes it impossible to silently delete the guard.
func TestCanonical_QueriesFilterPauseContainer(t *testing.T) {
	guard := `container!="POD",container!=""`
	for _, tc := range []struct {
		name string
		q    string
	}{
		{"cpu rate", Canonical.CPURate},
		{"memory working set", Canonical.MemoryWorkingSet},
		{"cpu p95 30d", Canonical.CPURateP95_30d},
		{"cpu p99 30d", Canonical.CPURateP99_30d},
		{"mem p99 30d", Canonical.MemoryWorkingP99_30d},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.q, guard) {
				t.Errorf("query missing pause-container guard:\n%s", tc.q)
			}
		})
	}
}

// Memory queries must read working_set_bytes, never usage_bytes. The
// doc's other 20-40% over-recommendation trap.
func TestCanonical_MemoryQueriesUseWorkingSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    string
	}{
		{"live memory", Canonical.MemoryWorkingSet},
		{"30d p99 memory", Canonical.MemoryWorkingP99_30d},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.q, "container_memory_working_set_bytes") {
				t.Errorf("memory query does not use working_set_bytes:\n%s", tc.q)
			}
			if strings.Contains(tc.q, "container_memory_usage_bytes") {
				t.Errorf("memory query uses usage_bytes (over-recommends):\n%s", tc.q)
			}
		})
	}
}

func TestCanonical_30DayQueriesUseQuantileOverTime(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    string
	}{
		{"cpu p95", Canonical.CPURateP95_30d},
		{"cpu p99", Canonical.CPURateP99_30d},
		{"mem p99", Canonical.MemoryWorkingP99_30d},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.q, "quantile_over_time") {
				t.Errorf("not a quantile_over_time query:\n%s", tc.q)
			}
			if !strings.Contains(tc.q, "[30d:1h]") {
				t.Errorf("missing [30d:1h] subquery:\n%s", tc.q)
			}
		})
	}
}

func TestCanonical_KSMQueriesUseKubeStateMetrics(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    string
	}{
		{"hpa min", Canonical.HPAMinReplicas},
		{"hpa max", Canonical.HPAMaxReplicas},
		{"hpa current", Canonical.HPACurrentReplicas},
		{"requests cpu", Canonical.PodRequestsCPU},
		{"requests memory", Canonical.PodRequestsMemory},
		{"limits cpu", Canonical.PodLimitsCPU},
		{"limits memory", Canonical.PodLimitsMemory},
		{"pod restarts", Canonical.PodRestarts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.HasPrefix(tc.q, "kube_") {
				t.Errorf("not a kube-state-metrics query:\n%s", tc.q)
			}
		})
	}
}

func TestHistoricalRange_Is30Days(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	r := HistoricalRange(now)
	if !r.End.Equal(now) {
		t.Errorf("End = %v, want %v", r.End, now)
	}
	if got := now.Sub(r.Start); got != 30*24*time.Hour {
		t.Errorf("range = %v, want 30d", got)
	}
	if r.Step != time.Hour {
		t.Errorf("step = %v, want 1h", r.Step)
	}
}

func TestDefaultProfile_BindsCanonical(t *testing.T) {
	// Scraper's DefaultProfile must point at the canonical queries so
	// changes to the doc-pinned PromQL automatically flow through.
	if DefaultProfile.CPURate != Canonical.CPURate {
		t.Error("DefaultProfile.CPURate diverged from Canonical")
	}
	if DefaultProfile.MemoryWorking != Canonical.MemoryWorkingSet {
		t.Error("DefaultProfile.MemoryWorking diverged from Canonical")
	}
	if DefaultProfile.OOMKilledIncrease != Canonical.OOMKilledIncrease {
		t.Error("DefaultProfile.OOMKilledIncrease diverged from Canonical")
	}
}
