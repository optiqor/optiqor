package cluster

import (
	"context"
	"fmt"
	"time"

	corev1informers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

// Config wires the live agent. ClusterID feeds the tenancy + workload
// identity hash; ResyncPeriod is the informer's cache-resync interval
// (not the watch reconnect cadence — that's client-go internal).
type Config struct {
	ClusterID    string
	Client       kubernetes.Interface
	ResyncPeriod time.Duration
	StopCh       <-chan struct{}
}

// Readers bundles every Tier-1 reader so the agent dispatcher consumes
// one struct. Each field satisfies the matching interface in
// internal/agent/k8s. Nil fields are skipped by validator (cluster
// signals are nil-safe in the workflow path).
type Readers struct {
	Events    *EventsR
	HPA       *HPAR
	Policy    *PolicyR
	VPA       *VPAR       // nil when the VPA CRD is absent
	Karpenter *KarpenterR // nil when the Karpenter CRD is absent
}

// Start spins up the SharedInformerFactory and blocks until every
// informer's cache has primed. Returns the Readers once safe to query;
// callers Cancel the StopCh on shutdown so each informer's watch loop
// exits cleanly.
func Start(ctx context.Context, cfg Config) (*Readers, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("cluster: nil k8s client")
	}
	if cfg.ClusterID == "" {
		return nil, fmt.Errorf("cluster: empty ClusterID")
	}
	if cfg.ResyncPeriod <= 0 {
		// 10 minutes matches the client-go default for non-leader-elected
		// controllers. Lower numbers cause needless apiserver chatter.
		cfg.ResyncPeriod = 10 * time.Minute
	}

	factory := corev1informers.NewSharedInformerFactory(cfg.Client, cfg.ResyncPeriod)

	events := newEventsReader(cfg.ClusterID, factory)
	hpa := newHPAReader(cfg.ClusterID, factory)
	policy := newPolicyReader(cfg.ClusterID, factory)

	stop := cfg.StopCh
	if stop == nil {
		ch := make(chan struct{})
		go func() {
			<-ctx.Done()
			close(ch)
		}()
		stop = ch
	}

	factory.Start(stop)

	// WaitForCacheSync returns false if stop fired before sync; treat
	// that as a startup error so the agent exits and the supervisor
	// restarts it rather than serving from an empty cache.
	if !cacheSynced(stop, factory) {
		return nil, fmt.Errorf("cluster: informer cache sync aborted")
	}

	return &Readers{
		Events: events,
		HPA:    hpa,
		Policy: policy,
	}, nil
}

// AttachDynamic adds the CRD-conditional readers (VPA, Karpenter) once
// the agent has run its discovery pre-flight. Split from Start so the
// pre-flight result can decide whether to skip a reader entirely —
// constructing the dynamic informer on a missing CRD logs a "no
// matches" error every resync.
func (r *Readers) AttachDynamic(vpa *VPAR, karpenter *KarpenterR) {
	r.VPA = vpa
	r.Karpenter = karpenter
}

func cacheSynced(stop <-chan struct{}, f corev1informers.SharedInformerFactory) bool {
	results := f.WaitForCacheSync(stop)
	for _, ok := range results {
		if !ok {
			return false
		}
	}
	return true
}
