package cluster

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// WorkloadRef is the minimal workload identifier the snapshot loop +
// Prom scraper need. The wider agentk8s.WorkloadRef carries ClusterID
// for the validator pipeline — kept distinct so this package isn't
// importing the wider type while still enumerating from informer
// caches.
type WorkloadRef struct {
	Namespace string
	Kind      string // "Deployment" | "StatefulSet" | "DaemonSet"
	Name      string
}

// WorkloadIndex caches Deployments + StatefulSets + DaemonSets so the
// snapshot loop can enumerate workloads cluster-wide without making
// a live List call every tick. Pod→workload ownership is resolved
// through ReplicaSet ownerRefs for Deployments and via direct
// ownerRefs for STS/DS.
type WorkloadIndex struct {
	deployments  cache.Indexer
	statefulSets cache.Indexer
	daemonSets   cache.Indexer
	replicaSets  cache.Indexer
	pods         cache.Indexer
}

// startWorkloadIndex registers the four extra informers + pods on the
// shared factory. Called from cluster.Start so callers don't manage
// the factory lifecycle.
func startWorkloadIndex(f informers.SharedInformerFactory) *WorkloadIndex {
	return &WorkloadIndex{
		deployments:  f.Apps().V1().Deployments().Informer().GetIndexer(),
		statefulSets: f.Apps().V1().StatefulSets().Informer().GetIndexer(),
		daemonSets:   f.Apps().V1().DaemonSets().Informer().GetIndexer(),
		replicaSets:  f.Apps().V1().ReplicaSets().Informer().GetIndexer(),
		pods:         f.Core().V1().Pods().Informer().GetIndexer(),
	}
}

// ListWorkloads returns every Deployment / StatefulSet / DaemonSet
// the agent watches, sorted by (namespace, kind, name) for stable
// snapshot output.
func (idx *WorkloadIndex) ListWorkloads() []WorkloadRef {
	if idx == nil {
		return nil
	}
	out := make([]WorkloadRef, 0, 32)
	if idx.deployments != nil {
		for _, raw := range idx.deployments.List() {
			if d, ok := raw.(*appsv1.Deployment); ok && d != nil {
				out = append(out, WorkloadRef{Namespace: d.Namespace, Kind: "Deployment", Name: d.Name})
			}
		}
	}
	if idx.statefulSets != nil {
		for _, raw := range idx.statefulSets.List() {
			if s, ok := raw.(*appsv1.StatefulSet); ok && s != nil {
				out = append(out, WorkloadRef{Namespace: s.Namespace, Kind: "StatefulSet", Name: s.Name})
			}
		}
	}
	if idx.daemonSets != nil {
		for _, raw := range idx.daemonSets.List() {
			if d, ok := raw.(*appsv1.DaemonSet); ok && d != nil {
				out = append(out, WorkloadRef{Namespace: d.Namespace, Kind: "DaemonSet", Name: d.Name})
			}
		}
	}
	return out
}

// PodOwnerKey is what the Prom scraper consumes — pod name in a
// namespace maps to controller kind + name. Resolved by walking pod
// owner refs through the optional ReplicaSet hop for Deployments.
type PodOwnerKey struct {
	Namespace string
	Pod       string
}

type PodWorkloadKey struct {
	Namespace string
	Kind      string
	Name      string
}

// PodOwners materialises pod→controller mapping for every pod the
// agent watches. Output feeds prom.Scraper.PodOwners so per-pod
// PromQL series can be rolled up onto workload rows.
func (idx *WorkloadIndex) PodOwners(_ context.Context) map[PodOwnerKey]PodWorkloadKey {
	if idx == nil || idx.pods == nil {
		return nil
	}
	rsByName := map[string]string{}
	if idx.replicaSets != nil {
		for _, raw := range idx.replicaSets.List() {
			rs, ok := raw.(*appsv1.ReplicaSet)
			if !ok || rs == nil {
				continue
			}
			for _, owner := range rs.OwnerReferences {
				if owner.Controller != nil && *owner.Controller && owner.Kind == "Deployment" {
					rsByName[rs.Namespace+"/"+rs.Name] = owner.Name
					break
				}
			}
		}
	}

	out := map[PodOwnerKey]PodWorkloadKey{}
	for _, raw := range idx.pods.List() {
		pod, ok := raw.(*corev1.Pod)
		if !ok || pod == nil {
			continue
		}
		key := PodOwnerKey{Namespace: pod.Namespace, Pod: pod.Name}
		for _, owner := range pod.OwnerReferences {
			if owner.Controller == nil || !*owner.Controller {
				continue
			}
			switch owner.Kind {
			case "StatefulSet", "DaemonSet":
				out[key] = PodWorkloadKey{Namespace: pod.Namespace, Kind: owner.Kind, Name: owner.Name}
			case "ReplicaSet":
				// Resolve the deployment behind the ReplicaSet.
				if deploy, found := rsByName[pod.Namespace+"/"+owner.Name]; found {
					out[key] = PodWorkloadKey{Namespace: pod.Namespace, Kind: "Deployment", Name: deploy}
				} else {
					// Bare ReplicaSet (rare) — record it as the controller.
					out[key] = PodWorkloadKey{Namespace: pod.Namespace, Kind: "ReplicaSet", Name: owner.Name}
				}
			}
			break
		}
	}
	return out
}
