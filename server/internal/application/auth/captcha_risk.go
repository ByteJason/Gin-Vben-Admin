package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidCaptchaRiskKey    = errors.New("captcha risk key is required")
	ErrInvalidCaptchaRiskPolicy = errors.New("captcha risk policy is invalid")
)

// CaptchaRiskStore tracks failed credentials for a bounded account/IP key.
// Implementations must make RecordFailure atomic when shared by API processes.
type CaptchaRiskStore interface {
	Requires(ctx context.Context, key string, threshold int, window time.Duration) (bool, error)
	RecordFailure(ctx context.Context, key string, window time.Duration) error
	Reset(ctx context.Context, key string) error
}

type captchaRiskState struct {
	failures  int
	expiresAt time.Time
}

// MemoryCaptchaRiskStore is deterministic for tests and single-process local
// development. Production deployments should inject a Redis implementation.
type MemoryCaptchaRiskStore struct {
	mu     sync.Mutex
	states map[string]captchaRiskState
	now    func() time.Time
}

func NewMemoryCaptchaRiskStore() *MemoryCaptchaRiskStore {
	return &MemoryCaptchaRiskStore{states: make(map[string]captchaRiskState), now: time.Now}
}

func (s *MemoryCaptchaRiskStore) Requires(ctx context.Context, key string, threshold int, window time.Duration) (bool, error) {
	if err := validateCaptchaRisk(ctx, key, threshold, window, s); err != nil {
		return false, err
	}
	normalized := strings.TrimSpace(key)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[normalized]
	if !ok || !state.expiresAt.After(now) {
		if ok {
			delete(s.states, normalized)
		}
		return false, nil
	}
	return state.failures >= threshold, nil
}

func (s *MemoryCaptchaRiskStore) RecordFailure(ctx context.Context, key string, window time.Duration) error {
	if err := validateCaptchaRisk(ctx, key, 1, window, s); err != nil {
		return err
	}
	normalized := strings.TrimSpace(key)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.states[normalized]
	if !state.expiresAt.After(now) {
		state = captchaRiskState{expiresAt: now.Add(window)}
	}
	state.failures++
	s.states[normalized] = state
	return nil
}

func (s *MemoryCaptchaRiskStore) Reset(ctx context.Context, key string) error {
	if err := validateCaptchaRiskKey(ctx, key, s); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.states, strings.TrimSpace(key))
	s.mu.Unlock()
	return nil
}

func validateCaptchaRisk(ctx context.Context, key string, threshold int, window time.Duration, store *MemoryCaptchaRiskStore) error {
	if err := validateCaptchaRiskKey(ctx, key, store); err != nil {
		return err
	}
	if threshold <= 0 || window <= 0 {
		return ErrInvalidCaptchaRiskPolicy
	}
	return nil
}

func validateCaptchaRiskKey(ctx context.Context, key string, store *MemoryCaptchaRiskStore) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if store == nil {
		return errors.New("captcha risk store is not initialized")
	}
	if strings.TrimSpace(key) == "" {
		return ErrInvalidCaptchaRiskKey
	}
	return nil
}
