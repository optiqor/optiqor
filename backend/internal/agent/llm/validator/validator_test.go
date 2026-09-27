package validator

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	wellFormed := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -1,3 +1,3 @@",
		" api:",
		"-  cpu: \"2\"",
		"+  cpu: \"1\"",
	}, "\n")

	zeroReplicas := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -1,2 +1,2 @@",
		"-replicaCount: 3",
		"+replicaCount: 0",
	}, "\n")

	zeroCPU := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -1,2 +1,2 @@",
		"-cpu: \"2\"",
		"+cpu: \"0\"",
	}, "\n")

	missingHunk := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"some prose",
	}, "\n")

	for _, tc := range []struct {
		name     string
		diff     string
		rejected bool
		codes    []string
	}{
		{name: "well-formed passes", diff: wellFormed, rejected: false},
		{name: "zero replicas rejected", diff: zeroReplicas, rejected: true, codes: []string{"replicas-zero"}},
		{name: "zero cpu rejected", diff: zeroCPU, rejected: true, codes: []string{"resource-zero"}},
		{name: "missing hunk rejected", diff: missingHunk, rejected: true, codes: []string{"diff-no-hunk"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Validate("", tc.diff, Options{})
			if r.Rejected != tc.rejected {
				t.Errorf("Rejected = %v, want %v (issues=%+v)", r.Rejected, tc.rejected, r.Issues)
			}
			if len(tc.codes) > 0 {
				codes := make(map[string]bool, len(r.Issues))
				for _, is := range r.Issues {
					codes[is.Code] = true
				}
				for _, want := range tc.codes {
					if !codes[want] {
						t.Errorf("issue code %q not found in %+v", want, r.Issues)
					}
				}
			}
		})
	}
}
