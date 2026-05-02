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

func TestRegistry_DuplicatePanics(t *testing.T) {
	r := newRegistry()
	r.Register(NewGitHub())
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate")
		}
	}()
	r.Register(NewGitHub())
}

func TestRegistry_NilPanics(t *testing.T) {
	r := newRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil")
		}
	}()
	r.Register(nil)
}

func TestRegistry_Providers_Sorted(t *testing.T) {
	r := newRegistry()
	r.Register(NewGitHub())
	if got := r.Providers(); len(got) != 1 || got[0] != ProviderGitHub {
		t.Errorf("Providers() = %v", got)
	}
}

func TestGitHub_VerifyWebhook_Valid(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	secret := []byte("hush")

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	header := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if err := NewGitHub().VerifyWebhook(secret, header, body); err != nil {
		t.Fatalf("VerifyWebhook valid: %v", err)
	}
}

func TestGitHub_VerifyWebhook_Tampered(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	secret := []byte("hush")

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	header := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// Body tampered after signing — verification must fail.
	tampered := []byte(`{"action":"closed"}`)
	if err := NewGitHub().VerifyWebhook(secret, header, tampered); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestGitHub_VerifyWebhook_BadHeader(t *testing.T) {
	cases := []string{
		"",               // empty
		"md5=abc",        // wrong algo
		"sha256=not-hex", // unhex
		"sha256=",        // empty digest
	}
	for _, h := range cases {
		err := NewGitHub().VerifyWebhook([]byte("s"), h, []byte("b"))
		if !errors.Is(err, ErrInvalidSignature) {
			t.Errorf("VerifyWebhook(%q) = %v, want ErrInvalidSignature", h, err)
		}
	}
}

func TestGitHub_PhaseStubs(t *testing.T) {
	g := NewGitHub()
	if _, err := g.PostComment(context.Background(), PullRequest{}, Comment{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("PostComment = %v, want ErrNotImplemented", err)
	}
	if _, err := g.OpenPR(context.Background(), OpenPRRequest{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("OpenPR = %v, want ErrNotImplemented", err)
	}
}

func TestRepo_String(t *testing.T) {
	r := Repo{Provider: ProviderGitHub, Owner: "optiqor", Name: "backend"}
	if got, want := r.String(), "github:optiqor/backend"; got != want {
		t.Errorf("Repo.String() = %q, want %q", got, want)
	}
}
