// Package orphaned_pvc flags PVCs that exist in-cluster but have no
// pod referencing them. Agent-mode only — static Helm analysis cannot
// detect this; the CLI parser intentionally does not extract volume
// info. EBS cost attribution comes from the CUR-backed billing
// source; without it the finding renders with MonthlyUSDCents=0 and
// a renderer hint.
package orphaned_pvc

import (
	"context"
	"errors"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// PVCRef is the narrow read the detector needs. The agent's k8s
// integration converts client-go PersistentVolumeClaim objects into
// this shape via a small adapter.
type PVCRef struct {
	Namespace        string
	Name             string
	StorageClass     string
	CapacityBytes    int64
	AgeDays          int
	Referenced       bool  // true when at least one Pod's volumes references this PVC
	MonthlyCostCents int64 // attributed via CUR; 0 means unattributed
}

// MinAgeDays is the lower bound — fresh PVCs may be referenced by
// a Pod that hasn't booted yet (Helm post-install hook, etc.).
const MinAgeDays = 7

// Detector emits a Finding per orphaned PVC older than MinAgeDays.
type Detector struct {
	MinAge int // override MinAgeDays
}

func NewDetector() *Detector { return &Detector{MinAge: MinAgeDays} }

func (d *Detector) Analyze(_ context.Context, _ tenancy.Context, pvcs []PVCRef) ([]rules.Finding, error) {
	if d.MinAge < 0 {
		return nil, errors.New("orphaned_pvc: negative MinAge")
	}
	threshold := d.MinAge
	if threshold == 0 {
		threshold = MinAgeDays
	}
	var out []rules.Finding
	for _, p := range pvcs {
		if p.Referenced {
			continue
		}
		if p.AgeDays < threshold {
			continue
		}
		out = append(out, rules.Finding{
			DetectorID:      "orphaned-pvc",
			Severity:        rules.SeverityMed,
			Category:        rules.CategoryCost,
			Workload:        p.Namespace + "/" + p.Name,
			Title:           "PersistentVolumeClaim has no referencing pod",
			Detail:          "Likely safe to delete after a 7-day soak; verify with the owning team.",
			MonthlyUSDCents: p.MonthlyCostCents,
		})
	}
	return out, nil
}
