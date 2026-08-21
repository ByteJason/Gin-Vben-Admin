// Package config loads and validates the runtime configuration for the server.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"example.com/gin-vben-admin/server/internal/domain/observability"
	"github.com/spf13/viper"
)

const defaultConfigPath = "configs/server.yaml"

// Config contains the runtime settings used by the HTTP service and its optional
// infrastructure dependencies.
type Config struct {
	Server        ServerConfig         `mapstructure:"server" yaml:"server"`
	Logging       LoggingConfig        `mapstructure:"logging" yaml:"logging"`
	Database      DatabaseConfig       `mapstructure:"database" yaml:"database"`
	Redis         RedisConfig          `mapstructure:"redis" yaml:"redis"`
	Auth          AuthConfig           `mapstructure:"auth" yaml:"auth"`
	Install       InstallConfig        `mapstructure:"install" yaml:"install"`
	Tenant        TenantConfig         `mapstructure:"tenant" yaml:"tenant"`
	Observability observability.Config `mapstructure:"observability" yaml:"observability"`

	// dynamicObservabilityLocked records values that came from an explicit
	// process environment, root .env, or YAML source. Persisted settings may
	// fill compiled defaults, but must not override these higher-authority
	// sources (DEC-018).
	dynamicObservabilityLocked map[string]bool
}

type ServerConfig struct {
	Addr            string        `mapstructure:"addr" yaml:"addr"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout" yaml:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout" yaml:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout" yaml:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout" yaml:"shutdown_timeout"`
}

type LoggingConfig struct {
	Level string `mapstructure:"level" yaml:"level"`
}

type DatabaseConfig struct {
	Enabled         bool          `mapstructure:"enabled" yaml:"enabled"`
	Driver          string        `mapstructure:"driver" yaml:"driver"`
	DSN             string        `mapstructure:"dsn" yaml:"dsn"`
	Mode            string        `mapstructure:"mode" yaml:"mode"`
	PrimaryDSN      string        `mapstructure:"primary_dsn" yaml:"primary_dsn"`
	ReplicaDSNs     []string      `mapstructure:"replica_dsns" yaml:"replica_dsns"`
	ReadPolicy      string        `mapstructure:"read_policy" yaml:"read_policy"`
	MaxOpenConns    int           `mapstructure:"max_open_conns" yaml:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns" yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime" yaml:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time" yaml:"conn_max_idle_time"`
	PingTimeout     time.Duration `mapstructure:"ping_timeout" yaml:"ping_timeout"`
}

type RedisConfig struct {
	Enabled      bool              `mapstructure:"enabled" yaml:"enabled"`
	Addr         string            `mapstructure:"addr" yaml:"addr"`
	Username     string            `mapstructure:"username" yaml:"username"`
	Password     string            `mapstructure:"password" yaml:"password"`
	DB           int               `mapstructure:"db" yaml:"db"`
	Namespace    string            `mapstructure:"namespace" yaml:"namespace"`
	Mode         string            `mapstructure:"mode" yaml:"mode"`
	Addrs        []string          `mapstructure:"addrs" yaml:"addrs"`
	MasterName   string            `mapstructure:"master_name" yaml:"master_name"`
	AddressMap   map[string]string `mapstructure:"address_map" yaml:"address_map"`
	DialTimeout  time.Duration     `mapstructure:"dial_timeout" yaml:"dial_timeout"`
	ReadTimeout  time.Duration     `mapstructure:"read_timeout" yaml:"read_timeout"`
	WriteTimeout time.Duration     `mapstructure:"write_timeout" yaml:"write_timeout"`
	PingTimeout  time.Duration     `mapstructure:"ping_timeout" yaml:"ping_timeout"`
}

