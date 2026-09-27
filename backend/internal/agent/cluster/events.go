package cluster

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	agentk8s "github.com/optiqor/optiqor/internal/agent/k8s"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// EventsR watches core/v1 Event objects. The validator pipeline reads
// OOMKilled / FailedScheduling / Evicted events as Tier-1 signals.
type EventsR struct {
	clusterID string
	indexer   cache.Indexer
}

func newEventsReader(clusterID string, f informers.SharedInformerFactory) *EventsR {
	inf := f.Core().V1().Events().Informer()
	return &EventsR{clusterID: clusterID, indexer: inf.GetIndexer()}
}

var _ agentk8s.EventsReader = (*EventsR)(nil)

// Recent returns events whose LastTimestamp is >= since and whose
// involvedObject matches w. Empty slice on no match; the caller never
// has to nil-check. Indexer reads are sub-millisecond against the
// shared cache.
func (r *EventsR) Recent(_ context.Context, _ tenancy.Context, w agentk8s.WorkloadRef, since time.Time) ([]agentk8s.Event, error) {
	if r.indexer == nil {
		return nil, nil
	}

	items := r.indexer.List()
	out := make([]agentk8s.Event, 0, 8)
	for _, raw := range items {
		ev, ok := raw.(*corev1.Event)
		if !ok || ev == nil {
			continue
		}
		if ev.InvolvedObject.Namespace != w.Namespace {
			continue
		}
		if w.Kind != "" && !strings.EqualFold(ev.InvolvedObject.Kind, w.Kind) {
			continue
		}
		if w.Name != "" && !matchesWorkloadName(ev.InvolvedObject.Name, w.Name) {
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
		first := ev.FirstTimestamp.Time
		if first.IsZero() {
			first = last
		}
		out = append(out, agentk8s.Event{
			Workload:  w,
			Reason:    ev.Reason,
			Type:      ev.Type,
			Count:     int(ev.Count),
			FirstSeen: first,
			LastSeen:  last,
			Message:   ev.Message,
		})
	}
	return out, nil
}

// matchesWorkloadName treats the WorkloadRef name as either the exact
// object name or a controller name that owns the involved object. Pod
// events carry the pod name, not the deployment; the validator passes
// the deployment name, so a prefix match against the ReplicaSet hash
// shape covers the common case.
func matchesWorkloadName(involved, want string) bool {
	if involved == want {
		return true
	}
	// Deployment "api" → ReplicaSet "api-<hash>" → Pod "api-<hash>-<hash>".
	// Trim trailing -<hash> segments and compare.
	stripped := involved
	for i := 0; i < 2; i++ {
		dash := strings.LastIndex(stripped, "-")
		if dash <= 0 {
			break
		}
		stripped = stripped[:dash]
		if stripped == want {
			return true
		}
	}
	return false
}
