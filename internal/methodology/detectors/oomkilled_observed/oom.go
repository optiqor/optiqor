// Package oomkilled_observed flags workloads whose memory request is
// too low: the cluster recorded OOMKill events in the analysis window.
// Sandbox can't see this; only the agent's Event stream surfaces it.
// Inverse of memory_overprovisioned — the recommendation here raises
// the request rather than lowering it.
package oomkilled_observed

import (
	"context"
	"errors"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// Sample is one workload's OOMKill exposure over the analysis window.
// CurrentRequestB and PeakRSSBytes drive the recommended bump; the
// agent reader fills them from `kube_pod_container_status_last_
// terminated_reason{reason="OOMKilled"}` + container_memory_max_usage.
type Sample struct {
	Workload            string
	Namespace           string
	OOMKillCount        int
	CurrentRequestB     int64
	PeakRSSBytes        int64
	MonthlyCostUSDCents int64
}

// Threshold tunes the firing rule. Default fires on any OOMKill — a
// single OOMKill is already a customer-visible incident.
type Threshold struct {
	MinKills int
}

func Default() Threshold { return Threshold{MinKills: 1} }

type Detector struct {
	Threshold Threshold
}

func NewDetector() *Detector { return &Detector{Threshold: Default()} }

func (d *Detector) Analyze(_ context.Context, _ tenancy.Context, samples []Sample) ([]rules.Finding, error) {
	if d.Threshold.MinKills < 0 {
		return nil, errors.New("oomkilled_observed: negative MinKills")
	}
	floor := d.Threshold.MinKills
	if floor == 0 {
		floor = 1
	}
	var out []rules.Finding
	for _, s := range samples {
		if s.OOMKillCount < floor {
			continue
		}
		out = append(out, rules.Finding{
			DetectorID:      "oomkilled-observed",
			Severity:        rules.SeverityHigh,
			Category:        rules.CategoryCost,
			Workload:        s.Workload,
			Title:           "Memory request too low; OOMKilled events observed",
			Detail:          "Raise request above the observed peak RSS to stop OOMKills.",
			MonthlyUSDCents: s.MonthlyCostUSDCents,
		})
	}
	return out, nil
}
