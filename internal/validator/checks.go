package validator

import (
	"context"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// PDBCheck rejects a replica reduction that would drop the workload
// below its PDB minAvailable. Most common Apply Fix safety hole.
type PDBCheck struct{}

func (PDBCheck) Name() string { return "pdb" }

func (PDBCheck) Check(_ context.Context, _ tenancy.Context, c Candidate) (*Verdict, error) {
	if c.Signals.PDB == nil || c.ProposedReplicas == 0 {
		return nil, nil
	}
	minAvail := c.Signals.PDB.MinAvailable
	if minAvail <= 0 {
		return nil, nil
	}
	if c.ProposedReplicas < minAvail {
		return &Verdict{
			Severity: SeverityHard,
			Reason:   "proposed replicas below PDB minAvailable",
		}, nil
	}
	return nil, nil
}

// ResourceQuotaCheck rejects a request that would push a namespace
// quota over budget. Reads quota usage from the cluster signal.
type ResourceQuotaCheck struct{}

func (ResourceQuotaCheck) Name() string { return "resourcequota" }

func (ResourceQuotaCheck) Check(_ context.Context, _ tenancy.Context, c Candidate) (*Verdict, error) {
	if c.Signals.Quota == nil {
		return nil, nil
	}
	q := c.Signals.Quota
	if c.ProposedCPU.Millicores > 0 && q.CPUMillicores > 0 {
		if q.UsedCPUMilli+c.ProposedCPU.Millicores > q.CPUMillicores {
			return &Verdict{
				Severity: SeverityHard,
				Reason:   "proposed CPU exceeds namespace ResourceQuota",
			}, nil
		}
	}
	if c.ProposedMemory.Bytes > 0 && q.MemoryBytes > 0 {
		if q.UsedMemoryB+c.ProposedMemory.Bytes > q.MemoryBytes {
			return &Verdict{
				Severity: SeverityHard,
				Reason:   "proposed memory exceeds namespace ResourceQuota",
			}, nil
		}
	}
	return nil, nil
}

// LimitRangeCheck rejects requests outside the namespace's
// LimitRange min/max envelope.
type LimitRangeCheck struct{}

func (LimitRangeCheck) Name() string { return "limitrange" }

func (LimitRangeCheck) Check(_ context.Context, _ tenancy.Context, c Candidate) (*Verdict, error) {
	if c.Signals.LimitRange == nil {
		return nil, nil
	}
	lr := c.Signals.LimitRange
	if c.ProposedCPU.Millicores > 0 {
		if lr.MaxCPUMillicores > 0 && c.ProposedCPU.Millicores > lr.MaxCPUMillicores {
			return &Verdict{Severity: SeverityHard, Reason: "proposed CPU above LimitRange max"}, nil
		}
		if lr.MinCPUMillicores > 0 && c.ProposedCPU.Millicores < lr.MinCPUMillicores {
			return &Verdict{Severity: SeverityHard, Reason: "proposed CPU below LimitRange min"}, nil
		}
	}
	if c.ProposedMemory.Bytes > 0 {
		if lr.MaxMemoryBytes > 0 && c.ProposedMemory.Bytes > lr.MaxMemoryBytes {
			return &Verdict{Severity: SeverityHard, Reason: "proposed memory above LimitRange max"}, nil
		}
		if lr.MinMemoryBytes > 0 && c.ProposedMemory.Bytes < lr.MinMemoryBytes {
			return &Verdict{Severity: SeverityHard, Reason: "proposed memory below LimitRange min"}, nil
		}
	}
	return nil, nil
}

// HPABoundsCheck rejects a static replica change when the workload has
// an HPA; the HPA would just resize it back. Returns a warn-level
// verdict instead of a hard reject — the renderer keeps the finding
// but tags it as superseded.
type HPABoundsCheck struct{}

func (HPABoundsCheck) Name() string { return "hpabounds" }

func (HPABoundsCheck) Check(_ context.Context, _ tenancy.Context, c Candidate) (*Verdict, error) {
	if c.Signals.HPA == nil || c.ProposedReplicas == 0 {
		return nil, nil
	}
	hpa := c.Signals.HPA
	if c.ProposedReplicas < hpa.MinReplicas {
		return &Verdict{Severity: SeverityHard, Reason: "proposed replicas below HPA minReplicas"}, nil
	}
	if c.ProposedReplicas > hpa.MaxReplicas {
		return &Verdict{Severity: SeverityHard, Reason: "proposed replicas above HPA maxReplicas"}, nil
	}
	// In-bounds change: warn that the HPA will dominate.
	return &Verdict{Severity: SeverityWarn, Reason: "workload has HPA — replica change will be overridden"}, nil
}

// DependencyCheck rejects a fix on a workload that other workloads
// depend on (Service selectors, owner references). Reduces blast
// radius for cross-team changes.
type DependencyCheck struct {
	MaxDependents int
}

func (DependencyCheck) Name() string { return "dependency" }

func (d DependencyCheck) Check(_ context.Context, _ tenancy.Context, c Candidate) (*Verdict, error) {
	maxDep := d.MaxDependents
	if maxDep == 0 {
		maxDep = 3
	}
	if len(c.Signals.Dependents) > maxDep {
		return &Verdict{
			Severity: SeverityHard,
			Reason:   "too many dependents for an automatic fix",
		}, nil
	}
	return nil, nil
}

// OOMRecentCheck rejects memory cuts on workloads that OOMKilled in
// the recent window. A cut would just turn one OOM into many.
type OOMRecentCheck struct{}

func (OOMRecentCheck) Name() string { return "oom-recent" }

func (OOMRecentCheck) Check(_ context.Context, _ tenancy.Context, c Candidate) (*Verdict, error) {
	if !c.Signals.OOMRecent {
		return nil, nil
	}
	if c.ProposedMemory.Bytes <= 0 {
		return nil, nil
	}
	return &Verdict{
		Severity: SeverityHard,
		Reason:   "recent OOMKill — memory cut rejected",
	}, nil
}

// Default returns the Phase-4 validator set in pipeline order.
func Default() []Validator {
	return []Validator{
		PDBCheck{},
		ResourceQuotaCheck{},
		LimitRangeCheck{},
		HPABoundsCheck{},
		DependencyCheck{},
		OOMRecentCheck{},
	}
}
