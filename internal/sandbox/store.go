package sandbox

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"
)

var (
	ErrNotFound = errors.New("sandbox: share not found")
	// ErrExpired distinguishes a row that lived but is past TTL from
	// one that never existed. Handlers translate it to 410 Gone so
	// caching layers know not to retry.
	ErrExpired = errors.New("sandbox: share expired")
)

// SharedAnalysis is what /r/<hash> serves. The structured fields let
// the share handler render JSON or HTML without re-parsing Body.
type SharedAnalysis struct {
	Hash      string
	Body      []byte // canonical JSON, ready to stream
	MediaType string

	Source    string
	Workloads int
	Findings  []rules.Finding

	CreatedAt time.Time
	ExpiresAt time.Time
}

// Store persists sanitised analyses under their content hash. Prod
// backs onto Postgres (PgStore); tests and local dev use InMemoryStore.
type Store interface {
	Put(ctx context.Context, sa SharedAnalysis) error
	Get(ctx context.Context, hash string) (SharedAnalysis, error)
}

// InMemoryStore is safe for concurrent use; Get respects ExpiresAt so
// callers never see stale data.
type InMemoryStore struct {
	mu  sync.RWMutex
	now func() time.Time
	m   map[string]SharedAnalysis
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{m: map[string]SharedAnalysis{}, now: time.Now}
}

func (s *InMemoryStore) Put(_ context.Context, sa SharedAnalysis) error {
	if sa.Hash == "" {
		return errors.New("sandbox: empty hash")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[sa.Hash] = sa
	return nil
}

// Get returns ErrNotFound when the hash was never stored and
// ErrExpired when it lived but is past TTL — the share handler maps
// these to 404 vs 410 respectively.
func (s *InMemoryStore) Get(_ context.Context, hash string) (SharedAnalysis, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sa, ok := s.m[hash]
	if !ok {
		return SharedAnalysis{}, ErrNotFound
	}
	if !sa.ExpiresAt.IsZero() && s.now().After(sa.ExpiresAt) {
		return SharedAnalysis{}, ErrExpired
	}
	return sa, nil
}

func (s *InMemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}
