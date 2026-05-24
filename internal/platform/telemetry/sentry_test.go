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

func TestReporter_Capture(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		wantCall int64
	}{
		{"swaps reporter and captures non-nil error", errors.New("boom"), 1},
		{"nil error is noop", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prev := Reporter()
			t.Cleanup(func() { SetReporter(prev) })

			rec := &recorder{}
			SetReporter(rec)
			Capture(context.Background(), tc.err, nil)
			if got := atomic.LoadInt64(&rec.calls); got != tc.wantCall {
				t.Errorf("calls = %d, want %d", got, tc.wantCall)
			}
		})
	}
}

func TestSetReporter_ReturnsPrevious(t *testing.T) {
	prev := Reporter()
	t.Cleanup(func() { SetReporter(prev) })

	rec := &recorder{}
	old := SetReporter(rec)
	if old == nil {
		t.Fatal("SetReporter should return the previous reporter")
	}
}

// Setting nil must fall back to a noop reporter so a misconfigured
// boot still lets Capture run without nil-deref panics.
func TestSetReporter_NilFallsBackToNoop(t *testing.T) {
	prev := Reporter()
	t.Cleanup(func() { SetReporter(prev) })

	SetReporter(nil)
	Capture(context.Background(), errors.New("x"), nil)
}

func TestNewSentryReporter(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  SentryConfig
	}{
		{"empty dsn not configured", SentryConfig{SampleRate: 1}},
		// Phase 1 stub: even a valid DSN returns ErrSentryNotConfigured
		// until Phase 5 wires the real Sentry SDK.
		{"dsn present still returns stub error", SentryConfig{DSN: "https://foo@sentry.io/123", SampleRate: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSentryReporter(tc.cfg)
			if !errors.Is(err, ErrSentryNotConfigured) {
				t.Errorf("expected ErrSentryNotConfigured, got %v", err)
			}
		})
	}
}

func TestSentryConfig_Validate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     SentryConfig
		wantErr bool
	}{
		{"max sample rate", SentryConfig{SampleRate: 1.0}, false},
		{"zero sample rate", SentryConfig{SampleRate: 0}, false},
		{"above 1 rejected", SentryConfig{SampleRate: 1.1}, true},
		{"negative rejected", SentryConfig{SampleRate: -0.1}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error for %+v", tc.cfg)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for %+v: %v", tc.cfg, err)
			}
		})
	}
}

func TestFlushReporter_Default(t *testing.T) {
	prev := Reporter()
	t.Cleanup(func() { SetReporter(prev) })
	SetReporter(NoopReporter())
	if !FlushReporter(100) {
		t.Error("noop FlushReporter should return true")
	}
}
