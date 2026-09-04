// Package rediscache provides namespaced JSON cache and distributed lock primitives.
package rediscache

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	monitorapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/monitor"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
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
	// AddressMap rewrites exact addresses advertised by Sentinel/Cluster to
	// externally reachable host:port values. An empty map keeps native discovery.
	AddressMap map[string]string
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
	client     redis.UniversalClient
	namespace  string
	addressMap map[string]string
	poolMax    *int
	mode       string
}

// New creates a Redis client for a standalone, Sentinel, or Cluster topology.
// It does not connect to Redis until a command is executed.
func New(config Config) (*Client, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}

	var client redis.UniversalClient
	var poolMax *int
	switch normalized.Mode {
	case ModeSingle:
		constructed := redis.NewClient(&redis.Options{
			Addr:         normalized.Addr,
			Username:     normalized.Username,
			Password:     normalized.Password,
			DB:           normalized.DB,
			DialTimeout:  normalized.DialTimeout,
			ReadTimeout:  normalized.ReadTimeout,
			WriteTimeout: normalized.WriteTimeout,
			Dialer:       normalized.dialer(),
		})
		client = constructed
		max := constructed.Options().PoolSize
		poolMax = &max
	case ModeSentinel:
		constructed := redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    normalized.MasterName,
			SentinelAddrs: normalized.Addrs,
			Username:      normalized.Username,
			Password:      normalized.Password,
			DB:            normalized.DB,
			DialTimeout:   normalized.DialTimeout,
			ReadTimeout:   normalized.ReadTimeout,
			WriteTimeout:  normalized.WriteTimeout,
			Dialer:        normalized.dialer(),
		})
		client = constructed
		max := constructed.Options().PoolSize
		poolMax = &max
	case ModeCluster:
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        normalized.Addrs,
			Username:     normalized.Username,
			Password:     normalized.Password,
			DialTimeout:  normalized.DialTimeout,
			ReadTimeout:  normalized.ReadTimeout,
			WriteTimeout: normalized.WriteTimeout,
			Dialer:       normalized.dialer(),
		})
	default:
		return nil, fmt.Errorf("%w: unsupported mode", ErrInvalidConfig)
	}

	return &Client{client: client, namespace: normalized.Namespace, addressMap: cloneAddressMap(normalized.AddressMap), poolMax: poolMax, mode: normalized.Mode}, nil
}

// Name implements the health dependency contract.
func (c *Client) Name() string {
	return "redis"
}

// Ping checks Redis connectivity.
func (c *Client) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// RedisRuntimeStats exposes only pool counters and the logical database key count
// for the operations snapshot. Authentication material and endpoint details
// remain private to the client.
func (c *Client) RedisRuntimeStats(ctx context.Context) (monitorapp.RedisRuntimeStats, error) {
	if c == nil || c.client == nil {
		return monitorapp.RedisRuntimeStats{}, errors.New("redis client is not initialized")
	}
	keyspace, keyspaceAvailable := int64(0), false
	// DBSize is a count only; it never returns key names or values. Some
	// clustered providers do not support it, so its capability is explicit.
	if size, sizeErr := c.client.DBSize(ctx).Result(); sizeErr == nil {
		keyspace = size
		keyspaceAvailable = true
	}
	return redisRuntimeStats(c.client.PoolStats(), c.poolMax, c.mode, keyspace, keyspaceAvailable), nil
}

func redisRuntimeStats(stats *redis.PoolStats, poolMax *int, mode string, keyspace int64, keyspaceAvailable bool) monitorapp.RedisRuntimeStats {
	result := monitorapp.RedisRuntimeStats{Mode: mode, ModeAvailable: mode != "", Keyspace: keyspace, KeyspaceAvailable: keyspaceAvailable}
	if stats == nil {
		return result
	}
	active := int(stats.TotalConns) - int(stats.IdleConns)
	if active < 0 {
		active = 0
	}
	var max *int
	if poolMax != nil {
		value := *poolMax
		max = &value
	}
	result.Pool = monitorapp.RedisPool{
		Max: max, Total: int(stats.TotalConns), Active: active, Idle: int(stats.IdleConns),
		Hits: stats.Hits, Misses: stats.Misses, Timeouts: stats.Timeouts, WaitCount: stats.WaitCount,
		WaitDurationMS: float64(stats.WaitDurationNs) / float64(time.Millisecond),
		Stale:          stats.StaleConns, Pending: stats.PendingRequests,
	}
	result.PoolAvailable = true
	return result
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
	return json.Unmarshal([]byte(payload), dst)
}

const takeJSONScript = `
local value = redis.call("GET", KEYS[1])
if not value then
  return nil
end
redis.call("DEL", KEYS[1])
return value
`

var takeJSON = redis.NewScript(takeJSONScript)

