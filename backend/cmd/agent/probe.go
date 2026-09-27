package main

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"

	"github.com/optiqor/optiqor/internal/agent/provisioner"
)

// k8sProbe wires provisioner.Probe to the live cluster: discovery for
// CRD presence + dynamic Get for the cluster-autoscaler Deployment.
// One-shot detection at boot, cached forever — re-detection requires
// an agent restart (deliberate: provisioner class is operationally
// rare to flip).
type k8sProbe struct {
	disc   discovery.DiscoveryInterface
	dynCli dynamic.Interface
}

var _ provisioner.Probe = (*k8sProbe)(nil)

func newK8sProbe(disc discovery.DiscoveryInterface, dynCli dynamic.Interface) *k8sProbe {
	return &k8sProbe{disc: disc, dynCli: dynCli}
}

func (p *k8sProbe) HasKarpenterCRD(_ context.Context) (bool, error) {
	if p == nil || p.disc == nil {
		return false, nil
	}
	groups, err := p.disc.ServerGroups()
	if err != nil {
		return false, fmt.Errorf("probe: server groups: %w", err)
	}
	for _, g := range groups.Groups {
		if strings.EqualFold(g.Name, "karpenter.sh") {
			return true, nil
		}
	}
	return false, nil
}

func (p *k8sProbe) HasClusterAutoscalerDeployment(ctx context.Context) (bool, error) {
	if p == nil || p.dynCli == nil {
		return false, nil
	}
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	list, err := p.dynCli.Resource(gvr).Namespace("kube-system").List(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=cluster-autoscaler",
		Limit:         1,
	})
	if err != nil {
		return false, fmt.Errorf("probe: list deployments: %w", err)
	}
	return len(list.Items) > 0, nil
}
