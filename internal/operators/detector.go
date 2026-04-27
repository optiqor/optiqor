// Package operators implements the four-layer Operator Coverage Engine
// described in todo.md. Phase 1 ships Layer 1: the owner-reference
// walker that classifies every workload as `direct` or
// `operator:<group/kind>`.
//
// The walker is pure-Go and takes a stable representation of an owner
// chain so it can be exercised in unit tests without a real K8s API.
// The agent's client-go integration converts informer cache items into
// this representation before calling Classify.
package operators

import (
	"fmt"
	"strings"
)

// OwnerRef mirrors the fields of metav1.OwnerReference that we care
// about. Kept here (instead of importing client-go) so this package
// stays a leaf in the dep graph and can be reused by both the backend
// and the agent without coupling.
type OwnerRef struct {
	APIVersion string // e.g. "kafka.strimzi.io/v1beta2"
	Kind       string // e.g. "Kafka"
	Name       string
	Controller bool // only the controlling owner reports the operator chain
}

// Workload is a stable, K8s-API-shaped tuple. We only need enough to
// classify; the full shape lives in internal/parser.
type Workload struct {
	Namespace string
	Kind      string // Pod, ReplicaSet, Deployment, StatefulSet, DaemonSet, Job, CronJob, ...
	Name      string
	Owners    []OwnerRef
}

// Classification is the result of the walker.
type Classification struct {
	// Direct is true when the workload's owner chain terminates at a
	// built-in K8s controller (Deployment, StatefulSet, DaemonSet,
	// CronJob/Job) and not a custom resource. Apply Fix is allowed.
	Direct bool

	// Operator is the "<group>/<kind>" of the controlling custom
	// resource (e.g. "kafka.strimzi.io/Kafka") when the workload is
	// owned by an operator. Empty for direct workloads.
	Operator string
}

// String returns "direct" or "operator:<group>/<kind>".
func (c Classification) String() string {
	if c.Direct {
		return "direct"
	}
	if c.Operator != "" {
		return "operator:" + c.Operator
	}
	return "unknown"
}

// builtIn is the set of K8s built-in controller kinds whose presence
// in an owner chain marks the workload as `direct`.
var builtIn = map[string]struct{}{
	"Deployment":  {},
	"ReplicaSet":  {},
	"StatefulSet": {},
	"DaemonSet":   {},
	"Job":         {},
	"CronJob":     {},
}

// Classify walks the controlling owner chain and returns a
// Classification. The walker stops at the first non-built-in
// controller (operator marker) it finds; if it never finds one and
// the chain is empty or all built-in, the workload is `direct`.
//
// Resolve is a callback that looks up an OwnerRef's owner chain. It
// returns the next owner ref ("" "" if this is the top of the chain)
// and ok=false when lookup fails. Allowing the walker to defer to the
// caller for resolution keeps the agent's informer cache out of this
// package.
func Classify(w Workload, resolve func(OwnerRef) (OwnerRef, bool)) Classification {
	visited := map[string]struct{}{} // cycle guard

	var ref OwnerRef
	if c := controllingOwner(w.Owners); c != nil {
		ref = *c
	} else {
		// No owners — the workload is itself a Pod or a top-level
		// resource. If its own kind is built-in, classify direct.
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

		// Built-in controller? Walk further up; the chain may still
		// terminate in an operator (e.g. CronJob → Job → Pod is
		// direct, but Strimzi's Kafka → StatefulSet → Pod is operator).
		if _, isBuiltIn := builtIn[ref.Kind]; isBuiltIn {
			next, found := resolve(ref)
			if !found || next.Kind == "" {
				return Classification{Direct: true}
			}
			ref = next
			continue
		}

		// Non-built-in: this is a custom resource. Mark operator-owned.
		group := apiGroup(ref.APIVersion)
		return Classification{Operator: group + "/" + ref.Kind}
	}
	return Classification{} // chain too deep; unknown
}

// controllingOwner returns the controller=true entry, falling back to
// the first owner if no controller is marked.
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

// FormatClass renders a Classification for the workload-class column
// in the workloads table. Stable wire format used in metrics labels.
func (c Classification) FormatClass() string {
	if c.Direct {
		return "direct"
	}
	if c.Operator == "" {
		return "unknown"
	}
	return fmt.Sprintf("operator:%s", c.Operator)
}
