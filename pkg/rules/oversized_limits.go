package rules

import (
	"fmt"

	"github.com/optiqor/optiqor-cli/pkg/parser"
)

// oversizedCPULimit and oversizedMemoryLimit fire when a workload's
// limit crosses an absolute threshold (4 vCPU, 16 GiB respectively).
// Above these thresholds the scheduler's bin-packing options shrink
// dramatically — only large nodes can host the pod, which raises the
// cluster's minimum-node-size and prevents Spot-instance binning.
//
// We pick an absolute threshold (rather than a request-relative one)
// because the scheduling effect kicks in regardless of utilisation.

const (
	oversizedCPULimitMillicores = 4000                    // 4 vCPU
	oversizedMemoryLimitBytes   = 16 * 1024 * 1024 * 1024 // 16 GiB
)

type oversizedCPULimit struct{}

func newOversizedCPULimit() Detector { return oversizedCPULimit{} }

func (oversizedCPULimit) ID() string   { return "oversized-cpu-limit" }
func (oversizedCPULimit) Name() string { return "Oversized CPU limit" }

func (oversizedCPULimit) Run(w parser.Workload) []Finding {
	if !w.Limits.CPU.Set || w.Limits.CPU.Value < oversizedCPULimitMillicores {
		return nil
	}
	return []Finding{{
		DetectorID: "oversized-cpu-limit",
		Workload:   w.Name,
		Title:      "CPU limit forces large-node scheduling",
		Detail:     fmt.Sprintf("CPU limit is %s. Above 4 vCPU, the pod can only land on large nodes; smaller / Spot instance types are excluded from bin-packing. Either split the workload or confirm the high limit is justified by P99.", w.Limits.CPU),
		Severity:   SeverityMed,
		Confidence: ConfidenceMed,
		Signal: &Signal{
			Label:       "CPU",
			Have:        float64(w.Requests.CPU.Value),
			Want:        float64(w.Limits.CPU.Value),
			HaveDisplay: w.Requests.CPU.String(),
			WantDisplay: w.Limits.CPU.String(),
			Note:        "limit > 4 vCPU",
		},
	}}
}

type oversizedMemoryLimit struct{}

func newOversizedMemoryLimit() Detector { return oversizedMemoryLimit{} }

func (oversizedMemoryLimit) ID() string   { return "oversized-memory-limit" }
func (oversizedMemoryLimit) Name() string { return "Oversized memory limit" }

func (oversizedMemoryLimit) Run(w parser.Workload) []Finding {
	if !w.Limits.Memory.Set || w.Limits.Memory.Value < oversizedMemoryLimitBytes {
		return nil
	}
	return []Finding{{
		DetectorID: "oversized-memory-limit",
		Workload:   w.Name,
		Title:      "Memory limit forces large-node scheduling",
		Detail:     fmt.Sprintf("Memory limit is %s. Above 16 GiB, the pod can only land on memory-class nodes; balanced / Spot bin-packing is excluded. Confirm the workload genuinely uses this much, or split the workload.", w.Limits.Memory),
		Severity:   SeverityMed,
		Confidence: ConfidenceMed,
		Signal: &Signal{
			Label:       "memory",
			Have:        float64(w.Requests.Memory.Value),
			Want:        float64(w.Limits.Memory.Value),
			HaveDisplay: w.Requests.Memory.String(),
			WantDisplay: w.Limits.Memory.String(),
			Note:        "limit > 16 GiB",
		},
	}}
}
