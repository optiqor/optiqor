package preflight

import (
	"context"
	"errors"
	"testing"
)

type fakeProbe struct {
	version              string
	versionErr           error
	promReachable        bool
	promReachableErr     error
	hasKarpenter         bool
	hasAutoscaler        bool
	hasRBAC              bool
	hasRBACErr           error
	namespaceCount       int
	namespaceCountErr    error
	unsupportedDetected  []string
	unsupportedDetectErr error
}

func (p *fakeProbe) K8sServerVersion(_ context.Context) (string, error) {
	return p.version, p.versionErr
}
func (p *fakeProbe) PrometheusReachable(_ context.Context, _ string) (bool, error) {
	return p.promReachable, p.promReachableErr
}
func (p *fakeProbe) HasKarpenterCRD(_ context.Context) (bool, error) { return p.hasKarpenter, nil }
func (p *fakeProbe) HasClusterAutoscaler(_ context.Context) (bool, error) {
	return p.hasAutoscaler, nil
}
func (p *fakeProbe) HasRBAC(_ context.Context, _, _ []string) (bool, error) {
	return p.hasRBAC, p.hasRBACErr
}
func (p *fakeProbe) NamespaceCount(_ context.Context) (int, error) {
	return p.namespaceCount, p.namespaceCountErr
}
func (p *fakeProbe) DetectUnsupportedProvisioners(_ context.Context) ([]string, error) {
	return p.unsupportedDetected, p.unsupportedDetectErr
}

func TestRun_HappyPath(t *testing.T) {
	r := &Runner{
		Probe: &fakeProbe{
			version:        "v1.31.4",
			promReachable:  true,
			hasKarpenter:   true,
			hasAutoscaler:  false,
			hasRBAC:        true,
			namespaceCount: 12,
		},
	}
	checks, err := r.Run(context.Background(), Config{PrometheusURL: "http://prom:9090"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(checks) != 7 {
		t.Fatalf("want 7 checks, got %d", len(checks))
	}
	for _, c := range checks {
		// Karpenter pass + autoscaler warn is the most common kube-prom-stack
		// install shape. Only the autoscaler check is allowed to be warn.
		if c.Status == StatusFail {
			t.Errorf("unexpected fail: %+v", c)
		}
	}
}

func TestRun_OldKubernetesFails(t *testing.T) {
	r := &Runner{Probe: &fakeProbe{version: "v1.26.0", promReachable: true, hasRBAC: true}}
	checks, _ := r.Run(context.Background(), Config{PrometheusURL: "http://prom:9090"})
	if checks[0].Status != StatusFail {
		t.Errorf("want fail on K8s 1.26, got %s", checks[0].Status)
	}
}

func TestRun_PromptedURLEmptyWarns(t *testing.T) {
	r := &Runner{Probe: &fakeProbe{version: "v1.31.0", hasRBAC: true}}
	checks, _ := r.Run(context.Background(), Config{})
	if checks[1].Status != StatusWarn {
		t.Errorf("missing prom URL should warn, got %s", checks[1].Status)
	}
}

func TestRun_RBACFailureSurfaces(t *testing.T) {
	r := &Runner{Probe: &fakeProbe{version: "v1.31.0", promReachable: true, hasRBACErr: errors.New("forbidden")}}
	checks, _ := r.Run(context.Background(), Config{PrometheusURL: "http://prom:9090"})
	if checks[2].Status != StatusFail {
		t.Errorf("rbac err should fail, got %s", checks[2].Status)
	}
}

func TestRun_UnsupportedProvisionerFailsClosed(t *testing.T) {
	r := &Runner{Probe: &fakeProbe{
		version:             "v1.31.0",
		promReachable:       true,
		hasRBAC:             true,
		unsupportedDetected: []string{"gke-node-auto-provisioning", "openshift-machine-api"},
	}}
	checks, _ := r.Run(context.Background(), Config{PrometheusURL: "http://prom:9090"})
	gate := findCheck(t, checks, "Unsupported provisioner gate")
	if gate.Status != StatusFail {
		t.Errorf("unsupported provisioners must fail closed; got %s", gate.Status)
	}
	if gate.RemediationLink == "" {
		t.Error("fail status must carry remediation link to issue tracker")
	}
}

func TestRun_UnsupportedProvisionerCleanPasses(t *testing.T) {
	r := &Runner{Probe: &fakeProbe{
		version:             "v1.31.0",
		promReachable:       true,
		hasRBAC:             true,
		unsupportedDetected: nil,
	}}
	checks, _ := r.Run(context.Background(), Config{PrometheusURL: "http://prom:9090"})
	gate := findCheck(t, checks, "Unsupported provisioner gate")
	if gate.Status != StatusPass {
		t.Errorf("empty detection must pass, got %s (%s)", gate.Status, gate.Detail)
	}
}

func TestRun_UnsupportedProvisionerProbeErrorWarns(t *testing.T) {
	r := &Runner{Probe: &fakeProbe{
		version:              "v1.31.0",
		promReachable:        true,
		hasRBAC:              true,
		unsupportedDetectErr: errors.New("CRD list timed out"),
	}}
	checks, _ := r.Run(context.Background(), Config{PrometheusURL: "http://prom:9090"})
	gate := findCheck(t, checks, "Unsupported provisioner gate")
	if gate.Status != StatusWarn {
		t.Errorf("probe error must warn (not silently pass / fail-close); got %s", gate.Status)
	}
}

func findCheck(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no check named %q in %v", name, checks)
	return Check{}
}

func TestRun_NilProbe(t *testing.T) {
	r := &Runner{}
	if _, err := r.Run(context.Background(), Config{}); err == nil {
		t.Error("nil probe should error")
	}
}

func TestVersionAtLeast(t *testing.T) {
	for _, tc := range []struct {
		v    string
		want bool
	}{
		{"v1.31.4", true},
		{"v1.28.0", true},
		{"v1.27.99", false},
		{"v2.0.0", true},
		{"1.30.5+abc", true},
		{"", false},
	} {
		if got := versionAtLeast(tc.v, 1, 28); got != tc.want {
			t.Errorf("versionAtLeast(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}
