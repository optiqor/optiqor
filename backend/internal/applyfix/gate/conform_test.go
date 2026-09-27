package gate

import (
	"context"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestConformValidator_Validate(t *testing.T) {
	chart := strings.Join([]string{
		"api:",
		"  image: nginx:1.27",
		"  resources:",
		"    requests:",
		"      cpu: \"2\"",
		"      memory: 4Gi",
		"",
	}, "\n")

	safeReduction := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -4,3 +4,3 @@",
		"     requests:",
		"-      cpu: \"2\"",
		"+      cpu: \"1\"",
		"       memory: 4Gi",
	}, "\n")

	dropsImage := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -2,3 +2,2 @@",
		" api:",
		"-  image: nginx:1.27",
		"   resources:",
	}, "\n")

	ctx := context.Background()
	tnt := tenancy.Context{TenantID: "t-c"}

	for _, tc := range []struct {
		name     string
		v        ConformValidator
		cand     Candidate
		wantStat Status
		errSub   string
	}{
		{
			name:     "passes when required keys survive",
			v:        ConformValidator{RequiredKeys: []string{"image", "resources"}},
			cand:     Candidate{ChartYAML: chart, UnifiedDiff: safeReduction},
			wantStat: StatusPassed,
		},
		{
			name:     "fails when required key dropped",
			v:        ConformValidator{RequiredKeys: []string{"image"}},
			cand:     Candidate{ChartYAML: chart, UnifiedDiff: dropsImage},
			wantStat: StatusFailed,
			errSub:   "required key removed",
		},
		{
			name:     "empty inputs fail",
			v:        ConformValidator{},
			cand:     Candidate{},
			wantStat: StatusFailed,
			errSub:   "empty input",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.v.Validate(ctx, tnt, tc.cand)
			if got.Status != tc.wantStat {
				t.Fatalf("status=%s, want %s (detail=%q err=%v)", got.Status, tc.wantStat, got.Detail, got.Err)
			}
			if tc.errSub != "" && !strings.Contains(got.Detail, tc.errSub) {
				t.Errorf("detail %q missing %q", got.Detail, tc.errSub)
			}
		})
	}
}

func TestCheckAPIVersionKindShape_RejectsEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   map[string]any
		err  string
	}{
		{name: "empty apiVersion", in: map[string]any{"apiVersion": "", "kind": "Deployment"}, err: "apiVersion is empty"},
		{name: "empty kind", in: map[string]any{"apiVersion": "apps/v1", "kind": ""}, err: "kind is empty"},
		{name: "well formed", in: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkAPIVersionKindShape(tc.in)
			if tc.err == "" {
				if err != nil {
					t.Errorf("unexpected err: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("err = %v, want substring %q", err, tc.err)
			}
		})
	}
}
