package telemetry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type recorder struct {
	calls int64
}

func (r *recorder) Capture(_ context.Context, _ error, _ map[string]string) {
	atomic.AddInt64(&r.calls, 1)
}
func (r *recorder) Flush(_ int) bool { return true }

func TestNoopReporter_DoesNothing(t *testing.T) {
	r := NoopReporter()
	r.Capture(context.Background(), errors.New("x"), nil)
	if !r.Flush(100) {
		t.Error("noop Flush should always succeed")
	}
}

func TestSetReporter_Swaps(t *testing.T) {
	prev := Reporter()
	defer SetReporter(prev) // restore for other tests

	rec := &recorder{}
	old := SetReporter(rec)
	if old == nil {
		t.Fatal("SetReporter should return the previous reporter")
	}
	Capture(context.Background(), errors.New("boom"), nil)
	if got := atomic.LoadInt64(&rec.calls); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestCapture_NilErrorIsNoop(t *testing.T) {
	prev := Reporter()
	defer SetReporter(prev)

	rec := &recorder{}
	SetReporter(rec)
	Capture(context.Background(), nil, nil)
	if got := atomic.LoadInt64(&rec.calls); got != 0 {
		t.Errorf("nil error should not capture; got %d calls", got)
	}
}

func TestSetReporter_NilFallsBackToNoop(_ *testing.T) {
	prev := Reporter()
	defer SetReporter(prev)

	SetReporter(nil)
	// Should not panic; the captured event simply goes nowhere.
	Capture(context.Background(), errors.New("x"), nil)
}

func TestSentryConfig_Validate(t *testing.T) {
	cases := []struct {
		c       SentryConfig
		wantErr bool
	}{
		{SentryConfig{SampleRate: 1.0}, false},
		{SentryConfig{SampleRate: 0}, false},
		{SentryConfig{SampleRate: 1.1}, true},
		{SentryConfig{SampleRate: -0.1}, true},
	}
	for _, tc := range cases {
		err := tc.c.Validate()
		if tc.wantErr && err == nil {
			t.Errorf("expected error for %+v", tc.c)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("unexpected error for %+v: %v", tc.c, err)
		}
	}
}

func TestNewSentryReporter_EmptyDSNNotConfigured(t *testing.T) {
	_, err := NewSentryReporter(SentryConfig{SampleRate: 1})
	if !errors.Is(err, ErrSentryNotConfigured) {
		t.Errorf("expected ErrSentryNotConfigured, got %v", err)
	}
}

func TestNewSentryReporter_DSNAlsoStubReturnsErrInPhase1(t *testing.T) {
	// Phase 1 stub still returns ErrSentryNotConfigured even with a
	// DSN. Tests guard the contract until Phase 5 lands.
	_, err := NewSentryReporter(SentryConfig{DSN: "https://foo@sentry.io/123", SampleRate: 1})
	if !errors.Is(err, ErrSentryNotConfigured) {
		t.Errorf("expected ErrSentryNotConfigured (Phase 1 stub), got %v", err)
	}
}

func TestFlushReporter_Default(t *testing.T) {
	prev := Reporter()
	defer SetReporter(prev)
	SetReporter(NoopReporter())
	if !FlushReporter(100) {
		t.Error("noop FlushReporter should return true")
	}
}
