// Package prwriter renders the Markdown PR comment Optiqor posts on
// every Helm/Kustomize PR. Pure functions over [Comment]; no GitHub
// API calls. Findings are stable-sorted so the same input renders
// byte-identical output across runs.
package prwriter

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// AccuracyDisclosureSandbox must match the CLI verbatim — keep both
// strings in lockstep so users see the same language end-to-end.
const AccuracyDisclosureSandbox = "Sandbox accuracy: ±40%. Install the Optiqor agent for exact numbers (optiqor.dev/get)."

const AccuracyDisclosureAgent = "Agent accuracy: ±15%. Backed by 30 days of Prometheus + your AWS bill."

// Mode selects which accuracy disclosure renders.
type Mode int

const (
	ModeSandbox Mode = iota
	ModeAgent
)

type Comment struct {
	Chart                  string
	Tenant                 string
	Workloads              int
	Findings               []rules.Finding
	MonthlySavingsUSDCents int64
	AnnualSavingsUSDCents  int64
	Mode                   Mode
	GeneratedAt            time.Time
	OptiqorAnalysisURL     string
	ApplyFixURL            string
	// UnifiedDiff renders inside a collapsed <details> block when set.
	// Render does not truncate — cap upstream if the diff is large.
	UnifiedDiff string
	// Narrative is the 1-2 sentence plain-English summary above the
	// cost table; empty falls back to the deterministic savings line.
	Narrative string
	// ProvisionerNote is the node-provisioner advisory pin
	// (provisioner.AdvisoryNote output). Empty for T1 (Karpenter)
	// because the recommendation is high-confidence by itself; non-empty
	// for T2/T3 to set merge-time expectations.
	ProvisionerNote string
	// SecurityVisible defaults off; customers opt in after they've
	// cleaned up cost.
	SecurityVisible bool
}

var ErrNoChart = errors.New("prwriter: chart name required")

func Render(c Comment) (string, error) {
	if c.Chart == "" {
		return "", ErrNoChart
	}
	cost, security := split(c.Findings)
	sortedCost := sortCostForDisplay(cost)
	view := view{
		Chart:              c.Chart,
		Workloads:          c.Workloads,
		CostFindings:       sortedCost,
		CostCount:          len(sortedCost),
		SecurityFindings:   security,
		SecurityCount:      len(security),
		ShowSecurity:       c.SecurityVisible && len(security) > 0,
		MonthlyUSD:         fmtUSD(c.MonthlySavingsUSDCents),
		AnnualUSD:          fmtUSD(c.AnnualSavingsUSDCents),
		ShowSavings:        c.MonthlySavingsUSDCents > 0,
		Narrative:          c.Narrative,
		ProvisionerNote:    c.ProvisionerNote,
		AccuracyDisclosure: AccuracyDisclosureSandbox,
		OptiqorAnalysisURL: c.OptiqorAnalysisURL,
		ApplyFixURL:        c.ApplyFixURL,
		GeneratedAtISO:     generatedAt(c.GeneratedAt).Format(time.RFC3339),
	}
	if c.Mode == ModeAgent {
		view.AccuracyDisclosure = AccuracyDisclosureAgent
	}
	if d := strings.TrimSpace(c.UnifiedDiff); d != "" {
		view.UnifiedDiff = d
		view.DiffFence = diffFence(d)
		view.DiffLines = countLines(d)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, view); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n") + "\n", nil
}

type view struct {
	Chart              string
	Workloads          int
	CostFindings       []rules.Finding
	CostCount          int
	SecurityFindings   []rules.Finding
	SecurityCount      int
	ShowSecurity       bool
	MonthlyUSD         string
	AnnualUSD          string
	ShowSavings        bool
	Narrative          string
	ProvisionerNote    string
	UnifiedDiff        string
	DiffFence          string
	DiffLines          int
	AccuracyDisclosure string
	OptiqorAnalysisURL string
	ApplyFixURL        string
	GeneratedAtISO     string
}

