//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/receipts"
	"github.com/optiqor/optiqor/internal/receipts/kms"
)

// TestKMSSigner_FakeRoundtrip exercises NewSigner + Sign against the
// in-tree FakeKMS. Pins ADR-0017's wire shape: `b64url(sig).b64url(payload)`,
// IssuerKeyID forced to the Signer's keyID, FakeKMS sees a 32-byte
// SHA-256 digest (the ECDSA_SHA_256 prep step).
func TestKMSSigner_FakeRoundtrip(t *testing.T) {
	fake := &kms.FakeKMS{}
	signer, err := kms.NewSigner("optiqor-receipt-2026-q3-ecdsa-p256", fake)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	out, err := signer.Sign(receipts.Receipt{
		ID:                       "rec-integration",
		TenantID:                 "tenant-int",
		ApplyFixID:               "afix-1",
		ObservedFromUTC:          now.Add(-30 * 24 * time.Hour),
		ObservedToUTC:            now,
		PredictedSavingsUSDCents: 50000,
		IssuerKeyID:              "_pinned by Sign_",
		IssuedAtUTC:              now,
	})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !strings.Contains(out, ".") {
		t.Errorf("wire shape missing sig.payload separator")
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("KMS called %d times, want 1", len(fake.Calls))
	}
	if fake.Calls[0].KeyID != "optiqor-receipt-2026-q3-ecdsa-p256" {
		t.Errorf("KeyID = %q", fake.Calls[0].KeyID)
	}
	if len(fake.Calls[0].Digest) != 32 {
		t.Errorf("digest len = %d, want 32 (SHA-256)", len(fake.Calls[0].Digest))
	}
}

// TestKMSSigner_AlgorithmIsLoadBearing — ADR-0017 mandates the keyID
// encode the algorithm so verifiers can dispatch ed25519 vs ecdsa.
func TestKMSSigner_AlgorithmIsLoadBearing(t *testing.T) {
	_, err := kms.NewSigner("optiqor-receipt-2026-q3-ed25519", &kms.FakeKMS{})
	if err == nil {
		t.Fatal("NewSigner accepted an Ed25519 keyID — must reject per ADR-0017")
	}
	if !strings.Contains(err.Error(), "ecdsa-p256") {
		t.Errorf("err %q should hint at the required algorithm tag", err.Error())
	}
}
