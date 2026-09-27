//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/optiqor/optiqor/internal/receipts"
)

func TestReceipts_SignStoreLoadVerify(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	tenantA := uuid.NewString()
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, 'A')`,
		tenantA, "recpt-"+tenantA[:8]); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyID := "test-2026-05-24"
	iss, err := receipts.NewIssuer(keyID, priv)
	if err != nil {
		t.Fatal(err)
	}
	reg := receipts.NewStaticRegistry()
	reg.Add(keyID, pub)

	receiptID := uuid.NewString()
	applyFixID := uuid.NewString()
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)

	original := receipts.Receipt{
		ID:                       receiptID,
		TenantID:                 tenantA,
		Workload:                 "default/api",
		ApplyFixID:               applyFixID,
		ObservedFromUTC:          now.Add(-30 * 24 * time.Hour),
		ObservedToUTC:            now,
		PredictedSavingsUSDCents: 12345,
		RealisedSavingsUSDCents:  11000,
		CloudBillSource:          "aws-cur:v1",
		IssuerKeyID:              keyID,
		IssuedAtUTC:              now,
	}

	signed, err := iss.Sign(original)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// payload is TEXT (not JSONB) so the signed bytes round-trip
	// unchanged; JSONB reformats on read and breaks Verify.
	canonical, err := receipts.Canonical(original)
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}

	if _, err := h.PG.Exec(ctx, `
		INSERT INTO receipts (id, tenant_id, apply_fix_id, tier, methodology,
		                      window_start, window_end,
		                      predicted_usd_cents, actual_usd_cents,
		                      payload, signature, signing_key_id, issued_at)
		VALUES ($1, $2, $3, 'hybrid', 'hybrid_v1.0',
		        $4, $5, $6, $7, $8, $9, $10, $11)`,
		receiptID, tenantA, nil,
		original.ObservedFromUTC, original.ObservedToUTC,
		original.PredictedSavingsUSDCents, original.RealisedSavingsUSDCents,
		string(canonical), sigBytes(signed), keyID, now,
	); err != nil {
		t.Fatalf("insert receipt: %v", err)
	}

	var (
		loadedPayloadJSON []byte
		loadedSig         []byte
	)
	if err := h.PG.QueryRow(ctx,
		`SELECT payload, signature FROM receipts WHERE id = $1`, receiptID).
		Scan(&loadedPayloadJSON, &loadedSig); err != nil {
		t.Fatalf("load: %v", err)
	}

	if !bytes.Equal(loadedPayloadJSON, canonical) {
		t.Errorf("payload bytes mutated by storage:\n  signed:   %q\n  loaded:   %q", canonical, loadedPayloadJSON)
	}
	rebuilt := encodeWire(loadedSig) + "." + encodeWire(loadedPayloadJSON)
	verified, err := receipts.Verify(rebuilt, reg)
	if err != nil {
		t.Fatalf("Verify after JSONB round-trip: %v\n  signed:    %q\n  rebuilt:   %q\n  canonical: %q\n  loaded:    %q",
			err, signed, rebuilt, canonical, loadedPayloadJSON)
	}
	if verified.ID != original.ID || verified.PredictedSavingsUSDCents != original.PredictedSavingsUSDCents {
		t.Errorf("payload drift: got %+v want %+v", verified, original)
	}
}

func sigBytes(signed string) []byte {
	sigB64, _, _ := strings.Cut(signed, ".")
	out, err := base64URLDecode(sigB64)
	if err != nil {
		panic(err)
	}
	return out
}

func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func encodeWire(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func TestReceipts_TamperedSignatureRejected(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	iss, _ := receipts.NewIssuer("tamper-test", priv)
	reg := receipts.NewStaticRegistry()
	reg.Add("tamper-test", pub)

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	signed, err := iss.Sign(receipts.Receipt{
		ID:                       uuid.NewString(),
		TenantID:                 uuid.NewString(),
		ApplyFixID:               uuid.NewString(),
		ObservedFromUTC:          now.Add(-time.Hour),
		ObservedToUTC:            now,
		PredictedSavingsUSDCents: 100,
		IssuerKeyID:              "tamper-test",
		IssuedAtUTC:              now,
	})
	if err != nil {
		t.Fatal(err)
	}

	sig, payload, _ := strings.Cut(signed, ".")
	tampered := flipFirstChar(sig) + "." + payload
	if _, err := receipts.Verify(tampered, reg); !errors.Is(err, receipts.ErrSignature) {
		t.Errorf("tampered sig: err = %v, want ErrSignature", err)
	}
}

func flipFirstChar(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}
