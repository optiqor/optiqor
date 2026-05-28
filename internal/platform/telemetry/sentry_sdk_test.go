package telemetry

import (
	"errors"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestRedactString_MasksAWSAndAnthropicKeys(t *testing.T) {
	for _, tc := range []struct {
		name        string
		in          string
		mustNotHave []string
	}{
		{
			name:        "aws access key id prefix",
			in:          "boot failed: AKIAIOSFODNN7EXAMPLE leaked into log line",
			mustNotHave: []string{"AKIAIOSFODNN7EXAMPLE"},
		},
		{
			name:        "aws sts short-lived key",
			in:          "request: ASIAIOSFODNN7TEMP key leaked into log",
			mustNotHave: []string{"ASIAIOSFODNN7TEMP"},
		},
		{
			name:        "github app server-to-server token",
			in:          "github call failed ghs_abcdefghijklmnopqrstuvwxyz0123456789 expired",
			mustNotHave: []string{"abcdefghijklmnopqrstuvwxyz"},
		},
		{
			name:        "anthropic api key",
			in:          "llm call: sk-ant-api03-realkeydatahere0123456789012345 boom",
			mustNotHave: []string{"realkeydatahere"},
		},
		{
			name:        "slack bot token",
			in:          "slack post: xoxb-12345-67890-realsecretpartHEREvalue blew up",
			mustNotHave: []string{"realsecretpartHERE"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := redactString(tc.in)
			for _, leaked := range tc.mustNotHave {
				if strings.Contains(got, leaked) {
					t.Errorf("redactString leaked %q\n  in: %s\n out: %s", leaked, tc.in, got)
				}
			}
			if !strings.Contains(got, "<redacted>") {
				t.Errorf("redactString did not insert <redacted> marker:\n%s", got)
			}
		})
	}
}

func TestRedactString_PEMPrivateKeyMasked(t *testing.T) {
	pem := "boot: -----BEGIN PRIVATE KEY-----\nMIIE.....\n-----END PRIVATE KEY-----"
	got := redactString(pem)
	if strings.Contains(got, "MIIE") {
		t.Errorf("PEM body leaked: %s", got)
	}
	if !strings.Contains(got, "<github-app-key-redacted>") {
		t.Errorf("missing PEM redaction marker: %s", got)
	}
}

func TestRedactBeforeSend_StripsExceptionAndExtra(t *testing.T) {
	event := &sentry.Event{
		Message: "anthropic call failed sk-ant-api03-secretkey0123456789012345",
		Exception: []sentry.Exception{
			{Value: "leaked AKIAIOSFODNN7EXAMPLE in stack frame"},
		},
		Extra: map[string]any{
			"github_token": "ghp_secrettoken0123456789012345abcd",
			"benign":       "ok",
		},
	}
	out := redactBeforeSend(event, nil)
	if strings.Contains(out.Message, "secretkey") {
		t.Errorf("message leaked: %s", out.Message)
	}
	if strings.Contains(out.Exception[0].Value, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("exception leaked")
	}
	if got, _ := out.Extra["github_token"].(string); strings.Contains(got, "secrettoken") {
		t.Errorf("extra leaked: %v", got)
	}
	if out.Extra["benign"].(string) != "ok" {
		t.Errorf("benign value mutated: %v", out.Extra["benign"])
	}
}

func TestRedactBeforeSend_NilEvent_Returns_Nil(t *testing.T) {
	if redactBeforeSend(nil, nil) != nil {
		t.Error("nil event must return nil (not panic)")
	}
}

func TestInitSentry_EmptyDSN_ErrNotConfigured(t *testing.T) {
	_, err := InitSentry(SentryConfig{})
	if !errors.Is(err, ErrSentryNotConfigured) {
		t.Errorf("err = %v, want ErrSentryNotConfigured", err)
	}
}

func TestInitSentry_InvalidSampleRate_Errors(t *testing.T) {
	_, err := InitSentry(SentryConfig{DSN: "https://test@localhost/1", SampleRate: 2})
	if err == nil {
		t.Error("want validation error on out-of-range sample rate")
	}
}
