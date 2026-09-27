// Package stale_namespace flags namespaces with no workload activity
// (no CPU, no network, no pod restarts) for more than the staleness
// window. Agent-mode only — requires Prometheus rollups.
package stale_namespace

import (
	"context"
	"errors"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// Activity is the per-namespace signal snapshot the agent assembles
// from Prometheus rollups. All counts cover the staleness window.
type Activity struct {
	Namespace               string
	WorkloadCount           int
	CPUMilliSecondsObserved int64
	NetworkBytes            int64
	PodRestarts             int
	MonthlyCostUSDCents     int64
}

// MinStaleDays gates how long the activity window must be empty.
// 30 days matches the playbook.
const MinStaleDays = 30

type Detector struct {
	StaleDays int // override
}

func NewDetector() *Detector { return &Detector{StaleDays: MinStaleDays} }

// Analyze emits a finding per stale namespace. CPU-zero AND
// network-zero AND restarts-zero is the conjunction; any of them
// non-zero means the namespace isn't stale.
func (d *Detector) Analyze(_ context.Context, _ tenancy.Context, snapshots []Activity) ([]rules.Finding, error) {
	if d.StaleDays < 0 {
		return nil, errors.New("stale_namespace: negative StaleDays")
	}
	var out []rules.Finding
	for _, a := range snapshots {
		if a.WorkloadCount == 0 {
			continue // empty namespace ≠ stale; could be a fresh app
		}
		if a.CPUMilliSecondsObserved > 0 || a.NetworkBytes > 0 || a.PodRestarts > 0 {
			continue
		}
		out = append(out, rules.Finding{
			DetectorID:      "stale-namespace",
			Severity:        rules.SeverityMed,
			Category:        rules.CategoryCost,
			Workload:        a.Namespace,
			Title:           "Namespace shows no activity over the staleness window",
			Detail:          "Verify with the team that owns the namespace before deleting.",
			MonthlyUSDCents: a.MonthlyCostUSDCents,
		})
	}
	return out, nil
}
