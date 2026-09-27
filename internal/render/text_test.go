package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/optiqor/optiqor-cli/internal/render/style"
	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// stripANSI removes basic ANSI/OSC sequences so assertions can check
// content irrespective of styling.
func stripANSI(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			out.WriteByte(s[i])
			i++
			continue
		}
		// CSI: ESC [ ... <letter>
		if i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		// OSC: ESC ] ... BEL or ESC \
		if i+1 < len(s) && s[i+1] == ']' {
			j := i + 2
			for j < len(s) && s[j] != 0x07 {
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					j += 2
					goto done
				}
				j++
			}
			if j < len(s) {
				j++ // consume BEL
			}
		done:
			i = j
			continue
		}
		// fallback: skip ESC + next char
		i += 2
	}
	return out.String()
}

func TestText_PlainAlwaysIncludesAccuracyDisclosure(t *testing.T) {
	cases := []Report{
		{Source: "empty", Workloads: 0},
		{Source: "with-findings", Workloads: 1, Findings: []rules.Finding{
			{DetectorID: "x", Workload: "api", Title: "Test finding", Severity: rules.SeverityHigh, Confidence: rules.ConfidenceHigh},
		}},
	}
	for _, r := range cases {
		var buf bytes.Buffer
		if err := Text(&buf, r, Options{Color: false}); err != nil {
			t.Fatalf("Text(%s): %v", r.Source, err)
		}
		if !strings.Contains(buf.String(), AccuracyDisclosure) {
			t.Fatalf("Text(%s): missing accuracy disclosure:\n%s", r.Source, buf.String())
		}
	}
}

func TestText_ColoredAlwaysIncludesAccuracyDisclosure(t *testing.T) {
	r := Report{Source: "x", Workloads: 1, Findings: []rules.Finding{
		{DetectorID: "x", Workload: "api", Title: "T", Severity: rules.SeverityHigh, Confidence: rules.ConfidenceHigh},
	}}
	var buf bytes.Buffer
	if err := Text(&buf, r, Options{Color: true}); err != nil {
		t.Fatal(err)
	}
	stripped := stripANSI(buf.String())
	if !strings.Contains(stripped, AccuracyDisclosure) {
		t.Fatalf("colored output missing disclosure (stripped):\n%s", stripped)
	}
}

func TestText_PlainNoANSI(t *testing.T) {
	r := Report{Source: "x", Workloads: 1, Findings: []rules.Finding{
		{DetectorID: "x", Workload: "api", Title: "T", Severity: rules.SeverityMed, Confidence: rules.ConfidenceMed, MonthlyUSDCents: 100},
	}}
	var buf bytes.Buffer
	_ = Text(&buf, r, Options{Color: false})
	if strings.Contains(buf.String(), "\x1b") {
		t.Fatalf("plain text output should not contain ANSI:\n%q", buf.String())
	}
}

func TestText_ColoredEmitsANSI(t *testing.T) {
	r := Report{Source: "x", Workloads: 1, Findings: []rules.Finding{
		{DetectorID: "x", Workload: "api", Title: "T", Severity: rules.SeverityHigh, Confidence: rules.ConfidenceHigh},
	}}
	var buf bytes.Buffer
	_ = Text(&buf, r, Options{Color: true})
	if !strings.Contains(buf.String(), "\x1b") {
		t.Fatalf("colored output should contain ANSI; got:\n%s", buf.String())
	}
}

