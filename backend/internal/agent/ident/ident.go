// Package ident issues + verifies the short-lived JWT the in-cluster
// agent attaches to every snapshot POST. mTLS proves which tenant
// owns the connection; this JWT proves recency — a 15-minute window
// caps the blast radius of a leaked cert pair.
//
// HS256 over (tenant_id, cluster_id, batch_id, audience, iat, exp).
// Same wire format as internal/applyfix/token so the verifier code
// path is familiar. Secret is provisioned at agent install time
// (Kubernetes Secret + KMS-sealed in production).
package ident

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

// MaxTTL caps how long an issued token stays valid even if the caller
// asks for more. Mirrors the technical_implementation.md §10.3.1
// "15 minute" agent-side rotation budget.
const MaxTTL = 15 * time.Minute

// Token is the canonical payload. Field order is load-bearing — JSON
// is canonical bytes for the MAC.
type Token struct {
	TenantID  string `json:"tenant_id"`
	ClusterID string `json:"cluster_id"`
	BatchID   string `json:"batch_id"`
	Audience  string `json:"aud"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

var (
	ErrMalformedToken = errors.New("ident: malformed token")
	ErrBadSignature   = errors.New("ident: signature mismatch")
	ErrExpired        = errors.New("ident: token expired")
	ErrNotYetValid    = errors.New("ident: token not yet valid (iat in future)")
	ErrShortSecret    = errors.New("ident: secret must be at least 32 bytes")
	ErrAudience       = errors.New("ident: audience mismatch")
)

// Issuer signs tokens for the agent's outbound POSTs. Reuse across
// snapshots — the constructor validates the secret length once.
type Issuer struct {
	secret []byte
	now    func() time.Time
	ttl    time.Duration
}

func NewIssuer(secret []byte, ttl time.Duration) (*Issuer, error) {
	if len(secret) < 32 {
		return nil, ErrShortSecret
	}
	if ttl <= 0 || ttl > MaxTTL {
		return nil, fmt.Errorf("ident: ttl must be in (0, %s]", MaxTTL)
	}
	return &Issuer{
		secret: append([]byte(nil), secret...),
		now:    time.Now,
		ttl:    ttl,
	}, nil
}

func (i *Issuer) WithClock(now func() time.Time) *Issuer {
	i.now = now
	return i
}

// Issue returns the wire-format token. tenant/cluster/batch are
// echoed back from the payload at verify time so the api handler can
// log them without re-parsing.
func (i *Issuer) Issue(tenantID, clusterID, batchID, audience string) (string, error) {
	if tenantID == "" || audience == "" {
		return "", errors.New("ident: tenant_id and aud are required")
	}
	now := i.now().UTC()
	tok := Token{
		TenantID:  tenantID,
		ClusterID: clusterID,
		BatchID:   batchID,
		Audience:  audience,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(i.ttl).Unix(),
	}
	body, err := json.Marshal(tok)
	if err != nil {
		return "", err
	}
	sig := mac(body, i.secret)
	return encode(body) + "." + encode(sig), nil
}

// Verifier validates incoming tokens against the same shared secret.
type Verifier struct {
	secret []byte
	now    func() time.Time
	skew   time.Duration
}

func NewVerifier(secret []byte) (*Verifier, error) {
	if len(secret) < 32 {
		return nil, ErrShortSecret
	}
	return &Verifier{
		secret: append([]byte(nil), secret...),
		now:    time.Now,
		// 30s allowance for legitimate clock skew between agent and api.
		// Tighter than the TTL by an order of magnitude.
		skew: 30 * time.Second,
	}, nil
}

func (v *Verifier) WithClock(now func() time.Time) *Verifier {
	v.now = now
	return v
}

// Verify returns the parsed token + nil error when signature, audience,
// and expiry all check out. Any failure returns a distinct sentinel so
// the api handler can log the precise reason without leaking secrets.
func (v *Verifier) Verify(wire, expectedAudience string) (Token, error) {
	parts := strings.SplitN(wire, ".", 2)
	if len(parts) != 2 {
		return Token{}, ErrMalformedToken
	}
	body, err := decode(parts[0])
	if err != nil {
		return Token{}, ErrMalformedToken
	}
	sig, err := decode(parts[1])
	if err != nil {
		return Token{}, ErrMalformedToken
	}
	want := mac(body, v.secret)
	if subtle.ConstantTimeCompare(sig, want) != 1 {
		return Token{}, ErrBadSignature
	}
	var tok Token
	if err := json.Unmarshal(body, &tok); err != nil {
		return Token{}, ErrMalformedToken
	}
	if expectedAudience != "" && tok.Audience != expectedAudience {
		return Token{}, ErrAudience
	}
	now := v.now().UTC().Unix()
	if tok.IssuedAt > now+int64(v.skew.Seconds()) {
		return Token{}, ErrNotYetValid
	}
	if tok.ExpiresAt+int64(v.skew.Seconds()) < now {
		return Token{}, ErrExpired
	}
	return tok, nil
}

func mac(body, secret []byte) []byte {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write(body)
	return h.Sum(nil)
}

func encode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
