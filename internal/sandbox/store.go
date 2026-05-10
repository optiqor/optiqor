package sandbox

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// ErrNotFound is returned by Store.Get when a hash is not present.
var ErrNotFound = errors.New("sandbox: share not found")

// SharedAnalysis is what /r/<hash> serves. The structured fields
// (Source, Workloads, Findings) let the share handler render either
// JSON (Accept: application/json) or HTML (default, via pkg/htmlrender)
// without re-parsing the cached body.
type SharedAnalysis struct {
	Hash      string
	Body      []byte // canonical JSON representation, ready to stream
	MediaType string

	// Structured echo of the analysis so callers can render alternate
	// formats. Populated by the analyze handler when storing.
	Source    string
	Workloads int
	Findings  []rules.Finding

	CreatedAt time.Time
	ExpiresAt time.Time
}

// Store persists sanitised analyses under their content hash. The
// production implementation backs onto Postgres; tests + local dev
// use [InMemoryStore].
type Store interface {
	Put(ctx context.Context, sa SharedAnalysis) error
	Get(ctx context.Context, hash string) (SharedAnalysis, error)
}

// InMemoryStore is the in-process implementation. Safe for concurrent
// use; entries respect ExpiresAt on read so callers don't see stale
// data.
type InMemoryStore struct {
	mu  sync.RWMutex
	now func() time.Time
	m   map[string]SharedAnalysis
}

// NewInMemoryStore returns an empty store using time.Now for expiry.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{m: map[string]SharedAnalysis{}, now: time.Now}
}

// Put stores the entry, overwriting any prior version.
func (s *InMemoryStore) Put(_ context.Context, sa SharedAnalysis) error {
	if sa.Hash == "" {
		return errors.New("sandbox: empty hash")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[sa.Hash] = sa
	return nil
}

// Get returns the entry or ErrNotFound. Expired entries are returned
// as ErrNotFound so callers can lazily prune.
func (s *InMemoryStore) Get(_ context.Context, hash string) (SharedAnalysis, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sa, ok := s.m[hash]
	if !ok {
		return SharedAnalysis{}, ErrNotFound
	}
	if !sa.ExpiresAt.IsZero() && s.now().After(sa.ExpiresAt) {
		return SharedAnalysis{}, ErrNotFound
	}
	return sa, nil
}

// Len is for tests.
func (s *InMemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}
