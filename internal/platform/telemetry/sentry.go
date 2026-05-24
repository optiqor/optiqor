package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrorReporter sends panics and unhandled errors to Sentry in prod.
// PII redaction and tenant tagging live in the adapter, not at call
// sites — Capture tags must already be PII-clean.
type ErrorReporter interface {
	Capture(ctx context.Context, err error, tags map[string]string)
	// Flush waits up to timeoutMs during graceful shutdown.
	Flush(timeoutMs int) bool
}

// NoopReporter is the safe default. Prod swaps it via SetReporter()
// once OPTIQOR_SENTRY_DSN is non-empty.
func NoopReporter() ErrorReporter { return noopReporter{} }

type noopReporter struct{}

func (noopReporter) Capture(_ context.Context, _ error, _ map[string]string) {}
func (noopReporter) Flush(_ int) bool                                        { return true }

var (
	reporterMu      sync.RWMutex
	currentReporter = NoopReporter()
)

// SetReporter swaps the global reporter and returns the previous one
// so tests can restore.
func SetReporter(r ErrorReporter) ErrorReporter {
	if r == nil {
		r = NoopReporter()
	}
	reporterMu.Lock()
	prev := currentReporter
	currentReporter = r
	reporterMu.Unlock()
	return prev
}

func Reporter() ErrorReporter {
	reporterMu.RLock()
	defer reporterMu.RUnlock()
	return currentReporter
}

func Capture(ctx context.Context, err error, tags map[string]string) {
	if err == nil {
		return
	}
	Reporter().Capture(ctx, err, tags)
}

func FlushReporter(timeoutMs int) bool {
	return Reporter().Flush(timeoutMs)
}

type SentryConfig struct {
	DSN         string
	Environment string  // dev / staging / prod
	Release     string  // e.g. "api@1.4.2"
	SampleRate  float64 // 0..1
}

// Validate accepts empty DSN ("no Sentry"); rejects sample rate
// outside [0,1].
func (c SentryConfig) Validate() error {
	if c.SampleRate < 0 || c.SampleRate > 1 {
		return fmt.Errorf("sentry: sample rate %v out of [0,1]", c.SampleRate)
	}
	return nil
}

// ErrSentryNotConfigured signals callers to fall back to NoopReporter.
var ErrSentryNotConfigured = errors.New("sentry: DSN not configured")

// NewSentryReporter is a Phase 1 stub: returns ErrSentryNotConfigured
// even with a non-empty DSN so api/worker boot compiles without pulling
// in sentry-go. Phase 5 replaces the body.
func NewSentryReporter(cfg SentryConfig) (ErrorReporter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.DSN == "" {
		return nil, ErrSentryNotConfigured
	}
	// TODO(phase-5): import getsentry/sentry-go, init the SDK with
	// PII-redaction transport, tenant-tagging via beforeSend hook.
	return nil, ErrSentryNotConfigured
}