// AuthConfig contains the runtime security policy for authentication.
// Secrets are required only when authentication is enabled.
type AuthConfig struct {
	Enabled              bool          `mapstructure:"enabled" yaml:"enabled"`
	JWTSecret            string        `mapstructure:"jwt_secret" yaml:"jwt_secret"`
	Issuer               string        `mapstructure:"issuer" yaml:"issuer"`
	Audience             string        `mapstructure:"audience" yaml:"audience"`
	AccessTTL            time.Duration `mapstructure:"access_ttl" yaml:"access_ttl"`
	RefreshTTL           time.Duration `mapstructure:"refresh_ttl" yaml:"refresh_ttl"`
	RefreshCookieName    string        `mapstructure:"refresh_cookie_name" yaml:"refresh_cookie_name"`
	SecureCookie         bool          `mapstructure:"secure_cookie" yaml:"secure_cookie"`
	BcryptCost           int           `mapstructure:"bcrypt_cost" yaml:"bcrypt_cost"`
	RateLimitWindow      time.Duration `mapstructure:"rate_limit_window" yaml:"rate_limit_window"`
	RateLimitMaxAttempts int           `mapstructure:"rate_limit_max_attempts" yaml:"rate_limit_max_attempts"`
	LockoutThreshold     int           `mapstructure:"lockout_threshold" yaml:"lockout_threshold"`
	LockoutDuration      time.Duration `mapstructure:"lockout_duration" yaml:"lockout_duration"`
	RegistrationEnabled  bool          `mapstructure:"registration_enabled" yaml:"registration_enabled"`
}

type InstallConfig struct {
	StateDir string `mapstructure:"state_dir" yaml:"state_dir"`
}

// TenantConfig controls the request tenant boundary. Multi-tenant mode still
// requires an explicit tenant header; DefaultID is reserved for bootstrap and
// single-tenant operation. Platform-admin resolution remains an authenticated
// application concern and is never configured from a request header.
type TenantConfig struct {
	Enabled            bool   `mapstructure:"enabled" yaml:"enabled"`
	Mode               string `mapstructure:"mode" yaml:"mode"`
	DefaultID          string `mapstructure:"default_id" yaml:"default_id"`
	TenantHeader       string `mapstructure:"tenant_header" yaml:"tenant_header"`
	OrganizationHeader string `mapstructure:"organization_header" yaml:"organization_header"`
}

func (cfg InstallConfig) MarkerPath() string {
	return filepath.Join(cfg.StateDir, ".installed")
}

// Summary is a redacted, log-safe view of a Config. It deliberately excludes
// database DSNs and all usernames and passwords.
type Summary struct {
	Server        ServerSummary        `json:"server"`
	Logging       LoggingSummary       `json:"logging"`
	Database      DatabaseSummary      `json:"database"`
	Redis         RedisSummary         `json:"redis"`
	Auth          AuthSummary          `json:"auth"`
	Install       InstallSummary       `json:"install"`
	Tenant        TenantSummary        `json:"tenant"`
	Observability observability.Config `json:"observability"`
}

type ServerSummary struct {
	Addr            string        `json:"addr"`
	ReadTimeout     time.Duration `json:"read_timeout"`
	WriteTimeout    time.Duration `json:"write_timeout"`
	IdleTimeout     time.Duration `json:"idle_timeout"`
	ShutdownTimeout time.Duration `json:"shutdown_timeout"`
}

type LoggingSummary struct {
	Level string `json:"level"`
}

type DatabaseSummary struct {
	Enabled      bool   `json:"enabled"`
	Driver       string `json:"driver"`
	Mode         string `json:"mode"`
	ReadPolicy   string `json:"read_policy"`
	ReplicaCount int    `json:"replica_count"`
}

type RedisSummary struct {
	Enabled      bool   `json:"enabled"`
	Mode         string `json:"mode"`
	AddressCount int    `json:"address_count"`
	MasterName   string `json:"master_name"`
	Namespace    string `json:"namespace"`
	DB           int    `json:"db"`
}

type AuthSummary struct {
	Enabled              bool          `json:"enabled"`
	Issuer               string        `json:"issuer"`
	Audience             string        `json:"audience"`
	AccessTTL            time.Duration `json:"access_ttl"`
	RefreshTTL           time.Duration `json:"refresh_ttl"`
	RefreshCookieName    string        `json:"refresh_cookie_name"`
	SecureCookie         bool          `json:"secure_cookie"`
	BcryptCost           int           `json:"bcrypt_cost"`
	RateLimitWindow      time.Duration `json:"rate_limit_window"`
	RateLimitMaxAttempts int           `json:"rate_limit_max_attempts"`
	LockoutThreshold     int           `json:"lockout_threshold"`
	LockoutDuration      time.Duration `json:"lockout_duration"`
	RegistrationEnabled  bool          `json:"registration_enabled"`
}

type InstallSummary struct {
	StateDirectoryAbsolute bool `json:"state_directory_absolute"`
}

type TenantSummary struct {
	Enabled            bool   `json:"enabled"`
	Mode               string `json:"mode"`
	DefaultID          string `json:"default_id"`
	TenantHeader       string `json:"tenant_header"`
	OrganizationHeader string `json:"organization_header"`
}

