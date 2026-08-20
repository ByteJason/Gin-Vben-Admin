package authplatform

import (
	"context"
	"sync"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
)

type MemoryTokenStore struct {
	mu     sync.RWMutex
	tokens map[string]authdomain.TokenRecord
}

func NewMemoryTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{tokens: make(map[string]authdomain.TokenRecord)}
}
func (s *MemoryTokenStore) Put(ctx context.Context, r authdomain.TokenRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.tokens[r.TokenID] = r
	s.mu.Unlock()
	return nil
}
func (s *MemoryTokenStore) Get(ctx context.Context, id string) (authdomain.TokenRecord, error) {
	if err := ctx.Err(); err != nil {
		return authdomain.TokenRecord{}, err
	}
	s.mu.RLock()
	v, ok := s.tokens[id]
	s.mu.RUnlock()
	if !ok {
		return authdomain.TokenRecord{}, authdomain.ErrInvalidToken
	}
	return v, nil
}
func (s *MemoryTokenStore) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.tokens, id)
	s.mu.Unlock()
	return nil
}
