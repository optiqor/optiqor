// Package kms provides a KMS-backed receipt signer. Per ADR-0017,
// production receipts are signed with ECDSA P-256 + SHA-256 because
// AWS KMS does not support Ed25519 native signing. The private key
// material never leaves KMS; every Sign call is logged in CloudTrail.
//
// This package contains the platform-agnostic Signer plus the
// KMSClient interface that lets tests swap in a deterministic fake.
// The aws-sdk-go-v2 wiring lands in a separate build-tagged adapter.
package kms

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/optiqor/optiqor/internal/receipts"
)

// KMSClient is the narrow subset of AWS KMS this signer uses. The real
// *kms.Client (aws-sdk-go-v2) satisfies it; tests use FakeKMS.
type KMSClient interface {
	Sign(ctx context.Context, keyID string, message []byte) (signature []byte, err error)
}

// SigningAlgorithm is fixed per ADR-0017; exposed so callers can pin
// it in logs / receipt provenance.
const SigningAlgorithm = "ECDSA_SHA_256"

// Signer satisfies the receipts.Issuer surface used by the Apply Fix
// path: same Sign(Receipt) (string, error) signature, same wire shape
// (`sig.payload`), different algorithm identifier in the key id.
//
// Sign is concurrency-safe.
type Signer struct {
	KeyID  string
	Client KMSClient
}

func NewSigner(keyID string, client KMSClient) (*Signer, error) {
	if keyID == "" {
		return nil, errors.New("receipts/kms: empty keyID")
	}
	if client == nil {
		return nil, errors.New("receipts/kms: nil KMSClient")
	}
	if !strings.Contains(keyID, "ecdsa-p256") {
		return nil, fmt.Errorf("receipts/kms: keyID %q must encode the algorithm (contain 'ecdsa-p256') per ADR-0017", keyID)
	}
	return &Signer{KeyID: keyID, Client: client}, nil
}

// Sign hashes the canonical receipt payload with SHA-256 (the prep
// step KMS expects when SigningAlgorithm = ECDSA_SHA_256) and asks
// KMS for the DER-encoded signature. The wire form stays
// `b64url(sig) "." b64url(payload)` so verifiers don't change shape;
// only the verification function dispatches on the keyID prefix to
// pick ecdsa.Verify over ed25519.Verify.
func (s *Signer) Sign(r receipts.Receipt) (string, error) {
	r.IssuerKeyID = s.KeyID
	if err := r.Validate(); err != nil {
		return "", err
	}
	payload, err := receipts.Canonical(r)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	sig, err := s.Client.Sign(context.Background(), s.KeyID, digest[:])
	if err != nil {
		return "", fmt.Errorf("receipts/kms: kms:Sign: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(sig) + "." + base64.RawURLEncoding.EncodeToString(payload), nil
}

// KeyID returns the keyID; matches the receipts.Issuer accessor shape.
func (s *Signer) KeyIDValue() string { return s.KeyID }

// FakeKMS satisfies KMSClient with a deterministic per-keyID
// signature for unit + integration tests. Real production wires the
// aws-sdk-go-v2 kms.Client behind a build tag.
type FakeKMS struct {
	// Err makes Sign return the given error. Useful for failure-path tests.
	Err error
	// Calls is appended on every Sign; tests inspect to verify keyID + payload.
	Calls []FakeCall
}

type FakeCall struct {
	KeyID  string
	Digest []byte
}

func (f *FakeKMS) Sign(_ context.Context, keyID string, digest []byte) ([]byte, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	f.Calls = append(f.Calls, FakeCall{KeyID: keyID, Digest: append([]byte(nil), digest...)})
	// Deterministic, non-zero signature blob; not a real ECDSA sig but
	// adequate for round-trip + algorithm-routing tests.
	h := sha256.Sum256(append([]byte("fake-kms:"+keyID+":"), digest...))
	return append([]byte("FAKE-ECDSA-SIG/"), h[:]...), nil
}
