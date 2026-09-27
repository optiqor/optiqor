// Package token issues and verifies short-lived signed tokens that
// SaaS uses when calling the in-cluster agent (gate/dryrun, future
// signed-token Apply Fix endpoint). HS256 over (tenant, apply-fix-id,
// expiry) with a shared secret loaded from AWS Secrets Manager in
// production; in dev the secret is a static value.
//
// Wire format: base64url(payload).base64url(sig). Same shape as the
// receipts package so callers can reuse one encoder.
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Token is the body the signer wraps. Field order is load-bearing —
// the canonical JSON encoder emits fields in declaration order and
// verification re-marshals to compare bytes.
type Token struct {
	TenantID   string `json:"tenant_id"`
	ApplyFixID string `json:"apply_fix_id"`
	Audience   string `json:"aud"`
	IssuedAt   int64  `json:"iat"`
	ExpiresAt  int64  `json:"exp"`
}

// Issuer signs tokens. Secret must be at least 32 bytes (HMAC-SHA256
// recommendation). One per process; reuse across requests.
type Issuer struct {
	secret []byte
	now    func() time.Time
	ttl    time.Duration
}

func NewIssuer(secret []byte, ttl time.Duration) (*Issuer, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("token: secret must be at least 32 bytes, got %d", len(secret))
	}
	if ttl <= 0 {
		return nil, errors.New("token: ttl must be positive")
	}
	return &Issuer{secret: append([]byte(nil), secret...), now: time.Now, ttl: ttl}, nil
}

// WithClock injects the time source; tests pin it.
func (i *Issuer) WithClock(now func() time.Time) *Issuer {
	i.now = now
	return i
}

// Issue returns a base64url(payload).base64url(sig) string. Audience
// names the agent endpoint the token authorises (e.g. "dryrun",
// "apply-fix-merge").
func (i *Issuer) Issue(tenantID, applyFixID, audience string) (string, error) {
	if tenantID == "" || applyFixID == "" || audience == "" {
		return "", errors.New("token: tenant_id, apply_fix_id, and aud are required")
	}
	now := i.now().UTC()
	tok := Token{
		TenantID:   tenantID,
		ApplyFixID: applyFixID,
		Audience:   audience,
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(i.ttl).Unix(),
	}
	payload, err := canonical(tok)
	if err != nil {
		return "", err
	}
	sig := mac(payload, i.secret)
	return encode(payload) + "." + encode(sig), nil
}

// Verifier checks tokens against the shared secret. Returns the
// decoded Token on success. Always errors when the token is expired
// or the signature doesn't match — never partial trust.
type Verifier struct {
	secret []byte
	now    func() time.Time
}

func NewVerifier(secret []byte) (*Verifier, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("token: secret must be at least 32 bytes, got %d", len(secret))
	}
	return &Verifier{secret: append([]byte(nil), secret...), now: time.Now}, nil
}

func (v *Verifier) WithClock(now func() time.Time) *Verifier {
	v.now = now
	return v
}

// Verify returns the decoded Token if the signature checks out and
// the token isn't expired.
func (v *Verifier) Verify(signed, expectedAudience string) (Token, error) {
	payloadB64, sigB64, ok := strings.Cut(signed, ".")
	if !ok {
		return Token{}, errors.New("token: malformed (expected payload.sig)")
	}
	payload, err := decode(payloadB64)
	if err != nil {
		return Token{}, fmt.Errorf("token: payload decode: %w", err)
	}
	sig, err := decode(sigB64)
	if err != nil {
		return Token{}, fmt.Errorf("token: signature decode: %w", err)
	}
	want := mac(payload, v.secret)
	if subtle.ConstantTimeCompare(sig, want) != 1 {
		return Token{}, ErrSignature
	}
	var tok Token
	if err := json.Unmarshal(payload, &tok); err != nil {
		return Token{}, fmt.Errorf("token: payload unmarshal: %w", err)
	}
	if expectedAudience != "" && tok.Audience != expectedAudience {
		return Token{}, fmt.Errorf("token: audience = %q, want %q", tok.Audience, expectedAudience)
	}
	now := v.now().UTC()
	if tok.ExpiresAt <= now.Unix() {
		return Token{}, ErrExpired
	}
	if tok.IssuedAt > now.Unix()+60 {
		return Token{}, errors.New("token: issued in the future")
	}
	return tok, nil
}

// ErrSignature surfaces as HTTP 401 in the agent; never echo the
// token body on failure.
var ErrSignature = errors.New("token: signature verification failed")

// ErrExpired surfaces as HTTP 401 with a "renew your token" hint.
var ErrExpired = errors.New("token: expired")

func mac(payload, secret []byte) []byte {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write(payload)
	return h.Sum(nil)
}

// canonical mirrors receipts.Canonical's shape: emit fields in
// declaration order, no HTML escape, no trailing newline.
func canonical(t Token) ([]byte, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func encode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
