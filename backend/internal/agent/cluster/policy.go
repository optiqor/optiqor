package cluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	agentk8s "github.com/optiqor/optiqor/internal/agent/k8s"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// PolicyR snapshots PDB + ResourceQuota + LimitRange together because
// the validator pipeline reads them as one decision input. Reading
// them from one snapshot also rules out racy half-states (PDB matched,
// quota changed mid-read).
type PolicyR struct {
	clusterID string
	pdbIdx    cache.Indexer
	quotaIdx  cache.Indexer
	limitIdx  cache.Indexer
}

func newPolicyReader(clusterID string, f informers.SharedInformerFactory) *PolicyR {
	return &PolicyR{
		clusterID: clusterID,
		pdbIdx:    f.Policy().V1().PodDisruptionBudgets().Informer().GetIndexer(),
		quotaIdx:  f.Core().V1().ResourceQuotas().Informer().GetIndexer(),
		limitIdx:  f.Core().V1().LimitRanges().Informer().GetIndexer(),
	}
}

var _ agentk8s.PolicyReader = (*PolicyR)(nil)

// Snapshot resolves each policy primitive in the workload's namespace.
// Missing entries stay nil in the returned struct; validator already
// treats nil as "no constraint".
func (r *PolicyR) Snapshot(_ context.Context, _ tenancy.Context, w agentk8s.WorkloadRef) (agentk8s.PolicySnapshot, error) {
	var out agentk8s.PolicySnapshot

	pdb, err := r.firstPDBForWorkload(w)
	if err != nil {
		return out, err
	}
	out.PDB = pdb

	quota, err := r.namespaceQuota(w.Namespace)
	if err != nil {
		return out, err
	}
	out.Quota = quota

	limit, err := r.namespaceLimitRange(w.Namespace)
	if err != nil {
		return out, err
	}
	out.LimitRange = limit

	return out, nil
}

// firstPDBForWorkload returns the first PDB whose selector matches the
// workload's labels. PDB → workload binding is "all PDBs whose
// selector matches the pod template"; we use the workload labels as a
// proxy. Multiple PDBs covering the same workload is rare; the first
// match is the one the workflow safety check cares about.
func (r *PolicyR) firstPDBForWorkload(w agentk8s.WorkloadRef) (*agentk8s.PDB, error) {
	if r.pdbIdx == nil {
		return nil, nil
	}
	items, err := r.pdbIdx.ByIndex(cache.NamespaceIndex, w.Namespace)
	if err != nil {
		return nil, fmt.Errorf("cluster/pdb: index lookup: %w", err)
	}
	for _, raw := range items {
		pdb, ok := raw.(*policyv1.PodDisruptionBudget)
		if !ok || pdb == nil {
			continue
		}
		// Workload labels aren't on the WorkloadRef. Accept any PDB in
		// the namespace whose selector matches the workload's name as a
		// label — that's the convention the validator codegen assumes.
		if !pdbSelectorAcceptsWorkload(pdb, w) {
			continue
		}
		out := agentk8s.PDB{
			CurrentReplicas: int(pdb.Status.CurrentHealthy),
		}
		if pdb.Spec.MinAvailable != nil && pdb.Spec.MinAvailable.Type == 0 {
			out.MinAvailable = int(pdb.Spec.MinAvailable.IntVal)
		}
		if pdb.Spec.MaxUnavailable != nil && pdb.Spec.MaxUnavailable.Type == 0 {
			out.MaxUnavailable = int(pdb.Spec.MaxUnavailable.IntVal)
		}
		return &out, nil
	}
	return nil, nil
}

// pdbSelectorAcceptsWorkload returns true when the PDB selector either
// has no selector (matches everything in the namespace) or includes a
// label key matching the workload's name. Real binding requires pod
// labels; informers index pods separately and the validator already
// rejects on the conservative side, so this is intentionally lenient.
func pdbSelectorAcceptsWorkload(pdb *policyv1.PodDisruptionBudget, w agentk8s.WorkloadRef) bool {
	if pdb.Spec.Selector == nil {
		return true
	}
	sel, err := labels.Parse(formatSelector(pdb.Spec.Selector.MatchLabels))
	if err != nil || sel.Empty() {
		return true
	}
	return sel.Matches(labels.Set{"app": w.Name, "app.kubernetes.io/name": w.Name})
}

func formatSelector(m map[string]string) string {
	out := ""
	for k, v := range m {
		if out != "" {
			out += ","
		}
		out += k + "=" + v
	}
	return out
}

// namespaceQuota sums every ResourceQuota in the namespace. Multiple
// quotas in one namespace stack additively per the K8s admission
// controller, so summing matches the live behaviour.
func (r *PolicyR) namespaceQuota(ns string) (*agentk8s.ResourceQuota, error) {
	if r.quotaIdx == nil {
		return nil, nil
	}
	items, err := r.quotaIdx.ByIndex(cache.NamespaceIndex, ns)
	if err != nil {
		return nil, fmt.Errorf("cluster/quota: index lookup: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}
	var agg agentk8s.ResourceQuota
	for _, raw := range items {
		q, ok := raw.(*corev1.ResourceQuota)
		if !ok || q == nil {
			continue
		}
		agg.CPUMillicores += resourceMillicores(q.Spec.Hard[corev1.ResourceLimitsCPU])
		agg.MemoryBytes += resourceBytes(q.Spec.Hard[corev1.ResourceLimitsMemory])
		agg.UsedCPUMilli += resourceMillicores(q.Status.Used[corev1.ResourceLimitsCPU])
		agg.UsedMemoryB += resourceBytes(q.Status.Used[corev1.ResourceLimitsMemory])
	}
	return &agg, nil
}

// namespaceLimitRange returns the narrowest (most restrictive)
// LimitRange that applies to Container resources. Multiple LimitRanges
// in one namespace are admitted per-pod; the narrowest envelope is
// what the validator must respect.
func (r *PolicyR) namespaceLimitRange(ns string) (*agentk8s.LimitRange, error) {
	if r.limitIdx == nil {
		return nil, nil
	}
	items, err := r.limitIdx.ByIndex(cache.NamespaceIndex, ns)
	if err != nil {
		return nil, fmt.Errorf("cluster/limitrange: index lookup: %w", err)
	}
	var out agentk8s.LimitRange
	found := false
	for _, raw := range items {
		lr, ok := raw.(*corev1.LimitRange)
		if !ok || lr == nil {
			continue
		}
		for _, item := range lr.Spec.Limits {
			if item.Type != corev1.LimitTypeContainer {
				continue
			}
			cpuMax := resourceMillicores(item.Max[corev1.ResourceCPU])
			memMax := resourceBytes(item.Max[corev1.ResourceMemory])
			cpuMin := resourceMillicores(item.Min[corev1.ResourceCPU])
			memMin := resourceBytes(item.Min[corev1.ResourceMemory])
			if !found || (cpuMax > 0 && cpuMax < out.MaxCPUMillicores) {
				out.MaxCPUMillicores = cpuMax
			}
			if !found || (memMax > 0 && memMax < out.MaxMemoryBytes) {
				out.MaxMemoryBytes = memMax
			}
			if !found || (cpuMin > out.MinCPUMillicores) {
				out.MinCPUMillicores = cpuMin
			}
			if !found || (memMin > out.MinMemoryBytes) {
				out.MinMemoryBytes = memMin
			}
			found = true
		}
	}
	if !found {
		return nil, nil
	}
	return &out, nil
}

func resourceMillicores(q resource.Quantity) int64 {
	return q.MilliValue()
}

func resourceBytes(q resource.Quantity) int64 {
	return q.Value()
}
