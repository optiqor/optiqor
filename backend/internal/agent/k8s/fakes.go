package k8s

import (
	"context"
	"sync"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// InMemoryEvents implements EventsReader against a goroutine-safe
// per-workload event log. Tests inject events with Record.
type InMemoryEvents struct {
	mu     sync.RWMutex
	events map[string][]Event
}

func NewInMemoryEvents() *InMemoryEvents {
	return &InMemoryEvents{events: map[string][]Event{}}
}

func (e *InMemoryEvents) Record(ev Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	k := refKey(ev.Workload)
	e.events[k] = append(e.events[k], ev)
}

func (e *InMemoryEvents) Recent(_ context.Context, _ tenancy.Context, w WorkloadRef, since time.Time) ([]Event, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	src := e.events[refKey(w)]
	if len(src) == 0 {
		return nil, nil
	}
	out := make([]Event, 0, len(src))
	for _, ev := range src {
		if !ev.LastSeen.Before(since) {
			out = append(out, ev)
		}
	}
	return out, nil
}

// InMemoryHPA is the fake for HPAReader.
type InMemoryHPA struct {
	mu    sync.RWMutex
	state map[string]HPAState
}

func NewInMemoryHPA() *InMemoryHPA { return &InMemoryHPA{state: map[string]HPAState{}} }

func (h *InMemoryHPA) Set(w WorkloadRef, s HPAState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state[refKey(w)] = s
}

func (h *InMemoryHPA) Get(_ context.Context, _ tenancy.Context, w WorkloadRef) (*HPAState, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.state[refKey(w)]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

// InMemoryPolicy implements PolicyReader for the three policy
// primitives in one fake.
type InMemoryPolicy struct {
	mu        sync.RWMutex
	snapshots map[string]PolicySnapshot
}

func NewInMemoryPolicy() *InMemoryPolicy {
	return &InMemoryPolicy{snapshots: map[string]PolicySnapshot{}}
}

func (p *InMemoryPolicy) Set(w WorkloadRef, s PolicySnapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.snapshots[refKey(w)] = s
}

func (p *InMemoryPolicy) Snapshot(_ context.Context, _ tenancy.Context, w WorkloadRef) (PolicySnapshot, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if s, ok := p.snapshots[refKey(w)]; ok {
		return s, nil
	}
	return PolicySnapshot{}, nil
}

// InMemoryVPA implements VPAReader.
type InMemoryVPA struct {
	mu    sync.RWMutex
	state map[string]VPARecommendation
}

func NewInMemoryVPA() *InMemoryVPA { return &InMemoryVPA{state: map[string]VPARecommendation{}} }

func (v *InMemoryVPA) Set(w WorkloadRef, r VPARecommendation) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.state[refKey(w)] = r
}

func (v *InMemoryVPA) Get(_ context.Context, _ tenancy.Context, w WorkloadRef) (*VPARecommendation, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if r, ok := v.state[refKey(w)]; ok {
		return &r, nil
	}
	return nil, nil
}

// InMemoryKarpenter implements KarpenterReader.
type InMemoryKarpenter struct {
	mu        sync.RWMutex
	nodepools []KarpenterNodePool
}

func NewInMemoryKarpenter() *InMemoryKarpenter { return &InMemoryKarpenter{} }

func (k *InMemoryKarpenter) Set(nps []KarpenterNodePool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.nodepools = append(k.nodepools[:0], nps...)
}

func (k *InMemoryKarpenter) List(_ context.Context, _ tenancy.Context) ([]KarpenterNodePool, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	out := make([]KarpenterNodePool, len(k.nodepools))
	copy(out, k.nodepools)
	return out, nil
}

func refKey(w WorkloadRef) string {
	return w.ClusterID + "/" + w.Namespace + "/" + w.Kind + "/" + w.Name
}
