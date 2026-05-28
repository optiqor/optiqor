package cluster

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/optiqor/optiqor/internal/operators"
)

func TestOwnerResolver_Resolve_FollowsControllerChain(t *testing.T) {
	// ReplicaSet "api-7c4f8b6d4" controlled by Deployment "api".
	rs := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "ReplicaSet",
			"metadata": map[string]any{
				"name":      "api-7c4f8b6d4",
				"namespace": "prod",
				"ownerReferences": []any{
					map[string]any{
						"apiVersion": "apps/v1",
						"kind":       "Deployment",
						"name":       "api",
						"controller": true,
					},
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	gvrToList := map[schema.GroupVersionResource]string{
		{Group: "apps", Version: "v1", Resource: "replicasets"}: "ReplicaSetList",
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToList, rs)

	r := NewOwnerResolver(client)
	owner, ok := r.Resolve(context.Background(), "prod", operators.OwnerRef{
		APIVersion: "apps/v1",
		Kind:       "ReplicaSet",
		Name:       "api-7c4f8b6d4",
		Controller: true,
	})
	if !ok {
		t.Fatal("Resolve: ok=false; want controller owner")
	}
	if owner.Kind != "Deployment" || owner.Name != "api" {
		t.Errorf("owner = %+v, want Deployment/api", owner)
	}
}

func TestOwnerResolver_Resolve_NoController_ReturnsFalse(t *testing.T) {
	dep := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      "api",
				"namespace": "prod",
			},
		},
	}

	scheme := runtime.NewScheme()
	gvrToList := map[schema.GroupVersionResource]string{
		{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToList, dep)

	r := NewOwnerResolver(client)
	_, ok := r.Resolve(context.Background(), "prod", operators.OwnerRef{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "api",
	})
	if ok {
		t.Error("top-of-chain must return ok=false")
	}
}

func TestPluralise_HandlesCommonSuffixes(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want string
	}{
		{"Deployment", "deployments"},
		{"ReplicaSet", "replicasets"},
		{"Kafka", "kafkas"},
		{"NetworkPolicy", "networkpolicies"},
		{"Ingress", "ingresses"},
		{"PatchSet", "patchsets"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			if got := pluralise(tc.kind); got != tc.want {
				t.Errorf("pluralise(%q) = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}

// metav1 must stay imported even though it isn't directly referenced —
// dynamicfake needs the registered GroupVersionKind under the hood.
var _ = metav1.ObjectMeta{}
