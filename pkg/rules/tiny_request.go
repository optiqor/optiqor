package rules

import (
	"fmt"

	"github.com/optiqor/optiqor-cli/pkg/parser"
)

// tinyCPURequest and tinyMemoryRequest fire when a workload sets
// requests so small they're almost certainly placeholders. They
// surface as LOW: probably a typo or copy-paste from a Helm scaffold,
// not a deliberate choice. Catching them early prevents charts from
// shipping with effectively-zero scheduling weight.

const (
	tinyCPUMillicores = 10               // 10m
	tinyMemoryBytes   = 32 * 1024 * 1024 // 32 MiB
)

type tinyCPURequest struct{}

func newTinyCPURequest() Detector { return tinyCPURequest{} }

func (tinyCPURequest) ID() string   { return "tiny-cpu-request" }
func (tinyCPURequest) Name() string { return "Suspiciously small CPU request" }

func (tinyCPURequest) Run(w parser.Workload) []Finding {
	if !w.Requests.CPU.Set {
		return nil
	}
	if w.Requests.CPU.Value == 0 || w.Requests.CPU.Value >= tinyCPUMillicores {
		return nil
	}
	return []Finding{{
		DetectorID: "tiny-cpu-request",
		Workload:   w.Name,
		Title:      "CPU request below the placeholder threshold",
		Detail:     fmt.Sprintf("requests.cpu is %s — below the 10m threshold most charts use as a sentinel. Probably a placeholder from a Helm scaffold. Set it to your observed P95 (or remove the limit-without-request asymmetry the scheduler is currently dealing with).", w.Requests.CPU),
		Severity:   SeverityLow,
		Confidence: ConfidenceHigh,
		Signal: &Signal{
			Label:       "CPU",
			Have:        float64(w.Requests.CPU.Value),
			Want:        float64(tinyCPUMillicores),
			HaveDisplay: w.Requests.CPU.String(),
			WantDisplay: "10m",
			Note:        "below 10m sentinel",
		},
	}}
}

type tinyMemoryRequest struct{}

func newTinyMemoryRequest() Detector { return tinyMemoryRequest{} }

func (tinyMemoryRequest) ID() string   { return "tiny-memory-request" }
func (tinyMemoryRequest) Name() string { return "Suspiciously small memory request" }

func (tinyMemoryRequest) Run(w parser.Workload) []Finding {
	if !w.Requests.Memory.Set {
		return nil
	}
	if w.Requests.Memory.Value == 0 || w.Requests.Memory.Value >= tinyMemoryBytes {
		return nil
	}
	return []Finding{{
		DetectorID: "tiny-memory-request",
		Workload:   w.Name,
		Title:      "Memory request below the placeholder threshold",
		Detail:     fmt.Sprintf("requests.memory is %s — below 32 MiB. A workload that genuinely needs less memory is a rarity; most charts setting tiny memory requests are using a placeholder. Set it to your observed P95.", w.Requests.Memory),
		Severity:   SeverityLow,
		Confidence: ConfidenceHigh,
		Signal: &Signal{
			Label:       "memory",
			Have:        float64(w.Requests.Memory.Value),
			Want:        float64(tinyMemoryBytes),
			HaveDisplay: w.Requests.Memory.String(),
			WantDisplay: "32Mi",
			Note:        "below 32Mi sentinel",
		},
	}}
}