// Default returns a complete configuration that starts the HTTP server without
// external infrastructure services.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Addr:            ":8080",
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    10 * time.Second,
			IdleTimeout:     60 * time.Second,
			ShutdownTimeout: 10 * time.Second,
		},
		Logging: LoggingConfig{Level: "info"},
		Database: DatabaseConfig{
			Driver:          "mysql",
			Mode:            "single",
			ReadPolicy:      "random",
			MaxOpenConns:    10,
			MaxIdleConns:    5,
			ConnMaxLifetime: time.Hour,
			ConnMaxIdleTime: 15 * time.Minute,
			PingTimeout:     5 * time.Second,
		},
		Redis: RedisConfig{
			Mode:         "single",
			Namespace:    "app:v1",
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
			PingTimeout:  3 * time.Second,
		},
		Auth: AuthConfig{
			Issuer:               "gin-vben-admin",
			Audience:             "admin",
			AccessTTL:            15 * time.Minute,
			RefreshTTL:           7 * 24 * time.Hour,
			RefreshCookieName:    "refresh_token",
			BcryptCost:           12,
			RateLimitWindow:      time.Minute,
			RateLimitMaxAttempts: 10,
			LockoutThreshold:     5,
			LockoutDuration:      15 * time.Minute,
			RegistrationEnabled:  false,
		},
		Install: InstallConfig{StateDir: filepath.FromSlash("../install")},
		Tenant: TenantConfig{
			Enabled:            true,
			Mode:               "single",
			DefaultID:          "default",
			TenantHeader:       "X-Tenant-ID",
			OrganizationHeader: "X-Org-ID",
		},
		Observability: observability.DefaultConfig(),
	}
}

// Load reads YAML settings and then applies environment variable overrides.
// When path is empty it uses SERVER_CONFIG, then configs/server.yaml. A missing
// implicit default file is allowed; a named file must exist.
func Load(path string) (Config, error) {
	configPath, explicit := resolvePath(path)
	v := newViper()

	if _, err := os.Stat(configPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) || explicit {
			return Config{}, fmt.Errorf("read configuration %q: %w", configPath, err)
		}
	} else {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("read configuration %q: %w", configPath, err)
		}
	}

	dotEnv, err := loadRootDotEnv(configPath)
	if err != nil {
		return Config{}, err
	}
	applyDotEnvOverrides(v, dotEnv)
	applyListEnvironmentOverrides(v, dotEnv)
	dynamicObservabilityLocked := explicitObservabilitySettings(v, dotEnv)

	cfg := Default()
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	cfg.dynamicObservabilityLocked = dynamicObservabilityLocked
	return cfg, nil
}

// DynamicObservabilityAllowed reports whether a persisted setting may fill
// this runtime value. Unknown keys are denied. Explicit process environment,
// root .env, and YAML values retain authority over database settings.
func (cfg Config) DynamicObservabilityAllowed(settingKey string) bool {
	if _, known := dynamicObservabilitySources[settingKey]; !known {
		return false
	}
	return !cfg.dynamicObservabilityLocked[settingKey]
}

// Validate checks the configuration before external clients are constructed.
func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.Server.Addr) == "" {
		return errors.New("server.addr is required")
	}
	if cfg.Server.ReadTimeout <= 0 || cfg.Server.WriteTimeout <= 0 || cfg.Server.IdleTimeout <= 0 || cfg.Server.ShutdownTimeout <= 0 {
		return errors.New("server timeouts must be positive")
	}
	if strings.TrimSpace(cfg.Logging.Level) == "" {
		return errors.New("logging.level is required")
	}
	if err := cfg.Database.validate(); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	if err := cfg.Redis.validate(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	if err := cfg.Auth.validate(); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := cfg.Install.validate(); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	if err := cfg.Tenant.validate(); err != nil {
		return fmt.Errorf("tenant: %w", err)
	}
	if err := cfg.Observability.Validate(); err != nil {
		return fmt.Errorf("observability: %w", err)
	}
	return nil
}

func (cfg InstallConfig) validate() error {
	if strings.TrimSpace(cfg.StateDir) == "" {
		return errors.New("state_dir is required")
	}
	clean := filepath.Clean(cfg.StateDir)
	root := string(filepath.Separator)
	if volume := filepath.VolumeName(clean); volume != "" {
		root = volume + string(filepath.Separator)
	}
	if clean == root {
		return errors.New("state_dir must not be a filesystem root")
	}
	return nil
}

