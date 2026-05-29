// Package preflight runs the "this is what Optiqor will do in your
// cluster" checks the operator sees before `helm install`. The Runner
// is the input seam — production probes the live cluster via discovery
// + a short-circuit Prom GET; tests inject a deterministic Probe.
//
// Per the ROADMAP, surfacing the check list reduces support load by
// 5-10x because the customer self-diagnoses RBAC / Prom / Karpenter
// gaps before opening a ticket.
package preflight

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Status names the outcome of one check. Renderer maps to a colour pip:
// pass → green, warn → amber, fail → red. Warn never blocks install.
type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// Check is one row in the preview page. RemediationLink points at the
// docs runbook step the operator follows when status != pass.
type Check struct {
	Name            string `json:"name"`
	Status          Status `json:"status"`
	Detail          string `json:"detail"`
	RemediationLink string `json:"remediation_link,omitempty"`
}

// Probe is the input. Production wires it through client-go discovery
// + a tiny HTTP probe at the Prometheus URL; tests inject canned
// answers. The interface stays narrow so a cluster the agent will
// never run against (CI fixtures) still type-checks.
type Probe interface {
	K8sServerVersion(ctx context.Context) (string, error)
	PrometheusReachable(ctx context.Context, url string) (bool, error)
	HasKarpenterCRD(ctx context.Context) (bool, error)
	HasClusterAutoscaler(ctx context.Context) (bool, error)
	HasRBAC(ctx context.Context, verbs []string, resources []string) (bool, error)
	NamespaceCount(ctx context.Context) (int, error)
	// DetectUnsupportedProvisioners returns the names of provisioners
	// present in the cluster that Optiqor does not yet have a Year-1
	// adapter for (GKE NAP, OpenShift Machine API, DigitalOcean, etc.).
	// Empty slice means the cluster is on a supported provisioner; the
	// installer fails closed when this returns anything non-empty. See
	// todo.md L334 — silent fallback to `static` mis-classifies the
	// cluster's bill basis and corrupts Receipt accuracy.
	DetectUnsupportedProvisioners(ctx context.Context) ([]string, error)
}

// Config carries the customer-supplied bits the Runner can't probe
// for. PrometheusURL is mandatory when the agent ships PromQL samples
// (PR #38). Empty disables that one check but the rest still run.
type Config struct {
	PrometheusURL string `json:"prometheus_url,omitempty"`
}

// Runner orchestrates the checks. Each check has a fixed budget; an
// expensive probe shouldn't keep the page spinning past the
// 6-second customer attention window.
type Runner struct {
	Probe   Probe
	Timeout time.Duration
}

// Run executes every check sequentially. Cheap and predictable — six
// I/O calls budgeted at 1s each. Parallel runs aren't worth the
// complexity at this scale.
func (r *Runner) Run(ctx context.Context, cfg Config) ([]Check, error) {
	if r == nil || r.Probe == nil {
		return nil, errors.New("preflight: nil Probe")
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 6 * time.Second
	}
	rCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out := []Check{
		r.k8sVersionCheck(rCtx),
		r.prometheusCheck(rCtx, cfg.PrometheusURL),
		r.rbacCheck(rCtx),
		r.karpenterCheck(rCtx),
		r.autoscalerCheck(rCtx),
		r.unsupportedProvisionerCheck(rCtx),
		r.namespaceCountCheck(rCtx),
	}
	return out, nil
}

func (r *Runner) k8sVersionCheck(ctx context.Context) Check {
	v, err := r.Probe.K8sServerVersion(ctx)
	if err != nil {
		return Check{
			Name:   "Kubernetes 1.28+",
			Status: StatusFail,
			Detail: "could not probe apiserver: " + err.Error(),
		}
	}
	if !versionAtLeast(v, 1, 28) {
		return Check{
			Name:            "Kubernetes 1.28+",
			Status:          StatusFail,
			Detail:          "server version " + v + " is below the supported floor",
			RemediationLink: "https://optiqor.dev/runbooks/k8s-version",
		}
	}
	return Check{Name: "Kubernetes 1.28+", Status: StatusPass, Detail: "server " + v}
}

func (r *Runner) prometheusCheck(ctx context.Context, url string) Check {
	if url == "" {
		return Check{
			Name:            "Prometheus URL",
			Status:          StatusWarn,
			Detail:          "no Prometheus URL supplied — agent will ship K8s state but no per-workload metrics",
			RemediationLink: "https://optiqor.dev/runbooks/prometheus",
		}
	}
	ok, err := r.Probe.PrometheusReachable(ctx, url)
	if err != nil {
		return Check{Name: "Prometheus reachable", Status: StatusFail, Detail: err.Error()}
	}
	if !ok {
		return Check{
			Name:            "Prometheus reachable",
			Status:          StatusFail,
			Detail:          "could not reach " + url + " from the cluster — check Service name + namespace",
			RemediationLink: "https://optiqor.dev/runbooks/prometheus",
		}
	}
	return Check{Name: "Prometheus reachable", Status: StatusPass, Detail: url}
}

