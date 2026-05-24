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
	view := view{
		Chart:              c.Chart,
		Workloads:          c.Workloads,
		CostFindings:       sortCostForDisplay(cost),
		SecurityFindings:   security,
		ShowSecurity:       c.SecurityVisible && len(security) > 0,
		MonthlyUSD:         fmtUSD(c.MonthlySavingsUSDCents),
		AnnualUSD:          fmtUSD(c.AnnualSavingsUSDCents),
		ShowSavings:        c.MonthlySavingsUSDCents > 0,
		AccuracyDisclosure: AccuracyDisclosureSandbox,
		OptiqorAnalysisURL: c.OptiqorAnalysisURL,
		ApplyFixURL:        c.ApplyFixURL,
		GeneratedAtISO:     generatedAt(c.GeneratedAt).Format(time.RFC3339),
	}
	if c.Mode == ModeAgent {
		view.AccuracyDisclosure = AccuracyDisclosureAgent
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
	SecurityFindings   []rules.Finding
	ShowSecurity       bool
	MonthlyUSD         string
	AnnualUSD          string
	ShowSavings        bool
	AccuracyDisclosure string
	OptiqorAnalysisURL string
	ApplyFixURL        string
	GeneratedAtISO     string
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

_Workloads analysed: {{.Workloads}}._

{{ if .CostFindings -}}
### Cost optimisations
| Severity | Workload | Title | Save / mo |
| --- | --- | --- | --- |
{{ range .CostFindings -}}
| {{.Severity}} | {{.Workload}} | {{.Title}} | {{ if gt .MonthlyUSDCents 0 }}save ~${{ printf "%d.%02d" (divCents .MonthlyUSDCents 100) (modCents .MonthlyUSDCents 100) }}{{ else }}—{{ end }} |
{{ end }}
{{- end }}

{{ if .ShowSecurity -}}
### Security findings (bonus)
_Spotted while parsing your chart. Cost is the headline; this is a side-effect._

| Severity | Workload | Title |
| --- | --- | --- |
{{ range .SecurityFindings -}}
| {{.Severity}} | {{.Workload}} | {{.Title}} |
{{ end }}
{{- end }}

---

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
