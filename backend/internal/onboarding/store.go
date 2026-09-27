package onboarding

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrTenantNotFound is what Service.Get checks before lazily creating
// a fresh State record.
var ErrTenantNotFound = errors.New("onboarding: tenant state not found")

type Store interface {
	Get(ctx context.Context, tenantID string) (State, error)
	Put(ctx context.Context, tenantID string, s State) error
}

// InMemoryStore is the dev + test Store; production swaps a Postgres
// adapter behind the same interface.
type InMemoryStore struct {
	mu sync.RWMutex
	m  map[string]State
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{m: map[string]State{}}
}

func (s *InMemoryStore) Get(_ context.Context, tenantID string) (State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[tenantID]
	if !ok {
		return State{}, ErrTenantNotFound
	}
	return v, nil
}

func (s *InMemoryStore) Put(_ context.Context, tenantID string, st State) error {
	if tenantID == "" {
		return errors.New("onboarding: empty tenant id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[tenantID] = st
	return nil
}

// Service keeps the HTTP handler a thin shell. Phase 2 has no separate
// signup event, so Service.Get treats first read as signup.
type Service struct {
	Store Store
	// Now defaults to time.Now().UTC(); tests pin it.
	Now func() time.Time
}

func NewService(s Store) *Service {
	return &Service{Store: s}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Get lazily creates a StageSignedUp record so a fresh tenant lands on
// a populated dashboard without a 404 round-trip.
func (s *Service) Get(ctx context.Context, tenantID string) (State, error) {
	if tenantID == "" {
		return State{}, errors.New("onboarding: empty tenant id")
	}
	st, err := s.Store.Get(ctx, tenantID)
	if errors.Is(err, ErrTenantNotFound) {
		st = New(s.now())
		if err := s.Store.Put(ctx, tenantID, st); err != nil {
			return State{}, err
		}
		return st, nil
	}
	return st, err
}

// Transition forwards to state.Advance, which rejects backwards moves
// and unknown stages with ErrIllegalTransition.
func (s *Service) Transition(ctx context.Context, tenantID string, next Stage) (State, error) {
	st, err := s.Get(ctx, tenantID)
	if err != nil {
		return State{}, err
	}
	if err := st.Advance(next, s.now()); err != nil {
		return State{}, err
	}
	if err := s.Store.Put(ctx, tenantID, st); err != nil {
		return State{}, err
	}
	return st, nil
}
