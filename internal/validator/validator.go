// Package validator runs the "Validation Before Recommendation"
// pipeline between candidate generation (internal/cost) and PR
// rendering (internal/prwriter). Each Validator inspects a Candidate
// against cluster constraints (PDBs, ResourceQuotas, LimitRanges,
// HPA bounds, dependency graph, recent OOMKills) and returns a Verdict.
// A rejected candidate never reaches the PR comment; the reason gets
// logged for detector-library tuning.
package validator

import (
	"context"
	"errors"
	"fmt"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Candidate is the cost-engine output the pipeline gates on. Narrower
// than rules.Finding so adding cluster signals doesn't widen the
// detector contract.
type Candidate struct {
	WorkloadID       string
	DetectorID       string
	Title            string
	ProposedCPU      Quantity
	ProposedMemory   Quantity
	ProposedReplicas int
	MonthlyUSDCents  int64
	// Cluster signals attached by the upstream observer; nil-safe.
	Signals ClusterSignals
}

// Quantity is the millicores / bytes view; zero means "unchanged".
type Quantity struct {
	Millicores int64
	Bytes      int64
}

// ClusterSignals is the agent-supplied constraint snapshot. Each
// pointer is optional — a nil PDB means the workload has none.
type ClusterSignals struct {
	PDB        *PDB
	Quota      *ResourceQuota
	LimitRange *LimitRange
	HPA        *HPA
	OOMRecent  bool
	Dependents []string
}

type PDB struct {
	MinAvailable    int
	MaxUnavailable  int
	CurrentReplicas int
}

type ResourceQuota struct {
	CPUMillicores int64
	MemoryBytes   int64
	UsedCPUMilli  int64
	UsedMemoryB   int64
}

type LimitRange struct {
	MaxCPUMillicores int64
	MaxMemoryBytes   int64
	MinCPUMillicores int64
	MinMemoryBytes   int64
}

type HPA struct {
	MinReplicas int
	MaxReplicas int
	CurrentReps int
}

type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarn
	SeverityHard
)

func (s Severity) String() string {
	switch s {
	case SeverityHard:
		return "hard"
	case SeverityWarn:
		return "warn"
	default:
		return "info"
	}
}

type Verdict struct {
	Validator string
	Severity  Severity
	Reason    string
}

// Validator inspects a Candidate against the cluster signals. Returns
// (nil, nil) when the candidate passes; a hard Verdict short-circuits
// the pipeline; warns accumulate.
type Validator interface {
	Name() string
	Check(ctx context.Context, t tenancy.Context, c Candidate) (*Verdict, error)
}

// Pipeline runs validators in registration order. Reject-on-hard:
// the first SeverityHard verdict stops the pipeline.
type Pipeline struct {
	validators []Validator
}

func NewPipeline(vs ...Validator) *Pipeline {
	if len(vs) == 0 {
		panic("validator: NewPipeline: zero validators")
	}
	return &Pipeline{validators: vs}
}

// Run returns the union of every validator's non-nil verdict and the
// first hard rejection (if any). A nil rejection means the candidate
// is dispatchable.
func (p *Pipeline) Run(ctx context.Context, t tenancy.Context, c Candidate) (Result, error) {
	out := Result{}
	for _, v := range p.validators {
		verdict, err := v.Check(ctx, t, c)
		if err != nil {
			return out, fmt.Errorf("validator %s: %w", v.Name(), err)
		}
		if verdict == nil {
			continue
		}
		verdict.Validator = v.Name()
		out.Verdicts = append(out.Verdicts, *verdict)
		if verdict.Severity == SeverityHard && out.Rejected == nil {
			cp := *verdict
			out.Rejected = &cp
		}
	}
	return out, nil
}

type Result struct {
	Verdicts []Verdict
	Rejected *Verdict // first hard rejection, nil when the candidate is dispatchable
}

var ErrUnimplemented = errors.New("validator: not implemented in this phase")
