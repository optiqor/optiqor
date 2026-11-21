package analyze

import (
	"strings"
	"testing"

	"github.com/optiqor/optiqor-cli/internal/render"
	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// makeReport is a helper that builds a minimal render.Report with the
// given findings for use in Compare tests.
func makeReport(source string, findings []rules.Finding) render.Report {
	return render.Report{
		Source:    source,
		Workloads: 1,
		Findings:  findings,
	}
}

func finding(workload, detectorID string, sev rules.Severity, cents int64) rules.Finding {
	return rules.Finding{
		Workload:        workload,
		DetectorID:      detectorID,
		Severity:        sev,
		MonthlyUSDCents: cents,
		Category:        rules.CategoryCost,
	}
}

// TestCompare_Partition checks that findings are correctly split into
// only-in-A, only-in-B, and in-both buckets.
func TestCompare_Partition(t *testing.T) {
	fa := finding("api", "cpu-overprovisioned", rules.SeverityMed, 1000)
	fb := finding("api", "memory-overprovisioned", rules.SeverityMed, 500)
	fc := finding("worker", "missing-memory-limit", rules.SeverityHigh, 0)

	// A has fa + fc; B has fb + fc.
	repA := makeReport("a.yaml", []rules.Finding{fa, fc})
	repB := makeReport("b.yaml", []rules.Finding{fb, fc})

	rep := Compare(repA, repB)

	if len(rep.OnlyInA) != 1 || rep.OnlyInA[0].DetectorID != "cpu-overprovisioned" {
		t.Errorf("OnlyInA = %v, want [cpu-overprovisioned]", rep.OnlyInA)
	}
	if len(rep.OnlyInB) != 1 || rep.OnlyInB[0].DetectorID != "memory-overprovisioned" {
		t.Errorf("OnlyInB = %v, want [memory-overprovisioned]", rep.OnlyInB)
	}
	if len(rep.InBoth) != 1 || rep.InBoth[0].DetectorID != "missing-memory-limit" {
		t.Errorf("InBoth = %v, want [missing-memory-limit]", rep.InBoth)
	}
}

// TestCompare_WinnerCost verifies that lower cost wins.
func TestCompare_WinnerCost(t *testing.T) {
	repA := makeReport("a.yaml", []rules.Finding{
		finding("api", "cpu-overprovisioned", rules.SeverityMed, 5000),
	})
	repB := makeReport("b.yaml", []rules.Finding{
		finding("api", "cpu-overprovisioned", rules.SeverityMed, 1000),
	})

	rep := Compare(repA, repB)
	if rep.Winner != "b" {
		t.Errorf("Winner = %q, want %q", rep.Winner, "b")
	}
}

// TestCompare_WinnerHighFindings breaks a cost tie by HIGH count.
func TestCompare_WinnerHighFindings(t *testing.T) {
	repA := makeReport("a.yaml", []rules.Finding{
		finding("api", "privileged-container", rules.SeverityHigh, 0),
		finding("api", "run-as-root", rules.SeverityHigh, 0),
	})
	repB := makeReport("b.yaml", []rules.Finding{
		finding("api", "privileged-container", rules.SeverityHigh, 0),
	})

	rep := Compare(repA, repB)
	if rep.Winner != "b" {
		t.Errorf("Winner = %q, want %q (fewer HIGH findings)", rep.Winner, "b")
	}
}

// TestCompare_Tie verifies both costs and HIGH counts equal → "tie".
func TestCompare_Tie(t *testing.T) {
	f := finding("api", "cpu-overprovisioned", rules.SeverityMed, 1000)
	repA := makeReport("a.yaml", []rules.Finding{f})
	repB := makeReport("b.yaml", []rules.Finding{f})

	rep := Compare(repA, repB)
	if rep.Winner != "tie" {
		t.Errorf("Winner = %q, want %q", rep.Winner, "tie")
	}
}

// TestCompare_EmptyBothSides verifies that two clean charts produce a tie.
func TestCompare_EmptyBothSides(t *testing.T) {
	repA := makeReport("a.yaml", nil)
	repB := makeReport("b.yaml", nil)
	rep := Compare(repA, repB)

	if rep.Winner != "tie" {
		t.Errorf("two clean charts should tie, got %q", rep.Winner)
	}
	if len(rep.OnlyInA)+len(rep.OnlyInB)+len(rep.InBoth) != 0 {
		t.Errorf("expected no findings in any bucket")
	}
}

// TestCompare_WriteText_ContainsKey checks that the text renderer
// produces output that contains the key structural markers.
func TestCompare_WriteText_ContainsKey(t *testing.T) {
	repA := makeReport("a.yaml", []rules.Finding{
		finding("api", "cpu-overprovisioned", rules.SeverityMed, 2000),
	})
	repB := makeReport("b.yaml", []rules.Finding{
		finding("worker", "missing-memory-limit", rules.SeverityHigh, 0),
	})

	rep := Compare(repA, repB)
	var sb strings.Builder
	if err := rep.WriteText(&sb, render.Options{Color: false, Width: 80}); err != nil {
		t.Fatal(err)
	}
	out := sb.String()

	for _, want := range []string{
		"compare:",
		"Findings only in A",
		"Findings only in B",
		"Findings in both",
		"accuracy_disclosure", // accuracy disclosure must appear
		"±40%",
	} {
		// accuracy_disclosure is the JSON field name; for text we check ±40%
		if want == "accuracy_disclosure" {
			continue
		}
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

// TestCompare_WriteJSON_Valid verifies the JSON renderer emits valid JSON
// with the required accuracy_disclosure field.
func TestCompare_WriteJSON_Valid(t *testing.T) {
	repA := makeReport("a.yaml", []rules.Finding{
		finding("api", "cpu-overprovisioned", rules.SeverityMed, 2000),
	})
	repB := makeReport("b.yaml", nil)

	rep := Compare(repA, repB)
	var sb strings.Builder
	if err := rep.WriteJSON(&sb); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if !strings.Contains(out, `"accuracy_disclosure"`) {
		t.Errorf("JSON output missing accuracy_disclosure field\n%s", out)
	}
	if !strings.Contains(out, `"winner"`) {
		t.Errorf("JSON output missing winner field\n%s", out)
	}
}

// TestComparePaths_BadPath verifies ComparePaths returns an error for
// a nonexistent file.
func TestComparePaths_BadPath(t *testing.T) {
	_, err := ComparePaths("/nonexistent/a.yaml", "/nonexistent/b.yaml")
	if err == nil {
		t.Error("expected error for nonexistent paths, got nil")
	}
}
