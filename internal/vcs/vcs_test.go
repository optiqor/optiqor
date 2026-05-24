package vcs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func TestRegistry_RegisterLookup(t *testing.T) {
	r := newRegistry()
	r.Register(NewGitHub())

	got, err := r.Lookup(ProviderGitHub)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.Provider() != ProviderGitHub {
		t.Errorf("provider = %q", got.Provider())
	}

	if _, err := r.Lookup(ProviderGitLab); err == nil {
		t.Error("expected error for missing provider")
	}
}

func TestRegistry_Register_Panics(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(r *Registry)
	}{
		{
			name: "duplicate",
			do: func(r *Registry) {
				r.Register(NewGitHub())
				r.Register(NewGitHub())
			},
		},
		{
			name: "nil-provider",
			do: func(r *Registry) {
				r.Register(nil)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRegistry()
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			tc.do(r)
		})
	}
}

func TestRegistry_Providers_Sorted(t *testing.T) {
	r := newRegistry()
	r.Register(NewGitHub())
	if got := r.Providers(); len(got) != 1 || got[0] != ProviderGitHub {
		t.Errorf("Providers() = %v", got)
	}
}

func TestGitHub_VerifyWebhook(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	secret := []byte("hush")
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	validHeader := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	for _, tc := range []struct {
		name    string
		secret  []byte
		header  string
		body    []byte
		wantErr error // nil means success
	}{
		{name: "valid", secret: secret, header: validHeader, body: body, wantErr: nil},
		{name: "tampered-body", secret: secret, header: validHeader, body: []byte(`{"action":"closed"}`), wantErr: ErrInvalidSignature},
		{name: "empty-header", secret: []byte("s"), header: "", body: []byte("b"), wantErr: ErrInvalidSignature},
		{name: "wrong-algorithm", secret: []byte("s"), header: "md5=abc", body: []byte("b"), wantErr: ErrInvalidSignature},
		{name: "non-hex-signature", secret: []byte("s"), header: "sha256=not-hex", body: []byte("b"), wantErr: ErrInvalidSignature},
		{name: "empty-signature", secret: []byte("s"), header: "sha256=", body: []byte("b"), wantErr: ErrInvalidSignature},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := NewGitHub().VerifyWebhook(tc.secret, tc.header, tc.body)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("VerifyWebhook: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGitHub_PhaseStubs(t *testing.T) {
	g := NewGitHub()
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{
			name: "post-comment",
			call: func() error {
				_, err := g.PostComment(context.Background(), PullRequest{}, Comment{})
				return err
			},
		},
		{
			name: "open-pr",
			call: func() error {
				_, err := g.OpenPR(context.Background(), OpenPRRequest{})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrNotImplemented) {
				t.Errorf("err = %v, want ErrNotImplemented", err)
			}
		})
	}
}

func TestRepo_String(t *testing.T) {
	r := Repo{Provider: ProviderGitHub, Owner: "optiqor", Name: "backend"}
	if got, want := r.String(), "github:optiqor/backend"; got != want {
		t.Errorf("Repo.String() = %q, want %q", got, want)
	}
}
