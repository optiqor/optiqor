// Package hpa_pinned_to_min flags HPAs that have sat at MinReplicas
// for the entire window with CPU well under the target — the floor is
// set too conservatively and is preventing the scaler from doing its
// job. Recommendation: drop MinReplicas by one. Agent-mode only;
// CLI sandbox doesn't see CurrentReplicas.
package hpa_pinned_to_min

import (
	"context"
	"errors"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// Sample is one HPA's window snapshot. AvgCPUUtilPct is the
// time-averaged percent over the window (0..100). When TargetCPUUtil
// is 0 the HPA targets memory; the detector still fires when
// CurrentReplicas == MinReplicas and the load signal is low because
// that's the symptom regardless of the scale target.
type Sample struct {
	Workload            string
	Namespace           string
	MinReplicas         int
	MaxReplicas         int
	CurrentReplicas     int
	TargetCPUUtilPct    int
	AvgCPUUtilPct       float64
	MonthlyCostUSDCents int64
}

// Threshold gates how slack the average has to be before we suggest
// dropping the floor. 0.5 of the target keeps false positives down
// for spiky workloads that still earn their floor on bursts.
type Threshold struct {
	MaxLoadRatio float64
}

func Default() Threshold { return Threshold{MaxLoadRatio: 0.5} }

type Detector struct {
	Threshold Threshold
}

func NewDetector() *Detector { return &Detector{Threshold: Default()} }

func (d *Detector) Analyze(_ context.Context, _ tenancy.Context, samples []Sample) ([]rules.Finding, error) {
	ratio := d.Threshold.MaxLoadRatio
	if ratio < 0 {
		return nil, errors.New("hpa_pinned_to_min: negative MaxLoadRatio")
	}
	if ratio == 0 {
		ratio = 0.5
	}
	var out []rules.Finding
	for _, s := range samples {
		if s.MinReplicas <= 1 {
			continue
		}
		if s.CurrentReplicas != s.MinReplicas {
			continue
		}
		if s.TargetCPUUtilPct <= 0 {
			continue
		}
		if s.AvgCPUUtilPct >= float64(s.TargetCPUUtilPct)*ratio {
			continue
		}
		out = append(out, rules.Finding{
			DetectorID:      "hpa-pinned-to-min",
			Severity:        rules.SeverityMed,
			Category:        rules.CategoryCost,
			Workload:        s.Workload,
			Title:           "HPA stuck at minReplicas; floor likely too high",
			Detail:          "Drop minReplicas by one and re-observe for a week.",
			MonthlyUSDCents: s.MonthlyCostUSDCents,
		})
	}
	return out, nil
}
