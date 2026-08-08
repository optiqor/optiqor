package rules

import "github.com/optiqor/optiqor-cli/pkg/parser"

// cpuWithoutMemoryRequest and memoryWithoutCPURequest fire when only
// one half of `requests` is declared. Half-declared requests put the
// pod in BestEffort QoS for the missing dimension, which:
//
//   - Removes scheduler bin-packing accuracy (the missing dimension
//     can't be reserved against), so the cluster systematically
//     under- or over-provisions
//   - Makes the pod first to be evicted under pressure on the
//     missing dimension
//
// Most charts want both dimensions or neither. Surfacing the asymmetry
// catches accidents where someone wrote one and forgot the other.

type cpuWithoutMemoryRequest struct{}

func newCPUWithoutMemoryRequest() Detector { return cpuWithoutMemoryRequest{} }

func (cpuWithoutMemoryRequest) ID() string   { return "cpu-without-memory-request" }
func (cpuWithoutMemoryRequest) Name() string { return "CPU request set without memory" }

func (cpuWithoutMemoryRequest) Run(w parser.Workload) []Finding {
	if !w.Requests.CPU.Set || w.Requests.Memory.Set {
		return nil
	}
	return []Finding{{
		DetectorID: "cpu-without-memory-request",
		Workload:   w.Name,
		Title:      "CPU request set without memory request",
		Detail:     "requests.cpu is set but requests.memory is not. The scheduler can't reserve memory accurately, so the pod is BestEffort for memory — first to evict under pressure. Add a memory request matching observed P95.",
		Severity:   SeverityLow,
		Confidence: ConfidenceHigh,
		Signal: &Signal{
			Label:       "memory",
			Have:        0,
			Want:        0,
			HaveDisplay: "unset",
			WantDisplay: "required",
			Note:        "memory request missing",
		},
	}}
}

type memoryWithoutCPURequest struct{}

func newMemoryWithoutCPURequest() Detector { return memoryWithoutCPURequest{} }

func (memoryWithoutCPURequest) ID() string   { return "memory-without-cpu-request" }
func (memoryWithoutCPURequest) Name() string { return "Memory request set without CPU" }

func (memoryWithoutCPURequest) Run(w parser.Workload) []Finding {
	if !w.Requests.Memory.Set || w.Requests.CPU.Set {
		return nil
	}
	return []Finding{{
		DetectorID: "memory-without-cpu-request",
		Workload:   w.Name,
		Title:      "Memory request set without CPU request",
		Detail:     "requests.memory is set but requests.cpu is not. The scheduler can't reserve CPU, so bin-packing assumes zero — pods can stack onto a single node and starve each other. Add a CPU request matching observed P95.",
		Severity:   SeverityLow,
		Confidence: ConfidenceHigh,
		Signal: &Signal{
			Label:       "CPU",
			Have:        0,
			Want:        0,
			HaveDisplay: "unset",
			WantDisplay: "required",
			Note:        "CPU request missing",
		},
	}}
}
