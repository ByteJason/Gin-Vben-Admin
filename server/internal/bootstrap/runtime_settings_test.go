package bootstrap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
)

func TestRuntimeSettingsProviderReturnsBoundedReadOnlyValues(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Addr = "127.0.0.1:8181"
	cfg.Database.DSN = "mysql://user:password@db.example.invalid/app"
	cfg.Redis.Password = "redis-secret"
	provider := runtimeSettingsProvider(cfg, nil)
	values, err := provider.LoadRuntimeModule(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"runtime.version", "runtime.database.status", "runtime.redis.status", "runtime.http.address", "runtime.node", "runtime.pending_restart"} {
		if _, ok := values[key]; !ok {
			t.Fatalf("missing runtime value %q", key)
		}
	}
	if got := string(values["runtime.http.address"].RawValue); got != `"127.0.0.1:8181"` {
		t.Fatalf("http address = %s", got)
	}
	for key, value := range values {
		if value.Sensitive || strings.Contains(string(value.RawValue), "password") || strings.Contains(string(value.RawValue), "redis-secret") {
			t.Fatalf("runtime value %q leaked deployment secret: %+v", key, value)
		}
		var decoded any
		if err := json.Unmarshal(value.RawValue, &decoded); err != nil {
			t.Fatalf("runtime value %q is invalid JSON: %v", key, err)
		}
	}
}
