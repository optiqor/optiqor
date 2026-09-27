// Package graph builds the workload→service→endpoint adjacency the
// validator pipeline uses to detect "no live traffic" (safe to scale
// to zero) and "dependents exist" (Apply Fix would break them).
//
// Pure functions over the agent's informer caches; client-go items
// arrive normalised through the Reader interface so the package stays
// usable from backend tests with in-memory fakes.
package graph

import (
	"context"
	"sort"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Reader is the input seam. Production wires it to the agent's
// SharedInformer indexers; tests inject deterministic slices.
type Reader interface {
	Services(ctx context.Context, t tenancy.Context) ([]Service, error)
	Endpoints(ctx context.Context, t tenancy.Context) ([]EndpointSlice, error)
	WorkloadLabels(ctx context.Context, t tenancy.Context) (map[WorkloadKey]map[string]string, error)
}

// WorkloadKey uniquely identifies a workload across namespaces. Kind
// is "Deployment" / "StatefulSet" / etc.
type WorkloadKey struct {
	Namespace string
	Kind      string
	Name      string
}

// Service mirrors the parts of corev1.Service we care about. Selector
// is empty for headless services that publish their own Endpoints.
type Service struct {
	Namespace string
	Name      string
	Selector  map[string]string
}

// EndpointSlice is the workload-facing summary of one Endpoints
// object. ReadyAddresses == 0 means no live traffic; the validator
// pipeline treats that as the "scale to zero" signal.
type EndpointSlice struct {
	Namespace      string
	Service        string
	ReadyAddresses int
}

// Edge represents one workload → service link. The validator pipeline
// reads Outgoing (services this workload exposes) and Incoming
// (workloads that select services this workload runs).
type Edge struct {
	From WorkloadKey
	To   WorkloadKey
	Via  string // service name; empty when the link is direct (pod IP).
}

// Snapshot is what Build returns: a deterministic, sorted adjacency
// view of the whole cluster. Empty when no workloads have labels —
// the validator treats that as "no graph signal".
type Snapshot struct {
	CapturedAt int64 // unix seconds — Build's responsibility to pin
	Workloads  []WorkloadKey
	Edges      []Edge
	// Liveness maps WorkloadKey → bool. True when at least one Service
	// targeting the workload's labels has a ReadyAddresses > 0
	// endpoint slice. False = candidate for scale-to-zero.
	Liveness map[WorkloadKey]bool
}

// Build walks the reader, joins services to workloads via label
// selectors, and computes liveness from the endpoint slices. Stable
// across calls when inputs match — sorts are total-ordered so byte
// comparisons in golden tests stay reliable.
func Build(ctx context.Context, r Reader, t tenancy.Context, capturedAt int64) (Snapshot, error) {
	labels, err := r.WorkloadLabels(ctx, t)
	if err != nil {
		return Snapshot{}, err
	}
	svcs, err := r.Services(ctx, t)
	if err != nil {
		return Snapshot{}, err
	}
	eps, err := r.Endpoints(ctx, t)
	if err != nil {
		return Snapshot{}, err
	}

	liveByNamespaceService := map[string]bool{}
	for _, e := range eps {
		key := e.Namespace + "/" + e.Service
		if e.ReadyAddresses > 0 {
			liveByNamespaceService[key] = true
		} else if _, ok := liveByNamespaceService[key]; !ok {
			liveByNamespaceService[key] = false
		}
	}

	snap := Snapshot{
		CapturedAt: capturedAt,
		Workloads:  make([]WorkloadKey, 0, len(labels)),
		Liveness:   make(map[WorkloadKey]bool, len(labels)),
	}
	for w := range labels {
		snap.Workloads = append(snap.Workloads, w)
		snap.Liveness[w] = false
	}
	sort.Slice(snap.Workloads, func(i, j int) bool {
		return workloadLess(snap.Workloads[i], snap.Workloads[j])
	})

	for _, svc := range svcs {
		if len(svc.Selector) == 0 {
			continue
		}
		key := svc.Namespace + "/" + svc.Name
		live := liveByNamespaceService[key]
		for w, l := range labels {
			if w.Namespace != svc.Namespace {
				continue
			}
			if !matchAll(l, svc.Selector) {
				continue
			}
			if live {
				snap.Liveness[w] = true
			}
			snap.Edges = append(snap.Edges, Edge{
				From: w,
				To:   WorkloadKey{Namespace: svc.Namespace, Kind: "Service", Name: svc.Name},
				Via:  svc.Name,
			})
		}
	}
	sort.Slice(snap.Edges, func(i, j int) bool {
		a, b := snap.Edges[i], snap.Edges[j]
		if a.From.Namespace != b.From.Namespace {
			return a.From.Namespace < b.From.Namespace
		}
		if a.From.Name != b.From.Name {
			return a.From.Name < b.From.Name
		}
		return a.Via < b.Via
	})

	return snap, nil
}

func matchAll(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

func workloadLess(a, b WorkloadKey) bool {
	if a.Namespace != b.Namespace {
		return a.Namespace < b.Namespace
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Name < b.Name
}
