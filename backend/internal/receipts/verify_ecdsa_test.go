package receipts

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestVerify_ECDSAReceipt_RoundTrip(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyID := "optiqor-receipt-2026-q3-ecdsa-p256"
	reg := newEcdsaRegistry()
	reg.Add(keyID, &priv.PublicKey)

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	r := Receipt{
		ID:                       "rec-ec-1",
		TenantID:                 "tenant-x",
		ApplyFixID:               "afix-1",
		ObservedFromUTC:          now.Add(-30 * 24 * time.Hour),
		ObservedToUTC:            now,
		PredictedSavingsUSDCents: 12345,
		IssuerKeyID:              keyID,
		IssuedAtUTC:              now,
	}
	canon, err := Canonical(r)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canon)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signed := base64.RawURLEncoding.EncodeToString(sig) + "." + base64.RawURLEncoding.EncodeToString(canon)

	got, err := Verify(signed, reg)
	if err != nil {
		t.Fatalf("Verify ECDSA: %v", err)
	}
	if got.ID != r.ID {
		t.Errorf("payload roundtrip lost: %+v", got)
	}
}

func TestVerify_ECDSAKey_PlainRegistryRejected(t *testing.T) {
	keyID := "optiqor-receipt-2026-q3-ecdsa-p256"
	plainReg := NewStaticRegistry()
	signed := dummySignedReceipt(t, keyID)
	_, err := Verify(signed, plainReg)
	if err == nil {
		t.Fatal("expected error: ECDSA keyID with Ed25519-only Registry")
	}
	if !strings.Contains(err.Error(), "AlgoRegistry") {
		t.Errorf("err %q should mention AlgoRegistry", err.Error())
	}
}

func TestVerify_ECDSASignatureTampered(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	keyID := "optiqor-receipt-2026-q3-ecdsa-p256"
	reg := newEcdsaRegistry()
	reg.Add(keyID, &priv.PublicKey)

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	r := Receipt{
		ID:                       "rec-ec-2",
		TenantID:                 "tenant-x",
		ApplyFixID:               "afix-1",
		ObservedFromUTC:          now.Add(-time.Hour),
		ObservedToUTC:            now,
		PredictedSavingsUSDCents: 1,
		IssuerKeyID:              keyID,
		IssuedAtUTC:              now,
	}
	canon, _ := Canonical(r)
	digest := sha256.Sum256(canon)
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	// Flip a byte in the signature so verify fails.
	sig[0] ^= 0xFF
	signed := base64.RawURLEncoding.EncodeToString(sig) + "." + base64.RawURLEncoding.EncodeToString(canon)

	_, err := Verify(signed, reg)
	if !errors.Is(err, ErrSignature) {
		t.Errorf("tampered ECDSA sig: err = %v, want ErrSignature", err)
	}
}

type ecdsaRegistry struct{ keys map[string]*ecdsa.PublicKey }

func newEcdsaRegistry() *ecdsaRegistry { return &ecdsaRegistry{keys: map[string]*ecdsa.PublicKey{}} }

func (r *ecdsaRegistry) Add(keyID string, pub *ecdsa.PublicKey) { r.keys[keyID] = pub }
func (r *ecdsaRegistry) PublicKey(_ string) (ed25519.PublicKey, error) {
	return nil, errors.New("ecdsaRegistry: not ed25519")
}
func (r *ecdsaRegistry) PublicKeyAny(keyID string) (crypto.PublicKey, error) {
	if p, ok := r.keys[keyID]; ok {
		return p, nil
	}
	return nil, errors.New("ecdsa keyID not found")
}

func dummySignedReceipt(t *testing.T, keyID string) string {
	t.Helper()
	r := Receipt{
		ID: "x", TenantID: "y", ApplyFixID: "z",
		ObservedFromUTC: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		ObservedToUTC:   time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
		IssuerKeyID:     keyID,
		IssuedAtUTC:     time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
	}
	canon, _ := Canonical(r)
	return base64.RawURLEncoding.EncodeToString([]byte("sig-bytes")) + "." + base64.RawURLEncoding.EncodeToString(canon)
}
