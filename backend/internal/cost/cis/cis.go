// Package cis attaches CIS Kubernetes Benchmark v1.9 control IDs to
// each security detector. Lets security buyers roll Optiqor findings
// into existing compliance dashboards without re-mapping.
//
// Control IDs reference CIS Kubernetes Benchmark v1.9 (2024-08).
package cis

// Mapping is the detector-id → CIS control set. Cost detectors return
// nil from Controls — only security findings carry compliance refs.
var mapping = map[string][]string{
	"run-as-root":                  {"5.7.4"}, // Minimize the admission of root containers
	"privileged-container":         {"5.2.1"}, // Minimize the admission of privileged containers
	"host-network":                 {"5.2.4"}, // Minimize host network sharing
	"allow-privilege-escalation":   {"5.2.5"}, // Minimize containers that allow privilege escalation
	"capabilities-not-dropped-all": {"5.2.8"}, // Minimize the admission of containers with Linux capabilities
	"read-only-root-fs-missing":    {"5.7.3"}, // Minimize containers without readOnlyRootFilesystem
	"image-pinned-latest":          {"5.1.4"}, // Minimize use of the latest image tag
	"missing-cpu-limit":            {"5.7.2"}, // Set requests/limits to avoid resource exhaustion
	"missing-memory-limit":         {"5.7.2"},
}

// Controls returns the CIS control IDs for a detector. Empty slice
// when the detector has no security mapping (cost-only detectors).
func Controls(detectorID string) []string {
	c, ok := mapping[detectorID]
	if !ok {
		return nil
	}
	out := make([]string, len(c))
	copy(out, c)
	return out
}

// Has reports whether the detector has any CIS mapping; used by the
// PR comment renderer to decide whether to print the "CIS: ..." line.
func Has(detectorID string) bool {
	_, ok := mapping[detectorID]
	return ok
}
