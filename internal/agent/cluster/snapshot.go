package cluster

import (
	"context"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"

	agentk8s "github.com/optiqor/optiqor/internal/agent/k8s"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// EventSummary mirrors the wire-shape carried in AgentSnapshot.Events.
// Kept agent-side to avoid the cluster package importing ingestion
// (which would invert the dependency direction — backend imports
// cluster, not the other way).
type EventSummary struct {
	Namespace string
	PodName   string
	Reason    string
	Type      string
	Count     int
	LastSeen  time.Time
	Message   string
}

// HPASummary mirrors AgentSnapshot.HPAs.
type HPASummary struct {
	Namespace       string
	Name            string
	MinReplicas     int
	MaxReplicas     int
	CurrentReplicas int
}

// PolicySummary mirrors AgentSnapshot.Policies — one row per
// namespace, with booleans indicating which primitives the namespace
// has. The validator pipeline reads this as a coarse signal; the
// fine-grained per-workload constraints come through PolicyReader.
type PolicySummary struct {
	Namespace   string
	HasPDB      bool
	HasQuota    bool
	HasLimitRng bool
}

// DrainEvents returns events whose LastSeen ≥ since, sliced into the
// wire-shape EventSummary. Empty since means "every event still in
// the indexer cache". limit is a safety net — Apply Fix uses 100 most
// recent across the cluster.
func (r *EventsR) DrainEvents(since time.Time, limit int) []EventSummary {
	if r == nil || r.indexer == nil {
		return nil
	}
	if limit <= 0 {
		limit = 100
	}
	items := r.indexer.List()
	out := make([]EventSummary, 0, len(items))
	for _, raw := range items {
		ev, ok := raw.(*corev1.Event)
		if !ok || ev == nil {
			continue
		}
		last := ev.LastTimestamp.Time
		if last.IsZero() {
			last = ev.EventTime.Time
		}
		if last.IsZero() {
			last = ev.CreationTimestamp.Time
		}
		if !since.IsZero() && last.Before(since) {
			continue
		}
		out = append(out, EventSummary{
			Namespace: ev.InvolvedObject.Namespace,
			PodName:   ev.InvolvedObject.Name,
			Reason:    ev.Reason,
			Type:      ev.Type,
			Count:     int(ev.Count),
			LastSeen:  last,
			Message:   ev.Message,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// ListHPAs returns every HorizontalPodAutoscaler the agent watches.
// The snapshot loop ships them in bulk so the backend can join
// against scaleTargetRef.
func (r *HPAR) ListHPAs() []HPASummary {
	if r == nil || r.indexer == nil {
		return nil
	}
	items := r.indexer.List()
	out := make([]HPASummary, 0, len(items))
	for _, raw := range items {
		hpa, ok := raw.(*autoscalingv2.HorizontalPodAutoscaler)
		if !ok || hpa == nil {
			continue
		}
		out = append(out, HPASummary{
			Namespace:       hpa.Namespace,
			Name:            hpa.Spec.ScaleTargetRef.Name,
			MinReplicas:     intOrZero(hpa.Spec.MinReplicas),
			MaxReplicas:     int(hpa.Spec.MaxReplicas),
			CurrentReplicas: int(hpa.Status.CurrentReplicas),
		})
	}
	return out
}

// ListPolicySummary returns a per-namespace boolean digest of which
// admission primitives exist. Used by validator to fast-skip
// per-workload constraint checks when the namespace carries none.
func (r *PolicyR) ListPolicySummary() []PolicySummary {
	if r == nil {
		return nil
	}
	byNs := map[string]*PolicySummary{}
	ensure := func(ns string) *PolicySummary {
		if p, ok := byNs[ns]; ok {
			return p
		}
		p := &PolicySummary{Namespace: ns}
		byNs[ns] = p
		return p
	}
	if r.pdbIdx != nil {
		for _, raw := range r.pdbIdx.List() {
			if pdb, ok := raw.(*policyv1.PodDisruptionBudget); ok && pdb != nil {
				ensure(pdb.Namespace).HasPDB = true
			}
		}
	}
	if r.quotaIdx != nil {
		for _, raw := range r.quotaIdx.List() {
			if q, ok := raw.(*corev1.ResourceQuota); ok && q != nil {
				ensure(q.Namespace).HasQuota = true
			}
		}
	}
	if r.limitIdx != nil {
		for _, raw := range r.limitIdx.List() {
			if lr, ok := raw.(*corev1.LimitRange); ok && lr != nil {
				ensure(lr.Namespace).HasLimitRng = true
			}
		}
	}
	out := make([]PolicySummary, 0, len(byNs))
	for _, p := range byNs {
		out = append(out, *p)
	}
	return out
}

// ListKarpenter calls the existing KarpenterR.List under a synthetic
// tenancy context — the reader's RLS bind is a no-op when the source
// is the in-cluster informer. The cluster.Readers consumer wraps this
// with its own ctx + timeout.
func (r *Readers) ListKarpenter(ctx context.Context) []agentk8s.KarpenterNodePool {
	if r == nil || r.Karpenter == nil {
		return nil
	}
	pools, _ := r.Karpenter.List(ctx, tenancy.Context{})
	return pools
}