func TestText_NoFindingsCelebrates(t *testing.T) {
	var buf bytes.Buffer
	r := Report{Source: "demo", Workloads: 5}
	if err := Text(&buf, r, Options{Color: false}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"5 workloads", "no cost waste detected", "Clean", "No findings"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestText_RendersFindings(t *testing.T) {
	var buf bytes.Buffer
	r := Report{
		Source:    "fixtures/basic-chart/values.yaml",
		Workloads: 2,
		Findings: []rules.Finding{
			{
				DetectorID:      "cpu-overprovisioned",
				Workload:        "api",
				Title:           "CPU request appears overprovisioned",
				Detail:          "Request 2 vs limit 2.5",
				MonthlyUSDCents: 12345,
				Severity:        rules.SeverityMed,
				Confidence:      rules.ConfidenceMed,
				Category:        rules.CategoryCost,
			},
			{
				DetectorID: "missing-memory-limit",
				Workload:   "worker",
				Title:      "Memory limit not set",
				Detail:     "Without a limit it can consume node memory unbounded.",
				Severity:   rules.SeverityHigh,
				Confidence: rules.ConfidenceHigh,
				Category:   rules.CategorySecurity,
			},
		},
	}
	if err := Text(&buf, r, Options{Color: false}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"optiqor",
		BrandTagline,
		"HIGH",
		"MED",
		"Memory limit not set",
		"CPU request appears overprovisioned",
		"$123.45",
		"optiqor.dev/get",
		"±40%",
		"confidence:",
		"Cost optimizations",
		"Security findings",
		"bonus",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestJSON_IncludesDisclosureAndShape(t *testing.T) {
	var buf bytes.Buffer
	r := Report{
		Source:    "demo",
		Workloads: 1,
		Findings: []rules.Finding{
			{DetectorID: "cpu-overprovisioned", Workload: "api", Title: "x", MonthlyUSDCents: 100, Severity: rules.SeverityMed, Confidence: rules.ConfidenceMed},
		},
	}
	if err := JSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	var got struct {
		AccuracyDisclosure string  `json:"accuracy_disclosure"`
		MonthlySavingsUSD  float64 `json:"monthly_savings_usd"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, buf.String())
	}
	if got.AccuracyDisclosure != AccuracyDisclosure {
		t.Errorf("disclosure missing or wrong: %q", got.AccuracyDisclosure)
	}
	if got.MonthlySavingsUSD != 1.0 {
		t.Errorf("monthly_savings_usd = %v, want 1.0", got.MonthlySavingsUSD)
	}
}

func TestFormatCents(t *testing.T) {
	cases := map[int64]string{
		0:     "0",
		1:     "0.01",
		100:   "1",
		12345: "123.45",
		99:    "0.99",
	}
	for in, want := range cases {
		if got := formatCents(in); got != want {
			t.Errorf("formatCents(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestPlural(t *testing.T) {
	if got := plural(1, "x", "xs"); got != "1 x" {
		t.Errorf("plural(1) = %q", got)
	}
	if got := plural(0, "x", "xs"); got != "0 xs" {
		t.Errorf("plural(0) = %q", got)
	}
	if got := plural(2, "x", "xs"); got != "2 xs" {
		t.Errorf("plural(2) = %q", got)
	}
}

func TestWrap(t *testing.T) {
	got := wrap("the quick brown fox jumps over the lazy dog", 12)
	if len(got) < 2 {
		t.Fatalf("expected wrap into multiple lines: %v", got)
	}
	for _, line := range got {
		if len(line) > 12 && !strings.Contains(line, " ") {
			// single long word is allowed to overflow, but the
			// general invariant is that wrapped lines stay ≤ width.
			continue
		}
		if len([]rune(line)) > 12+1 { // +1 slack for word boundary
			t.Errorf("wrap line too wide: %q", line)
		}
	}
}

func TestWrap_Empty(t *testing.T) {
	if got := wrap("", 10); got != nil {
		t.Errorf("wrap(empty) = %v, want nil", got)
	}
}

// The header rule once reused the padded badge style (two cells too wide,
// dark text with no background in colour mode), and every card was two
// cells wider than the terminal.
func TestWriteCostFinding_HeaderMatchesCardWidth(t *testing.T) {
	for _, tc := range []struct {
		name  string
		color bool
		sev   rules.Severity
	}{
		{"plain-med", false, rules.SeverityMed},
		{"color-high", true, rules.SeverityHigh},
		{"color-med", true, rules.SeverityMed},
		{"color-low", true, rules.SeverityLow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			writeCostFinding(&b, style.NewTheme(tc.color), rules.Finding{
				DetectorID:      "cpu-overprovisioned",
				Workload:        "api",
				Title:           "CPU request appears overprovisioned",
				Detail:          "Consider halving the request.",
				Severity:        tc.sev,
				Confidence:      rules.ConfidenceMed,
				MonthlyUSDCents: 2920,
			}, 80)
			lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
			header, bottom := lines[0], lines[len(lines)-1]
			if got, want := visibleRuneCount(header), visibleRuneCount(bottom); got != want {
				t.Errorf("header is %d cells, card is %d:\n%s\n%s", got, want, stripANSI(header), stripANSI(bottom))
			}
			for _, l := range lines {
				if n := visibleRuneCount(l); n > 80 {
					t.Errorf("card line is %d cells, wider than the 80-column terminal: %q", n, stripANSI(l))
					break
				}
			}
			if strings.Contains(header, "38;2;15;15;15") {
				t.Errorf("severity word drawn in the badge's dark foreground: %q", header)
			}
		})
	}
}
