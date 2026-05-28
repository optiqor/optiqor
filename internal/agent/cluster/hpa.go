package cluster

import (
	"context"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	agentk8s "github.com/optiqor/optiqor/internal/agent/k8s"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// HPAR resolves the HorizontalPodAutoscaler bound to a workload. We
// search by scaleTargetRef.{kind,name,namespace} rather than label
// selector — the spec says scaleTargetRef is authoritative and label
// selectors can drift after rename.
type HPAR struct {
	clusterID string
	indexer   cache.Indexer
}

func newHPAReader(clusterID string, f informers.SharedInformerFactory) *HPAR {
	inf := f.Autoscaling().V2().HorizontalPodAutoscalers().Informer()
	return &HPAR{clusterID: clusterID, indexer: inf.GetIndexer()}
}

var _ agentk8s.HPAReader = (*HPAR)(nil)

// Get returns nil + nil when no HPA targets w — workload simply isn't
// HPA-managed. Returns the snapshot when found; error reserved for
// indexer transport failures.
func (r *HPAR) Get(_ context.Context, _ tenancy.Context, w agentk8s.WorkloadRef) (*agentk8s.HPAState, error) {
	if r.indexer == nil {
		return nil, nil
	}
	items, err := r.indexer.ByIndex(cache.NamespaceIndex, w.Namespace)
	if err != nil {
		return nil, fmt.Errorf("cluster/hpa: index lookup: %w", err)
	}
	for _, raw := range items {
		hpa, ok := raw.(*autoscalingv2.HorizontalPodAutoscaler)
		if !ok || hpa == nil {
			continue
		}
		target := hpa.Spec.ScaleTargetRef
		if target.Kind != w.Kind || target.Name != w.Name {
			continue
		}
		return &agentk8s.HPAState{
			MinReplicas:       intOrZero(hpa.Spec.MinReplicas),
			MaxReplicas:       int(hpa.Spec.MaxReplicas),
			CurrentReplicas:   int(hpa.Status.CurrentReplicas),
			TargetCPUUtil:     targetUtil(hpa.Spec.Metrics, autoscalingv2.ResourceMetricSourceType, "cpu"),
			TargetMemoryUtil:  targetUtil(hpa.Spec.Metrics, autoscalingv2.ResourceMetricSourceType, "memory"),
			ConditionsHealthy: conditionsHealthy(hpa.Status.Conditions),
		}, nil
	}
	return nil, nil
}

func intOrZero(p *int32) int {
	if p == nil {
		return 0
	}
	return int(*p)
}

// targetUtil returns the AverageUtilization percent for the named
// resource metric. Zero when the target is absent or unset — callers
// treat zero as "no target", per HPAState's documented contract.
func targetUtil(metrics []autoscalingv2.MetricSpec, kind autoscalingv2.MetricSourceType, name string) int {
	for _, m := range metrics {
		if m.Type != kind || m.Resource == nil {
			continue
		}
		if string(m.Resource.Name) != name {
			continue
		}
		if m.Resource.Target.AverageUtilization == nil {
			continue
		}
		return int(*m.Resource.Target.AverageUtilization)
	}
	return 0
}

// conditionsHealthy is true unless ScalingActive=False or
// AbleToScale=False is present in the latest conditions slice.
func conditionsHealthy(conds []autoscalingv2.HorizontalPodAutoscalerCondition) bool {
	for _, c := range conds {
		switch c.Type {
		case autoscalingv2.AbleToScale, autoscalingv2.ScalingActive:
			if c.Status != "True" {
				return false
			}
		}
	}
	return true
}
