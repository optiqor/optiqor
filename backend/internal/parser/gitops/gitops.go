// Package gitops provides a normalised view of declarative GitOps
// resources. Each supported tool (ArgoCD now, Flux at Phase 7) has a
// sub-package that produces []Source values.
//
// A Source is "where do the manifests come from?" — git URL + revision
// + path, or a Helm chart reference. Once normalised, the cost engine
// and PR writer can reason uniformly across both ecosystems.
package gitops

// Tool names the GitOps tool a Source originates from.
type Tool string

const (
	ToolArgoCD Tool = "argocd"
	ToolFlux   Tool = "flux"
)

// Source is the normalised manifest origin.
type Source struct {
	// Tool that emitted this source.
	Tool Tool

	// ApplicationName is the controlling resource's name (e.g. an
	// ArgoCD Application or a Flux HelmRelease).
	ApplicationName string
	// Namespace is the namespace of the controlling resource.
	Namespace string

	// RepoURL is the git origin or chart repository.
	RepoURL string
	// TargetRevision is the git ref (branch/tag/commit) or chart version.
	TargetRevision string
	// Path is the directory inside the repo (empty when Helm chart).
	Path string
	// Chart is set when the source is a Helm chart (mutually exclusive
	// with Path; ArgoCD allows both via separate Source entries).
	Chart string

	// Destination is where ArgoCD/Flux applies the manifests.
	DestServer    string // e.g. "https://kubernetes.default.svc"
	DestNamespace string

	// Values is the raw Helm values document if the source is a chart.
	// Empty for non-Helm sources; populated when ArgoCD's
	// `helm.values` or `helm.valueFiles` is set inline.
	Values string
}
