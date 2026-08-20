// Package rediscache provides namespaced JSON cache and distributed lock primitives.
package rediscache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// ModeSingle connects to one Redis server.
	ModeSingle = "single"
	// ModeSentinel discovers the Redis primary via Redis Sentinel.
	ModeSentinel = "sentinel"
	// ModeCluster connects to a Redis Cluster.
	ModeCluster = "cluster"

	defaultAddress   = "127.0.0.1:6379"
	defaultNamespace = "app:v1"
	defaultTimeout   = 3 * time.Second
)

var (
	// ErrCacheMiss indicates that a cache entry does not exist.
	ErrCacheMiss = errors.New("redis cache miss")
	// ErrLockNotAcquired indicates that another lock owner currently holds the lock.
	ErrLockNotAcquired = errors.New("redis lock not acquired")
	// ErrInvalidKey indicates that a namespace, key, or key segment is invalid.
	ErrInvalidKey = errors.New("invalid redis cache key")
	// ErrInvalidTTL indicates that a cache or lock TTL is not positive.
	ErrInvalidTTL = errors.New("redis cache ttl must be positive")
	// ErrInvalidConfig indicates that the selected Redis topology configuration is invalid.
	ErrInvalidConfig = errors.New("invalid redis cache configuration")
)

// Config configures a Redis cache client. New constructs the selected topology
// client but deliberately does not probe the network; callers can use Ping for
// readiness checks.
type Config struct {
	Mode       string
	Addr       string
	Addrs      []string
	MasterName string
	Username   string
	Password   string
	DB         int
	Namespace  string

	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Client is a namespaced Redis cache client.
type Client struct {
	client    redis.UniversalClient
	namespace string
}

// New creates a Redis client for a standalone, Sentinel, or Cluster topology.
// It does not connect to Redis until a command is executed.
func New(config Config) (*Client, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}

	var client redis.UniversalClient
	switch normalized.Mode {
	case ModeSingle:
		client = redis.NewClient(&redis.Options{
			Addr:         normalized.Addr,
			Username:     normalized.Username,
			Password:     normalized.Password,
			DB:           normalized.DB,
			DialTimeout:  normalized.DialTimeout,
			ReadTimeout:  normalized.ReadTimeout,
			WriteTimeout: normalized.WriteTimeout,
		})
	case ModeSentinel:
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    normalized.MasterName,
			SentinelAddrs: normalized.Addrs,
			Username:      normalized.Username,
			Password:      normalized.Password,
			DB:            normalized.DB,
			DialTimeout:   normalized.DialTimeout,
			ReadTimeout:   normalized.ReadTimeout,
			WriteTimeout:  normalized.WriteTimeout,
		})
	case ModeCluster:
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        normalized.Addrs,
			Username:     normalized.Username,
			Password:     normalized.Password,
			DialTimeout:  normalized.DialTimeout,
			ReadTimeout:  normalized.ReadTimeout,
			WriteTimeout: normalized.WriteTimeout,
		})
	default:
		return nil, fmt.Errorf("%w: unsupported mode", ErrInvalidConfig)
	}

	return &Client{client: client, namespace: normalized.Namespace}, nil
}

// Name implements the health dependency contract.
func (c *Client) Name() string {
	return "redis"
}

// Ping checks Redis connectivity.
func (c *Client) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Key builds a physical cache key under this client's namespace.
func (c *Client) Key(parts ...string) (string, error) {
	if len(parts) == 0 {
		return "", ErrInvalidKey
	}
	for _, part := range parts {
		if !isSafeSegment(part) {
			return "", ErrInvalidKey
		}
	}
	return c.namespace + ":" + strings.Join(parts, ":"), nil
}

// SetJSON serializes value as JSON and stores it at a namespaced key.
func (c *Client) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	if !c.isPhysicalKey(key) {
		return ErrInvalidKey
	}
	if ttl <= 0 {
		return ErrInvalidTTL
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key, payload, ttl).Err()
}

// GetJSON loads and decodes JSON at a namespaced key. Missing entries map to
// ErrCacheMiss so callers do not depend on the Redis client implementation.
func (c *Client) GetJSON(ctx context.Context, key string, dst any) error {
	if !c.isPhysicalKey(key) {
		return ErrInvalidKey
	}

	payload, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return ErrCacheMiss
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, dst)
}

// Delete removes a namespaced key.
func (c *Client) Delete(ctx context.Context, key string) error {
	if !c.isPhysicalKey(key) {
		return ErrInvalidKey
	}
	return c.client.Del(ctx, key).Err()
}

const incrementWithTTLScript = `
local count = redis.call("INCR", KEYS[1])
if count == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return count
`

var incrementWithTTL = redis.NewScript(incrementWithTTLScript)

// Increment atomically increments a namespaced counter and applies the TTL to
// its first write. It is the primitive used by distributed rate-limiters.
func (c *Client) Increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if !c.isPhysicalKey(key) {
		return 0, ErrInvalidKey
	}
	if ttl <= 0 {
		return 0, ErrInvalidTTL
	}
	return incrementWithTTL.Run(ctx, c.client, []string{key}, ttl.Milliseconds()).Int64()
}