// diffFence returns a backtick fence one character longer than any
// run of backticks inside the diff body. Keeps a values.yaml fragment
// that uses ``` from breaking the surrounding code block.
func diffFence(s string) string {
	longest, cur := 0, 0
	for _, r := range s {
		if r == '`' {
			cur++
			if cur > longest {
				longest = cur
			}
		} else {
			cur = 0
		}
	}
	return strings.Repeat("`", longest+3)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// generatedAt truncates to the minute so renders stay diff-stable
// across CI reruns within the same minute.
func generatedAt(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC().Truncate(time.Minute)
	}
	return t.UTC().Truncate(time.Minute)
}

func split(in []rules.Finding) (cost, sec []rules.Finding) {
	cost = make([]rules.Finding, 0, len(in))
	sec = make([]rules.Finding, 0, len(in))
	for _, f := range in {
		if f.Category == rules.CategorySecurity {
			sec = append(sec, f)
		} else {
			cost = append(cost, f)
		}
	}
	return
}

func sortCostForDisplay(in []rules.Finding) []rules.Finding {
	out := make([]rules.Finding, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.MonthlyUSDCents > 0) != (b.MonthlyUSDCents > 0) {
			return a.MonthlyUSDCents > b.MonthlyUSDCents
		}
		if a.MonthlyUSDCents != b.MonthlyUSDCents {
			return a.MonthlyUSDCents > b.MonthlyUSDCents
		}
		if a.Workload != b.Workload {
			return a.Workload < b.Workload
		}
		return a.DetectorID < b.DetectorID
	})
	return out
}

func fmtUSD(cents int64) string {
	if cents == 0 {
		return "$0"
	}
	dollars := cents / 100
	c := cents % 100
	if c == 0 {
		return fmt.Sprintf("$%d", dollars)
	}
	return fmt.Sprintf("$%d.%02d", dollars, c)
}

const tmplBody = `## Optiqor analysis — {{.Chart}}

{{ if .ShowSavings -}}
**Potential savings:** {{.MonthlyUSD}} / month · ~{{.AnnualUSD}} / year
{{- else -}}
**No cost optimisations detected** — this chart is already clean.
{{- end }}

{{ if .Narrative -}}
{{.Narrative}}

{{ end -}}
_Workloads analysed: {{.Workloads}}._

{{ if .CostFindings -}}
<details open>
<summary><b>Cost optimisations</b> · {{.CostCount}} finding{{ if ne .CostCount 1 }}s{{ end }}{{ if .ShowSavings }} · save {{.MonthlyUSD}}/mo{{ end }}</summary>

| Severity | Workload | Title | Save / mo |
| --- | --- | --- | --- |
{{ range .CostFindings -}}
| {{.Severity}} | {{.Workload}} | {{.Title}} | {{ if gt .MonthlyUSDCents 0 }}save ~${{ printf "%d.%02d" (divCents .MonthlyUSDCents 100) (modCents .MonthlyUSDCents 100) }}{{ else }}—{{ end }} |
{{ end }}
</details>
{{- end }}

{{ if .UnifiedDiff -}}
<details open>
<summary><b>Apply Fix preview</b> · {{.DiffLines}}-line <code>values.yaml</code> diff</summary>

{{.DiffFence}}diff
{{.UnifiedDiff}}
{{.DiffFence}}
</details>
{{- end }}

{{ if .ShowSecurity -}}
<details>
<summary><b>Security findings</b> (bonus) · {{.SecurityCount}}</summary>

_Spotted while parsing your chart. Cost is the headline; this is a side-effect._

| Severity | Workload | Title |
| --- | --- | --- |
{{ range .SecurityFindings -}}
| {{.Severity}} | {{.Workload}} | {{.Title}} |
{{ end }}
</details>
{{- end }}

---

{{ if .ProvisionerNote -}}
> **Provisioner context** — {{.ProvisionerNote}}

{{ end -}}
> {{.AccuracyDisclosure}}

{{ if .OptiqorAnalysisURL -}}
[Full analysis →]({{.OptiqorAnalysisURL}}){{ if .ApplyFixURL }} · [Apply Fix PR →]({{.ApplyFixURL}}){{ end }}
{{ end }}
<sub>Generated at {{.GeneratedAtISO}} by optiqor.dev</sub>
`

var tmpl = template.Must(template.
	New("comment").
	Funcs(template.FuncMap{
		"divCents": func(a, b int64) int64 { return a / b },
		"modCents": func(a, b int64) int64 { return a % b },
	}).
	Parse(tmplBody))
