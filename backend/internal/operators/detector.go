// Package operators implements Layer 1 of the four-layer Operator
// Coverage Engine (todo.md): the owner-reference walker that classifies
// every workload as `direct` or `operator:<group/kind>`. Pure Go so it
// runs in unit tests without a real K8s API; the agent's client-go
// integration converts informer items into OwnerRef before calling.
package operators

import (
	"fmt"
	"strings"
)

// OwnerRef mirrors metav1.OwnerReference. Kept here instead of
// importing client-go so this package stays a leaf in the dep graph and
// is reusable by both backend and agent.
type OwnerRef struct {
	APIVersion string
	Kind       string
	Name       string
	Controller bool
}

// Workload is a stable, K8s-API-shaped tuple. The full shape lives in
// internal/parser; we only need enough to classify.
type Workload struct {
	Namespace string
	Kind      string
	Name      string
	Owners    []OwnerRef
}

type Classification struct {
	// Direct is true when the controlling owner chain terminates at a
	// built-in K8s controller. Apply Fix is allowed.
	Direct bool

	// Operator is "<group>/<kind>" of the controlling custom resource
	// (e.g. "kafka.strimzi.io/Kafka"). Empty for direct workloads.
	Operator string
}

func (c Classification) String() string {
	if c.Direct {
		return "direct"
	}
	if c.Operator != "" {
		return "operator:" + c.Operator
	}
	return "unknown"
}

// builtIn is the set of K8s built-in controller kinds. Presence in an
// owner chain marks the workload `direct`.
var builtIn = map[string]struct{}{
	"Deployment":  {},
	"ReplicaSet":  {},
	"StatefulSet": {},
	"DaemonSet":   {},
	"Job":         {},
	"CronJob":     {},
}

// Classify walks the controlling owner chain. The walker stops at the
// first non-built-in controller (the operator marker); if it never
// finds one, the workload is `direct`.
//
// resolve looks up an OwnerRef's next owner. Deferring lookup to the
// caller keeps the agent's informer cache out of this package.
func Classify(w Workload, resolve func(OwnerRef) (OwnerRef, bool)) Classification {
	visited := map[string]struct{}{} // cycle guard

	var ref OwnerRef
	if c := controllingOwner(w.Owners); c != nil {
		ref = *c
	} else {
		if _, builtinKind := builtIn[w.Kind]; builtinKind {
			return Classification{Direct: true}
		}
		// Pod with no owner chain (rare): treat as direct.
		return Classification{Direct: true}
	}

	for depth := 0; depth < 16; depth++ {
		key := ref.APIVersion + "/" + ref.Kind + "/" + ref.Name
		if _, dup := visited[key]; dup {
			return Classification{} // cycle detected; unknown
		}
		visited[key] = struct{}{}

		// Built-in: keep walking. CronJob → Job → Pod is direct, but
		// Strimzi's Kafka → StatefulSet → Pod is operator-owned, so we
		// can't stop at the first built-in.
		if _, isBuiltIn := builtIn[ref.Kind]; isBuiltIn {
			next, found := resolve(ref)
			if !found || next.Kind == "" {
				return Classification{Direct: true}
			}
			ref = next
			continue
		}

		group := apiGroup(ref.APIVersion)
		return Classification{Operator: group + "/" + ref.Kind}
	}
	return Classification{} // chain too deep; unknown
}

// controllingOwner returns the controller=true entry, falling back to
// the first owner when none is marked.
func controllingOwner(owners []OwnerRef) *OwnerRef {
	for i := range owners {
		if owners[i].Controller {
			return &owners[i]
		}
	}
	if len(owners) > 0 {
		return &owners[0]
	}
	return nil
}

// apiGroup extracts "kafka.strimzi.io" from "kafka.strimzi.io/v1beta2".
// Core types ("v1") return "".
func apiGroup(apiVersion string) string {
	idx := strings.IndexByte(apiVersion, '/')
	if idx < 0 {
		return ""
	}
	return apiVersion[:idx]
}

// FormatClass is the stable wire format used in metrics labels.
func (c Classification) FormatClass() string {
	if c.Direct {
		return "direct"
	}
	if c.Operator == "" {
		return "unknown"
	}
	return fmt.Sprintf("operator:%s", c.Operator)
}
