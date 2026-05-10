// Package receipts issues and verifies Optiqor's Ed25519-signed
// Verified Receipts.
//
// A Receipt is the cryptographic claim "this Apply Fix saved this
// many dollars, measured against the customer's cloud bill between
// these timestamps". The public verifier at optiqor.dev/r/<id> lets
// finance teams confirm the claim without trusting Optiqor's word.
//
// Wire format:
//
//	signed_receipt = b64url(signature)  "."  b64url(canonical_payload_json)
//
// Verification:
//
//	1. Decode both halves.
//	2. Hash the payload with SHA-512.
//	3. Verify the Ed25519 signature against the issuer's published
//	   public key.
//	4. Compare the receipt's `issuer_key_id` against the issuer-key
//	   registry to detect rotated or revoked keys.
//
// Determinism: the canonical payload uses the [Canonical] helper to
// encode JSON with sorted keys and stable struct field order. Two
// signers presented with the same Receipt MUST produce identical
// signatures.
package receipts

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Receipt is the canonical signed claim. Keep the struct field order
// stable forever — re-ordering breaks signature verification on every
// previously issued receipt. Add new fields at the bottom with an
// `omitempty` tag and a v2 KeyID.
type Receipt struct {
	// ID is the receipt's stable identifier (e.g. UUIDv7). Carries the
	// transparency-log index.
	ID string `json:"id"`
	// TenantID anchors the receipt to the customer who realised the
	// savings.
	TenantID string `json:"tenant_id"`
	// Workload is the workload the Apply Fix landed against.
	Workload string `json:"workload"`
	// ApplyFixID links back to the apply_fixes row in Postgres so an
	// auditor can replay the PR + dry-run + merge artefacts.
	ApplyFixID string `json:"apply_fix_id"`
	// ObservedFromUTC and ObservedToUTC bracket the measurement window
	// against the cloud bill.
	ObservedFromUTC time.Time `json:"observed_from_utc"`
	ObservedToUTC   time.Time `json:"observed_to_utc"`
	// PredictedSavingsUSDCents is what Optiqor said before the merge.
	PredictedSavingsUSDCents int64 `json:"predicted_savings_usd_cents"`
	// RealisedSavingsUSDCents is what the bill said after the window.
	RealisedSavingsUSDCents int64 `json:"realised_savings_usd_cents"`
	// CloudBillSource identifies which dataset backs the claim — CUR
	// row range, Azure Cost Mgmt export, Hetzner invoice, etc.
	CloudBillSource string `json:"cloud_bill_source"`
	// IssuerKeyID is the rotating signer key. The verifier looks it up
	// in the public registry.
	IssuerKeyID string `json:"issuer_key_id"`
	// IssuedAtUTC is when the signer ran.
	IssuedAtUTC time.Time `json:"issued_at_utc"`
}

// Validate returns the first invariant violation found, or nil. The
// signer rejects invalid receipts; the verifier double-checks them.
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

// Issuer signs receipts with the holder of a private Ed25519 key. One
// Issuer per running process; the private key lives in
// AWS Secrets Manager and is loaded on boot.
type Issuer struct {
	keyID      string
	privateKey ed25519.PrivateKey
}

// NewIssuer constructs an Issuer from raw key material. The seed is
// validated; an empty key id rejects.
func NewIssuer(keyID string, priv ed25519.PrivateKey) (*Issuer, error) {
	if keyID == "" {
		return nil, errors.New("receipts: keyID must be non-empty")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("receipts: private key size = %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
	return &Issuer{keyID: keyID, privateKey: priv}, nil
}

// GenerateIssuer returns an Issuer with a freshly-generated key. Use
// for tests; production keys are loaded from KMS.
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

// Sign returns the wire-format signed receipt. The Receipt's
// IssuerKeyID is overwritten with the Issuer's key id so callers can
// pass a partially-populated struct.
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

// KeyID returns the issuer's key id so callers can record it alongside
// the signed receipt.
func (i *Issuer) KeyID() string { return i.keyID }

// Registry maps a key id to the public key registered for it. The
// transparency-log surface is built on top.
type Registry interface {
	PublicKey(keyID string) (ed25519.PublicKey, error)
}

// StaticRegistry is the in-memory implementation. Production uses a
// DB-backed registry that the audit pipeline reads from.
type StaticRegistry struct{ keys map[string]ed25519.PublicKey }

// NewStaticRegistry returns an empty registry.
func NewStaticRegistry() *StaticRegistry {
	return &StaticRegistry{keys: map[string]ed25519.PublicKey{}}
}

// Add registers a public key under the given id.
func (s *StaticRegistry) Add(keyID string, pub ed25519.PublicKey) {
	s.keys[keyID] = append(ed25519.PublicKey(nil), pub...)
}

// PublicKey looks up a registered key.
func (s *StaticRegistry) PublicKey(keyID string) (ed25519.PublicKey, error) {
	k, ok := s.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("receipts: no public key registered for %q", keyID)
	}
	return k, nil
}

// Verify decodes a wire-format signed receipt, looks up the issuer
// key, checks the signature, validates invariants, and returns the
// receipt. The (Receipt, error) shape is chosen so the failure mode is
// explicit: a non-nil error always means "do not trust this claim".
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
// validate against the registered public key. Surface this as 401 in
// the public verification endpoint — never echo the receipt body.
var ErrSignature = errors.New("receipts: signature verification failed")

func encode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
