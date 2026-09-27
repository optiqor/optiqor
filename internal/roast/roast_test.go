package roast

import (
	"strings"
	"testing"

	"github.com/optiqor/optiqor-cli/internal/render"
	"github.com/optiqor/optiqor-cli/pkg/rules"
)

func TestApply(t *testing.T) {
	for _, tc := range []struct {
		name  string
		in    render.Report
		check func(t *testing.T, in, out render.Report)
	}{
		{
			name: "rewrites-known-titles",
			in: render.Report{
				Findings: []rules.Finding{
					{DetectorID: "cpu-overprovisioned", Title: "CPU request appears overprovisioned"},
					{DetectorID: "missing-memory-limit", Title: "Memory limit not set"},
				},
			},
			check: func(t *testing.T, in, out render.Report) {
				t.Helper()
				if out.Findings[0].Title == in.Findings[0].Title {
					t.Errorf("cpu-overprovisioned title should have been rewritten")
				}
				if out.Findings[1].Title == in.Findings[1].Title {
					t.Errorf("missing-memory-limit title should have been rewritten")
				}
			},
		},
		{
			name: "leaves-unknown-title-alone",
			in: render.Report{
				Findings: []rules.Finding{
					{DetectorID: "future-detector-not-yet-roasted", Title: "Some title"},
				},
			},
			check: func(t *testing.T, _, out render.Report) {
				t.Helper()
				if out.Findings[0].Title != "Some title" {
					t.Errorf("unknown detector title was rewritten: %q", out.Findings[0].Title)
				}
			},
		},
		{
			name: "does-not-mutate-input",
			in: render.Report{
				Findings: []rules.Finding{
					{DetectorID: "cpu-overprovisioned", Title: "original"},
				},
			},
			check: func(t *testing.T, in, _ render.Report) {
				t.Helper()
				if in.Findings[0].Title != "original" {
					t.Errorf("input mutated: %q != %q", in.Findings[0].Title, "original")
				}
			},
		},
		{
			// Hard rule: only Title changes. Detail, MonthlyUSDCents,
			// Severity, Confidence, Category, Signal must round-trip.
			name: "preserves-material-fields",
			in: render.Report{
				Findings: []rules.Finding{{
					DetectorID:      "cpu-overprovisioned",
					Workload:        "api",
					Title:           "CPU request appears overprovisioned",
					Detail:          "Request 2 vs limit 2.5",
					MonthlyUSDCents: 12345,
					Severity:        rules.SeverityMed,
					Confidence:      rules.ConfidenceMed,
					Category:        rules.CategoryCost,
					Signal: &rules.Signal{
						Label: "CPU", Have: 2, Want: 2.5,
						HaveDisplay: "2", WantDisplay: "2.5", Note: "80% of limit",
					},
				}},
			},
			check: func(t *testing.T, _, out render.Report) {
				t.Helper()
				f := out.Findings[0]
				if f.Detail != "Request 2 vs limit 2.5" {
					t.Errorf("Detail rewritten")
				}
				if f.MonthlyUSDCents != 12345 {
					t.Errorf("MonthlyUSDCents changed")
				}
				if f.Severity != rules.SeverityMed {
					t.Errorf("Severity changed")
				}
				if f.Confidence != rules.ConfidenceMed {
					t.Errorf("Confidence changed")
				}
				if f.Category != rules.CategoryCost {
					t.Errorf("Category changed")
				}
				if f.Signal == nil || f.Signal.Note != "80% of limit" {
					t.Errorf("Signal lost or rewritten")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := Apply(tc.in)
			tc.check(t, tc.in, out)
		})
	}
}

func TestTagline_AndFooter_NotEmpty(t *testing.T) {
	if strings.TrimSpace(Tagline) == "" {
		t.Error("Tagline empty")
	}
	if strings.TrimSpace(FooterQuip) == "" {
		t.Error("FooterQuip empty")
	}
}

// A detector added without a roast line silently falls back to its plain
// title under --roast.
func TestTitles_CoverAllDetectors(t *testing.T) {
	for _, d := range rules.All() {
		t.Run(d.ID(), func(t *testing.T) {
			if _, ok := titles[d.ID()]; !ok {
				t.Errorf("no roast title for detector %q", d.ID())
			}
		})
	}
}
