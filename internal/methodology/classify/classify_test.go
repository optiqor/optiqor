package classify

import (
	"errors"
	"testing"
	"time"
)

func TestSandboxClassifier_Classify(t *testing.T) {
	samples := []Sample{
		{Time: time.Unix(0, 0), Value: 100},
		{Time: time.Unix(60, 0), Value: 200},
		{Time: time.Unix(120, 0), Value: 50},
	}
	for _, tc := range []struct {
		name        string
		in          []Sample
		wantClass   Class
		wantSamples int
		wantErr     error
	}{
		{
			name:        "any-input-returns-steady",
			in:          samples,
			wantClass:   ClassSteady,
			wantSamples: 3,
		},
		{
			name:    "empty-input-returns-insufficient-data",
			in:      nil,
			wantErr: ErrInsufficientData,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := SandboxClassifier{}
			r, err := c.Classify(tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r.Class != tc.wantClass {
				t.Errorf("class = %s, want %s", r.Class, tc.wantClass)
			}
			if r.SampleCount != tc.wantSamples {
				t.Errorf("sample count = %d, want %d", r.SampleCount, tc.wantSamples)
			}
		})
	}
}
