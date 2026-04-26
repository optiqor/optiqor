package vcs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// GitHub is the GitHub source. Webhook verification ships now (used
// by Phase 4 when the GitHub App webhook receiver lands); PostComment
// and OpenPR use the full go-github client which arrives with Apply
// Fix in Phase 4.
type GitHub struct{}

// NewGitHub returns the registered-by-default GitHub source.
func NewGitHub() *GitHub { return &GitHub{} }

func (*GitHub) Provider() Provider { return ProviderGitHub }

// VerifyWebhook validates GitHub's `X-Hub-Signature-256` header using
// HMAC-SHA256 over the raw body. The header format is
// "sha256=<hex>". Constant-time comparison guards against timing
// attacks.
func (*GitHub) VerifyWebhook(secret []byte, signatureHeader string, body []byte) error {
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return ErrInvalidSignature
	}
	got, err := hex.DecodeString(strings.TrimPrefix(signatureHeader, "sha256="))
	if err != nil {
		return ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, secret)
	if _, err := mac.Write(body); err != nil {
		return fmt.Errorf("vcs/github: hmac write: %w", err)
	}
	want := mac.Sum(nil)
	if !hmac.Equal(got, want) {
		return ErrInvalidSignature
	}
	return nil
}

// PostComment creates / updates a PR comment. Phase 4 wires go-github;
// the stub returns ErrNotImplemented so callers can register the
// source today and gate behavior with a feature flag.
func (*GitHub) PostComment(_ context.Context, _ PullRequest, _ Comment) (Comment, error) {
	return Comment{}, ErrNotImplemented
}

// OpenPR opens a PR with an Apply Fix diff. Phase 4.
func (*GitHub) OpenPR(_ context.Context, _ OpenPRRequest) (PullRequest, error) {
	return PullRequest{}, ErrNotImplemented
}
