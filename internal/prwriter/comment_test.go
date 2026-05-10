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

func TestRender_DeterministicAcrossRuns(t *testing.T) {
	// PR-comment renders must be diff-stable so CI doesn't keep
	// re-posting "different" comments. Same input → identical bytes.
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
	a, _ := Render(sample(t1))
	b, _ := Render(sample(t2))
	if a != b {
		t.Errorf("same-minute renders should match: \n%s\nvs\n%s", a, b)
	}
}

func TestRender_CostFindingsLeadBiggestDollarFirst(t *testing.T) {
	now := time.Date(2026, 5, 11, 14, 30, 0, 0, time.UTC)
	out, err := Render(sample(now))
	if err != nil {
		t.Fatal(err)
	}
	cpuIdx := strings.Index(out, "CPU overprovisioned")
	memIdx := strings.Index(out, "Memory overprovisioned")
	if cpuIdx < 0 || memIdx < 0 {
		t.Fatalf("missing finding lines:\n%s", out)
	}
	if cpuIdx > memIdx {
		t.Errorf("CPU ($20/mo) should render before Memory ($9.20/mo)\nout:\n%s", out)
	}
}

func TestRender_SecurityIsBonusSection(t *testing.T) {
	now := time.Date(2026, 5, 11, 14, 30, 0, 0, time.UTC)
	out, err := Render(sample(now))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "### Security findings (bonus)") {
		t.Errorf("security section missing bonus marker:\n%s", out)
	}
	if !strings.Contains(out, "side-effect") {
		t.Errorf("missing bonus framing\n%s", out)
	}
	cost := strings.Index(out, "### Cost optimisations")
	sec := strings.Index(out, "### Security findings")
	if cost < 0 || sec < 0 || cost > sec {
		t.Errorf("cost section must precede security section")
	}
}

func TestRender_SecurityHiddenWhenToggleOff(t *testing.T) {
	now := time.Date(2026, 5, 11, 14, 30, 0, 0, time.UTC)
	c := sample(now)
	c.SecurityVisible = false
	out, _ := Render(c)
	if strings.Contains(out, "### Security findings") {
		t.Errorf("security section should not render when toggle off")
	}
}

func TestRender_SandboxDisclosureExactlyMatchesCLI(t *testing.T) {
	now := time.Date(2026, 5, 11, 14, 30, 0, 0, time.UTC)
	c := sample(now)
	c.Mode = ModeSandbox
	out, _ := Render(c)
	if !strings.Contains(out, AccuracyDisclosureSandbox) {
		t.Errorf("missing sandbox disclosure:\n%s", out)
	}
}

func TestRender_AgentModeShowsAgentDisclosure(t *testing.T) {
	now := time.Date(2026, 5, 11, 14, 30, 0, 0, time.UTC)
	c := sample(now)
	c.Mode = ModeAgent
	out, _ := Render(c)
	if !strings.Contains(out, "Agent accuracy: ±15%") {
		t.Errorf("missing agent disclosure:\n%s", out)
	}
	if strings.Contains(out, "±40%") {
		t.Errorf("agent mode rendered sandbox disclosure")
	}
}

func TestRender_NoSavingsShowsCleanMessage(t *testing.T) {
	now := time.Date(2026, 5, 11, 14, 30, 0, 0, time.UTC)
	c := sample(now)
	c.MonthlySavingsUSDCents = 0
	c.AnnualSavingsUSDCents = 0
	c.Findings = nil
	out, _ := Render(c)
	if !strings.Contains(out, "No cost optimisations detected") {
		t.Errorf("clean message missing:\n%s", out)
	}
}

func TestFmtUSD(t *testing.T) {
	cases := map[int64]string{0: "$0", 100: "$1", 12345: "$123.45", 5: "$0.05"}
	for in, want := range cases {
		if got := fmtUSD(in); got != want {
			t.Errorf("fmtUSD(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestRender_ApplyFixURL_LinkShownWhenPresent(t *testing.T) {
	now := time.Date(2026, 5, 11, 14, 30, 0, 0, time.UTC)
	c := sample(now)
	c.ApplyFixURL = "https://github.com/acme/api/pull/42"
	out, _ := Render(c)
	if !strings.Contains(out, "Apply Fix PR") {
		t.Errorf("apply-fix link missing:\n%s", out)
	}
}

func TestRender_OmitsApplyFixURL_WhenAbsent(t *testing.T) {
	now := time.Date(2026, 5, 11, 14, 30, 0, 0, time.UTC)
	c := sample(now)
	c.ApplyFixURL = ""
	out, _ := Render(c)
	if strings.Contains(out, "Apply Fix PR") {
		t.Errorf("apply-fix link rendered when URL absent")
	}
}
