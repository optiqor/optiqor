package analyze

import (
	"fmt"
	"sort"

	"github.com/optiqor/optiqor-cli/internal/render"
	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// findingKey uniquely identifies a finding for cross-chart matching.
// Two findings match when they share the same workload name and
// detector ID — regardless of severity, detail, or dollar estimate,
// which may legitimately differ between chart variants.
type findingKey struct {
	Workload   string
	DetectorID string
}

// CompareReport is the output of ComparePaths / Compare.
type CompareReport struct {
	A string `json:"a"` // label / path of the first chart
	B string `json:"b"` // label / path of the second chart

	// Cost totals (sum of MonthlyUSDCents across all findings).
	CostA int64 `json:"cost_a_monthly_usd_cents"`
	CostB int64 `json:"cost_b_monthly_usd_cents"`

	// Winner is "a", "b", or "tie".
	Winner string `json:"winner"`

	// OnlyInA are findings that appear in A but not in B (same workload+detector).
	OnlyInA []rules.Finding `json:"only_in_a"`
	// OnlyInB are findings that appear in B but not in A.
	OnlyInB []rules.Finding `json:"only_in_b"`
	// InBoth are findings present in both charts (matched by workload+detector).
	// The Finding values come from chart A.
	InBoth []rules.Finding `json:"in_both"`

	// ReportA / ReportB hold the full per-chart reports so callers
	// can access workload counts, source paths, etc.
	ReportA render.Report `json:"report_a"`
	ReportB render.Report `json:"report_b"`
}

// Compare runs analysis on both readers and partitions findings.
func Compare(a, b render.Report) CompareReport {
	// Build a set of finding keys for each chart.
	setA := make(map[findingKey]rules.Finding, len(a.Findings))
	for _, f := range a.Findings {
		setA[findingKey{f.Workload, f.DetectorID}] = f
	}
	setB := make(map[findingKey]rules.Finding, len(b.Findings))
	for _, f := range b.Findings {
		setB[findingKey{f.Workload, f.DetectorID}] = f
	}

	var onlyA, onlyB, both []rules.Finding

	for k, f := range setA {
		if _, ok := setB[k]; ok {
			both = append(both, f)
		} else {
			onlyA = append(onlyA, f)
		}
	}
	for k, f := range setB {
		if _, ok := setA[k]; !ok {
			onlyB = append(onlyB, f)
		}
	}

	// Sort all three slices for deterministic output.
	sortFindings(onlyA)
	sortFindings(onlyB)
	sortFindings(both)

	var costA, costB int64
	for _, f := range a.Findings {
		costA += f.MonthlyUSDCents
	}
	for _, f := range b.Findings {
		costB += f.MonthlyUSDCents
	}

	winner := declareWinner(a, b, costA, costB)

	return CompareReport{
		A:       a.Source,
		B:       b.Source,
		CostA:   costA,
		CostB:   costB,
		Winner:  winner,
		OnlyInA: onlyA,
		OnlyInB: onlyB,
		InBoth:  both,
		ReportA: a,
		ReportB: b,
	}
}

// ComparePaths opens both paths, runs analysis, and returns the CompareReport.
func ComparePaths(a, b string) (CompareReport, error) {
	repA, err := RunPath(a)
	if err != nil {
		return CompareReport{}, fmt.Errorf("compare: analyze %s: %w", a, err)
	}
	repB, err := RunPath(b)
	if err != nil {
		return CompareReport{}, fmt.Errorf("compare: analyze %s: %w", b, err)
	}
	return Compare(repA, repB), nil
}

// declareWinner picks the better chart. Lower cost wins; ties go to
// fewer HIGH findings; absolute ties are reported as "tie".
func declareWinner(a, b render.Report, costA, costB int64) string {
	if costA < costB {
		return "a"
	}
	if costB < costA {
		return "b"
	}
	// Costs equal — compare HIGH finding counts.
	highA, highB := countHigh(a.Findings), countHigh(b.Findings)
	if highA < highB {
		return "a"
	}
	if highB < highA {
		return "b"
	}
	return "tie"
}

func countHigh(findings []rules.Finding) int {
	n := 0
	for _, f := range findings {
		if f.Severity == rules.SeverityHigh {
			n++
		}
	}
	return n
}

func sortFindings(fs []rules.Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Workload != fs[j].Workload {
			return fs[i].Workload < fs[j].Workload
		}
		return fs[i].DetectorID < fs[j].DetectorID
	})
}
