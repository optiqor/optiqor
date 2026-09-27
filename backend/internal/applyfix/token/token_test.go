package token

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIssueVerify_Roundtrip(t *testing.T) {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}
	t0 := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return t0 }

	iss, err := NewIssuer(secret, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	iss.WithClock(clock)
	ver, _ := NewVerifier(secret)
	ver.WithClock(clock)

	signed, err := iss.Issue("tenant-1", "afix-9", "dryrun")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := ver.Verify(signed, "dryrun")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if tok.TenantID != "tenant-1" || tok.ApplyFixID != "afix-9" || tok.Audience != "dryrun" {
		t.Errorf("decoded = %+v", tok)
	}
}

func TestVerify_Failures(t *testing.T) {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	t0 := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	iss, _ := NewIssuer(secret, 5*time.Minute)
	iss.WithClock(func() time.Time { return t0 })

	signed, _ := iss.Issue("t-1", "afix", "dryrun")

	for _, tc := range []struct {
		name     string
		mutate   func(string) string
		verAtNow time.Time
		audience string
		wantErr  error
		errSub   string
	}{
		{name: "valid", verAtNow: t0, audience: "dryrun"},
		{name: "wrong audience", verAtNow: t0, audience: "merge", errSub: "audience"},
		{name: "expired by 1 second", verAtNow: t0.Add(5*time.Minute + time.Second), audience: "dryrun", wantErr: ErrExpired},
		{name: "tampered signature", mutate: flipFirst, verAtNow: t0, audience: "dryrun", wantErr: ErrSignature},
		{name: "tampered payload", mutate: flipLastChar, verAtNow: t0, audience: "dryrun", wantErr: ErrSignature},
		{name: "malformed", mutate: func(string) string { return "no-dot-here" }, verAtNow: t0, audience: "dryrun", errSub: "malformed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokString := signed
			if tc.mutate != nil {
				tokString = tc.mutate(signed)
			}
			ver, _ := NewVerifier(secret)
			ver.WithClock(func() time.Time { return tc.verAtNow })
			_, err := ver.Verify(tokString, tc.audience)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("err = %v, want %v", err, tc.wantErr)
				}
			case tc.errSub != "":
				if err == nil || !strings.Contains(err.Error(), tc.errSub) {
					t.Errorf("err = %v, want substring %q", err, tc.errSub)
				}
			default:
				if err != nil {
					t.Errorf("unexpected err: %v", err)
				}
			}
		})
	}
}

func TestNewIssuer_RejectsShortSecret(t *testing.T) {
	_, err := NewIssuer([]byte("short"), time.Minute)
	if err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("err = %v, want substring '32 bytes'", err)
	}
}

func TestIssue_RejectsEmptyFields(t *testing.T) {
	secret := make([]byte, 32)
	iss, _ := NewIssuer(secret, time.Minute)
	for _, tc := range []struct{ tenant, fix, aud string }{
		{"", "f", "a"},
		{"t", "", "a"},
		{"t", "f", ""},
	} {
		if _, err := iss.Issue(tc.tenant, tc.fix, tc.aud); err == nil {
			t.Errorf("Issue(%q, %q, %q) accepted empty input", tc.tenant, tc.fix, tc.aud)
		}
	}
}

func flipFirst(s string) string {
	b := []byte(s)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}

func flipLastChar(s string) string {
	b := []byte(s)
	i := len(b) - 1
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	return string(b)
}