func (cfg TenantConfig) validate() error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.Mode != "single" && cfg.Mode != "multi" {
		return fmt.Errorf("mode must be single or multi, got %q", cfg.Mode)
	}
	if err := validateTenantValue(cfg.DefaultID, "default_id"); err != nil {
		return err
	}
	if err := validateTenantHeader(cfg.TenantHeader, "tenant_header"); err != nil {
		return err
	}
	return validateTenantHeader(cfg.OrganizationHeader, "organization_header")
}

func validateTenantValue(value, field string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > 128 || strings.ContainsAny(value, "\r\n\t") {
		return fmt.Errorf("%s is invalid", field)
	}
	return nil
}

func validateTenantHeader(value, field string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\t :") {
		return fmt.Errorf("%s is invalid", field)
	}
	return nil
}

func (cfg AuthConfig) validate() error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.AccessTTL <= 0 || cfg.RefreshTTL <= 0 || cfg.RefreshTTL <= cfg.AccessTTL {
		return errors.New("access_ttl and refresh_ttl must be positive, with refresh_ttl greater than access_ttl")
	}
	if cfg.BcryptCost < 10 || cfg.BcryptCost > 14 {
		return errors.New("bcrypt_cost must be between 10 and 14")
	}
	if cfg.RateLimitWindow <= 0 || cfg.RateLimitMaxAttempts <= 0 {
		return errors.New("rate limit window and max attempts must be positive")
	}
	if cfg.LockoutThreshold <= 0 || cfg.LockoutDuration <= 0 {
		return errors.New("lockout threshold and duration must be positive")
	}
	if strings.TrimSpace(cfg.RefreshCookieName) == "" || strings.ContainsAny(cfg.RefreshCookieName, "\r\n;= ") {
		return errors.New("refresh_cookie_name must be a valid cookie name")
	}
	if len([]byte(cfg.JWTSecret)) < 32 {
		return errors.New("jwt_secret must contain at least 32 bytes when auth is enabled")
	}
	if strings.TrimSpace(cfg.Issuer) == "" || strings.TrimSpace(cfg.Audience) == "" {
		return errors.New("issuer and audience are required when auth is enabled")
	}
	return nil
}

func (cfg DatabaseConfig) validate() error {
	if cfg.MaxOpenConns < 0 || cfg.MaxIdleConns < 0 {
		return errors.New("connection pool sizes must not be negative")
	}
	if cfg.MaxOpenConns > 0 && cfg.MaxIdleConns > cfg.MaxOpenConns {
		return errors.New("max_idle_conns must not exceed max_open_conns")
	}
	if cfg.ConnMaxLifetime < 0 || cfg.ConnMaxIdleTime < 0 || cfg.PingTimeout < 0 {
		return errors.New("connection durations must not be negative")
	}
	if !cfg.Enabled {
		return nil
	}
	if cfg.Driver != "mysql" && cfg.Driver != "postgres" {
		return fmt.Errorf("driver must be mysql or postgres, got %q", cfg.Driver)
	}
	switch cfg.Mode {
	case "single", "cluster_endpoint":
		if strings.TrimSpace(cfg.DSN) == "" {
			return fmt.Errorf("dsn is required for mode %q", cfg.Mode)
		}
	case "read_write":
		if strings.TrimSpace(cfg.PrimaryDSN) == "" || len(nonEmpty(cfg.ReplicaDSNs)) == 0 {
			return errors.New("primary_dsn and at least one replica_dsns entry are required for read_write mode")
		}
	default:
		return fmt.Errorf("mode must be single, read_write, or cluster_endpoint, got %q", cfg.Mode)
	}
	if cfg.ReadPolicy != "random" && cfg.ReadPolicy != "round_robin" {
		return fmt.Errorf("read_policy must be random or round_robin, got %q", cfg.ReadPolicy)
	}
	return nil
}

// MigrationDSN returns the write endpoint used by the explicit migration CLI.
// Read replicas are never eligible migration targets.
func (cfg DatabaseConfig) MigrationDSN() (string, error) {
	if !cfg.Enabled {
		return "", errors.New("database is disabled")
	}

	var dsn string
	switch cfg.Mode {
	case "single", "cluster_endpoint":
		dsn = cfg.DSN
	case "read_write":
		dsn = cfg.PrimaryDSN
	default:
		return "", errors.New("database mode has no migration endpoint")
	}
	if strings.TrimSpace(dsn) == "" {
		return "", errors.New("database migration endpoint is empty")
	}
	return dsn, nil
}

