// Package vpa_divergence flags workloads where an in-cluster VPA in
// Off or Initial mode disagrees with the live request — VPA's
// recommendation is materially below what's set, which is a free
// signal of overprovisioning the cluster has already computed for us.
// ADR-0012: VPA is one signal, never the actuator.
package vpa_divergence

import (
	"context"
	"errors"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// Sample is one workload's current request paired with the VPA's
// recommendation. Mode is one of "Off", "Initial", "Auto" — Auto means
// VPA is already mutating the workload, so this detector stays out of
// its way.
type Sample struct {
	Workload            string
	Namespace           string
	Mode                string
	CurrentCPUm         int64
	CurrentMemB         int64
	RecommendCPUm       int64
	RecommendMemB       int64
	MonthlyCostUSDCents int64
}

// MinDivergence is the fractional gap that triggers a finding. 0.2
// means VPA recommends ≥20% lower than the current request. Below
// that, the noise floor of VPA + our cost math isn't worth a PR.
type Threshold struct {
	MinDivergence float64
}

func Default() Threshold { return Threshold{MinDivergence: 0.20} }

type Detector struct {
	Threshold Threshold
}

func NewDetector() *Detector { return &Detector{Threshold: Default()} }

func (d *Detector) Analyze(_ context.Context, _ tenancy.Context, samples []Sample) ([]rules.Finding, error) {
	gap := d.Threshold.MinDivergence
	if gap < 0 {
		return nil, errors.New("vpa_divergence: negative MinDivergence")
	}
	if gap == 0 {
		gap = 0.20
	}
	var out []rules.Finding
	for _, s := range samples {
		if s.Mode == "Auto" {
			continue
		}
		if s.Mode != "Off" && s.Mode != "Initial" {
			continue
		}
		if diverges(s.CurrentCPUm, s.RecommendCPUm, gap) || diverges(s.CurrentMemB, s.RecommendMemB, gap) {
			out = append(out, rules.Finding{
				DetectorID:      "vpa-recommendation-divergence",
				Severity:        rules.SeverityMed,
				Category:        rules.CategoryCost,
				Workload:        s.Workload,
				Title:           "VPA recommendation diverges from current request",
				Detail:          "VPA in " + s.Mode + " mode suggests a lower request; consider applying.",
				MonthlyUSDCents: s.MonthlyCostUSDCents,
			})
		}
	}
	return out, nil
}

func diverges(current, recommend int64, gap float64) bool {
	if current <= 0 || recommend <= 0 {
		return false
	}
	if recommend >= current {
		return false
	}
	return float64(current-recommend)/float64(current) >= gap
}
