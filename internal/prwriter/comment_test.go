package prwriter

import (
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"
)

func sample(now time.Time) Comment {
	return Comment{
		Chart:                  "charts/api",
		Tenant:                 "tenant-abc",
		Workloads:              3,
		MonthlySavingsUSDCents: 2920,
		AnnualSavingsUSDCents:  35040,
		GeneratedAt:            now,
		OptiqorAnalysisURL:     "https://optiqor.dev/r/abc123",
		SecurityVisible:        true,
		Findings: []rules.Finding{
			{DetectorID: "memory-overprovisioned", Workload: "api", Title: "Memory overprovisioned", Severity: rules.SeverityMed, MonthlyUSDCents: 920, Category: rules.CategoryCost},
			{DetectorID: "cpu-overprovisioned", Workload: "api", Title: "CPU overprovisioned", Severity: rules.SeverityMed, MonthlyUSDCents: 2000, Category: rules.CategoryCost},
			{DetectorID: "run-as-root", Workload: "worker", Title: "Container runs as root", Severity: rules.SeverityHigh, Category: rules.CategorySecurity},
		},
	}
}

func TestRender_NoChart_Errors(t *testing.T) {
	if _, err := Render(Comment{}); err == nil {
		t.Error("want error when chart missing")
	}
}

func TestRender(t *testing.T) {
	now := time.Date(2026, 5, 11, 14, 30, 0, 0, time.UTC)

	for _, tc := range []struct {
		name   string
		mut    func(*Comment)
		assert func(t *testing.T, out string)
	}{
		{
			name: "cost-findings-lead-biggest-dollar-first",
			assert: func(t *testing.T, out string) {
				t.Helper()
				cpuIdx := strings.Index(out, "CPU overprovisioned")
				memIdx := strings.Index(out, "Memory overprovisioned")
				if cpuIdx < 0 || memIdx < 0 {
					t.Fatalf("missing finding lines:\n%s", out)
				}
				if cpuIdx > memIdx {
					t.Errorf("CPU ($20/mo) should render before Memory ($9.20/mo)\nout:\n%s", out)
				}
			},
		},
		{
			name: "security-is-bonus-section",
			assert: func(t *testing.T, out string) {
				t.Helper()
				if !strings.Contains(out, "Security findings</b> (bonus)") {
					t.Errorf("security section missing bonus marker:\n%s", out)
				}
				if !strings.Contains(out, "side-effect") {
					t.Errorf("missing bonus framing\n%s", out)
				}
				cost := strings.Index(out, "Cost optimisations</b>")
				sec := strings.Index(out, "Security findings</b>")
				if cost < 0 || sec < 0 || cost > sec {
					t.Errorf("cost section must precede security section\n%s", out)
				}
			},
		},
		{
			name: "security-hidden-when-toggle-off",
			mut:  func(c *Comment) { c.SecurityVisible = false },
			assert: func(t *testing.T, out string) {
				t.Helper()
				if strings.Contains(out, "Security findings</b>") {
					t.Errorf("security section should not render when toggle off")
				}
			},
		},
		{
			name: "cost-section-is-collapsible-details",
			assert: func(t *testing.T, out string) {
				t.Helper()
				if !strings.Contains(out, "<details open>\n<summary><b>Cost optimisations</b>") {
					t.Errorf("cost table not wrapped in default-open details:\n%s", out)
				}
			},
		},
		{
			name: "diff-preview-renders-when-set",
			mut: func(c *Comment) {
				c.UnifiedDiff = "--- a/values.yaml\n+++ b/values.yaml\n@@ -1,3 +1,3 @@\n-cpu: \"2\"\n+cpu: \"1500m\""
			},
			assert: func(t *testing.T, out string) {
				t.Helper()
				if !strings.Contains(out, "Apply Fix preview</b>") {
					t.Errorf("diff preview summary missing:\n%s", out)
				}
				if !strings.Contains(out, "```diff\n--- a/values.yaml") {
					t.Errorf("diff body missing inside fence:\n%s", out)
				}
			},
		},
		{
			name: "diff-section-omitted-when-no-diff",
			assert: func(t *testing.T, out string) {
				t.Helper()
				if strings.Contains(out, "Apply Fix preview") {
					t.Errorf("diff section should not render when UnifiedDiff empty")
				}
			},
		},
		{
			name: "diff-fence-escapes-embedded-backticks",
			mut: func(c *Comment) {
				c.UnifiedDiff = "@@ -1 +1 @@\n-name: ```literal```\n+name: ````literal````"
			},
			assert: func(t *testing.T, out string) {
				t.Helper()
				if !strings.Contains(out, "`````diff") {
					t.Errorf("fence must be 5 backticks to escape 4-backtick body:\n%s", out)
				}
			},
		},
		{
			name: "narrative-renders-above-workloads",
			mut: func(c *Comment) {
				c.Narrative = "Trim api's CPU request from 2 to 1.5 vCPU based on 30d P95 of 1.2."
			},
			assert: func(t *testing.T, out string) {
				t.Helper()
				nIdx := strings.Index(out, "Trim api's CPU")
				wIdx := strings.Index(out, "Workloads analysed")
				if nIdx < 0 || wIdx < 0 || nIdx > wIdx {
					t.Errorf("narrative must render above workloads line:\n%s", out)
				}
			},
		},
		{
			name: "sandbox-disclosure-matches-cli",
			mut:  func(c *Comment) { c.Mode = ModeSandbox },
			assert: func(t *testing.T, out string) {
				t.Helper()
				if !strings.Contains(out, AccuracyDisclosureSandbox) {
					t.Errorf("missing sandbox disclosure:\n%s", out)
				}
			},
		},
		{
			name: "agent-mode-shows-agent-disclosure",
			mut:  func(c *Comment) { c.Mode = ModeAgent },
			assert: func(t *testing.T, out string) {
				t.Helper()
				if !strings.Contains(out, "Agent accuracy: ±15%") {
					t.Errorf("missing agent disclosure:\n%s", out)
				}
				if strings.Contains(out, "±40%") {
					t.Errorf("agent mode rendered sandbox disclosure")
				}
			},
		},
		{
			name: "no-savings-shows-clean-message",
			mut: func(c *Comment) {
				c.MonthlySavingsUSDCents = 0
				c.AnnualSavingsUSDCents = 0
				c.Findings = nil
			},
			assert: func(t *testing.T, out string) {
				t.Helper()
				if !strings.Contains(out, "No cost optimisations detected") {
					t.Errorf("clean message missing:\n%s", out)
				}
			},
		},
		{
			name: "apply-fix-link-shown-when-present",
			mut:  func(c *Comment) { c.ApplyFixURL = "https://github.com/acme/api/pull/42" },
			assert: func(t *testing.T, out string) {
				t.Helper()
				if !strings.Contains(out, "Apply Fix PR") {
					t.Errorf("apply-fix link missing:\n%s", out)
				}
			},
		},
		{
			name: "apply-fix-link-omitted-when-absent",
			mut:  func(c *Comment) { c.ApplyFixURL = "" },
			assert: func(t *testing.T, out string) {
				t.Helper()
				if strings.Contains(out, "Apply Fix PR") {
					t.Errorf("apply-fix link rendered when URL absent")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := sample(now)
			if tc.mut != nil {
				tc.mut(&c)
			}
			out, err := Render(c)
			if err != nil {
				t.Fatal(err)
			}
			tc.assert(t, out)
		})
	}
}

// Non-determinism here makes CI re-post "different" comments on every rerun.
func TestRender_DeterministicAcrossRuns(t *testing.T) {
	now := time.Date(2026, 5, 11, 14, 30, 0, 0, time.UTC)
	a, err := Render(sample(now))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(sample(now))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("Render is non-deterministic")
	}
}

func TestRender_GeneratedAtTruncatedToMinute(t *testing.T) {
	t1 := time.Date(2026, 5, 11, 14, 30, 15, 0, time.UTC)
	t2 := time.Date(2026, 5, 11, 14, 30, 45, 0, time.UTC)
	a, err := Render(sample(t1))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(sample(t2))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("same-minute renders should match: \n%s\nvs\n%s", a, b)
	}
}

func TestFmtUSD(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   int64
		want string
	}{
		{name: "zero", in: 0, want: "$0"},
		{name: "one-dollar", in: 100, want: "$1"},
		{name: "fractional-dollars", in: 12345, want: "$123.45"},
		{name: "sub-dollar-cents", in: 5, want: "$0.05"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmtUSD(tc.in); got != tc.want {
				t.Errorf("fmtUSD(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
