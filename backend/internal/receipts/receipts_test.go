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

func TestNewIssuer_RejectsBadKey(t *testing.T) {
	for _, tc := range []struct {
		name  string
		keyID string
		key   ed25519.PrivateKey
	}{
		{"empty key id", "", make(ed25519.PrivateKey, ed25519.PrivateKeySize)},
		{"truncated private key", "k1", ed25519.PrivateKey{0x01}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewIssuer(tc.keyID, tc.key); err == nil {
				t.Error("want error")
			}
		})
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

func TestVerify(t *testing.T) {
	for _, tc := range []struct {
		name string
		// build returns the signed string to verify and the registry
		// that should be queried. Returning both lets each row construct
		// its own keyset (e.g. unknown key id, wrong public key).
		build   func(t *testing.T) (signed string, reg Registry)
		wantErr error // errors.Is target; nil means "any non-nil err is acceptable"
	}{
		{
			name: "tampered payload byte fails",
			build: func(t *testing.T) (string, Registry) {
				t.Helper()
				iss, pub, err := GenerateIssuer("k1")
				if err != nil {
					t.Fatal(err)
				}
				signed, _ := iss.Sign(mkReceipt())
				dot := strings.IndexByte(signed, '.')
				return signed[:dot+5] + "x" + signed[dot+6:], registryWith(t, pub)
			},
		},
		{
			name: "tampered signature prefix fails",
			build: func(t *testing.T) (string, Registry) {
				t.Helper()
				iss, pub, err := GenerateIssuer("k1")
				if err != nil {
					t.Fatal(err)
				}
				signed, _ := iss.Sign(mkReceipt())
				return "AAAA" + signed[4:], registryWith(t, pub)
			},
		},
		{
			name: "issuer key id not in registry fails",
			build: func(t *testing.T) (string, Registry) {
				t.Helper()
				iss, pub, err := GenerateIssuer("rotated-key")
				if err != nil {
					t.Fatal(err)
				}
				// Issuer signs under "rotated-key"; registry only knows "k1".
				signed, _ := iss.Sign(mkReceipt())
				return signed, registryWith(t, pub)
			},
		},
		{
			name: "registry trusts wrong public key for id",
			build: func(t *testing.T) (string, Registry) {
				t.Helper()
				iss, _, err := GenerateIssuer("k1")
				if err != nil {
					t.Fatal(err)
				}
				// Different keypair registered under the same id —
				// simulates a registry tricked into trusting the wrong pubkey.
				otherPub, _, _ := ed25519.GenerateKey(nil)
				signed, _ := iss.Sign(mkReceipt())
				return signed, registryWith(t, otherPub)
			},
			wantErr: ErrSignature,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signed, reg := tc.build(t)
			_, err := Verify(signed, reg)
			if err == nil {
				t.Fatal("want verify failure")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestVerify_MalformedInputFails(t *testing.T) {
	reg := NewStaticRegistry()
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"empty string", ""},
		{"single part no separator", "only-one-part"},
		{"non-base64 parts", "!!!.!!!"},
		{"empty payload between dots", "AA..BB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Verify(tc.in, reg); err == nil {
				t.Errorf("Verify(%q) wanted error", tc.in)
			}
		})
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
	base := mkReceipt()
	base.IssuerKeyID = "k1"

	for _, tc := range []struct {
		name    string
		mut     func(*Receipt)
		wantErr bool
	}{
		{name: "valid receipt passes", mut: func(*Receipt) {}, wantErr: false},
		{
			name:    "inverted observation window fails",
			mut:     func(r *Receipt) { r.ObservedToUTC = r.ObservedFromUTC.Add(-time.Hour) },
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mut(&r)
			err := r.Validate()
			if tc.wantErr && err == nil {
				t.Error("want validation error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// registryWith builds a registry trusting pub under key id "k1" — the
// canonical id used across every TestVerify row.
func registryWith(t *testing.T, pub ed25519.PublicKey) Registry {
	t.Helper()
	r := NewStaticRegistry()
	r.Add("k1", pub)
	return r
}
