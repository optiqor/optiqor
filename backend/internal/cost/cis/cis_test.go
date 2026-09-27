package cis

import "testing"

func TestControls(t *testing.T) {
	for _, tc := range []struct {
		detector string
		want     []string
	}{
		{detector: "run-as-root", want: []string{"5.7.4"}},
		{detector: "privileged-container", want: []string{"5.2.1"}},
		{detector: "host-network", want: []string{"5.2.4"}},
		{detector: "allow-privilege-escalation", want: []string{"5.2.5"}},
		{detector: "capabilities-not-dropped-all", want: []string{"5.2.8"}},
		{detector: "read-only-root-fs-missing", want: []string{"5.7.3"}},
		{detector: "image-pinned-latest", want: []string{"5.1.4"}},
		{detector: "missing-cpu-limit", want: []string{"5.7.2"}},
		{detector: "missing-memory-limit", want: []string{"5.7.2"}},
		{detector: "cpu-overprovisioned", want: nil},
		{detector: "memory-overprovisioned", want: nil},
		{detector: "unknown-detector", want: nil},
	} {
		t.Run(tc.detector, func(t *testing.T) {
			got := Controls(tc.detector)
			if len(got) != len(tc.want) {
				t.Fatalf("Controls(%q) = %v, want %v", tc.detector, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("Controls(%q)[%d] = %q, want %q", tc.detector, i, got[i], tc.want[i])
				}
			}
			if Has(tc.detector) != (tc.want != nil) {
				t.Errorf("Has(%q) = %v, want %v", tc.detector, Has(tc.detector), tc.want != nil)
			}
		})
	}
}

func TestControls_ReturnedSliceIsACopy(t *testing.T) {
	got := Controls("run-as-root")
	if len(got) == 0 {
		t.Fatal("expected at least one control")
	}
	got[0] = "tampered"
	again := Controls("run-as-root")
	if again[0] == "tampered" {
		t.Error("Controls returns a shared slice — package map can be mutated by callers")
	}
}
