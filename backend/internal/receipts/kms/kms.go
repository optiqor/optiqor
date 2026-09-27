// Package kms signs receipts via AWS KMS ECDSA P-256 + SHA-256 per
// ADR-0017. Key material never leaves KMS; every Sign hits CloudTrail.
// The aws-sdk-go-v2 wiring lands behind a build tag.
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

// Sign uses context.Background; workflow code should call SignContext
// so the kms:Sign call inherits the request deadline + trace span.
func (s *Signer) Sign(r receipts.Receipt) (string, error) {
	return s.SignContext(context.Background(), r)
}

func (s *Signer) SignContext(ctx context.Context, r receipts.Receipt) (string, error) {
	r.IssuerKeyID = s.KeyID
	if err := r.Validate(); err != nil {
		return "", err
	}
	payload, err := receipts.Canonical(r)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	sig, err := s.Client.Sign(ctx, s.KeyID, digest[:])
	if err != nil {
		return "", fmt.Errorf("receipts/kms: kms:Sign: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(sig) + "." + base64.RawURLEncoding.EncodeToString(payload), nil
}

// KeyID returns the keyID; matches the receipts.Issuer accessor shape.
func (s *Signer) KeyIDValue() string { return s.KeyID }

// FakeKMS is the deterministic test double for KMSClient.
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
