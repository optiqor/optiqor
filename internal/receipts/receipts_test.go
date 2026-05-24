package receipts

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"
)

func mkReceipt() Receipt {
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	return Receipt{
		ID:                       "rcpt_01HQ0",
		TenantID:                 "tenant-abc",
		Workload:                 "api",
		ApplyFixID:               "afix_01HP9",
		ObservedFromUTC:          from,
		ObservedToUTC:            from.Add(30 * 24 * time.Hour),
		PredictedSavingsUSDCents: 12000,
		RealisedSavingsUSDCents:  11500,
		CloudBillSource:          "aws/cur:2026-05",
		IssuedAtUTC:              from.Add(31 * 24 * time.Hour),
	}
}

func TestIssuer_RejectsBadKey(t *testing.T) {
	if _, err := NewIssuer("", make(ed25519.PrivateKey, ed25519.PrivateKeySize)); err == nil {
		t.Error("want error on empty keyID")
	}
	if _, err := NewIssuer("k1", ed25519.PrivateKey{0x01}); err == nil {
		t.Error("want error on truncated private key")
	}
}

func TestSign_RejectsInvalidReceipt(t *testing.T) {
	iss, _, err := GenerateIssuer("k1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := iss.Sign(Receipt{}); err == nil {
		t.Error("want error signing empty receipt")
	}
}

func TestSignVerify_RoundTrip(t *testing.T) {
	iss, pub, err := GenerateIssuer("k1")
	if err != nil {
		t.Fatal(err)
	}
	reg := NewStaticRegistry()
	reg.Add("k1", pub)

	signed, err := iss.Sign(mkReceipt())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	got, err := Verify(signed, reg)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.ID != "rcpt_01HQ0" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.IssuerKeyID != "k1" {
		t.Errorf("KeyID = %q, want k1 (issuer must overwrite)", got.IssuerKeyID)
	}
}

func TestSign_Deterministic(t *testing.T) {
	// Transparency-log indexes assume Sign is functionally pure: same
	// receipt + same key MUST produce identical wire bytes.
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	iss, err := NewIssuer("k1", priv)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewStaticRegistry()
	reg.Add("k1", pub)

	a, err := iss.Sign(mkReceipt())
	if err != nil {
		t.Fatal(err)
	}
	b, err := iss.Sign(mkReceipt())
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("Sign is non-deterministic:\n%s\nvs\n%s", a, b)
	}
}

func TestVerify_TamperedPayloadFails(t *testing.T) {
	iss, pub, err := GenerateIssuer("k1")
	if err != nil {
		t.Fatal(err)
	}
	reg := NewStaticRegistry()
	reg.Add("k1", pub)
	signed, _ := iss.Sign(mkReceipt())

	dot := strings.IndexByte(signed, '.')
	tampered := signed[:dot+5] + "x" + signed[dot+6:]
	_, err = Verify(tampered, reg)
	if err == nil {
		t.Fatal("want verify failure on tampered payload")
	}
}

func TestVerify_TamperedSignatureFails(t *testing.T) {
	iss, pub, err := GenerateIssuer("k1")
	if err != nil {
		t.Fatal(err)
	}
	reg := NewStaticRegistry()
	reg.Add("k1", pub)
	signed, _ := iss.Sign(mkReceipt())

	tampered := "AAAA" + signed[4:]
	_, err = Verify(tampered, reg)
	if !errors.Is(err, ErrSignature) && err == nil {
		t.Fatal("want signature error")
	}
}

func TestVerify_UnknownKeyIDFails(t *testing.T) {
	iss, pub, err := GenerateIssuer("rotated-key")
	if err != nil {
		t.Fatal(err)
	}
	reg := NewStaticRegistry()
	reg.Add("k1", pub) // issuer signs under "rotated-key"; registry only knows "k1"

	signed, _ := iss.Sign(mkReceipt())
	if _, err := Verify(signed, reg); err == nil {
		t.Fatal("want verify failure when issuer key not in registry")
	}
}

func TestVerify_WrongPublicKeyFails(t *testing.T) {
	iss, _, err := GenerateIssuer("k1")
	if err != nil {
		t.Fatal(err)
	}
	// Different keypair registered under the same id — simulates a
	// registry tricked into trusting the wrong pubkey.
	otherPub, _, _ := ed25519.GenerateKey(nil)
	reg := NewStaticRegistry()
	reg.Add("k1", otherPub)
	signed, _ := iss.Sign(mkReceipt())

	_, err = Verify(signed, reg)
	if !errors.Is(err, ErrSignature) {
		t.Errorf("want ErrSignature, got %v", err)
	}
}

func TestVerify_MalformedInputFails(t *testing.T) {
	reg := NewStaticRegistry()
	cases := []string{
		"",
		"only-one-part",
		"!!!.!!!",
		"AA..BB",
	}
	for _, c := range cases {
		if _, err := Verify(c, reg); err == nil {
			t.Errorf("Verify(%q) wanted error", c)
		}
	}
}

func TestCanonical_StableFieldOrder(t *testing.T) {
	r := mkReceipt()
	a, err := Canonical(r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Canonical(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("Canonical is non-deterministic")
	}
}

func TestReceipt_Validate(t *testing.T) {
	valid := mkReceipt()
	valid.IssuerKeyID = "k1"
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid receipt should pass: %v", err)
	}
	bad := valid
	bad.ObservedToUTC = bad.ObservedFromUTC.Add(-time.Hour)
	if err := bad.Validate(); err == nil {
		t.Error("inverted observation window should fail validation")
	}
}
