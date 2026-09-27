package cluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"

	agentk8s "github.com/optiqor/optiqor/internal/agent/k8s"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// VerticalPodAutoscaler is the v1 GVR shipped by github.com/kubernetes/autoscaler.
// We read it via dynamic client + unstructured to avoid pinning the
// vpa.autoscaling.k8s.io API package — VPA is optional, the agent
// must still build cleanly when the dep is absent in customer
// clusters that don't run VPA.
var vpaGVR = schema.GroupVersionResource{
	Group:    "autoscaling.k8s.io",
	Version:  "v1",
	Resource: "verticalpodautoscalers",
}

// VPAR resolves the VPA pointing at a workload via spec.targetRef.
// Nil reader (CRD absent) is OK — Readers.VPA stays nil and validator
// treats the signal as "no VPA recommendation available".
type VPAR struct {
	clusterID string
	client    dynamic.Interface
}

// NewVPAReader returns nil + nil when the CRD is absent. The agent
// must surface that case as "skip", not "error" — a customer who's
// never installed VPA must still onboard.
func NewVPAReader(client dynamic.Interface, disc discovery.DiscoveryInterface, clusterID string) (*VPAR, error) {
	if client == nil || disc == nil {
		return nil, fmt.Errorf("cluster/vpa: nil client or discovery")
	}
	if !crdPresent(disc, vpaGVR.GroupVersion().String(), "VerticalPodAutoscaler") {
		return nil, nil
	}
	return &VPAR{clusterID: clusterID, client: client}, nil
}

var _ agentk8s.VPAReader = (*VPAR)(nil)

// Get returns nil + nil when no VPA targets w. Errors are reserved
// for apiserver transport failures (network, auth).
func (r *VPAR) Get(ctx context.Context, _ tenancy.Context, w agentk8s.WorkloadRef) (*agentk8s.VPARecommendation, error) {
	if r == nil || r.client == nil {
		return nil, nil
	}
	list, err := r.client.Resource(vpaGVR).Namespace(w.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cluster/vpa: list: %w", err)
	}
	for i := range list.Items {
		item := &list.Items[i]
		if !vpaTargets(item, w) {
			continue
		}
		return parseVPA(item), nil
	}
	return nil, nil
}

func vpaTargets(u *unstructured.Unstructured, w agentk8s.WorkloadRef) bool {
	target, found, err := unstructured.NestedMap(u.Object, "spec", "targetRef")
	if err != nil || !found {
		return false
	}
	kind, _ := target["kind"].(string)
	name, _ := target["name"].(string)
	return kind == w.Kind && name == w.Name
}

// parseVPA reads spec.updatePolicy.updateMode + status.recommendation
// containerRecommendations[0]. We surface only the first container —
// the validator pipeline operates per-workload, not per-container.
func parseVPA(u *unstructured.Unstructured) *agentk8s.VPARecommendation {
	mode := "Off"
	if v, _, _ := unstructured.NestedString(u.Object, "spec", "updatePolicy", "updateMode"); v != "" {
		mode = v
	}

	recs, _, _ := unstructured.NestedSlice(u.Object, "status", "recommendation", "containerRecommendations")
	if len(recs) == 0 {
		return &agentk8s.VPARecommendation{Mode: mode}
	}
	first, ok := recs[0].(map[string]any)
	if !ok {
		return &agentk8s.VPARecommendation{Mode: mode}
	}
	out := agentk8s.VPARecommendation{Mode: mode}
	out.RecommendCPUm = readResourceMilli(first, "target", "cpu")
	out.RecommendMemB = readResourceBytes(first, "target", "memory")
	out.UpperBoundCPUm = readResourceMilli(first, "upperBound", "cpu")
	out.UpperBoundMemB = readResourceBytes(first, "upperBound", "memory")
	out.LowerBoundCPUm = readResourceMilli(first, "lowerBound", "cpu")
	out.LowerBoundMemB = readResourceBytes(first, "lowerBound", "memory")
	return &out
}

func readResourceMilli(m map[string]any, path ...string) int64 {
	s := nestedString(m, path...)
	if s == "" {
		return 0
	}
	if strings.HasSuffix(s, "m") {
		v, err := strconv.ParseInt(strings.TrimSuffix(s, "m"), 10, 64)
		if err != nil {
			return 0
		}
		return v
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		// Try as float CPU ("0.5") → 500m.
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return 0
		}
		return int64(f * 1000)
	}
	return v * 1000
}

func readResourceBytes(m map[string]any, path ...string) int64 {
	s := nestedString(m, path...)
	if s == "" {
		return 0
	}
	// Trim K8s memory suffixes (Ki, Mi, Gi). We only need rough magnitude
	// for budget gating; precise conversion lives in the validator.
	multiplier := int64(1)
	for suffix, mult := range memSuffixes {
		if strings.HasSuffix(s, suffix) {
			s = strings.TrimSuffix(s, suffix)
			multiplier = mult
			break
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v * multiplier
}

var memSuffixes = map[string]int64{
	"Ki": 1024,
	"Mi": 1024 * 1024,
	"Gi": 1024 * 1024 * 1024,
	"Ti": 1024 * 1024 * 1024 * 1024,
	"K":  1000,
	"M":  1000 * 1000,
	"G":  1000 * 1000 * 1000,
	"T":  1000 * 1000 * 1000 * 1000,
}

func nestedString(m map[string]any, path ...string) string {
	cur := m
	for i, p := range path {
		v, ok := cur[p]
		if !ok {
			return ""
		}
		if i == len(path)-1 {
			s, _ := v.(string)
			return s
		}
		cur, ok = v.(map[string]any)
		if !ok {
			return ""
		}
	}
	return ""
}

// crdPresent returns true when the apiserver advertises the given
// kind under the given groupVersion. Negative answer is cached
// implicitly by client-go's discovery cache — the call is cheap on
// repeat.
func crdPresent(disc discovery.DiscoveryInterface, groupVersion, kind string) bool {
	if disc == nil {
		return false
	}
	resources, err := disc.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return false
	}
	for _, r := range resources.APIResources {
		if r.Kind == kind {
			return true
		}
	}
	return false
}