// TakeJSON atomically reads and removes a namespaced JSON value. It is used
// for one-time credentials such as captcha challenges where a separate GET
// followed by DEL could allow two concurrent verifications to succeed.
func (c *Client) TakeJSON(ctx context.Context, key string, dst any) error {
	if !c.isPhysicalKey(key) {
		return ErrInvalidKey
	}
	value, err := takeJSON.Run(ctx, c.client, []string{key}).Result()
	if errors.Is(err, redis.Nil) {
		return ErrCacheMiss
	}
	if err != nil {
		return err
	}
	payload, ok := value.(string)
	if !ok {
		return fmt.Errorf("redis take returned unexpected value type %T", value)
	}
	return json.Unmarshal([]byte(payload), dst)
}

// Delete removes a namespaced key.
func (c *Client) Delete(ctx context.Context, key string) error {
	if !c.isPhysicalKey(key) {
		return ErrInvalidKey
	}
	return c.client.Del(ctx, key).Err()
}

const settingsRevisionTTL = 24 * time.Hour

// setRevisionIfGreaterScript makes the advisory revision monotonic even when
// two application nodes finish their database commits out of order. It uses a
// single Redis key so the operation also remains valid on Redis Cluster (the
// module-value deletion is intentionally kept as a separate command).
const setRevisionIfGreaterScript = `
local current = redis.call("GET", KEYS[1])
local incoming = tonumber(ARGV[1])
if not incoming then
  return redis.error_reply("invalid settings revision")
end
if (not current) or (not tonumber(current)) or tonumber(current) < incoming then
  redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
  return 1
end
return 0
`

var setRevisionIfGreater = redis.NewScript(setRevisionIfGreaterScript)

// settingsScopeSegment returns a stable, non-sensitive Redis key segment for
// the tenant/organization carried by ctx.  Settings revisions are advisory
// metadata, but they still have to obey the same isolation boundary as the
// database rows they describe.  A digest avoids putting tenant identifiers in
// Redis key names and remains valid even when an identifier contains a key
// separator (tenant.Context validates whitespace/control characters, not every
// punctuation character).
//
// Calls made by process-level jobs without a tenant context use a dedicated
// "global" segment.  They never share a key with a tenant-scoped request, so a
// missing context cannot accidentally publish a value into a tenant bucket.
func settingsScopeSegment(ctx context.Context) string {
	scope, ok := tenant.FromContext(ctx)
	if !ok {
		return "global"
	}
	payload := strings.TrimSpace(scope.TenantID) + "\x00" + strings.TrimSpace(scope.Organization)
	digest := sha256.Sum256([]byte(payload))
	return "tenant-" + hex.EncodeToString(digest[:])
}

// settingsModuleKey and settingsRevisionKey centralize the physical key
// layout. Keeping the scope segment in both paths prevents a module cache
// invalidation from updating one tenant's revision while deleting another
// tenant's value cache.
func (c *Client) settingsModuleKey(ctx context.Context, module string) (string, error) {
	return c.Key("settings", "module", settingsScopeSegment(ctx), module)
}

func (c *Client) settingsRevisionKey(ctx context.Context, module string) (string, error) {
	return c.Key("settings", "revision", settingsScopeSegment(ctx), module)
}

// SettingsValueKey returns the physical key for a redacted per-key settings
// cache entry.  Although the current settings service primarily uses module
// revisions, keeping this legacy value path scoped prevents a future cache
// reader from reintroducing cross-tenant leakage.
func (c *Client) SettingsValueKey(ctx context.Context, key string) (string, error) {
	return c.Key("settings", "value", settingsScopeSegment(ctx), key)
}

// InvalidateModule removes the optional redacted module cache and records only
// its monotonically increasing revision. No setting values (and especially no
// sensitive values) are sent through Redis; instances that observe a newer
// revision reload the complete current state from the database.
func (c *Client) InvalidateModule(ctx context.Context, module string, revision int64) error {
	if c == nil || c.client == nil {
		return errors.New("redis cache is not initialized")
	}
	module = strings.ToLower(strings.TrimSpace(module))
	if !isSafeSegment(module) || revision < 0 {
		return ErrInvalidKey
	}
	moduleKey, err := c.settingsModuleKey(ctx, module)
	if err != nil {
		return err
	}
	if err := c.Delete(ctx, moduleKey); err != nil {
		return err
	}
	revisionKey, err := c.settingsRevisionKey(ctx, module)
	if err != nil {
		return err
	}
	// Store the revision as a JSON number rather than a JSON object. This keeps
	// the Lua compare-and-set script numeric and remains valid JSON for the
	// generic cache helpers. ModuleRevision accepts the historical object form
	// as a rolling-upgrade compatibility fallback.
	_, err = setRevisionIfGreater.Run(ctx, c.client, []string{revisionKey}, strconv.FormatInt(revision, 10), settingsRevisionTTL.Milliseconds()).Int64()
	return err
}

