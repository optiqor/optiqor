package cluster

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/optiqor/optiqor/internal/operators"
)

// OwnerResolver walks the controller chain for a workload via the
// dynamic client. operators.Classify is pure-Go and takes a resolver
// callback; this adapter bridges client-go to that callback.
//
// One round-trip per hop. Owner chains are short (Pod → ReplicaSet →
// Deployment is depth 2; Pod → StatefulSet → CRD is depth 2). The
// 16-hop guard in operators.Classify makes a runaway chain impossible.
type OwnerResolver struct {
	client dynamic.Interface
}

func NewOwnerResolver(client dynamic.Interface) *OwnerResolver {
	return &OwnerResolver{client: client}
}

// Resolve returns the controller-true owner of ref, or ok=false when
// ref has no controlling owner (i.e. it's the top of the chain).
// Errors fall back to ok=false so a stale informer doesn't abort the
// workflow — the operator classification just becomes Conservative.
//
// namespace scopes the GET. K8s ownerReferences cannot cross
// namespaces, so the workload's namespace propagates down every hop.
func (r *OwnerResolver) Resolve(ctx context.Context, namespace string, ref operators.OwnerRef) (operators.OwnerRef, bool) {
	if r == nil || r.client == nil {
		return operators.OwnerRef{}, false
	}
	gvr, err := guessGVR(ref.APIVersion, ref.Kind)
	if err != nil {
		return operators.OwnerRef{}, false
	}

	u, err := r.client.Resource(gvr).Namespace(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil || u == nil {
		return operators.OwnerRef{}, false
	}
	for _, o := range u.GetOwnerReferences() {
		if o.Controller != nil && *o.Controller {
			return operators.OwnerRef{
				APIVersion: o.APIVersion,
				Kind:       o.Kind,
				Name:       o.Name,
				Controller: true,
			}, true
		}
	}
	return operators.OwnerRef{}, false
}

// AsCallback returns a closure that operators.Classify accepts. ctx
// is captured for cancellation propagation; namespace pins the GET
// scope (owner chain never crosses namespaces).
func (r *OwnerResolver) AsCallback(ctx context.Context, namespace string) func(operators.OwnerRef) (operators.OwnerRef, bool) {
	return func(ref operators.OwnerRef) (operators.OwnerRef, bool) {
		return r.Resolve(ctx, namespace, ref)
	}
}

// guessGVR maps an OwnerReference (which only carries apiVersion +
// kind) to a GVR. For built-in kinds we hardcode the resource name;
// for CRDs we lowercase + pluralise as a best-effort fallback. A
// missing GVR errors out so the resolver short-circuits to ok=false.
func guessGVR(apiVersion, kind string) (schema.GroupVersionResource, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("guessGVR: parse: %w", err)
	}
	resource, ok := builtinResources[kind]
	if !ok {
		// CRD fallback: lowercase + simple plural. Works for the common
		// case (Kafka → kafkas, RabbitmqCluster → rabbitmqclusters).
		resource = pluralise(kind)
	}
	return gv.WithResource(resource), nil
}

var builtinResources = map[string]string{
	"Deployment":            "deployments",
	"StatefulSet":           "statefulsets",
	"DaemonSet":             "daemonsets",
	"ReplicaSet":            "replicasets",
	"Job":                   "jobs",
	"CronJob":               "cronjobs",
	"Pod":                   "pods",
	"ReplicationController": "replicationcontrollers",
}

// pluralise applies the simplest English pluralisation — sufficient
// for the CRD names CNCF operators ship (no irregular forms in the
// Optiqor coverage table). When this fails for a specific operator,
// add an explicit entry to builtinResources.
func pluralise(kind string) string {
	lower := lowerKind(kind)
	switch {
	case endsIn(lower, "s"), endsIn(lower, "x"), endsIn(lower, "ch"), endsIn(lower, "sh"):
		return lower + "es"
	case endsIn(lower, "y") && len(lower) > 1 && !isVowel(lower[len(lower)-2]):
		return lower[:len(lower)-1] + "ies"
	default:
		return lower + "s"
	}
}

func lowerKind(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

func endsIn(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

func isVowel(c byte) bool {
	switch c {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}
