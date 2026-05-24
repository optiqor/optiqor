package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func newSigner(t *testing.T, now time.Time) *Signer {
	t.Helper()
	s := NewSigner([]byte("test-secret-do-not-use-in-prod-32B!"))
	s.Now = func() time.Time { return now }
	return s
}

func validSession(now time.Time) Session {
	return Session{
		TenantID:    "tenant-1",
		WorkspaceID: "ws-1",
		Subject:     "alice@example.test",
		Name:        "Alice",
		IssuedAt:    now,
		ExpiresAt:   now.Add(time.Hour),
	}
}

func TestNewSigner_PanicsOnEmptySecret(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on empty secret")
		}
	}()
	_ = NewSigner(nil)
}

func TestSigner_IssueVerifyRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	s := newSigner(t, now)

	token, err := s.Issue(validSession(now))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if strings.Count(token, ".") != 2 {
		t.Errorf("compact JWT should have 3 segments, got %q", token)
	}

	got, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.TenantID != "tenant-1" || got.Subject != "alice@example.test" || got.Name != "Alice" {
		t.Errorf("claims round-trip: %+v", got)
	}
}

func TestSigner_RejectsTamperedSignature(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	s := newSigner(t, now)
	token, err := s.Issue(validSession(now))
	if err != nil {
		t.Fatal(err)
	}
	// Flip one byte in the signature segment.
	parts := strings.Split(token, ".")
	parts[2] = "X" + parts[2][1:]
	bad := strings.Join(parts, ".")

	_, err = s.Verify(bad)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("want ErrInvalidToken on tampered sig, got %v", err)
	}
}

func TestSigner_RejectsWrongSecret(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	good := newSigner(t, now)
	token, _ := good.Issue(validSession(now))

	other := NewSigner([]byte("different-secret-but-also-32-bytes!"))
	other.Now = func() time.Time { return now }
	if _, err := other.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("verifier with different secret must reject, got %v", err)
	}
}

func TestSigner_RejectsExpired(t *testing.T) {
	issuedAt := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	s := newSigner(t, issuedAt)
	token, _ := s.Issue(validSession(issuedAt))

	// Move clock past expiry.
	s.Now = func() time.Time { return issuedAt.Add(2 * time.Hour) }
	if _, err := s.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expired token must be rejected, got %v", err)
	}
}

func TestSigner_RejectsAlgNone(t *testing.T) {
	// Guards against the classic alg=none JWT downgrade.
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	s := newSigner(t, now)

	headerNone := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0"              // {"alg":"none","typ":"JWT"}
	payload := "eyJ0aWQiOiJ4Iiwic3ViIjoieSIsImV4cCI6OTk5OTk5OTk5OX0" // {"tid":"x","sub":"y","exp":9999999999}
	signed := headerNone + "." + payload
	bad := signed + "." + s.mac(signed) // valid HMAC, still must reject.

	if _, err := s.Verify(bad); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("alg=none must be rejected even with a valid HMAC, got %v", err)
	}
}

func TestSigner_RejectsMalformedShape(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	s := newSigner(t, now)
	for _, bad := range []string{"", "abc", "a.b", "a.b.c.d", "...."} {
		if _, err := s.Verify(bad); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("malformed token %q: want ErrInvalidToken, got %v", bad, err)
		}
	}
}

func TestSession_ValidEnforcesRequiredClaims(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		mut  func(*Session)
		want string
	}{
		{"missing tid", func(s *Session) { s.TenantID = "" }, "tid"},
		{"missing sub", func(s *Session) { s.Subject = "" }, "sub"},
		{"missing exp", func(s *Session) { s.ExpiresAt = time.Time{} }, "exp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ses := validSession(now)
			tc.mut(&ses)
			err := ses.Valid()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error mentioning %q, got %v", tc.want, err)
			}
		})
	}
}
