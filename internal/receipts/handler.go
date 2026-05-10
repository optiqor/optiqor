package receipts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/optiqor/backend/internal/tenancy"
)

// Store is the persistence seam for issued receipts. Production wires
// the receipts table behind it; tests + dev mode use [InMemoryStore].
type Store interface {
	Save(ctx context.Context, t tenancy.Context, id string, signed string, r Receipt) error
	Get(ctx context.Context, id string) (signed string, r Receipt, err error)
}

// ErrReceiptNotFound is returned by Store.Get when the id is unknown.
var ErrReceiptNotFound = errors.New("receipts: not found")

// InMemoryStore is the in-process [Store] used by tests and dev. Safe
// for concurrent use.
type InMemoryStore struct {
	mu sync.RWMutex
	m  map[string]entry
}

type entry struct {
	Signed  string
	Receipt Receipt
}

// NewInMemoryStore returns an empty store.
func NewInMemoryStore() *InMemoryStore { return &InMemoryStore{m: map[string]entry{}} }

// Save stores the receipt under id, overwriting any prior entry.
func (s *InMemoryStore) Save(_ context.Context, _ tenancy.Context, id, signed string, r Receipt) error {
	if id == "" {
		return errors.New("receipts: empty id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = entry{Signed: signed, Receipt: r}
	return nil
}

// Get returns the signed + parsed receipt or ErrReceiptNotFound.
func (s *InMemoryStore) Get(_ context.Context, id string) (string, Receipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.m[id]
	if !ok {
		return "", Receipt{}, ErrReceiptNotFound
	}
	return e.Signed, e.Receipt, nil
}

// Handler serves GET /v1/receipts/{id}. Public, unauth: a Receipt is
// designed to be independently verifiable.
type Handler struct {
	Store    Store
	Registry Registry
}

// VerifyResponse is the JSON envelope returned to the verifier.
type VerifyResponse struct {
	Receipt        Receipt `json:"receipt"`
	Signed         string  `json:"signed"`
	Verified       bool    `json:"verified"`
	IssuerKeyID    string  `json:"issuer_key_id"`
	VerifierNotice string  `json:"verifier_notice"`
}

// Get returns the stored receipt with a freshly-computed verification
// flag so the caller knows whether the Optiqor server itself still
// trusts the signature today.
//
//   400 — missing id
//   404 — unknown id
//   500 — store / registry failures
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	signed, parsed, err := h.Store.Get(r.Context(), id)
	if errors.Is(err, ErrReceiptNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "store: "+err.Error(), http.StatusInternalServerError)
		return
	}
	resp := VerifyResponse{
		Receipt:        parsed,
		Signed:         signed,
		IssuerKeyID:    parsed.IssuerKeyID,
		VerifierNotice: "Verification keys are published at https://optiqor.dev/keys",
	}
	if h.Registry != nil {
		if _, vErr := Verify(signed, h.Registry); vErr == nil {
			resp.Verified = true
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// Mount registers the receipt route on the given mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/receipts/{id}", h.Get)
}
