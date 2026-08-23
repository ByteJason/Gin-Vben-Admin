package iamplatform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	iamapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/iam"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
)

var ErrPermissionCacheUnavailable = errors.New("iam permission cache is unavailable")

const permissionVersionTTL = 365 * 24 * time.Hour

// RedisPermissionCache stores authorization decisions behind a monotonically
// increasing namespace version. Invalidation changes one version key instead
// of scanning or deleting arbitrary Redis keys.
type RedisPermissionCache struct {
	cache      *rediscache.Client
	versionTTL time.Duration
}

var _ iamapp.DecisionCache = (*RedisPermissionCache)(nil)

func NewRedisPermissionCache(cache *rediscache.Client, versionTTL ...time.Duration) *RedisPermissionCache {
	ttl := permissionVersionTTL
	if len(versionTTL) > 0 && versionTTL[0] > 0 {
		ttl = versionTTL[0]
	}
	return &RedisPermissionCache{cache: cache, versionTTL: ttl}
}

func (c *RedisPermissionCache) Get(ctx context.Context, subject domain.Subject, request domain.Request) (bool, bool, error) {
	version, err := c.version(ctx)
	if err != nil {
		return false, false, err
	}
	key, err := c.decisionKey(version, subject, request)
	if err != nil {
		return false, false, err
	}
	var allowed bool
	if err := c.cache.GetJSON(ctx, key, &allowed); err != nil {
		if errors.Is(err, rediscache.ErrCacheMiss) {
			return false, false, nil
		}
		return false, false, err
	}
	return allowed, true, nil
}

func (c *RedisPermissionCache) Set(ctx context.Context, subject domain.Subject, request domain.Request, allowed bool, ttl time.Duration) error {
	if err := c.validate(); err != nil {
		return err
	}
	if ttl <= 0 {
		return rediscache.ErrInvalidTTL
	}
	version, err := c.version(ctx)
	if err != nil {
		return err
	}
	key, err := c.decisionKey(version, subject, request)
	if err != nil {
		return err
	}
	return c.cache.SetJSON(ctx, key, allowed, ttl)
}

func (c *RedisPermissionCache) Invalidate(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	key, err := c.cache.Key("iam", "permission", "version")
	if err != nil {
		return err
	}
	_, err = c.cache.Increment(ctx, key, c.versionTTL)
	return err
}

func (c *RedisPermissionCache) version(ctx context.Context) (int64, error) {
	if err := c.validate(); err != nil {
		return 0, err
	}
	key, err := c.cache.Key("iam", "permission", "version")
	if err != nil {
		return 0, err
	}
	var version int64
	if err := c.cache.GetJSON(ctx, key, &version); err != nil {
		if errors.Is(err, rediscache.ErrCacheMiss) {
			return 0, nil
		}
		return 0, err
	}
	return version, nil
}

func (c *RedisPermissionCache) decisionKey(version int64, subject domain.Subject, request domain.Request) (string, error) {
	if err := c.validate(); err != nil {
		return "", err
	}
	digest := decisionDigest(subject, request)
	return c.cache.Key("iam", "permission", "decision", strconv.FormatInt(version, 10), digest)
}

func (c *RedisPermissionCache) validate() error {
	if c == nil || c.cache == nil {
		return ErrPermissionCacheUnavailable
	}
	if c.versionTTL <= 0 {
		return rediscache.ErrInvalidTTL
	}
	return nil
}

type decisionIdentity struct {
	UserID    string   `json:"user_id"`
	RoleIDs   []string `json:"role_ids,omitempty"`
	Domain    string   `json:"domain,omitempty"`
	Superuser bool     `json:"superuser,omitempty"`
	Request   struct {
		Domain string `json:"domain,omitempty"`
		Method string `json:"method,omitempty"`
		Path   string `json:"path,omitempty"`
		Action string `json:"action,omitempty"`
		Object string `json:"object,omitempty"`
	} `json:"request"`
}

func decisionDigest(subject domain.Subject, request domain.Request) string {
	roles := append([]string(nil), subject.RoleIDs...)
	sort.Strings(roles)
	identity := decisionIdentity{
		UserID: subject.UserID, RoleIDs: roles, Domain: subject.Domain, Superuser: subject.Superuser,
	}
	identity.Request.Domain = request.Domain
	identity.Request.Method = request.Method
	identity.Request.Path = request.Path
	identity.Request.Action = request.Action
	identity.Request.Object = request.Object
	payload, _ := json.Marshal(identity)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
