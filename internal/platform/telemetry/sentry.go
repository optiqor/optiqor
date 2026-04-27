package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrorReporter sends panics and unhandled errors to a remote sink
// (Sentry in production). Phase 1 ships a no-op implementation; the
// real getsentry/sentry-go adapter swaps in via SetReporter() at boot
// once SEVRO_SENTRY_DSN is non-empty.
//
// The contract is intentionally narrow so domain code only ever
// touches Capture and Flush; the adapter handles request enrichment,
// PII redaction, and tenant tagging.
type ErrorReporter interface {
	// Capture records err with optional structured tags. Tags must
	// not contain PII; the redaction policy lives in the adapter.
	Capture(ctx context.Context, err error, tags map[string]string)

	// Flush waits up to timeout for in-flight events to be sent.
	// Used during graceful shutdown.
	Flush(timeoutMs int) bool
}

// NoopReporter is the safe default. Calls have no effect; never
// returns an error. Production binaries replace it via SetReporter()
// once Sentry is configured.
func NoopReporter() ErrorReporter { return noopReporter{} }

type noopReporter struct{}

func (noopReporter) Capture(_ context.Context, _ error, _ map[string]string) {}
func (noopReporter) Flush(_ int) bool                                        { return true }

// reporterMu guards the package-global reporter. Domain code calls
// Capture() at top level; tests can swap reporters via SetReporter.
var (
	reporterMu      sync.RWMutex
	currentReporter ErrorReporter = NoopReporter()
)

// SetReporter swaps the global ErrorReporter. Returns the previous
// reporter so callers can restore it (useful in tests).
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

// Reporter returns the current global ErrorReporter.
func Reporter() ErrorReporter {
	reporterMu.RLock()
	defer reporterMu.RUnlock()
	return currentReporter
}

// Capture is a top-level convenience that uses the global reporter.
func Capture(ctx context.Context, err error, tags map[string]string) {
	if err == nil {
		return
	}
	Reporter().Capture(ctx, err, tags)
}

// FlushReporter waits for the current reporter to drain. Returns
// true on success.
func FlushReporter(timeoutMs int) bool {
	return Reporter().Flush(timeoutMs)
}

// SentryConfig is the minimal config the eventual sentry-go adapter
// requires. Phase 1 surface; the adapter reads this at boot.
type SentryConfig struct {
	DSN         string
	Environment string  // dev / staging / prod
	Release     string  // e.g. "api@1.4.2"
	SampleRate  float64 // 0..1; default 1
}

// Validate returns an error if the config is unusable. Empty DSN is
// valid (means "no Sentry"); sample rate out of range is not.
func (c SentryConfig) Validate() error {
	if c.SampleRate < 0 || c.SampleRate > 1 {
		return fmt.Errorf("sentry: sample rate %v out of [0,1]", c.SampleRate)
	}
	return nil
}

// ErrSentryNotConfigured is returned by NewSentryReporter when no DSN
// is supplied. Caller should fall back to the noop reporter.
var ErrSentryNotConfigured = errors.New("sentry: DSN not configured")

// NewSentryReporter builds a real Sentry-backed reporter. Phase 1
// returns ErrSentryNotConfigured for any non-empty DSN so the api/worker
// boot path compiles without pulling in sentry-go; the Phase 5 adapter
// replaces this body.
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
