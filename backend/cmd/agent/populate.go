package main

import (
	"context"
	"sync"
	"time"

	"github.com/optiqor/optiqor/internal/agent/cluster"
	"github.com/optiqor/optiqor/internal/agent/prom"
	"github.com/optiqor/optiqor/internal/agent/provisioner"
	"github.com/optiqor/optiqor/internal/ingestion"
)

// populator builds the rich AgentSnapshot body once per tick. State
// that's safe to cache across ticks (provisioner class detection,
// agent boot time) lives on the struct; everything else is fetched
// fresh from the informer caches.
type populator struct {
	readers       *cluster.Readers
	scraper       *prom.Scraper
	clusterID     string
	agentVersion  string
	probe         provisioner.Probe
	classOnce     sync.Once
	cachedClass   provisioner.Class
	cachedClassOK bool
	eventsCutoff  time.Duration
}

// Build assembles the snapshot for a single tick. err is best-effort —
// the loop logs and continues even on partial data so a transient
// Prom blip doesn't drop signals the validator would have used.
func (p *populator) Build(ctx context.Context, batchID string, now time.Time, dataFreshness time.Duration) (ingestion.AgentSnapshot, error) {
	snap := ingestion.AgentSnapshot{
		BatchID:      batchID,
		CapturedAt:   now,
		ClusterID:    p.clusterID,
		AgentVersion: p.agentVersion,
		Health: &ingestion.AgentHealthSnap{
			Version:              p.agentVersion,
			DataFreshnessSeconds: int(dataFreshness / time.Second),
		},
	}

	if p.readers == nil {
		return snap, nil
	}

	if p.readers.Workloads != nil {
		for _, w := range p.readers.Workloads.ListWorkloads() {
			snap.Workloads = append(snap.Workloads, ingestion.WorkloadSnap{
				Namespace: w.Namespace,
				Kind:      w.Kind,
				Name:      w.Name,
			})
		}
	}

	if p.readers.Events != nil {
		since := now.Add(-p.eventsCutoff)
		for _, e := range p.readers.Events.DrainEvents(since, 100) {
			snap.Events = append(snap.Events, ingestion.EventSnap{
				Namespace: e.Namespace,
				PodName:   e.PodName,
				Reason:    e.Reason,
				Type:      e.Type,
				Count:     e.Count,
				LastSeen:  e.LastSeen.UTC(),
				Message:   e.Message,
			})
		}
	}

	if p.readers.HPA != nil {
		for _, h := range p.readers.HPA.ListHPAs() {
			snap.HPAs = append(snap.HPAs, ingestion.HPASnap{
				Namespace:       h.Namespace,
				Name:            h.Name,
				MinReplicas:     h.MinReplicas,
				MaxReplicas:     h.MaxReplicas,
				CurrentReplicas: h.CurrentReplicas,
			})
		}
	}

	if p.readers.Policy != nil {
		for _, ps := range p.readers.Policy.ListPolicySummary() {
			snap.Policies = append(snap.Policies, ingestion.PolicySnap{
				Namespace:   ps.Namespace,
				HasPDB:      ps.HasPDB,
				HasQuota:    ps.HasQuota,
				HasLimitRng: ps.HasLimitRng,
			})
		}
	}

	for _, np := range p.readers.ListKarpenter(ctx) {
		snap.NodePools = append(snap.NodePools, ingestion.NodePoolSnap{
			Name:              np.Name,
			NodeCountCurrent:  np.NodeCountCurrent,
			ConsolidationMode: np.ConsolidationMode,
			Requirements:      np.Requirements,
		})
	}

	if p.probe != nil {
		p.classOnce.Do(func() {
			class, err := provisioner.Detect(ctx, p.probe)
			if err == nil {
				p.cachedClass = class
				p.cachedClassOK = true
			}
		})
		if p.cachedClassOK {
			snap.ProvisionerClass = string(p.cachedClass)
		}
	}

	if p.scraper != nil {
		obs, err := p.scraper.Scrape(ctx)
		if err != nil && len(obs) == 0 {
			return snap, err
		}
		for _, o := range obs {
			snap.PromSamples = append(snap.PromSamples, ingestion.PromSampleSnap{
				Namespace:      o.Namespace,
				Kind:           o.Kind,
				Name:           o.Name,
				CPUUsageCores:  o.CPUUsageCores,
				MemoryWorking:  int64(o.MemoryWorking),
				OOMKilledCount: o.OOMKilledCount,
				At:             o.At.UTC(),
			})
		}
	}

	return snap, nil
}
