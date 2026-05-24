package vcs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// GitHub is the GitHub source. Webhook verification ships now;
// PostComment / OpenPR wire the go-github client in Phase 4 alongside
// Apply Fix.
type GitHub struct{}

func NewGitHub() *GitHub { return &GitHub{} }

func (*GitHub) Provider() Provider { return ProviderGitHub }

// VerifyWebhook validates X-Hub-Signature-256 (HMAC-SHA256 over the
// raw body, "sha256=<hex>"). hmac.Equal is constant-time to keep the
// secret out of reach of timing attacks.
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

// Stub so callers can register the source today and gate with a
// feature flag; Phase 4 wires go-github.
func (*GitHub) PostComment(_ context.Context, _ PullRequest, _ Comment) (Comment, error) {
	return Comment{}, ErrNotImplemented
}

func (*GitHub) OpenPR(_ context.Context, _ OpenPRRequest) (PullRequest, error) {
	return PullRequest{}, ErrNotImplemented
}
