package file

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sync"
	"time"
)

// MemoryStore is a deterministic local provider for unit and development
// environments. It deliberately does not emulate a remote object service.
type MemoryStore struct {
	mu      sync.RWMutex
	objects map[string]Object
	BaseURL string
}

func NewMemoryStore(baseURL string) *MemoryStore {
	return &MemoryStore{objects: make(map[string]Object), BaseURL: baseURL}
}

func (s *MemoryStore) Put(_ context.Context, object Object) error {
	if object.Key == "" {
		return ErrInvalidUpload
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.objects == nil {
		s.objects = make(map[string]Object)
	}
	s.objects[object.Key] = object
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.objects[key]; !ok {
		return ErrFileNotFound
	}
	delete(s.objects, key)
	return nil
}

func (s *MemoryStore) SignURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", ErrInvalidUpload
	}
	s.mu.RLock()
	_, ok := s.objects[key]
	s.mu.RUnlock()
	if !ok {
		return "", ErrFileNotFound
	}
	base := s.BaseURL
	if base == "" {
		base = "http://memory.invalid/objects"
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse memory store URL: %w", err)
	}
	u.Path = path.Join(u.Path, key)
	q := u.Query()
	q.Set("expires_in", ttl.String())
	u.RawQuery = q.Encode()
	return u.String(), nil
}
