//go:build integration

package integration

import (
	"errors"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/applyfix/token"
)

// TestToken_IssueVerifyRoundtrip pins the contract Phase-5 onboarding
// + the agent's signed-token endpoint depend on: HMAC-SHA256 over a
// canonical payload, base64url-encoded wire shape, audience-bound,
// expiry-enforced.
func TestToken_IssueVerifyRoundtrip(t *testing.T) {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	iss, err := token.NewIssuer(secret, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	iss.WithClock(func() time.Time { return now })

	ver, _ := token.NewVerifier(secret)
	ver.WithClock(func() time.Time { return now })

	signed, err := iss.Issue("tenant-int", "afix-100", "dryrun")
	if err != nil {
		t.Fatal(err)
	}

	tok, err := ver.Verify(signed, "dryrun")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if tok.TenantID != "tenant-int" || tok.ApplyFixID != "afix-100" {
		t.Errorf("decoded = %+v", tok)
	}

	// Verify rejects wrong audience.
	if _, err := ver.Verify(signed, "merge"); err == nil {
		t.Error("Verify accepted wrong audience")
	}

	// Verify rejects expired token.
	verExpired, _ := token.NewVerifier(secret)
	verExpired.WithClock(func() time.Time { return now.Add(6 * time.Minute) })
	if _, err := verExpired.Verify(signed, "dryrun"); !errors.Is(err, token.ErrExpired) {
		t.Errorf("expired Verify: err = %v, want ErrExpired", err)
	}
}

// TestToken_DifferentSecretRejects pins that a verifier with a
// different secret can never accept a token — load-bearing for
// per-tenant or per-environment secret rotation.
func TestToken_DifferentSecretRejects(t *testing.T) {
	secretA := make([]byte, 32)
	secretB := make([]byte, 32)
	for i := range secretA {
		secretA[i] = byte(i + 1)
		secretB[i] = byte(i + 2)
	}

	iss, _ := token.NewIssuer(secretA, time.Minute)
	signed, _ := iss.Issue("t", "a", "dryrun")

	ver, _ := token.NewVerifier(secretB)
	if _, err := ver.Verify(signed, "dryrun"); !errors.Is(err, token.ErrSignature) {
		t.Errorf("Verify with wrong secret: err = %v, want ErrSignature", err)
	}
}
