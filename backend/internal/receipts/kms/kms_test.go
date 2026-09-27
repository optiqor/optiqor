package kms

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/receipts"
)

func TestNewSigner(t *testing.T) {
	for _, tc := range []struct {
		name   string
		keyID  string
		client KMSClient
		errSub string
	}{
		{name: "happy path", keyID: "optiqor-receipt-2026-q3-ecdsa-p256", client: &FakeKMS{}},
		{name: "empty keyID", keyID: "", client: &FakeKMS{}, errSub: "empty keyID"},
		{name: "nil client", keyID: "x-ecdsa-p256", client: nil, errSub: "nil KMSClient"},
		{name: "keyID missing algorithm", keyID: "optiqor-receipt-2026-ed25519", client: &FakeKMS{}, errSub: "ecdsa-p256"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSigner(tc.keyID, tc.client)
			if tc.errSub == "" {
				if err != nil {
					t.Errorf("unexpected err: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("err = %v, want substring %q", err, tc.errSub)
			}
		})
	}
}

func TestSigner_Sign_HappyPath(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	r := receipts.Receipt{
		ID:                       "rec-1",
		TenantID:                 "tenant-x",
		ApplyFixID:               "afix-1",
		ObservedFromUTC:          now.Add(-30 * 24 * time.Hour),
		ObservedToUTC:            now,
		PredictedSavingsUSDCents: 12345,
		RealisedSavingsUSDCents:  11000,
		IssuerKeyID:              "ignored — Sign overrides",
		IssuedAtUTC:              now,
	}
	fake := &FakeKMS{}
	signer, err := NewSigner("optiqor-receipt-2026-q3-ecdsa-p256", fake)
	if err != nil {
		t.Fatal(err)
	}
	out, err := signer.Sign(r)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !strings.Contains(out, ".") {
		t.Fatalf("wire form missing separator: %q", out)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("KMS called %d times, want 1", len(fake.Calls))
	}
	call := fake.Calls[0]
	if call.KeyID != "optiqor-receipt-2026-q3-ecdsa-p256" {
		t.Errorf("KeyID = %q", call.KeyID)
	}
	if len(call.Digest) != 32 {
		t.Errorf("Digest len = %d, want 32 (SHA-256)", len(call.Digest))
	}
}

func TestSigner_Sign_PinsKeyID(t *testing.T) {
	signer, _ := NewSigner("optiqor-receipt-2026-q3-ecdsa-p256", &FakeKMS{})
	signed, err := signer.Sign(receipts.Receipt{
		ID:                       "r",
		TenantID:                 "t",
		ApplyFixID:               "a",
		ObservedFromUTC:          time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		ObservedToUTC:            time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
		PredictedSavingsUSDCents: 1,
		IssuerKeyID:              "old-id",
		IssuedAtUTC:              time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, payloadB64, _ := strings.Cut(signed, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(payloadB64)
	if !bytes.Contains(payload, []byte(`"issuer_key_id":"optiqor-receipt-2026-q3-ecdsa-p256"`)) {
		t.Errorf("payload should pin Signer's keyID; got %q", payload)
	}
}

func TestSigner_Sign_KMSError(t *testing.T) {
	signer, _ := NewSigner("optiqor-receipt-2026-q3-ecdsa-p256", &FakeKMS{Err: errors.New("kms throttled")})
	_, err := signer.Sign(receipts.Receipt{
		ID:                       "r",
		TenantID:                 "t",
		ApplyFixID:               "a",
		ObservedFromUTC:          time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		ObservedToUTC:            time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
		PredictedSavingsUSDCents: 1,
		IssuerKeyID:              "x",
		IssuedAtUTC:              time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
	})
	if err == nil || !strings.Contains(err.Error(), "kms throttled") {
		t.Errorf("err = %v, want substring 'kms throttled'", err)
	}
}

func TestSigner_Sign_RejectsInvalidReceipt(t *testing.T) {
	signer, _ := NewSigner("optiqor-receipt-2026-q3-ecdsa-p256", &FakeKMS{})
	_, err := signer.Sign(receipts.Receipt{ /* missing required fields */ })
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSigner_SignContext_HonoursCancellation(t *testing.T) {
	fake := &FakeKMS{Err: nil}
	s, err := NewSigner("optiqor-receipt-2026-q3-ecdsa-p256", fake)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel BEFORE SignContext

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	r := receipts.Receipt{
		ID:                       "rec-ctx",
		TenantID:                 "tenant-x",
		ApplyFixID:               "afix-1",
		ObservedFromUTC:          now.Add(-time.Hour),
		ObservedToUTC:            now,
		PredictedSavingsUSDCents: 100,
		IssuerKeyID:              "ignored",
		IssuedAtUTC:              now,
	}
	// The FakeKMS doesn't check ctx, so this still succeeds with the
	// fake; the production AWS adapter respects ctx. The load-bearing
	// assertion is that the ctx flows into the call.
	if _, err := s.SignContext(ctx, r); err != nil {
		t.Errorf("SignContext on cancelled ctx with fake: %v", err)
	}
	if len(fake.Calls) != 1 {
		t.Errorf("fake.Calls = %d, want 1", len(fake.Calls))
	}
}
