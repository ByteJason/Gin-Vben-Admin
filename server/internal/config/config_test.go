package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultIsUsable(t *testing.T) {
	cfg := Default()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default().Validate() error = %v", err)
	}
	if cfg.Server.Addr != ":8080" {
		t.Fatalf("default server address = %q, want :8080", cfg.Server.Addr)
	}
	if cfg.Server.ReadTimeout <= 0 || cfg.Server.WriteTimeout <= 0 || cfg.Server.IdleTimeout <= 0 || cfg.Server.ShutdownTimeout <= 0 {
		t.Fatalf("default server timeouts must all be positive: %#v", cfg.Server)
	}
}

func TestLoadReadsYAMLAndEnvironmentTakesPrecedence(t *testing.T) {
	path := writeConfigFile(t, `
logging:
  level: warn
server:
  addr: 127.0.0.1:9080
  read_timeout: 12s
  write_timeout: 13s
  idle_timeout: 14s
  shutdown_timeout: 15s
database:
  enabled: true
  driver: postgres
  mode: read_write
  primary_dsn: postgres://primary:secret@db/primary
  replica_dsns:
    - postgres://replica:secret@db/replica
  read_policy: round_robin
  max_open_conns: 20
  max_idle_conns: 10
  conn_max_lifetime: 30m
  conn_max_idle_time: 5m
  ping_timeout: 2s
redis:
  enabled: true
  mode: sentinel
  addrs: [redis-a:26379, redis-b:26379]
  master_name: mymaster
  namespace: app
  dial_timeout: 1s
  read_timeout: 2s
  write_timeout: 3s
  ping_timeout: 4s
`)
	t.Setenv("SERVER_ADDR", "127.0.0.1:9090")
	t.Setenv("LOGGING_LEVEL", "debug")
	t.Setenv("DATABASE_REPLICA_DSNS", "postgres://one@db/one, postgres://two@db/two")
	t.Setenv("REDIS_ADDRS", "redis-c:26379, redis-d:26379")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Addr != "127.0.0.1:9090" || cfg.Logging.Level != "debug" {
		t.Fatalf("environment values did not override YAML: server=%q level=%q", cfg.Server.Addr, cfg.Logging.Level)
	}
	if cfg.Server.ReadTimeout != 12*time.Second || cfg.Server.WriteTimeout != 13*time.Second || cfg.Server.IdleTimeout != 14*time.Second || cfg.Server.ShutdownTimeout != 15*time.Second {
		t.Fatalf("YAML durations not decoded: %#v", cfg.Server)
	}
	if cfg.Database.Mode != "read_write" || cfg.Database.Driver != "postgres" || cfg.Database.ReadPolicy != "round_robin" {
		t.Fatalf("database topology not loaded: %#v", cfg.Database)
	}
	if got, want := cfg.Database.ReplicaDSNs, []string{"postgres://one@db/one", "postgres://two@db/two"}; !sameStrings(got, want) {
		t.Fatalf("DATABASE_REPLICA_DSNS = %#v, want %#v", got, want)
	}
	if got, want := cfg.Redis.Addrs, []string{"redis-c:26379", "redis-d:26379"}; !sameStrings(got, want) {
		t.Fatalf("REDIS_ADDRS = %#v, want %#v", got, want)
	}
}

func TestLoadPathSelectionAndMissingFilePolicy(t *testing.T) {
	t.Run("SERVER_CONFIG is used when path is empty", func(t *testing.T) {
		path := writeConfigFile(t, "server:\n  addr: 127.0.0.1:9010\n")
		t.Setenv("SERVER_CONFIG", path)

		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load(\"\") error = %v", err)
		}
		if cfg.Server.Addr != "127.0.0.1:9010" {
			t.Fatalf("SERVER_CONFIG was not used, addr = %q", cfg.Server.Addr)
		}
	})

	t.Run("missing explicit path is an error", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
		if err == nil {
			t.Fatal("Load(explicit missing path) error = nil, want error")
		}
	})

	t.Run("missing default path retains defaults", func(t *testing.T) {
		t.Setenv("SERVER_CONFIG", "")
		t.Chdir(t.TempDir())

		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load(default missing file) error = %v", err)
		}
		if cfg.Server.Addr != ":8080" {
			t.Fatalf("missing default path addr = %q, want :8080", cfg.Server.Addr)
		}
	})
}