// ModuleRevision reads the advisory Redis revision used by reconciliation
// workers. A cache miss is returned as ErrCacheMiss; callers must then load
// current values from the database rather than falling back to defaults.
func (c *Client) ModuleRevision(ctx context.Context, module string) (int64, error) {
	if c == nil || c.client == nil {
		return 0, errors.New("redis cache is not initialized")
	}
	module = strings.ToLower(strings.TrimSpace(module))
	if !isSafeSegment(module) {
		return 0, ErrInvalidKey
	}
	key, err := c.settingsRevisionKey(ctx, module)
	if err != nil {
		return 0, err
	}
	var raw json.RawMessage
	if err := c.GetJSON(ctx, key, &raw); err != nil {
		return 0, err
	}
	var revision int64
	if err := json.Unmarshal(raw, &revision); err != nil {
		// Older instances stored {"revision":N}; accept that shape while the
		// fleet rolls forward, but never treat malformed data as revision zero.
		var payload struct {
			Revision int64 `json:"revision"`
		}
		if objectErr := json.Unmarshal(raw, &payload); objectErr != nil {
			return 0, ErrInvalidKey
		}
		revision = payload.Revision
	}
	if revision < 0 {
		return 0, ErrInvalidKey
	}
	return revision, nil
}

// DeleteLegacyMailSettings removes cache entries created by the retired
// configuration-centre mail surface. Patterns are deliberately constrained to
// the configured namespace and settings/config prefixes; independent mail
// module keys (smtp accounts, outbox messages and templates) are not touched.
// The operation is idempotent and safe to retry after a transient Redis error.
func (c *Client) DeleteLegacyMailSettings(ctx context.Context) error {
	if c == nil || c.client == nil {
		return errors.New("redis cache is not initialized")
	}
	patterns := legacyMailSettingPatterns(c.namespace)
	for _, pattern := range patterns {
		var batch []string
		iterator := c.client.Scan(ctx, 0, pattern, 256).Iterator()
		for iterator.Next(ctx) {
			key := iterator.Val()
			// Scan is namespaced by pattern, but retain this guard so a future
			// client implementation cannot delete an externally supplied key.
			if !c.isPhysicalKey(key) {
				continue
			}
			batch = append(batch, key)
			if len(batch) == 256 {
				if err := c.client.Del(ctx, batch...).Err(); err != nil {
					return err
				}
				batch = batch[:0]
			}
		}
		if err := iterator.Err(); err != nil {
			return err
		}
		if len(batch) > 0 {
			if err := c.client.Del(ctx, batch...).Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

// legacyMailSettingPatterns returns only the historical configuration-centre
// namespaces and dot-delimited setting keys.  In particular, a key such as
// `mail:accounts:*` or `settings:mail:account` belongs to an independent mail
// capability and must survive this cleanup.
func legacyMailSettingPatterns(namespace string) []string {
	patterns := make([]string, 0, 18)
	for _, bucket := range []string{"settings", "setting", "config"} {
		for _, key := range []string{"mail", "email", "smtp"} {
			prefix := namespace + ":" + bucket + ":"
			patterns = append(patterns, prefix+key, prefix+key+".*")
			// A few pre-module builds nested values under `value` or `module`;
			// retain those exact buckets (including the module aggregate key)
			// without matching arbitrary mail keys.
			patterns = append(patterns,
				prefix+"value:"+key, prefix+"value:"+key+".*",
				prefix+"module:"+key, prefix+"module:"+key+".*",
			)
		}
	}
	return patterns
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

// mapAddress is deterministic and side-effect free, allowing topology
// discovery to be tested without opening a network connection.
func (c *Client) mapAddress(address string) string {
	if c == nil || c.addressMap == nil {
		return address
	}
	if mapped, ok := c.addressMap[address]; ok && strings.TrimSpace(mapped) != "" {
		return mapped
	}
	return address
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
	config.AddressMap = cloneAddressMap(config.AddressMap)
	for advertised, reachable := range config.AddressMap {
		if !isSafeAddress(advertised) || !isSafeAddress(reachable) {
			return Config{}, fmt.Errorf("%w: address map contains unsafe endpoint", ErrInvalidConfig)
		}
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

func (config Config) dialer() func(context.Context, string, string) (net.Conn, error) {
	if len(config.AddressMap) == 0 {
		return nil
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if mapped, ok := config.AddressMap[address]; ok && strings.TrimSpace(mapped) != "" {
			address = mapped
		}
		dialer := &net.Dialer{Timeout: config.DialTimeout, KeepAlive: 5 * time.Minute}
		return dialer.DialContext(ctx, network, address)
	}
}

func cloneAddressMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return output
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
