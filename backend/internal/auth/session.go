package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Session mirrors tenancy.Context so the Phase-5 JWT extractor can
// rebuild a tenancy.Context without translating fields.
type Session struct {
	TenantID    string    `json:"tid"`
	WorkspaceID string    `json:"wid,omitempty"`
	Subject     string    `json:"sub"` // Auth.js user identity (email or provider id)
	Name        string    `json:"name,omitempty"`
	IssuedAt    time.Time `json:"iat"`
	ExpiresAt   time.Time `json:"exp"`
}

// Valid checks structural completeness only. Expiry is a Signer.Verify
// concern because it needs a clock.
func (s Session) Valid() error {
	switch {
	case s.TenantID == "":
		return errors.New("auth: session missing tid")
	case s.Subject == "":
		return errors.New("auth: session missing sub")
	case s.ExpiresAt.IsZero():
		return errors.New("auth: session missing exp")
	}
	return nil
}

// Signer issues and verifies sessions. Rotate the secret via deploy,
// not at runtime.
type Signer struct {
	secret []byte
	// Now defaults to time.Now().UTC(); tests pin it.
	Now func() time.Time
}

// NewSigner panics on an empty secret so a misconfigured boot fails
// closed instead of issuing tokens nobody can verify.
func NewSigner(secret []byte) *Signer {
	if len(secret) == 0 {
		panic("auth: empty signing secret")
	}
	return &Signer{secret: secret}
}

// ErrInvalidToken covers every unverifiable-token case. Callers treat
// them all as 401.
var ErrInvalidToken = errors.New("auth: invalid token")

// Issue signs s as a compact HS256 JWT. Caller sets IssuedAt + ExpiresAt.
func (g *Signer) Issue(s Session) (string, error) {
	if err := s.Valid(); err != nil {
		return "", err
	}
	header, err := json.Marshal(struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}{Alg: "HS256", Typ: "JWT"})
	if err != nil {
		return "", fmt.Errorf("auth: marshal header: %w", err)
	}
	payload, err := json.Marshal(jwtClaims{
		TenantID:    s.TenantID,
		WorkspaceID: s.WorkspaceID,
		Subject:     s.Subject,
		Name:        s.Name,
		IssuedAt:    s.IssuedAt.Unix(),
		ExpiresAt:   s.ExpiresAt.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("auth: marshal claims: %w", err)
	}
	h := b64(header)
	p := b64(payload)
	signed := h + "." + p
	return signed + "." + g.mac(signed), nil
}

// Verify returns the embedded Session or wraps ErrInvalidToken.
func (g *Signer) Verify(token string) (Session, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Session{}, fmt.Errorf("%w: want 3 segments, got %d", ErrInvalidToken, len(parts))
	}
	signed := parts[0] + "." + parts[1]
	want := g.mac(signed)
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return Session{}, fmt.Errorf("%w: signature mismatch", ErrInvalidToken)
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Session{}, fmt.Errorf("%w: header decode: %w", ErrInvalidToken, err)
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return Session{}, fmt.Errorf("%w: header unmarshal: %w", ErrInvalidToken, err)
	}
	if header.Alg != "HS256" {
		// "alg: none" and provider-confusion attacks both fail here.
		return Session{}, fmt.Errorf("%w: unsupported alg %q", ErrInvalidToken, header.Alg)
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Session{}, fmt.Errorf("%w: payload decode: %w", ErrInvalidToken, err)
	}
	var c jwtClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return Session{}, fmt.Errorf("%w: payload unmarshal: %w", ErrInvalidToken, err)
	}

	s := Session{
		TenantID:    c.TenantID,
		WorkspaceID: c.WorkspaceID,
		Subject:     c.Subject,
		Name:        c.Name,
		IssuedAt:    time.Unix(c.IssuedAt, 0).UTC(),
		ExpiresAt:   time.Unix(c.ExpiresAt, 0).UTC(),
	}
	if err := s.Valid(); err != nil {
		return Session{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if g.now().After(s.ExpiresAt) {
		return Session{}, fmt.Errorf("%w: expired at %s", ErrInvalidToken, s.ExpiresAt.Format(time.RFC3339))
	}
	return s, nil
}

func (g *Signer) mac(input string) string {
	h := hmac.New(sha256.New, g.secret)
	_, _ = h.Write([]byte(input))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (g *Signer) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now().UTC()
}

func b64(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// jwtClaims uses short keys + Unix-second timestamps so the encoded
// token fits comfortably inside a cookie.
type jwtClaims struct {
	TenantID    string `json:"tid"`
	WorkspaceID string `json:"wid,omitempty"`
	Subject     string `json:"sub"`
	Name        string `json:"name,omitempty"`
	IssuedAt    int64  `json:"iat"`
	ExpiresAt   int64  `json:"exp"`
}
