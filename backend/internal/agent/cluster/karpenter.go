package cluster

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"

	agentk8s "github.com/optiqor/optiqor/internal/agent/k8s"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// karpenterNodePoolGVR is the v1 GVR Karpenter ships post-Beta1 (Karpenter 1.0+, late 2024).
// Pre-1.0 used karpenter.sh/v1beta1 — out of scope for Phase 5; customers running v1beta1
// see the agent skip the reader (logged once at boot).
var karpenterNodePoolGVR = schema.GroupVersionResource{
	Group:    "karpenter.sh",
	Version:  "v1",
	Resource: "nodepools",
}

// KarpenterR lists NodePool objects across the cluster. Karpenter is
// cluster-scoped (no namespace) so we list against the dynamic client
// without a namespace filter.
type KarpenterR struct {
	clusterID string
	client    dynamic.Interface
}

// NewKarpenterReader returns nil + nil when the Karpenter CRD is
// absent. The validator pipeline already treats KarpenterReader nil as
// "no NodePool context"; recommendations still emit, they just don't
// quote the NodePool footer.
func NewKarpenterReader(client dynamic.Interface, disc discovery.DiscoveryInterface, clusterID string) (*KarpenterR, error) {
	if client == nil || disc == nil {
		return nil, fmt.Errorf("cluster/karpenter: nil client or discovery")
	}
	if !crdPresent(disc, karpenterNodePoolGVR.GroupVersion().String(), "NodePool") {
		return nil, nil
	}
	return &KarpenterR{clusterID: clusterID, client: client}, nil
}

var _ agentk8s.KarpenterReader = (*KarpenterR)(nil)

// List returns every NodePool the cluster exposes. Disrupting reads
// the Disrupting=True condition; absent condition = false.
func (r *KarpenterR) List(ctx context.Context, _ tenancy.Context) ([]agentk8s.KarpenterNodePool, error) {
	if r == nil || r.client == nil {
		return nil, nil
	}
	list, err := r.client.Resource(karpenterNodePoolGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cluster/karpenter: list: %w", err)
	}
	out := make([]agentk8s.KarpenterNodePool, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, parseNodePool(&list.Items[i]))
	}
	return out, nil
}

func parseNodePool(u *unstructured.Unstructured) agentk8s.KarpenterNodePool {
	np := agentk8s.KarpenterNodePool{
		Name:         u.GetName(),
		Requirements: map[string][]string{},
	}

	reqs, _, _ := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "requirements")
	for _, raw := range reqs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key, _ := m["key"].(string)
		values, _ := m["values"].([]any)
		if key == "" || len(values) == 0 {
			continue
		}
		out := make([]string, 0, len(values))
		for _, v := range values {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		np.Requirements[key] = out
	}

	if v, _, _ := unstructured.NestedString(u.Object, "spec", "disruption", "consolidationPolicy"); v != "" {
		np.ConsolidationMode = v
	}

	if limits, found, _ := unstructured.NestedMap(u.Object, "spec", "limits"); found {
		if cpu, ok := limits["cpu"].(string); ok && cpu != "" {
			// Cap reading: NodePool limits are budget caps, not node counts.
			// NodeCountLimit stays 0 (unbounded) when limits is CPU/mem only.
			_ = cpu
		}
	}

	conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	for _, raw := range conds {
		c, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := c["type"].(string)
		status, _ := c["status"].(string)
		if typ == "Disrupting" && status == "True" {
			np.Disrupting = true
		}
	}

	if n, _, _ := unstructured.NestedInt64(u.Object, "status", "resources", "nodes"); n > 0 {
		np.NodeCountCurrent = int(n)
	}

	return np
}

// IsClusterAutoscalerPresent is the T2 detector — looks for a
// Deployment named "cluster-autoscaler" in kube-system. Caller passes
// the policy reader's namespace+kind index; we keep this function in
// the karpenter file because both gate the provisioner-class decision.
func IsClusterAutoscalerPresent(ctx context.Context, client dynamic.Interface) (bool, error) {
	if client == nil {
		return false, nil
	}
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	list, err := client.Resource(gvr).Namespace("kube-system").List(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=cluster-autoscaler",
	})
	if err != nil {
		return false, fmt.Errorf("cluster/autoscaler probe: %w", err)
	}
	return len(list.Items) > 0, nil
}