func (cfg RedisConfig) validate() error {
	if cfg.DB < 0 {
		return errors.New("db must not be negative")
	}
	if cfg.DialTimeout < 0 || cfg.ReadTimeout < 0 || cfg.WriteTimeout < 0 || cfg.PingTimeout < 0 {
		return errors.New("timeouts must not be negative")
	}
	if len(cfg.Namespace) > 128 || strings.TrimSpace(cfg.Namespace) != cfg.Namespace || strings.ContainsAny(cfg.Namespace, "\r\n\t") {
		return errors.New("namespace must be a trimmed string of at most 128 characters")
	}
	for advertised, reachable := range cfg.AddressMap {
		if strings.TrimSpace(advertised) == "" || strings.TrimSpace(reachable) == "" || strings.ContainsAny(advertised+reachable, "\r\n") {
			return errors.New("address_map endpoints must be non-empty and single-line")
		}
	}
	if !cfg.Enabled {
		return nil
	}
	switch cfg.Mode {
	case "single":
		if strings.TrimSpace(cfg.Addr) == "" {
			return errors.New("addr is required for single mode")
		}
	case "sentinel":
		if len(nonEmpty(cfg.Addrs)) == 0 || strings.TrimSpace(cfg.MasterName) == "" {
			return errors.New("addrs and master_name are required for sentinel mode")
		}
	case "cluster":
		if len(nonEmpty(cfg.Addrs)) < 2 {
			return errors.New("at least two addrs entries are required for cluster mode")
		}
		if cfg.DB != 0 {
			return errors.New("db must be zero for cluster mode")
		}
	default:
		return fmt.Errorf("mode must be single, sentinel, or cluster, got %q", cfg.Mode)
	}
	return nil
}

// SafeSummary returns configuration details suitable for structured logs. It
// never returns a DSN, username, or password.
func (cfg Config) SafeSummary() Summary {
	return Summary{
		Server: ServerSummary{
			Addr:            cfg.Server.Addr,
			ReadTimeout:     cfg.Server.ReadTimeout,
			WriteTimeout:    cfg.Server.WriteTimeout,
			IdleTimeout:     cfg.Server.IdleTimeout,
			ShutdownTimeout: cfg.Server.ShutdownTimeout,
		},
		Logging: LoggingSummary{Level: cfg.Logging.Level},
		Database: DatabaseSummary{
			Enabled:      cfg.Database.Enabled,
			Driver:       cfg.Database.Driver,
			Mode:         cfg.Database.Mode,
			ReadPolicy:   cfg.Database.ReadPolicy,
			ReplicaCount: len(nonEmpty(cfg.Database.ReplicaDSNs)),
		},
		Redis: RedisSummary{
			Enabled:      cfg.Redis.Enabled,
			Mode:         cfg.Redis.Mode,
			AddressCount: redisAddressCount(cfg.Redis),
			MasterName:   cfg.Redis.MasterName,
			Namespace:    cfg.Redis.Namespace,
			DB:           cfg.Redis.DB,
		},
		Auth: AuthSummary{
			Enabled:              cfg.Auth.Enabled,
			Issuer:               cfg.Auth.Issuer,
			Audience:             cfg.Auth.Audience,
			AccessTTL:            cfg.Auth.AccessTTL,
			RefreshTTL:           cfg.Auth.RefreshTTL,
			RefreshCookieName:    cfg.Auth.RefreshCookieName,
			SecureCookie:         cfg.Auth.SecureCookie,
			BcryptCost:           cfg.Auth.BcryptCost,
			RateLimitWindow:      cfg.Auth.RateLimitWindow,
			RateLimitMaxAttempts: cfg.Auth.RateLimitMaxAttempts,
			LockoutThreshold:     cfg.Auth.LockoutThreshold,
			LockoutDuration:      cfg.Auth.LockoutDuration,
			RegistrationEnabled:  cfg.Auth.RegistrationEnabled,
		},
		Install: InstallSummary{StateDirectoryAbsolute: filepath.IsAbs(cfg.Install.StateDir)},
		Tenant: TenantSummary{
			Enabled:            cfg.Tenant.Enabled,
			Mode:               cfg.Tenant.Mode,
			DefaultID:          cfg.Tenant.DefaultID,
			TenantHeader:       cfg.Tenant.TenantHeader,
			OrganizationHeader: cfg.Tenant.OrganizationHeader,
		},
		Observability: func() observability.Config {
			redacted := cfg.Observability
			redacted.OTLPAPIKey = ""
			return redacted
		}(),
	}
}

