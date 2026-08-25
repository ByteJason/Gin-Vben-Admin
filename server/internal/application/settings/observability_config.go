package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	domainobs "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/observability"
)

var observabilitySettingKeys = []string{
	"observability.metrics.enabled",
	"observability.metrics.endpoint",
	"observability.tracing.enabled",
	"observability.tracing.endpoint",
	"observability.tracing.protocol",
	"observability.tracing.tls_verify",
	"observability.tracing.sample_rate",
	"observability.otlp.api_key",
}

var observabilitySettingKeySet = func() map[string]struct{} {
	keys := make(map[string]struct{}, len(observabilitySettingKeys))
	for _, key := range observabilitySettingKeys {
		keys[key] = struct{}{}
	}
	return keys
}()

// IsObservabilitySettingKey is the shared allowlist for the dedicated
// observability settings transport. Keeping the boundary beside the runtime
// overlay prevents an IAM path grant from widening to unrelated settings.
func IsObservabilitySettingKey(key string) bool {
	_, ok := observabilitySettingKeySet[key]
	return ok
}

// ResolveObservabilityConfig overlays persisted values on a validated fallback
// without exposing secret contents in errors. allow preserves higher-authority
// configuration sources; a nil predicate allows every known persisted key.
func ResolveObservabilityConfig(ctx context.Context, repo Repository, fallback domainobs.Config, allow func(string) bool) (domainobs.Config, error) {
	resolved := fallback
	if repo == nil {
		return resolved, resolved.Validate()
	}
	for _, key := range observabilitySettingKeys {
		if allow != nil && !allow(key) {
			continue
		}
		record, err := repo.Current(ctx, key)
		if errors.Is(err, ErrSettingNotFound) {
			continue
		}
		if err != nil {
			return domainobs.Config{}, fmt.Errorf("read persisted observability setting %q: %w", key, err)
		}
		if err := applyObservabilitySetting(&resolved, key, record.RawValue); err != nil {
			return domainobs.Config{}, err
		}
	}
	if err := resolved.Validate(); err != nil {
		return domainobs.Config{}, fmt.Errorf("validate persisted observability settings: %w", err)
	}
	return resolved, nil
}

func applyObservabilitySetting(target *domainobs.Config, key string, raw []byte) error {
	decode := func(destination any) error {
		if err := json.Unmarshal(raw, destination); err != nil {
			return fmt.Errorf("decode persisted observability setting %q: invalid JSON", key)
		}
		return nil
	}
	switch key {
	case "observability.metrics.enabled":
		return decode(&target.MetricsEnabled)
	case "observability.metrics.endpoint":
		return decode(&target.MetricsEndpoint)
	case "observability.tracing.enabled":
		return decode(&target.TracingEnabled)
	case "observability.tracing.endpoint":
		return decode(&target.OTLPEndpoint)
	case "observability.tracing.protocol":
		return decode(&target.OTLPProtocol)
	case "observability.tracing.tls_verify":
		return decode(&target.TLSVerify)
	case "observability.tracing.sample_rate":
		return decode(&target.SampleRate)
	case "observability.otlp.api_key":
		return decode(&target.OTLPAPIKey)
	default:
		return fmt.Errorf("unknown persisted observability setting %q", key)
	}
}
