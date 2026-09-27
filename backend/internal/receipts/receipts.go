// Package receipts issues and verifies Ed25519-signed Verified Receipts.
//
// Wire format: b64url(signature) "." b64url(canonical_payload_json).
// Determinism of the payload bytes (see [Canonical]) is load-bearing:
// two signers presented with the same Receipt MUST produce identical
// signatures, since transparency-log indexes assume signatures are
// functionally pure.
package receipts

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Receipt is the canonical signed claim. Field order is load-bearing:
// re-ordering breaks signature verification on every previously issued
// receipt. Add new fields at the bottom with `omitempty` and a v2 KeyID.
type Receipt struct {
	ID                       string    `json:"id"`
	TenantID                 string    `json:"tenant_id"`
	Workload                 string    `json:"workload"`
	ApplyFixID               string    `json:"apply_fix_id"`
	ObservedFromUTC          time.Time `json:"observed_from_utc"`
	ObservedToUTC            time.Time `json:"observed_to_utc"`
	PredictedSavingsUSDCents int64     `json:"predicted_savings_usd_cents"`
	RealisedSavingsUSDCents  int64     `json:"realised_savings_usd_cents"`
	CloudBillSource          string    `json:"cloud_bill_source"`
	IssuerKeyID              string    `json:"issuer_key_id"`
	IssuedAtUTC              time.Time `json:"issued_at_utc"`
}

// Validate returns the first invariant violation found, or nil. Both
// signer and verifier run this so a forged Receipt that bypasses one
// path still fails the other.
func (r Receipt) Validate() error {
	switch {
	case r.ID == "":
		return errors.New("receipts: missing id")
	case r.TenantID == "":
		return errors.New("receipts: missing tenant_id")
	case r.ApplyFixID == "":
		return errors.New("receipts: missing apply_fix_id")
	case r.IssuerKeyID == "":
		return errors.New("receipts: missing issuer_key_id")
	case r.ObservedFromUTC.IsZero(), r.ObservedToUTC.IsZero():
		return errors.New("receipts: observed window must be set")
	case !r.ObservedToUTC.After(r.ObservedFromUTC):
		return errors.New("receipts: observed_to must be after observed_from")
	case r.IssuedAtUTC.IsZero():
		return errors.New("receipts: issued_at must be set")
	}
	return nil
}

// Issuer holds the private Ed25519 key used to sign receipts. One per
// process; the key lives in AWS Secrets Manager and loads at boot.
type Issuer struct {
	keyID      string
	privateKey ed25519.PrivateKey
}

func NewIssuer(keyID string, priv ed25519.PrivateKey) (*Issuer, error) {
	if keyID == "" {
		return nil, errors.New("receipts: keyID must be non-empty")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("receipts: private key size = %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
	return &Issuer{keyID: keyID, privateKey: priv}, nil
}

// GenerateIssuer returns an Issuer with a freshly-generated key. Tests
// only; production keys are loaded from KMS.
func GenerateIssuer(keyID string) (*Issuer, ed25519.PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("receipts: keygen: %w", err)
	}
	i, err := NewIssuer(keyID, priv)
	if err != nil {
		return nil, nil, err
	}
	return i, pub, nil
}

// Sign overwrites r.IssuerKeyID with the Issuer's key id before signing,
// so callers can pass a partially-populated struct.
func (i *Issuer) Sign(r Receipt) (string, error) {
	r.IssuerKeyID = i.keyID
	if err := r.Validate(); err != nil {
		return "", err
	}
	payload, err := Canonical(r)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(i.privateKey, payload)
	return encode(sig) + "." + encode(payload), nil
}

func (i *Issuer) KeyID() string { return i.keyID }

// Registry maps a key id to its public key. The interface returns the
// raw Ed25519 bytes for backwards compatibility; verifiers that need
// algorithm dispatch implement AlgoRegistry instead.
type Registry interface {
	PublicKey(keyID string) (ed25519.PublicKey, error)
}

// AlgoRegistry is the dispatch-aware shape Verify prefers when given.
// PublicKey returns whichever crypto.PublicKey matches keyID's algorithm
// tag (ADR-0017 mandates the algorithm appears in the keyID, e.g.
// "optiqor-receipt-2026-q3-ecdsa-p256"). Implementations may return
// either *ecdsa.PublicKey or ed25519.PublicKey.
type AlgoRegistry interface {
	PublicKeyAny(keyID string) (crypto.PublicKey, error)
}

type StaticRegistry struct{ keys map[string]ed25519.PublicKey }

func NewStaticRegistry() *StaticRegistry {
	return &StaticRegistry{keys: map[string]ed25519.PublicKey{}}
}

func (s *StaticRegistry) Add(keyID string, pub ed25519.PublicKey) {
	s.keys[keyID] = append(ed25519.PublicKey(nil), pub...)
}

func (s *StaticRegistry) PublicKey(keyID string) (ed25519.PublicKey, error) {
	k, ok := s.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("receipts: no public key registered for %q", keyID)
	}
	return k, nil
}

// Verify decodes a wire-format signed receipt, looks up the issuer
// key, and checks the signature. Dispatches on the keyID's algorithm
// tag (ADR-0017): keyIDs containing "ecdsa-p256" verify via ECDSA P-256
// + SHA-256; everything else uses Ed25519. A non-nil error always
// means "do not trust this claim".
//
// Pass either Registry (Ed25519-only) or AlgoRegistry — Verify type-
// asserts at call time so legacy Ed25519 callers stay source-compatible.
func Verify(signed string, reg Registry) (Receipt, error) {
	sigB64, payloadB64, ok := strings.Cut(signed, ".")
	if !ok {
		return Receipt{}, errors.New("receipts: malformed signed receipt: expected sig.payload")
	}
	sig, err := decode(sigB64)
	if err != nil {
		return Receipt{}, fmt.Errorf("receipts: signature decode: %w", err)
	}
	payload, err := decode(payloadB64)
	if err != nil {
		return Receipt{}, fmt.Errorf("receipts: payload decode: %w", err)
	}
	var r Receipt
	if err := unmarshal(payload, &r); err != nil {
		return Receipt{}, fmt.Errorf("receipts: payload unmarshal: %w", err)
	}
	if err := r.Validate(); err != nil {
		return Receipt{}, err
	}
	if strings.Contains(r.IssuerKeyID, "ecdsa-p256") {
		algo, ok := reg.(AlgoRegistry)
		if !ok {
			return Receipt{}, fmt.Errorf("receipts: keyID %q is ECDSA but Registry doesn't satisfy AlgoRegistry", r.IssuerKeyID)
		}
		pub, err := algo.PublicKeyAny(r.IssuerKeyID)
		if err != nil {
			return Receipt{}, err
		}
		ecPub, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			return Receipt{}, fmt.Errorf("receipts: keyID %q resolved to non-ECDSA key", r.IssuerKeyID)
		}
		digest := sha256.Sum256(payload)
		if !ecdsa.VerifyASN1(ecPub, digest[:], sig) {
			return Receipt{}, ErrSignature
		}
		return r, nil
	}
	pub, err := reg.PublicKey(r.IssuerKeyID)
	if err != nil {
		return Receipt{}, err
	}
	if !ed25519.Verify(pub, payload, sig) {
		return Receipt{}, ErrSignature
	}
	return r, nil
}

// ErrSignature is returned by [Verify] when the signature does not
// validate. Surface as 401 in the public verifier; never echo the
// receipt body on failure.
var ErrSignature = errors.New("receipts: signature verification failed")

func encode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
