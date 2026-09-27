// Package k8s carries the read-only cluster-state interfaces the
// validator pipeline + agent-mode detectors consume. Production
// implementations live behind a build tag and call client-go;
// in-tree fakes satisfy the same contract so backend tests run
// without a real apiserver.
//
// ADR-0008 mandates the agent stays read-only — every reader here
// covers a `get`/`list`/`watch` verb.
package k8s

import (
	"context"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// WorkloadRef identifies a target across every reader. Stable shape
// so adding readers doesn't widen the contract for existing ones.
type WorkloadRef struct {
	ClusterID string
	Namespace string
	Kind      string
	Name      string
}

// Event mirrors core/v1 Event narrowed to the fields the validator
// pipeline actually reads.
type Event struct {
	Workload  WorkloadRef
	Reason    string // e.g. "OOMKilled", "FailedScheduling"
	Type      string // "Normal" | "Warning"
	Count     int
	FirstSeen time.Time
	LastSeen  time.Time
	Message   string
}

// EventsReader streams cluster Events for a window. Returns nil slice
// when nothing matched — the caller never has to nil-check.
type EventsReader interface {
	Recent(ctx context.Context, t tenancy.Context, w WorkloadRef, since time.Time) ([]Event, error)
}

// HPAState is the spec+status narrow read.
type HPAState struct {
	MinReplicas       int
	MaxReplicas       int
	CurrentReplicas   int
	TargetCPUUtil     int  // percent; 0 means no CPU target
	TargetMemoryUtil  int  // percent; 0 means no memory target
	ConditionsHealthy bool // false when ScalingActive=False or AbleToScale=False
}

type HPAReader interface {
	Get(ctx context.Context, t tenancy.Context, w WorkloadRef) (*HPAState, error)
}

// PDB / ResourceQuota / LimitRange — the three policy primitives the
// validator pipeline already models in validator.ClusterSignals.
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

// PolicyReader returns the three together because the validator
// pipeline reads them as one snapshot. Missing entries are nil.
type PolicyReader interface {
	Snapshot(ctx context.Context, t tenancy.Context, w WorkloadRef) (PolicySnapshot, error)
}

type PolicySnapshot struct {
	PDB        *PDB
	Quota      *ResourceQuota
	LimitRange *LimitRange
}

// VPARecommendation feeds the "VPA as one signal" co-existence path
// (ADR-0012). Mode is one of "Off", "Initial", "Auto".
type VPARecommendation struct {
	Mode           string
	RecommendCPUm  int64
	RecommendMemB  int64
	UpperBoundCPUm int64
	UpperBoundMemB int64
	LowerBoundCPUm int64
	LowerBoundMemB int64
}

type VPAReader interface {
	Get(ctx context.Context, t tenancy.Context, w WorkloadRef) (*VPARecommendation, error)
}

// KarpenterNodePool is the consolidation-aware node-lifecycle reader.
// Empty Limits means the NodePool has no upper bound.
type KarpenterNodePool struct {
	Name              string
	Disrupting        bool // a consolidation pass is currently running
	NodeCountCurrent  int
	NodeCountLimit    int                 // 0 = unbounded
	Requirements      map[string][]string // labelKey -> allowed values
	ConsolidationMode string              // "WhenEmpty" | "WhenUnderutilized" | ""
}

type KarpenterReader interface {
	List(ctx context.Context, t tenancy.Context) ([]KarpenterNodePool, error)
}
