package gate

import (
	"context"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestRenderValidator_Validate(t *testing.T) {
	chart := strings.Join([]string{
		"api:",
		"  resources:",
		"    requests:",
		"      cpu: \"2\"",
		"      memory: 4Gi",
		"    limits:",
		"      cpu: \"4\"",
		"      memory: 8Gi",
		"",
	}, "\n")

	goodDiff := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -3,3 +3,3 @@",
		"     requests:",
		"-      cpu: \"2\"",
		"+      cpu: \"1\"",
		"       memory: 4Gi",
	}, "\n")

	badYAMLDiff := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -3,3 +3,3 @@",
		"     requests:",
		"-      cpu: \"2\"",
		"+      cpu: { not closed",
		"       memory: 4Gi",
	}, "\n")

	contextMissDiff := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -3,3 +3,3 @@",
		"     nonsense:",
		"-      cpu: \"2\"",
		"+      cpu: \"1\"",
		"       memory: 4Gi",
	}, "\n")

	ctx := context.Background()
	tnt := tenancy.Context{TenantID: "t-1"}
	v := RenderValidator{}

	for _, tc := range []struct {
		name     string
		cand     Candidate
		wantStat Status
		errSub   string
	}{
		{name: "happy path applies cleanly", cand: Candidate{ChartYAML: chart, UnifiedDiff: goodDiff}, wantStat: StatusPassed},
		{name: "empty diff fails", cand: Candidate{ChartYAML: chart}, wantStat: StatusFailed, errSub: "empty diff"},
		{name: "empty chart fails", cand: Candidate{UnifiedDiff: goodDiff}, wantStat: StatusFailed, errSub: "empty chart"},
		{name: "context miss fails", cand: Candidate{ChartYAML: chart, UnifiedDiff: contextMissDiff}, wantStat: StatusFailed, errSub: "diff did not apply"},
		{name: "post-diff yaml invalid fails", cand: Candidate{ChartYAML: chart, UnifiedDiff: badYAMLDiff}, wantStat: StatusFailed, errSub: "post-diff yaml invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := v.Validate(ctx, tnt, tc.cand)
			if got.Status != tc.wantStat {
				t.Fatalf("status = %s, want %s (detail=%q err=%v)", got.Status, tc.wantStat, got.Detail, got.Err)
			}
			if tc.errSub != "" && !strings.Contains(got.Detail, tc.errSub) {
				t.Errorf("detail %q missing %q", got.Detail, tc.errSub)
			}
		})
	}
}

func TestApplyUnifiedDiff_HandlesMultipleHunks(t *testing.T) {
	src := strings.Join([]string{"a", "b", "c", "d", "e", "f", "g"}, "\n")
	diff := strings.Join([]string{
		"--- a/file",
		"+++ b/file",
		"@@ -1,3 +1,3 @@",
		" a",
		"-b",
		"+B",
		" c",
		"@@ -5,3 +5,3 @@",
		" e",
		"-f",
		"+F",
		" g",
	}, "\n")
	got, err := applyUnifiedDiff(src, diff)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{"a", "B", "c", "d", "e", "F", "g"}, "\n")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