func newViper() *viper.Viper {
	cfg := Default()
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("server.addr", cfg.Server.Addr)
	v.SetDefault("server.read_timeout", cfg.Server.ReadTimeout)
	v.SetDefault("server.write_timeout", cfg.Server.WriteTimeout)
	v.SetDefault("server.idle_timeout", cfg.Server.IdleTimeout)
	v.SetDefault("server.shutdown_timeout", cfg.Server.ShutdownTimeout)
	v.SetDefault("logging.level", cfg.Logging.Level)
	v.SetDefault("database.enabled", cfg.Database.Enabled)
	v.SetDefault("database.driver", cfg.Database.Driver)
	v.SetDefault("database.dsn", cfg.Database.DSN)
	v.SetDefault("database.mode", cfg.Database.Mode)
	v.SetDefault("database.primary_dsn", cfg.Database.PrimaryDSN)
	v.SetDefault("database.replica_dsns", cfg.Database.ReplicaDSNs)
	v.SetDefault("database.read_policy", cfg.Database.ReadPolicy)
	v.SetDefault("database.max_open_conns", cfg.Database.MaxOpenConns)
	v.SetDefault("database.max_idle_conns", cfg.Database.MaxIdleConns)
	v.SetDefault("database.conn_max_lifetime", cfg.Database.ConnMaxLifetime)
	v.SetDefault("database.conn_max_idle_time", cfg.Database.ConnMaxIdleTime)
	v.SetDefault("database.ping_timeout", cfg.Database.PingTimeout)
	v.SetDefault("redis.enabled", cfg.Redis.Enabled)
	v.SetDefault("redis.addr", cfg.Redis.Addr)
	v.SetDefault("redis.username", cfg.Redis.Username)
	v.SetDefault("redis.password", cfg.Redis.Password)
	v.SetDefault("redis.db", cfg.Redis.DB)
	v.SetDefault("redis.namespace", cfg.Redis.Namespace)
	v.SetDefault("redis.mode", cfg.Redis.Mode)
	v.SetDefault("redis.addrs", cfg.Redis.Addrs)
	v.SetDefault("redis.master_name", cfg.Redis.MasterName)
	v.SetDefault("redis.address_map", cfg.Redis.AddressMap)
	v.SetDefault("redis.dial_timeout", cfg.Redis.DialTimeout)
	v.SetDefault("redis.read_timeout", cfg.Redis.ReadTimeout)
	v.SetDefault("redis.write_timeout", cfg.Redis.WriteTimeout)
	v.SetDefault("redis.ping_timeout", cfg.Redis.PingTimeout)
	v.SetDefault("auth.enabled", cfg.Auth.Enabled)
	v.SetDefault("auth.jwt_secret", cfg.Auth.JWTSecret)
	v.SetDefault("auth.issuer", cfg.Auth.Issuer)
	v.SetDefault("auth.audience", cfg.Auth.Audience)
	v.SetDefault("auth.access_ttl", cfg.Auth.AccessTTL)
	v.SetDefault("auth.refresh_ttl", cfg.Auth.RefreshTTL)
	v.SetDefault("auth.refresh_cookie_name", cfg.Auth.RefreshCookieName)
	v.SetDefault("auth.secure_cookie", cfg.Auth.SecureCookie)
	v.SetDefault("auth.bcrypt_cost", cfg.Auth.BcryptCost)
	v.SetDefault("auth.rate_limit_window", cfg.Auth.RateLimitWindow)
	v.SetDefault("auth.rate_limit_max_attempts", cfg.Auth.RateLimitMaxAttempts)
	v.SetDefault("auth.lockout_threshold", cfg.Auth.LockoutThreshold)
	v.SetDefault("auth.lockout_duration", cfg.Auth.LockoutDuration)
	v.SetDefault("auth.registration_enabled", cfg.Auth.RegistrationEnabled)
	v.SetDefault("install.state_dir", cfg.Install.StateDir)
	v.SetDefault("tenant.enabled", cfg.Tenant.Enabled)
	v.SetDefault("tenant.mode", cfg.Tenant.Mode)
	v.SetDefault("tenant.default_id", cfg.Tenant.DefaultID)
	v.SetDefault("tenant.tenant_header", cfg.Tenant.TenantHeader)
	v.SetDefault("tenant.organization_header", cfg.Tenant.OrganizationHeader)
	v.SetDefault("observability.metrics_enabled", cfg.Observability.MetricsEnabled)
	v.SetDefault("observability.metrics_endpoint", cfg.Observability.MetricsEndpoint)
	v.SetDefault("observability.tracing_enabled", cfg.Observability.TracingEnabled)
	v.SetDefault("observability.otlp_endpoint", cfg.Observability.OTLPEndpoint)
	v.SetDefault("observability.otlp_protocol", cfg.Observability.OTLPProtocol)
	v.SetDefault("observability.tls_verify", cfg.Observability.TLSVerify)
	v.SetDefault("observability.sample_rate", cfg.Observability.SampleRate)
	v.SetDefault("observability.otlp_api_key", cfg.Observability.OTLPAPIKey)

	for key, environment := range environmentBindings {
		_ = v.BindEnv(key, environment)
	}
	return v
}

