package prom

import (
	"context"
	"sort"
	"sync"
	"time"
)

// WorkloadObservation is the per-workload row the scraper emits each
// tick. The values are best-effort: a missing PromQL series returns
// zero, not an error, so a partial Prometheus outage degrades
// gracefully (validator still sees other signals).
type WorkloadObservation struct {
	Namespace      string
	Kind           string // controller kind; "Deployment" | "StatefulSet" | "DaemonSet"
	Name           string
	CPUUsageCores  float64
	MemoryWorking  float64 // bytes
	OOMKilledCount int     // 7-day window
	At             time.Time
}

// QueryProfile names the canonical PromQL the scraper executes. Kept
// inside the package so a customer Prom that aliases the metrics can
// be retuned by replacing the Profile, not the scrape loop.
type QueryProfile struct {
	CPURate           string
	MemoryWorking     string
	OOMKilledIncrease string // returns a count over a window
	OOMWindow         time.Duration
}

// DefaultProfile matches a kube-prometheus-stack install with
// kube-state-metrics + cAdvisor. Each query groups by (namespace,
// pod, workload kind+name) so the scraper can map series → workload
// identity without a second query.
// DefaultProfile reads the canonical query set. Centralising the
// PromQL in canonical.go keeps the cAdvisor / kube-state-metrics
// query strings out of sync with the doc impossible.
var DefaultProfile = QueryProfile{
	CPURate:           Canonical.CPURate,
	MemoryWorking:     Canonical.MemoryWorkingSet,
	OOMKilledIncrease: Canonical.OOMKilledIncrease,
	OOMWindow:         Canonical.OOMWindow,
}

// Scraper aggregates the canonical query set into a slice of
// WorkloadObservation. The pod→workload index is refreshed every tick
// by the snapshot loop while Scrape() reads it concurrently; the
// mutex on `owners` makes the swap race-free. Access only via
// SetPodOwners + the internal reader inside Scrape — direct field
// reads from outside the package will trigger the data-race detector.
type Scraper struct {
	Client  Client
	Profile QueryProfile
	NowFunc func() time.Time

	mu     sync.RWMutex
	owners map[PodKey]WorkloadKey
}

// PodKey + WorkloadKey are deliberately separate types so a caller
// passing the wrong direction lights up at compile time.
type PodKey struct {
	Namespace string
	Pod       string
}

type WorkloadKey struct {
	Namespace string
	Kind      string
	Name      string
}

// NewScraper applies safe defaults: empty profile falls through to
// DefaultProfile; nil clock uses time.Now (callers inject for tests).
func NewScraper(client Client, owners map[PodKey]WorkloadKey) *Scraper {
	return &Scraper{
		Client:  client,
		Profile: DefaultProfile,
		NowFunc: time.Now,
		owners:  owners,
	}
}

// SetPodOwners swaps the pod→workload index. The snapshot loop calls
// this every tick after rebuilding the index from the informer caches.
// The new map fully replaces the old; the old map is never mutated
// after publication so a concurrent Scrape() holding a snapshot stays
// consistent for its iteration.
func (s *Scraper) SetPodOwners(owners map[PodKey]WorkloadKey) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.owners = owners
	s.mu.Unlock()
}

func (s *Scraper) ownerFor(pk PodKey) (WorkloadKey, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	wk, ok := s.owners[pk]
	return wk, ok
}

// Scrape runs every query in the profile and merges the results by
// workload. Returns a slice sorted by (namespace, kind, name) for
// deterministic snapshot output. A query error on one metric doesn't
// abort the whole scrape; the failing field stays zero and the error
// is the LAST one returned for the caller to log.
func (s *Scraper) Scrape(ctx context.Context) ([]WorkloadObservation, error) {
	if s == nil || s.Client == nil {
		return nil, nil
	}
	now := s.now()
	byWorkload := map[WorkloadKey]*WorkloadObservation{}

	apply := func(samples []Sample, set func(o *WorkloadObservation, v float64)) {
		for _, sm := range samples {
			pk := PodKey{Namespace: sm.Labels["namespace"], Pod: sm.Labels["pod"]}
			wk, ok := s.ownerFor(pk)
			if !ok {
				continue
			}
			obs, exists := byWorkload[wk]
			if !exists {
				obs = &WorkloadObservation{
					Namespace: wk.Namespace,
					Kind:      wk.Kind,
					Name:      wk.Name,
					At:        sm.At,
				}
				byWorkload[wk] = obs
			}
			set(obs, sm.Value)
		}
	}

	var lastErr error
	if cpu, err := s.Client.Query(ctx, s.Profile.CPURate, now); err != nil {
		lastErr = err
	} else {
		apply(cpu, func(o *WorkloadObservation, v float64) { o.CPUUsageCores += v })
	}
	if mem, err := s.Client.Query(ctx, s.Profile.MemoryWorking, now); err != nil {
		lastErr = err
	} else {
		apply(mem, func(o *WorkloadObservation, v float64) { o.MemoryWorking += v })
	}
	if oom, err := s.Client.Query(ctx, s.Profile.OOMKilledIncrease, now); err != nil {
		lastErr = err
	} else {
		apply(oom, func(o *WorkloadObservation, v float64) { o.OOMKilledCount += int(v) })
	}

	out := make([]WorkloadObservation, 0, len(byWorkload))
	for _, o := range byWorkload {
		if o.At.IsZero() {
			o.At = now
		}
		out = append(out, *o)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out, lastErr
}

func (s *Scraper) now() time.Time {
	if s.NowFunc != nil {
		return s.NowFunc().UTC()
	}
	return time.Now().UTC()
}
