// Package narrative builds the 2-sentence plain-English summary that
// goes above every PR comment's cost table. Readability win that
// surfaces "what changed and why" without the reader having to parse
// the diff first.
//
// The summary is LLM-generated in production (Haiku — small input,
// small output, deterministic-enough prompt). Tests + dev use the
// TemplateGenerator that produces a stable byte sequence with no
// network egress.
package narrative

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// Request bundles the data both Template and LLM generators need.
type Request struct {
	Workload          string
	BeforeCPUMilli    int64
	AfterCPUMilli     int64
	BeforeMemoryBytes int64
	AfterMemoryBytes  int64
	P95CPUMilli       int64 // 0 means no observed signal
	Confidence        string
	MonthlyUSDCents   int64
	Findings          []rules.Finding
}

// Generator returns a narrative for a Request. Implementations MUST
// produce a string that ends in a period and contains no markdown.
type Generator interface {
	Generate(ctx context.Context, t tenancy.Context, r Request) (string, error)
}

// TemplateGenerator builds a deterministic 2-sentence summary from
// the cost numbers + signals. No LLM call.
type TemplateGenerator struct{}

func (TemplateGenerator) Generate(_ context.Context, _ tenancy.Context, r Request) (string, error) {
	if r.Workload == "" {
		return "", errors.New("narrative: empty workload")
	}
	var first string
	switch {
	case r.AfterCPUMilli > 0 && r.BeforeCPUMilli > r.AfterCPUMilli:
		first = fmt.Sprintf(
			"This PR trims %s's CPU request from %s to %s.",
			r.Workload, formatCPU(r.BeforeCPUMilli), formatCPU(r.AfterCPUMilli),
		)
	case r.AfterMemoryBytes > 0 && r.BeforeMemoryBytes > r.AfterMemoryBytes:
		first = fmt.Sprintf(
			"This PR trims %s's memory request from %s to %s.",
			r.Workload, formatMemory(r.BeforeMemoryBytes), formatMemory(r.AfterMemoryBytes),
		)
	default:
		first = fmt.Sprintf("This PR proposes a configuration change on %s.", r.Workload)
	}
	second := buildSecond(r)
	return strings.TrimSpace(first+" "+second) + saving(r), nil
}

func buildSecond(r Request) string {
	switch {
	case r.P95CPUMilli > 0:
		return fmt.Sprintf(
			"Observed P95 over 30d sat at %s; we recommend the trimmed value with %s confidence",
			formatCPU(r.P95CPUMilli), strings.ToLower(strNoEmpty(r.Confidence, "medium")),
		)
	case r.Confidence != "":
		return fmt.Sprintf("Detector confidence: %s", strings.ToLower(r.Confidence))
	default:
		return "Apply to recover the savings shown below"
	}
}

func saving(r Request) string {
	if r.MonthlyUSDCents <= 0 {
		return "."
	}
	return fmt.Sprintf(", saving ~$%d/mo.", r.MonthlyUSDCents/100)
}

func formatCPU(milli int64) string {
	if milli >= 1000 && milli%1000 == 0 {
		return fmt.Sprintf("%d vCPU", milli/1000)
	}
	if milli >= 1000 {
		return fmt.Sprintf("%.1f vCPU", float64(milli)/1000.0)
	}
	return fmt.Sprintf("%dm", milli)
}

func formatMemory(bytes int64) string {
	const giB = int64(1) << 30
	const miB = int64(1) << 20
	if bytes >= giB {
		return fmt.Sprintf("%dGi", bytes/giB)
	}
	return fmt.Sprintf("%dMi", bytes/miB)
}

func strNoEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
