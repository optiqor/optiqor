// Package provisioner detects which node-lifecycle controller the
// cluster runs and emits a provisioner class the recommendation
// engine reads to cap aggressiveness. Three tiers (ROADMAP §Phase 5):
//
//	T1 karpenter   — full confidence; NodePool reader supplies caps.
//	T2 autoscaler  — full Apply Fix with ASG-shape hints.
//	T3 static      — recommendations stay advisory; confidence capped
//	                 at Medium with a manual-step caveat.
//
// Detection is one-time at agent install; the result is persisted in
// clusters.node_provisioner_class. Re-detection is opt-in via the
// agent's `--detect-provisioner` flag.
package provisioner

import (
	"context"
	"errors"
)

// Class names match the existing CHECK constraint in migration 0001
// (clusters.node_provisioner_class). Mismatched strings would silently
// blow up the INSERT at agent registration time.
type Class string

const (
	ClassKarpenter  Class = "karpenter"
	ClassAutoscaler Class = "autoscaler"
	ClassStatic     Class = "static"
)

// Probe is the seam to client-go. Production implementations use the
// discovery client + dynamic client; tests inject deterministic answers.
type Probe interface {
	// HasKarpenterCRD returns true when the karpenter.sh/NodePool CRD
	// is discoverable through the apiserver.
	HasKarpenterCRD(ctx context.Context) (bool, error)
	// HasClusterAutoscalerDeployment returns true when a Deployment
	// named cluster-autoscaler is present in kube-system.
	HasClusterAutoscalerDeployment(ctx context.Context) (bool, error)
}

// Detect resolves the provisioner class. Karpenter wins over CA when
// both are present — Karpenter takes precedence in practice because
// it's the consolidating controller (CA leaves nodes in place).
func Detect(ctx context.Context, p Probe) (Class, error) {
	if p == nil {
		return ClassStatic, errors.New("provisioner: nil probe")
	}
	if ok, err := p.HasKarpenterCRD(ctx); err != nil {
		return ClassStatic, err
	} else if ok {
		return ClassKarpenter, nil
	}
	if ok, err := p.HasClusterAutoscalerDeployment(ctx); err != nil {
		return ClassStatic, err
	} else if ok {
		return ClassAutoscaler, nil
	}
	return ClassStatic, nil
}

// AdvisoryNote returns the human-friendly footer the PR-comment
// renderer pins under the cost table. Empty string for T1 — the PR
// stays clean. The strings are wire surfaces; treat changes as a UX
// shift, not a refactor.
func AdvisoryNote(c Class) string {
	switch c {
	case ClassKarpenter:
		return ""
	case ClassAutoscaler:
		return "Cluster Autoscaler detected — savings assume the ASG can scale to the recommended count. Verify the ASG min/max bounds before merging."
	case ClassStatic:
		return "No autoscaler detected — savings are advisory. Apply the resource change, then manually right-size the node group."
	}
	return ""
}

// ConfidenceCap returns the maximum confidence band the recommendation
// engine emits for this class. ROADMAP §Phase 5 caps T3 at Medium;
// T1/T2 are uncapped (the engine's regular scoring stands).
func ConfidenceCap(c Class) string {
	if c == ClassStatic {
		return "medium"
	}
	return ""
}
