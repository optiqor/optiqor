package prom

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewAuthenticator_RejectsMissingCredentials(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  AuthConfig
		want string
	}{
		{"bearer without token or file", AuthConfig{Mode: AuthBearer}, "BearerToken or BearerFile"},
		{"basic without username", AuthConfig{Mode: AuthBasic, Password: "p"}, "Username"},
		{"mtls without cert", AuthConfig{Mode: AuthMTLS, ClientKeyFile: "k"}, "ClientCertFile"},
		{"mtls without key", AuthConfig{Mode: AuthMTLS, ClientCertFile: "c"}, "ClientKeyFile"},
		{"unknown mode", AuthConfig{Mode: "fancy"}, "unknown mode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewAuthenticator(tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestNewAuthenticator_NoneAndDefaultsAreNoop(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  AuthConfig
	}{
		{"explicit none", AuthConfig{Mode: AuthNone}},
		{"empty falls back to none", AuthConfig{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := NewAuthenticator(tc.cfg)
			if err != nil {
				t.Fatalf("NewAuthenticator: %v", err)
			}
			req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
			if err := a.Apply(req); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if req.Header.Get("Authorization") != "" {
				t.Errorf("none mode set Authorization header: %q", req.Header.Get("Authorization"))
			}
		})
	}
}

func TestAuthenticator_BearerSetsHeader(t *testing.T) {
	a, err := NewAuthenticator(AuthConfig{Mode: AuthBearer, BearerToken: "tok-123"})
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok-123")
	}
}

func TestAuthenticator_BasicSetsHeader(t *testing.T) {
	a, err := NewAuthenticator(AuthConfig{Mode: AuthBasic, Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	user, pass, ok := req.BasicAuth()
	if !ok || user != "u" || pass != "p" {
		t.Errorf("basic auth not set: ok=%v u=%q p=%q", ok, user, pass)
	}
}

func TestFileTokenAuth_RefreshesOnInterval(t *testing.T) {
	var reads atomic.Int32
	clock := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	a := &fileTokenAuth{
		path:     "/fake/token",
		scheme:   "Bearer",
		refresh:  30 * time.Second,
		clock:    func() time.Time { return clock },
		readFile: func(string) ([]byte, error) { reads.Add(1); return []byte("tok-" + clock.Format("15:04:05")), nil },
	}

	// First read populates the cache.
	req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	if err := a.Apply(req); err != nil {
		t.Fatalf("Apply 1: %v", err)
	}
	got1 := req.Header.Get("Authorization")
	if reads.Load() != 1 {
		t.Fatalf("first call reads = %d, want 1", reads.Load())
	}

	// 10s later — inside the refresh window. No new read; same token.
	clock = clock.Add(10 * time.Second)
	req2 := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	if err := a.Apply(req2); err != nil {
		t.Fatalf("Apply 2: %v", err)
	}
	if reads.Load() != 1 {
		t.Errorf("reads after 10s = %d, want 1 (cache hit)", reads.Load())
	}
	if req2.Header.Get("Authorization") != got1 {
		t.Errorf("cached token changed under cache hit; before=%q after=%q", got1, req2.Header.Get("Authorization"))
	}

	// 40s after first read — outside refresh window. File re-read; new token.
	clock = clock.Add(31 * time.Second)
	req3 := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	if err := a.Apply(req3); err != nil {
		t.Fatalf("Apply 3: %v", err)
	}
	if reads.Load() != 2 {
		t.Errorf("reads after refresh window = %d, want 2", reads.Load())
	}
	if req3.Header.Get("Authorization") == got1 {
		t.Errorf("token did not refresh: still %q", got1)
	}
}

func TestFileTokenAuth_ReadFailureSurfaces(t *testing.T) {
	a := &fileTokenAuth{
		path:     "/no-such-file",
		scheme:   "Bearer",
		refresh:  time.Second,
		clock:    time.Now,
		readFile: func(string) ([]byte, error) { return nil, errors.New("ENOENT") },
	}
	req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	err := a.Apply(req)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "ENOENT") {
		t.Errorf("error should wrap ENOENT, got %v", err)
	}
}

func TestFileTokenAuth_EmptyFileIsError(t *testing.T) {
	a := &fileTokenAuth{
		path:     "/empty",
		scheme:   "Bearer",
		refresh:  time.Second,
		clock:    time.Now,
		readFile: func(string) ([]byte, error) { return []byte("\n  \n"), nil },
	}
	req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	if err := a.Apply(req); err == nil {
		t.Fatal("empty token should not be accepted")
	}
}

func TestIsMTLS_DetectsMode(t *testing.T) {
	a, err := NewAuthenticator(AuthConfig{Mode: AuthMTLS, ClientCertFile: "/tmp/c", ClientKeyFile: "/tmp/k"})
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	if !IsMTLS(a) {
		t.Error("IsMTLS should be true for mtls mode")
	}
	bearer, _ := NewAuthenticator(AuthConfig{Mode: AuthBearer, BearerToken: "x"})
	if IsMTLS(bearer) {
		t.Error("IsMTLS should be false for bearer mode")
	}
}
