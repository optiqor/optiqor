package classify

import (
	"errors"
	"testing"
	"time"
)

func TestSandboxClassifier_ReturnsSteadyForAnyInput(t *testing.T) {
	c := SandboxClassifier{}
	samples := []Sample{
		{Time: time.Unix(0, 0), Value: 100},
		{Time: time.Unix(60, 0), Value: 200},
		{Time: time.Unix(120, 0), Value: 50},
	}
	r, err := c.Classify(samples)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Class != ClassSteady {
		t.Errorf("class = %s, want %s", r.Class, ClassSteady)
	}
	if r.SampleCount != 3 {
		t.Errorf("sample count = %d, want 3", r.SampleCount)
	}
}

func TestSandboxClassifier_EmptyInputReturnsInsufficientData(t *testing.T) {
	c := SandboxClassifier{}
	_, err := c.Classify(nil)
	if !errors.Is(err, ErrInsufficientData) {
		t.Fatalf("err = %v, want ErrInsufficientData", err)
	}
}
