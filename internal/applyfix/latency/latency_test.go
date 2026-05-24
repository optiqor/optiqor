package latency

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/platform/telemetry"
)

func TestRecorder_RegistersOneHistogramPerStep(t *testing.T) {
	reg := telemetry.NewRegistry()
	r := NewRecorder(reg)
	for _, s := range AllSteps {
		if r.hists[s] == nil {
			t.Errorf("missing histogram for step %q", s)
		}
	}
}

func TestRecorder_ObserveRecordsLatency(t *testing.T) {
	reg := telemetry.NewRegistry()
	r := NewRecorder(reg)
	r.Observe(StepCompose, 250*time.Millisecond)
	snap := r.hists[StepCompose].Snapshot()
	if snap.Count != 1 {
		t.Errorf("Count = %d, want 1", snap.Count)
	}
	if snap.Sum < 0.249 || snap.Sum > 0.251 {
		t.Errorf("Sum = %f, want ~0.25", snap.Sum)
	}
}

func TestRecorder_TimePropagatesErrorAndStillRecords(t *testing.T) {
	reg := telemetry.NewRegistry()
	r := NewRecorder(reg)
	want := errors.New("compose failed")
	err := r.Time(context.Background(), StepCompose, func() error {
		time.Sleep(5 * time.Millisecond)
		return want
	})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
	if r.hists[StepCompose].Snapshot().Count != 1 {
		t.Errorf("histogram should record even on error")
	}
}

func TestRecorder_NilSafe(t *testing.T) {
	var r *Recorder
	r.Observe(StepCompose, time.Second) // must not panic
}

func TestRecorder_UnknownStepIsNoop(t *testing.T) {
	reg := telemetry.NewRegistry()
	r := NewRecorder(reg)
	r.Observe(Step("not-a-step"), time.Second) // must not panic + must not register a new histogram
	for _, s := range AllSteps {
		if r.hists[s].Snapshot().Count != 0 {
			t.Errorf("histogram %q recorded unexpectedly", s)
		}
	}
}
