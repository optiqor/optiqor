package cluster

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestKarpenterReader_List_ParsesNodePoolSpec(t *testing.T) {
	np := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "karpenter.sh/v1",
			"kind":       "NodePool",
			"metadata":   map[string]any{"name": "default"},
			"spec": map[string]any{
				"template": map[string]any{
					"spec": map[string]any{
						"requirements": []any{
							map[string]any{
								"key":      "karpenter.k8s.aws/instance-family",
								"operator": "In",
								"values":   []any{"m5", "r5", "c5"},
							},
						},
					},
				},
				"disruption": map[string]any{
					"consolidationPolicy": "WhenUnderutilized",
				},
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{"type": "Ready", "status": "True"},
				},
				"resources": map[string]any{
					"nodes": int64(7),
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	gvrToList := map[schema.GroupVersionResource]string{
		karpenterNodePoolGVR: "NodePoolList",
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToList, np)

	r := &KarpenterR{clusterID: "c1", client: client}
	got, err := r.List(context.Background(), tenancy.Context{TenantID: "t1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 nodepool, got %d", len(got))
	}
	pool := got[0]
	if pool.Name != "default" {
		t.Errorf("name = %q", pool.Name)
	}
	if pool.ConsolidationMode != "WhenUnderutilized" {
		t.Errorf("consolidation = %q", pool.ConsolidationMode)
	}
	families := pool.Requirements["karpenter.k8s.aws/instance-family"]
	if len(families) != 3 || families[0] != "m5" {
		t.Errorf("families = %+v", families)
	}
	if pool.NodeCountCurrent != 7 {
		t.Errorf("current = %d", pool.NodeCountCurrent)
	}
	if pool.Disrupting {
		t.Errorf("Disrupting=true without Disrupting condition")
	}
}

func TestKarpenterReader_List_DisruptingConditionFlipsFlag(t *testing.T) {
	np := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "karpenter.sh/v1",
			"kind":       "NodePool",
			"metadata":   map[string]any{"name": "spot"},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{"type": "Disrupting", "status": "True"},
				},
			},
		},
	}
	scheme := runtime.NewScheme()
	gvrToList := map[schema.GroupVersionResource]string{
		karpenterNodePoolGVR: "NodePoolList",
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToList, np)

	r := &KarpenterR{clusterID: "c1", client: client}
	got, _ := r.List(context.Background(), tenancy.Context{})
	if len(got) != 1 || !got[0].Disrupting {
		t.Errorf("want Disrupting=true, got %+v", got)
	}
}

func TestKarpenterReader_NilClient_ReturnsNilNoError(t *testing.T) {
	var r *KarpenterR
	got, err := r.List(context.Background(), tenancy.Context{})
	if err != nil || got != nil {
		t.Errorf("nil reader: got = %v, err = %v", got, err)
	}
}
