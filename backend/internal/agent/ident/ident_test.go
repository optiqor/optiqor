package ident

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const testSecret = "0123456789abcdef0123456789abcdef" // 32 bytes

func newClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestIssueAndVerify_RoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	iss, err := NewIssuer([]byte(testSecret), 5*time.Minute)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	iss.WithClock(newClock(now))

	wire, err := iss.Issue("11111111-1111-1111-1111-111111111111", "c1", "batch-99", "agent-snapshot")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !strings.Contains(wire, ".") {
		t.Fatalf("wire missing separator: %s", wire)
	}

	ver, _ := NewVerifier([]byte(testSecret))
	ver.WithClock(newClock(now.Add(2 * time.Minute)))
	tok, err := ver.Verify(wire, "agent-snapshot")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if tok.TenantID != "11111111-1111-1111-1111-111111111111" || tok.ClusterID != "c1" || tok.BatchID != "batch-99" {
		t.Errorf("token = %+v", tok)
	}
}

func TestVerify_ExpiredToken_Errors(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	iss, _ := NewIssuer([]byte(testSecret), 1*time.Minute)
	iss.WithClock(newClock(now))
	wire, _ := iss.Issue("t1", "c1", "b1", "agent-snapshot")

	ver, _ := NewVerifier([]byte(testSecret))
	ver.WithClock(newClock(now.Add(10 * time.Minute)))
	_, err := ver.Verify(wire, "agent-snapshot")
	if !errors.Is(err, ErrExpired) {
		t.Errorf("err = %v, want ErrExpired", err)
	}
}

func TestVerify_BadSignature_ConstantTimeCompare(t *testing.T) {
	iss, _ := NewIssuer([]byte(testSecret), 5*time.Minute)
	wire, _ := iss.Issue("t1", "c1", "b1", "agent-snapshot")

	otherSecret := "fedcba9876543210fedcba9876543210"
	ver, _ := NewVerifier([]byte(otherSecret))
	_, err := ver.Verify(wire, "agent-snapshot")
	if !errors.Is(err, ErrBadSignature) {
		t.Errorf("err = %v, want ErrBadSignature", err)
	}
}

func TestVerify_AudienceMismatch_Errors(t *testing.T) {
	iss, _ := NewIssuer([]byte(testSecret), 5*time.Minute)
	wire, _ := iss.Issue("t1", "c1", "b1", "agent-snapshot")

	ver, _ := NewVerifier([]byte(testSecret))
	_, err := ver.Verify(wire, "different-audience")
	if !errors.Is(err, ErrAudience) {
		t.Errorf("err = %v, want ErrAudience", err)
	}
}

func TestNewIssuer_ShortSecret_Errors(t *testing.T) {
	_, err := NewIssuer([]byte("short"), 5*time.Minute)
	if !errors.Is(err, ErrShortSecret) {
		t.Errorf("err = %v, want ErrShortSecret", err)
	}
}

func TestNewIssuer_ExceedsMaxTTL_Errors(t *testing.T) {
	_, err := NewIssuer([]byte(testSecret), 1*time.Hour)
	if err == nil || !strings.Contains(err.Error(), "ttl must be in") {
		t.Errorf("err = %v, want ttl bounds error", err)
	}
}

func TestVerify_MalformedToken(t *testing.T) {
	ver, _ := NewVerifier([]byte(testSecret))
	for _, tc := range []string{"", "nodots", "only.one.too.many"} {
		if _, err := ver.Verify(tc, "agent-snapshot"); !errors.Is(err, ErrMalformedToken) {
			t.Errorf("Verify(%q): err = %v, want ErrMalformedToken", tc, err)
		}
	}
}