var environmentBindings = map[string]string{
	"server.addr":                    "SERVER_ADDR",
	"server.read_timeout":            "SERVER_READ_TIMEOUT",
	"server.write_timeout":           "SERVER_WRITE_TIMEOUT",
	"server.idle_timeout":            "SERVER_IDLE_TIMEOUT",
	"server.shutdown_timeout":        "SERVER_SHUTDOWN_TIMEOUT",
	"logging.level":                  "LOGGING_LEVEL",
	"database.enabled":               "DATABASE_ENABLED",
	"database.driver":                "DATABASE_DRIVER",
	"database.dsn":                   "DATABASE_DSN",
	"database.mode":                  "DATABASE_MODE",
	"database.primary_dsn":           "DATABASE_PRIMARY_DSN",
	"database.replica_dsns":          "DATABASE_REPLICA_DSNS",
	"database.read_policy":           "DATABASE_READ_POLICY",
	"database.max_open_conns":        "DATABASE_MAX_OPEN_CONNS",
	"database.max_idle_conns":        "DATABASE_MAX_IDLE_CONNS",
	"database.conn_max_lifetime":     "DATABASE_CONN_MAX_LIFETIME",
	"database.conn_max_idle_time":    "DATABASE_CONN_MAX_IDLE_TIME",
	"database.ping_timeout":          "DATABASE_PING_TIMEOUT",
	"redis.enabled":                  "REDIS_ENABLED",
	"redis.addr":                     "REDIS_ADDR",
	"redis.username":                 "REDIS_USERNAME",
	"redis.password":                 "REDIS_PASSWORD",
	"redis.db":                       "REDIS_DB",
	"redis.namespace":                "REDIS_NAMESPACE",
	"redis.mode":                     "REDIS_MODE",
	"redis.addrs":                    "REDIS_ADDRS",
	"redis.master_name":              "REDIS_MASTER_NAME",
	"redis.dial_timeout":             "REDIS_DIAL_TIMEOUT",
	"redis.read_timeout":             "REDIS_READ_TIMEOUT",
	"redis.write_timeout":            "REDIS_WRITE_TIMEOUT",
	"redis.ping_timeout":             "REDIS_PING_TIMEOUT",
	"auth.enabled":                   "AUTH_ENABLED",
	"auth.jwt_secret":                "AUTH_JWT_SECRET",
	"auth.issuer":                    "AUTH_ISSUER",
	"auth.audience":                  "AUTH_AUDIENCE",
	"auth.access_ttl":                "AUTH_ACCESS_TTL",
	"auth.refresh_ttl":               "AUTH_REFRESH_TTL",
	"auth.refresh_cookie_name":       "AUTH_REFRESH_COOKIE_NAME",
	"auth.secure_cookie":             "AUTH_SECURE_COOKIE",
	"auth.bcrypt_cost":               "AUTH_BCRYPT_COST",
	"auth.rate_limit_window":         "AUTH_RATE_LIMIT_WINDOW",
	"auth.rate_limit_max_attempts":   "AUTH_RATE_LIMIT_MAX_ATTEMPTS",
	"auth.lockout_threshold":         "AUTH_LOCKOUT_THRESHOLD",
	"auth.lockout_duration":          "AUTH_LOCKOUT_DURATION",
	"auth.registration_enabled":      "AUTH_REGISTRATION_ENABLED",
	"install.state_dir":              "INSTALL_STATE_DIR",
	"tenant.enabled":                 "TENANT_ENABLED",
	"tenant.mode":                    "TENANT_MODE",
	"tenant.default_id":              "TENANT_DEFAULT_ID",
	"tenant.tenant_header":           "TENANT_HEADER",
	"tenant.organization_header":     "TENANT_ORGANIZATION_HEADER",
	"observability.metrics_enabled":  "OBSERVABILITY_METRICS_ENABLED",
	"observability.metrics_endpoint": "OBSERVABILITY_METRICS_ENDPOINT",
	"observability.tracing_enabled":  "OBSERVABILITY_TRACING_ENABLED",
	"observability.otlp_endpoint":    "OBSERVABILITY_OTLP_ENDPOINT",
	"observability.otlp_protocol":    "OBSERVABILITY_OTLP_PROTOCOL",
	"observability.tls_verify":       "OBSERVABILITY_TLS_VERIFY",
	"observability.sample_rate":      "OBSERVABILITY_SAMPLE_RATE",
	"observability.otlp_api_key":     "OBSERVABILITY_OTLP_API_KEY",
}

