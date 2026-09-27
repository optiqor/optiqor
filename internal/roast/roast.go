// Package roast applies a tone-pass to a report for `--roast`.
//
// Hard rules (mirroring CLAUDE.md):
//   - No LLM calls. Roast titles are a static map keyed by detector ID.
//   - Detail / dollar / severity / confidence are NEVER rewritten —
//     only Title and the report-level Tagline change.
//   - The mandatory accuracy disclosure stays exactly as-is; roast
//     appends a quip after it, it never replaces it.
package roast

import (
	"github.com/optiqor/optiqor-cli/internal/render"
	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// Tagline replaces render.BrandTagline under the brand mark when
// --roast is set; the renderer reads it via render.Options.
const Tagline = "Helm chart cost roast — your YAML deserves it"

// FooterQuip appends after the accuracy disclosure (which is
// mandatory and untouched).
const FooterQuip = "Receipts > vibes. Install the agent for the actual bill: optiqor.dev/get"

// titles: unknown detector IDs keep their original Title (graceful
// degradation). Ordering tracks pkg/rules.All() so reviewers can scan
// top-to-bottom and confirm every detector has a line.
//
//nolint:gosec // G101 false positive: detector IDs and joke titles, no credentials.
var titles = map[string]string{
	// ---- Cost / efficiency ----------------------------------------
	"cpu-overprovisioned":            "CPU on a buffet plan, eating air",
	"memory-overprovisioned":         "RAM hoarder spotted",
	"cpu-limit-far-above-request":    "CPU limit cosplaying as the request",
	"memory-limit-far-above-request": "Memory limit set to ‘yes’",
	"oversized-cpu-limit":            "CPU limit thinks it’s a mainframe",
	"oversized-memory-limit":         "Memory limit measured in optimism",
	"replicas-too-high":              "Replicas: more is more, allegedly",
	"excessive-replica-count":        "Counting replicas like sheep, but billed",
	"unbounded-image-tag":            "Image tag :latest, the eternal mystery",
	"cpu-without-memory-request":     "CPU set, memory hopes for the best",
	"memory-without-cpu-request":     "Memory set, CPU left to vibes",
	"cpu-request-equals-limit":       "Guaranteed QoS, guaranteed bill",
	"memory-request-equals-limit":    "Memory request equals limit, equals waste",
	"tiny-cpu-request":               "CPU request smaller than a tweet",
	"tiny-memory-request":            "Memory request: ‘ehh, should be fine’",
	"idle-workload":                  "replicas: 0, a Deployment in witness protection",

	// ---- Security (still surfaced as bonus) -----------------------
	"missing-cpu-limit":               "No CPU limit — let it cook",
	"missing-memory-limit":            "OOM is just a feature flag now",
	"image-pinned-latest":             "Pinned to :latest, prayed to gods",
	"run-as-root":                     "runAsNonRoot=false, runAsBoldness=true",
	"runs-as-uid-zero":                "UID 0 because regulations are a suggestion",
	"privileged-container":            "privileged=true, consequences=later",
	"host-network":                    "hostNetwork — sharing is caring",
	"host-pid":                        "hostPID enabled, neighbors visible",
	"host-ipc":                        "hostIPC enabled, gossip enabled",
	"read-only-root-fs-missing":       "rootfs read-write, regrets read-only",
	"allow-privilege-escalation":      "allowPrivilegeEscalation: yolo",
	"host-path-volume":                "hostPath mount, host’s problem",
	"dangerous-capability-added":      "Adding caps like party hats",
	"capabilities-not-dropped-all":    "drop ALL? Couldn’t be us",
	"service-account-token-automount": "SA token auto-mounted, attacker auto-pivots",
}

// Apply returns a roasted copy of r; the input is not mutated. The
// returned Report owns a fresh Findings slice.
func Apply(r render.Report) render.Report {
	out := r
	out.Findings = make([]rules.Finding, len(r.Findings))
	for i, f := range r.Findings {
		f.Title = titleFor(f)
		out.Findings[i] = f
	}
	return out
}

// titleFor returns the roast title if one exists, else the original.
// Exposed for tests.
func titleFor(f rules.Finding) string {
	if alt, ok := titles[f.DetectorID]; ok {
		return alt
	}
	return f.Title
}
