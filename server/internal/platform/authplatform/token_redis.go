package authplatform

import (
	"context"
	"errors"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
)

type RedisTokenStore struct{ cache *rediscache.Client }

func NewRedisTokenStore(cache *rediscache.Client) *RedisTokenStore {
	return &RedisTokenStore{cache: cache}
}
func (s *RedisTokenStore) key(id string) (string, error) { return s.cache.Key("auth-token", id) }
func (s *RedisTokenStore) Put(ctx context.Context, r authdomain.TokenRecord) error {
	key, err := s.key(r.TokenID)
	if err != nil {
		return err
	}
	ttl := time.Until(r.ExpiresAt)
	if ttl <= 0 {
		return authdomain.ErrInvalidToken
	}
	return s.cache.SetJSON(ctx, key, r, ttl)
}
func (s *RedisTokenStore) Get(ctx context.Context, id string) (authdomain.TokenRecord, error) {
	key, err := s.key(id)
	if err != nil {
		return authdomain.TokenRecord{}, err
	}
	var r authdomain.TokenRecord
	if err := s.cache.GetJSON(ctx, key, &r); err != nil {
		if errors.Is(err, rediscache.ErrCacheMiss) {
			return r, authdomain.ErrInvalidToken
		}
		return r, err
	}
	return r, nil
}
func (s *RedisTokenStore) Delete(ctx context.Context, id string) error {
	key, err := s.key(id)
	if err != nil {
		return err
	}
	return s.cache.Delete(ctx, key)
}