type observabilitySource struct {
	configKey   string
	environment string
}

var dynamicObservabilitySources = map[string]observabilitySource{
	"observability.metrics.enabled":     {configKey: "observability.metrics_enabled", environment: "OBSERVABILITY_METRICS_ENABLED"},
	"observability.metrics.endpoint":    {configKey: "observability.metrics_endpoint", environment: "OBSERVABILITY_METRICS_ENDPOINT"},
	"observability.tracing.enabled":     {configKey: "observability.tracing_enabled", environment: "OBSERVABILITY_TRACING_ENABLED"},
	"observability.tracing.endpoint":    {configKey: "observability.otlp_endpoint", environment: "OBSERVABILITY_OTLP_ENDPOINT"},
	"observability.tracing.protocol":    {configKey: "observability.otlp_protocol", environment: "OBSERVABILITY_OTLP_PROTOCOL"},
	"observability.tracing.tls_verify":  {configKey: "observability.tls_verify", environment: "OBSERVABILITY_TLS_VERIFY"},
	"observability.tracing.sample_rate": {configKey: "observability.sample_rate", environment: "OBSERVABILITY_SAMPLE_RATE"},
	"observability.otlp.api_key":        {configKey: "observability.otlp_api_key", environment: "OBSERVABILITY_OTLP_API_KEY"},
}

func explicitObservabilitySettings(v *viper.Viper, dotEnv map[string]string) map[string]bool {
	locked := make(map[string]bool, len(dynamicObservabilitySources))
	for settingKey, source := range dynamicObservabilitySources {
		_, fromProcess := os.LookupEnv(source.environment)
		_, fromDotEnv := dotEnv[source.environment]
		if fromProcess || fromDotEnv || v.InConfig(source.configKey) {
			locked[settingKey] = true
		}
	}
	return locked
}

func resolvePath(path string) (string, bool) {
	if strings.TrimSpace(path) != "" {
		return path, true
	}
	if envPath := strings.TrimSpace(os.Getenv("SERVER_CONFIG")); envPath != "" {
		return envPath, true
	}
	return filepath.FromSlash(defaultConfigPath), false
}

func applyListEnvironmentOverrides(v *viper.Viper, dotEnv map[string]string) {
	if value, ok := os.LookupEnv("DATABASE_REPLICA_DSNS"); ok {
		v.Set("database.replica_dsns", splitCommaSeparated(value))
	} else if value, ok := dotEnv["DATABASE_REPLICA_DSNS"]; ok {
		v.Set("database.replica_dsns", splitCommaSeparated(value))
	}
	if value, ok := os.LookupEnv("REDIS_ADDRS"); ok {
		v.Set("redis.addrs", splitCommaSeparated(value))
	} else if value, ok := dotEnv["REDIS_ADDRS"]; ok {
		v.Set("redis.addrs", splitCommaSeparated(value))
	}
}

func splitCommaSeparated(value string) []string {
	return nonEmpty(strings.Split(value, ","))
}

func nonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func redisAddressCount(cfg RedisConfig) int {
	if cfg.Mode == "single" {
		if strings.TrimSpace(cfg.Addr) == "" {
			return 0
		}
		return 1
	}
	return len(nonEmpty(cfg.Addrs))
}
