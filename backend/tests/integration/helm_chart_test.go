//go:build integration

package integration

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestHelmChart_TemplateRendersAllResources checks the agent chart
// produces every resource the install wizard depends on. Failure here
// means a customer install would land in a half-built state.
func TestHelmChart_TemplateRendersAllResources(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not on PATH")
	}
	chartDir := chartPath(t)

	out, err := exec.Command("helm", "template", "optiqor", chartDir,
		"--set", "tenantID=test-tenant",
		"--namespace", "optiqor",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	for _, kind := range []string{
		"kind: ServiceAccount",
		"kind: ClusterRole",
		"kind: ClusterRoleBinding",
		"kind: Deployment",
		"kind: NetworkPolicy",
		"kind: Service",
	} {
		if !bytes.Contains(out, []byte(kind)) {
			t.Errorf("rendered chart missing %s", kind)
		}
	}
}

// TestHelmChart_RBACIsReadOnly fails the build if a write verb sneaks
// into the agent's ClusterRole. ADR-0008 principle 1.
func TestHelmChart_RBACIsReadOnly(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not on PATH")
	}
	out, err := exec.Command("helm", "template", "optiqor", chartPath(t),
		"--set", "tenantID=test-tenant",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	for _, banned := range []string{"create", "update", "patch", "delete", "deletecollection"} {
		// Block only at the verbs-list shape (`- create`) — substrings
		// in resource names ("create-snapshot") shouldn't trigger.
		if bytes.Contains(out, []byte("\n      - "+banned+"\n")) ||
			bytes.Contains(out, []byte("\n        - "+banned+"\n")) {
			t.Errorf("ClusterRole contains write verb: %q", banned)
		}
	}
}

// TestHelmChart_NetworkPolicyDeniesInbound asserts the rendered policy
// has ingress: [] — ADR-0008 principle 2.
func TestHelmChart_NetworkPolicyDeniesInbound(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not on PATH")
	}
	out, err := exec.Command("helm", "template", "optiqor", chartPath(t),
		"--set", "tenantID=test-tenant",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("ingress: []")) {
		t.Error("NetworkPolicy must declare ingress: [] (zero inbound)")
	}
}

// TestHelmChart_KubeconformAcceptsManifests pipes helm template through
// kubeconform. Skipped if neither binary is available so local dev
// without the tools doesn't break the suite; CI installs both.
func TestHelmChart_KubeconformAcceptsManifests(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not on PATH")
	}
	kubeconform, err := exec.LookPath("kubeconform")
	if err != nil {
		t.Skip("kubeconform not on PATH")
	}

	tplOut, err := exec.Command("helm", "template", "optiqor", chartPath(t),
		"--set", "tenantID=test-tenant",
		"--namespace", "optiqor",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, tplOut)
	}

	cmd := exec.Command(kubeconform, "-strict", "-summary")
	cmd.Stdin = bytes.NewReader(tplOut)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kubeconform: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Invalid: 0") {
		t.Errorf("kubeconform reported invalid manifests:\n%s", out)
	}
}

func chartPath(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", "..", "deploy", "helm", "optiqor-agent"))
}