func (r *Runner) rbacCheck(ctx context.Context) Check {
	verbs := []string{"get", "list", "watch"}
	resources := []string{"events", "pods", "deployments", "horizontalpodautoscalers", "poddisruptionbudgets"}
	ok, err := r.Probe.HasRBAC(ctx, verbs, resources)
	if err != nil {
		return Check{Name: "ServiceAccount RBAC", Status: StatusFail, Detail: err.Error()}
	}
	if !ok {
		return Check{
			Name:            "ServiceAccount RBAC",
			Status:          StatusFail,
			Detail:          "the agent's ServiceAccount cannot get/list/watch all six Tier-1 GVRs",
			RemediationLink: "https://optiqor.dev/runbooks/agent-rbac",
		}
	}
	return Check{Name: "ServiceAccount RBAC", Status: StatusPass, Detail: "all 6 GVRs reachable"}
}

func (r *Runner) karpenterCheck(ctx context.Context) Check {
	ok, _ := r.Probe.HasKarpenterCRD(ctx)
	if ok {
		return Check{Name: "Karpenter detected", Status: StatusPass, Detail: "T1 provisioner-class will register"}
	}
	return Check{
		Name:   "Karpenter detected",
		Status: StatusWarn,
		Detail: "no Karpenter NodePool CRD — agent will fall through to cluster-autoscaler / static detection",
	}
}

func (r *Runner) autoscalerCheck(ctx context.Context) Check {
	ok, _ := r.Probe.HasClusterAutoscaler(ctx)
	if ok {
		return Check{Name: "cluster-autoscaler detected", Status: StatusPass, Detail: "T2 provisioner-class will register"}
	}
	return Check{Name: "cluster-autoscaler detected", Status: StatusWarn, Detail: "no Deployment named cluster-autoscaler in kube-system"}
}

// unsupportedProvisionerCheck fails closed when the cluster runs a
// Year-1 unsupported provisioner (GKE NAP, OpenShift Machine API,
// DigitalOcean, etc.). Routing the customer through `static` would
// mis-classify their bill basis and corrupt Receipt accuracy — better
// to reject the install with a tracked issue link than to ship bad
// numbers downstream.
func (r *Runner) unsupportedProvisionerCheck(ctx context.Context) Check {
	names, err := r.Probe.DetectUnsupportedProvisioners(ctx)
	if err != nil {
		return Check{
			Name:   "Unsupported provisioner gate",
			Status: StatusWarn,
			Detail: "could not probe for unsupported provisioners: " + err.Error(),
		}
	}
	if len(names) == 0 {
		return Check{
			Name:   "Unsupported provisioner gate",
			Status: StatusPass,
			Detail: "no unsupported provisioner CRDs detected",
		}
	}
	return Check{
		Name:            "Unsupported provisioner gate",
		Status:          StatusFail,
		Detail:          "detected " + strings.Join(names, ", ") + " — Optiqor has no Year-1 adapter for these provisioners. Year-1 supported: Karpenter, EKS MNG, EKS+CAS+ASG, standalone ASG, static node groups.",
		RemediationLink: "https://github.com/optiqor/optiqor/issues/new?labels=provisioner-adapter&template=unsupported-provisioner.md",
	}
}

func (r *Runner) namespaceCountCheck(ctx context.Context) Check {
	n, err := r.Probe.NamespaceCount(ctx)
	if err != nil {
		return Check{Name: "Namespace footprint", Status: StatusWarn, Detail: err.Error()}
	}
	return Check{Name: "Namespace footprint", Status: StatusPass, Detail: itoa(n) + " namespaces will be watched"}
}

// versionAtLeast accepts the "v1.30.5" / "1.30.5+abc" shapes the apiserver
// returns. Falls through to false on any parse weirdness rather than
// trying to be clever — a misreported version is operator-visible.
func versionAtLeast(v string, wantMajor, wantMinor int) bool {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, ok := atoi(parts[0])
	if !ok {
		return false
	}
	minor, ok := atoi(strings.TrimSuffix(parts[1], "+"))
	if !ok {
		// Some forks append a +commit tag straight to the minor; strip the
		// alpha suffix.
		minor, ok = atoi(stripNonDigits(parts[1]))
		if !ok {
			return false
		}
	}
	if major > wantMajor {
		return true
	}
	if major < wantMajor {
		return false
	}
	return minor >= wantMinor
}

func atoi(s string) (int, bool) {
	n := 0
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	out := []byte{}
	if n < 0 {
		out = append(out, '-')
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(append(out, digits...))
}

func stripNonDigits(s string) string {
	out := make([]byte, 0, len(s))
	for _, c := range s {
		if c >= '0' && c <= '9' {
			out = append(out, byte(c))
		}
	}
	return string(out)
}