// Lock is an acquired Redis lock. Its owner token is generated locally and is
// never exposed, logged, or accepted from callers.
type Lock struct {
	client *Client
	key    string
	owner  string
}

// AcquireLock atomically acquires a namespaced lock. A lock name is one safe
// key segment, and the actual Redis key is built as namespace:lock:name.
func (c *Client) AcquireLock(ctx context.Context, name string, ttl time.Duration) (*Lock, error) {
	if !isSafeSegment(name) {
		return nil, ErrInvalidKey
	}
	if ttl <= 0 {
		return nil, ErrInvalidTTL
	}

	key, err := c.Key("lock", name)
	if err != nil {
		return nil, err
	}
	owner, err := newOwnerToken()
	if err != nil {
		return nil, err
	}

	acquired, err := c.client.SetNX(ctx, key, owner, ttl).Result()
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, ErrLockNotAcquired
	}
	return &Lock{client: c, key: key, owner: owner}, nil
}

const compareAndDeleteScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`

var compareAndDelete = redis.NewScript(compareAndDeleteScript)

// Release removes the lock only when this Lock still owns it. A stale or
// foreign owner receives ErrLockNotAcquired and never deletes another owner's lock.
func (l *Lock) Release(ctx context.Context) error {
	if l == nil || l.client == nil || l.owner == "" || !l.client.isPhysicalKey(l.key) {
		return ErrLockNotAcquired
	}

	deleted, err := compareAndDelete.Run(ctx, l.client.client, []string{l.key}, l.owner).Int()
	if err != nil {
		return err
	}
	if deleted != 1 {
		return ErrLockNotAcquired
	}
	return nil
}

func (c *Client) isPhysicalKey(key string) bool {
	prefix := c.namespace + ":"
	if !strings.HasPrefix(key, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(key, prefix), ":")
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if !isSafeSegment(part) {
			return false
		}
	}
	return true
}

// Close releases Redis client resources.
func (c *Client) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

func normalizeConfig(config Config) (Config, error) {
	config.Mode = strings.TrimSpace(config.Mode)
	if config.Mode == "" {
		config.Mode = ModeSingle
	}
	if config.DB < 0 {
		return Config{}, fmt.Errorf("%w: db must not be negative", ErrInvalidConfig)
	}

	if config.Namespace == "" {
		config.Namespace = defaultNamespace
	}
	if config.Namespace != strings.TrimSpace(config.Namespace) || !isSafeNamespace(config.Namespace) {
		return Config{}, fmt.Errorf("%w: namespace", ErrInvalidKey)
	}

	config.DialTimeout = withDefaultTimeout(config.DialTimeout)
	config.ReadTimeout = withDefaultTimeout(config.ReadTimeout)
	config.WriteTimeout = withDefaultTimeout(config.WriteTimeout)

	switch config.Mode {
	case ModeSingle:
		config.Addr = strings.TrimSpace(config.Addr)
		if config.Addr == "" {
			config.Addr = defaultAddress
		}
		if !isSafeAddress(config.Addr) {
			return Config{}, fmt.Errorf("%w: single address", ErrInvalidConfig)
		}
	case ModeSentinel:
		if config.MasterName = strings.TrimSpace(config.MasterName); !isSafeSegment(config.MasterName) {
			return Config{}, fmt.Errorf("%w: sentinel master name", ErrInvalidConfig)
		}
		if !validAddresses(config.Addrs, 1) {
			return Config{}, fmt.Errorf("%w: sentinel addresses", ErrInvalidConfig)
		}
	case ModeCluster:
		if config.DB != 0 {
			return Config{}, fmt.Errorf("%w: cluster db must be zero", ErrInvalidConfig)
		}
		if !validAddresses(config.Addrs, 2) {
			return Config{}, fmt.Errorf("%w: cluster addresses", ErrInvalidConfig)
		}
	default:
		return Config{}, fmt.Errorf("%w: mode", ErrInvalidConfig)
	}

	return config, nil
}

func withDefaultTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultTimeout
	}
	return timeout
}

func isSafeNamespace(namespace string) bool {
	for _, segment := range strings.Split(namespace, ":") {
		if !isSafeSegment(segment) {
			return false
		}
	}
	return true
}

func isSafeSegment(segment string) bool {
	return segment != "" && segment == strings.TrimSpace(segment) && !strings.Contains(segment, ":")
}

func isSafeAddress(address string) bool {
	return address != "" && address == strings.TrimSpace(address) && !strings.ContainsAny(address, "\r\n")
}

func validAddresses(addresses []string, min int) bool {
	if len(addresses) < min {
		return false
	}
	for _, address := range addresses {
		if !isSafeAddress(address) {
			return false
		}
	}
	return true
}

func newOwnerToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
