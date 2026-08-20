package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidLoginAttemptKey    = errors.New("login attempt key is required")
	ErrInvalidLoginAttemptPolicy = errors.New("login attempt policy is invalid")
)

// LoginAttemptStore tracks failed credentials independently from the request
// rate limiter. Implementations must make RecordFailure atomic for a single
// identifier when shared by multiple API processes.
type LoginAttemptStore interface {
	IsLocked(ctx context.Context, identifier string) (bool, error)
	RecordFailure(ctx context.Context, identifier string) (bool, error)
	Reset(ctx context.Context, identifier string) error
}

type loginAttemptState struct {
	failures  int
	lockedTil time.Time
}

// MemoryLoginAttemptStore is deterministic for unit tests and single-process
// development. Distributed deployments should inject the Redis adapter.
type MemoryLoginAttemptStore struct {
	mu        sync.Mutex
	states    map[string]loginAttemptState
	threshold int
	duration  time.Duration
	now       func() time.Time
}

func NewMemoryLoginAttemptStore(threshold int, duration time.Duration) *MemoryLoginAttemptStore {
	return &MemoryLoginAttemptStore{
		states:    make(map[string]loginAttemptState),
		threshold: threshold,
		duration:  duration,
		now:       time.Now,
	}
}

func (s *MemoryLoginAttemptStore) IsLocked(ctx context.Context, identifier string) (bool, error) {
	if err := validateLoginAttempt(ctx, identifier, s); err != nil {
		return false, err
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[strings.TrimSpace(identifier)]
	if !state.lockedTil.IsZero() && now.Before(state.lockedTil) {
		return true, nil
	}
	if !state.lockedTil.IsZero() {
		delete(s.states, strings.TrimSpace(identifier))
	}
	return false, nil
}

func (s *MemoryLoginAttemptStore) RecordFailure(ctx context.Context, identifier string) (bool, error) {
	if err := validateLoginAttempt(ctx, identifier, s); err != nil {
		return false, err
	}
	key := strings.TrimSpace(identifier)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[key]
	if !state.lockedTil.IsZero() && now.Before(state.lockedTil) {
		return true, nil
	}
	if !state.lockedTil.IsZero() && !now.Before(state.lockedTil) {
		state = loginAttemptState{}
	}
	state.failures++
	if state.failures >= s.threshold {
		state.lockedTil = now.Add(s.duration)
		s.states[key] = state
		return true, nil
	}
	s.states[key] = state
	return false, nil
}

func (s *MemoryLoginAttemptStore) Reset(ctx context.Context, identifier string) error {
	if err := validateLoginAttempt(ctx, identifier, s); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.states, strings.TrimSpace(identifier))
	s.mu.Unlock()
	return nil
}

func validateLoginAttempt(ctx context.Context, identifier string, store *MemoryLoginAttemptStore) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if store == nil {
		return errors.New("login attempt store is not initialized")
	}
	if strings.TrimSpace(identifier) == "" {
		return ErrInvalidLoginAttemptKey
	}
	if store.threshold <= 0 || store.duration <= 0 {
		return ErrInvalidLoginAttemptPolicy
	}
	return nil
}