func TestValidateRejectsInvalidTopologyAndRanges(t *testing.T) {
	valid := Default()
	valid.Database = DatabaseConfig{Enabled: true, Driver: "mysql", Mode: "single", DSN: "user:secret@tcp(db:3306)/app", ReadPolicy: "random"}
	valid.Redis = RedisConfig{Enabled: true, Mode: "single", Addr: "redis:6379"}

	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"unknown database driver", func(c *Config) { c.Database.Driver = "sqlite" }},
		{"single database requires DSN", func(c *Config) { c.Database.DSN = "" }},
		{"read write database requires primary and replica", func(c *Config) {
			c.Database.Mode, c.Database.DSN, c.Database.PrimaryDSN, c.Database.ReplicaDSNs = "read_write", "", "", nil
		}},
		{"cluster endpoint database requires DSN", func(c *Config) { c.Database.Mode, c.Database.DSN = "cluster_endpoint", "" }},
		{"database pool range", func(c *Config) { c.Database.MaxOpenConns = -1 }},
		{"database duration range", func(c *Config) { c.Database.PingTimeout = -time.Second }},
		{"single redis requires address", func(c *Config) { c.Redis.Addr = "" }},
		{"sentinel redis requires addresses and master", func(c *Config) {
			c.Redis.Mode, c.Redis.Addr, c.Redis.Addrs, c.Redis.MasterName = "sentinel", "", []string{"redis-a:26379"}, ""
		}},
		{"cluster redis requires two addresses", func(c *Config) { c.Redis.Mode, c.Redis.Addr, c.Redis.Addrs = "cluster", "", []string{"redis-a:6379"} }},
		{"cluster redis requires database zero", func(c *Config) {
			c.Redis.Mode, c.Redis.Addr, c.Redis.Addrs, c.Redis.DB = "cluster", "", []string{"redis-a:6379", "redis-b:6379"}, 1
		}},
		{"redis database range", func(c *Config) { c.Redis.DB = -1 }},
		{"redis duration range", func(c *Config) { c.Redis.DialTimeout = -time.Second }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			cfg.Database.ReplicaDSNs = append([]string(nil), valid.Database.ReplicaDSNs...)
			cfg.Redis.Addrs = append([]string(nil), valid.Redis.Addrs...)
			tt.edit(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
		})
	}
}

func TestSafeSummaryNeverContainsCredentialsOrDSNs(t *testing.T) {
	cfg := Default()
	cfg.Database = DatabaseConfig{
		Enabled:     true,
		Driver:      "postgres",
		Mode:        "read_write",
		PrimaryDSN:  "postgres://primary-user:primary-secret@db/primary",
		ReplicaDSNs: []string{"postgres://replica-user:replica-secret@db/replica"},
	}
	cfg.Redis = RedisConfig{
		Enabled:    true,
		Mode:       "sentinel",
		Addrs:      []string{"redis-a:26379", "redis-b:26379"},
		MasterName: "mymaster",
		Username:   "redis-user",
		Password:   "redis-secret",
	}

	summary, err := json.Marshal(cfg.SafeSummary())
	if err != nil {
		t.Fatalf("marshal SafeSummary() = %v", err)
	}
	got := string(summary)
	for _, secret := range []string{"primary-user", "primary-secret", "replica-user", "replica-secret", "redis-user", "redis-secret", "postgres://"} {
		if strings.Contains(got, secret) {
			t.Fatalf("SafeSummary leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, `"mode":"read_write"`) || !strings.Contains(got, `"replica_count":1`) || !strings.Contains(got, `"address_count":2`) {
		t.Fatalf("SafeSummary did not retain topology summary: %s", got)
	}
}

func TestDatabaseMigrationDSNAlwaysTargetsTheWriteEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		config  DatabaseConfig
		want    string
		wantErr bool
	}{
		{
			name:   "single",
			config: DatabaseConfig{Enabled: true, Mode: "single", DSN: "single-dsn"},
			want:   "single-dsn",
		},
		{
			name:   "read write",
			config: DatabaseConfig{Enabled: true, Mode: "read_write", PrimaryDSN: "primary-dsn", ReplicaDSNs: []string{"replica-dsn"}},
			want:   "primary-dsn",
		},
		{
			name:   "cluster endpoint",
			config: DatabaseConfig{Enabled: true, Mode: "cluster_endpoint", DSN: "cluster-dsn"},
			want:   "cluster-dsn",
		},
		{
			name:    "disabled",
			config:  DatabaseConfig{Mode: "single", DSN: "ignored"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.config.MigrationDSN()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("MigrationDSN() = %q, want error", got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("MigrationDSN() = %q, %v; want %q, nil", got, err, tt.want)
			}
		})
	}
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
