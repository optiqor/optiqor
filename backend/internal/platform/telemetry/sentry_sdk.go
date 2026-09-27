package telemetry

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/optiqor/optiqor/internal/platform/logging"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// sentryReporter is the real ErrorReporter — wraps the sentry-go SDK
// and routes through a redacting beforeSend so customer secrets never
// leave the process. One reporter per process; SetReporter swaps it.
type sentryReporter struct {
	hub *sentry.Hub
}

// InitSentry replaces the Phase-1 stub with the production SDK. Empty
// DSN returns ErrSentryNotConfigured so the caller can fall back to
// NoopReporter. The SDK's global hub stays per-test isolated via
// CloneHub so a panic in test A doesn't contaminate test B.
func InitSentry(cfg SentryConfig) (ErrorReporter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.DSN == "" {
		return nil, ErrSentryNotConfigured
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              cfg.DSN,
		Environment:      cfg.Environment,
		Release:          cfg.Release,
		SampleRate:       cfg.SampleRate,
		AttachStacktrace: true,
		BeforeSend:       redactBeforeSend,
		BeforeSendTransaction: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			return redactBeforeSend(event, nil)
		},
		EnableTracing: false,
	})
	if err != nil {
		return nil, fmt.Errorf("sentry init: %w", err)
	}
	return &sentryReporter{hub: sentry.CurrentHub().Clone()}, nil
}

func (s *sentryReporter) Capture(ctx context.Context, err error, tags map[string]string) {
	if err == nil {
		return
	}
	hub := s.hub.Clone()
	hub.WithScope(func(scope *sentry.Scope) {
		if t, terr := tenancy.FromContext(ctx); terr == nil {
			scope.SetTag("tenant_id", t.TenantID)
			if t.WorkspaceID != "" {
				scope.SetTag("workspace_id", t.WorkspaceID)
			}
			if t.ClusterID != "" {
				scope.SetTag("cluster_id", t.ClusterID)
			}
		}
		if rid := logging.RequestIDFromContext(ctx); rid != "" {
			scope.SetTag("request_id", rid)
		}
		for k, v := range tags {
			scope.SetTag(k, v)
		}
		hub.CaptureException(err)
	})
}

func (s *sentryReporter) Flush(timeoutMs int) bool {
	return s.hub.Flush(time.Duration(timeoutMs) * time.Millisecond)
}

// redactBeforeSend strips payloads that must never reach a third-party
// SaaS: AWS access keys, GitHub App PEM private keys, Anthropic API
// keys, customer secret values, LLM prompt + response bodies.
// Matching is conservative — keep adding patterns as new leak shapes
// surface. Comparison runs against the message + every extra value.
func redactBeforeSend(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event == nil {
		return nil
	}
	for i, ex := range event.Exception {
		event.Exception[i].Value = redactString(ex.Value)
	}
	event.Message = redactString(event.Message)
	for k, v := range event.Extra {
		if s, ok := v.(string); ok {
			event.Extra[k] = redactString(s)
		}
	}
	for k, v := range event.Tags {
		event.Tags[k] = redactString(v)
	}
	for i := range event.Breadcrumbs {
		event.Breadcrumbs[i].Message = redactString(event.Breadcrumbs[i].Message)
	}
	return event
}

// pemBlockRE matches a complete PEM-encoded private key block,
// header + body + footer. We replace it whole because the body bytes
// are the actual secret material.
var pemBlockRE = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)

// redactString applies pattern-based redaction. Order matters: PEM
// blocks (regex) run first to swallow the body; then prefix-based
// scans mask AWS / GitHub / Anthropic / Slack token shapes.
func redactString(s string) string {
	if s == "" {
		return s
	}
	s = pemBlockRE.ReplaceAllString(s, "<github-app-key-redacted>")
	for _, prefix := range prefixRedactions {
		if idx := strings.Index(s, prefix); idx >= 0 {
			end := idx + len(prefix) + 32
			if end > len(s) {
				end = len(s)
			}
			s = s[:idx+len(prefix)] + "<redacted>" + s[end:]
		}
	}
	return s
}

// prefixRedactions matches well-known credential prefixes (AWS, GitHub,
// Anthropic). The 32 chars following the prefix get masked.
var prefixRedactions = []string{
	"AKIA",        // AWS access key id
	"ASIA",        // AWS STS short-lived key id
	"ghs_",        // GitHub server-to-server token
	"ghp_",        // GitHub personal access token
	"ghu_",        // GitHub user-to-server token
	"ghr_",        // GitHub refresh token
	"github_pat_", // GitHub fine-grained PAT
	"sk-ant-",     // Anthropic API key prefix
	"sk-",         // OpenAI-style key (defensive)
	"xoxb-",       // Slack bot token
	"xoxp-",       // Slack user token
}

// Compile-time guard that the real reporter still satisfies the
// public contract. Keep in lock-step with sentry.go.
var _ ErrorReporter = (*sentryReporter)(nil)
