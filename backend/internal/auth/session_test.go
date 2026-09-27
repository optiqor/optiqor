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

func TestSigner_Verify(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name  string
		token func(t *testing.T) (verifier *Signer, token string)
	}{
		{
			name: "tampered signature",
			token: func(t *testing.T) (*Signer, string) {
				t.Helper()
				s := newSigner(t, now)
				tok, err := s.Issue(validSession(now))
				if err != nil {
					t.Fatal(err)
				}
				parts := strings.Split(tok, ".")
				parts[2] = "X" + parts[2][1:]
				return s, strings.Join(parts, ".")
			},
		},
		{
			name: "wrong secret",
			token: func(t *testing.T) (*Signer, string) {
				t.Helper()
				good := newSigner(t, now)
				tok, _ := good.Issue(validSession(now))
				other := NewSigner([]byte("different-secret-but-also-32-bytes!"))
				other.Now = func() time.Time { return now }
				return other, tok
			},
		},
		{
			name: "expired",
			token: func(t *testing.T) (*Signer, string) {
				t.Helper()
				s := newSigner(t, now)
				tok, _ := s.Issue(validSession(now))
				s.Now = func() time.Time { return now.Add(2 * time.Hour) }
				return s, tok
			},
		},
		{
			// Guards against the classic alg=none JWT downgrade: header
			// claims unsigned, payload carries a valid HMAC so a naive
			// verifier would accept it.
			name: "alg none with valid hmac",
			token: func(t *testing.T) (*Signer, string) {
				t.Helper()
				s := newSigner(t, now)
				headerNone := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0"
				payload := "eyJ0aWQiOiJ4Iiwic3ViIjoieSIsImV4cCI6OTk5OTk5OTk5OX0"
				signed := headerNone + "." + payload
				return s, signed + "." + s.mac(signed)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifier, tok := tc.token(t)
			if _, err := verifier.Verify(tok); !errors.Is(err, ErrInvalidToken) {
				t.Errorf("want ErrInvalidToken, got %v", err)
			}
		})
	}

	t.Run("malformed shape", func(t *testing.T) {
		s := newSigner(t, now)
		for _, bad := range []string{"", "abc", "a.b", "a.b.c.d", "...."} {
			if _, err := s.Verify(bad); !errors.Is(err, ErrInvalidToken) {
				t.Errorf("malformed token %q: want ErrInvalidToken, got %v", bad, err)
			}
		}
	})
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
