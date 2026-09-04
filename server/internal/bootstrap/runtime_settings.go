package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	monitorapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/monitor"
	settingsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
)

// runtimeSettingsProvider exposes the bounded runtime-environment view used by
// System Settings. It deliberately does not copy DSNs, Redis addresses,
// credentials, or any other deployment secret into the settings response.
// Probe failures are represented by an "unknown" status and do not make the
// read-only settings page unavailable.
func runtimeSettingsProvider(cfg config.Config, monitor *monitorapp.Service) settingsapp.RuntimeModuleProvider {
	node := strings.TrimSpace(os.Getenv("HOSTNAME"))
	if node == "" {
		node, _ = os.Hostname()
	}
	if node == "" {
		node = "unknown"
	}
	now := time.Now().UTC()
	value := func(key, raw string, source settingsapp.Source) settingsapp.StoredSetting {
		return settingsapp.StoredSetting{Key: key, RawValue: json.RawMessage(raw), Source: source, UpdatedAt: now}
	}
	return settingsapp.RuntimeModuleProviderFunc(func(ctx context.Context) (map[string]settingsapp.StoredSetting, error) {
		// Deployment-owned values are intentionally labelled with YAML as the
		// default file/config source. The source policy remains visible in each
		// definition, and environment overrides are still enforced by the normal
		// resolver when a deployment integrates a source-aware provider.
		values := map[string]settingsapp.StoredSetting{
			"runtime.version":         value("runtime.version", `"unknown"`, settingsapp.SourceDefault),
			"runtime.database.status": value("runtime.database.status", `"unknown"`, settingsapp.SourceDefault),
			"runtime.database.source": value("runtime.database.source", `"default"`, settingsapp.SourceDefault),
			"runtime.redis.status":    value("runtime.redis.status", `"unknown"`, settingsapp.SourceDefault),
			"runtime.redis.source":    value("runtime.redis.source", `"default"`, settingsapp.SourceDefault),
			"runtime.http.address":    value("runtime.http.address", quoteJSON(cfg.Server.Addr), settingsapp.SourceYAML),
			"runtime.node":            value("runtime.node", quoteJSON(node), settingsapp.SourceDefault),
			"runtime.pending_restart": value("runtime.pending_restart", `false`, settingsapp.SourceDefault),
		}
		if monitor == nil {
			return values, nil
		}
		overview, err := monitor.ServerStatus(ctx)
		if err != nil {
			return values, nil
		}
		if overview.Version != "" {
			values["runtime.version"] = value("runtime.version", quoteJSON(overview.Version), settingsapp.SourceDefault)
		}
		values["runtime.database.status"] = value("runtime.database.status", quoteJSON(string(overview.Database.Status)), settingsapp.SourceDefault)
		values["runtime.redis.status"] = value("runtime.redis.status", quoteJSON(string(overview.Redis.Status)), settingsapp.SourceDefault)
		// A dependency that is not configured is still a deployment fact. Keep
		// the value explicit so operators can distinguish it from an unknown
		// probe result without exposing the underlying connection details.
		if !cfg.Database.Enabled {
			values["runtime.database.status"] = value("runtime.database.status", `"not_configured"`, settingsapp.SourceDefault)
		}
		if !cfg.Redis.Enabled {
			values["runtime.redis.status"] = value("runtime.redis.status", `"not_configured"`, settingsapp.SourceDefault)
		}
		return values, nil
	})
}

func quoteJSON(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}
