package settings

import (
	"context"
	"strings"
	"testing"

	domainobs "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/observability"
)

func TestResolveObservabilityConfigOverlaysOnlyAllowedPersistedValues(t *testing.T) {
	repository := NewMemoryRepository()
	for key, value := range map[string]string{
		"observability.metrics.enabled":     `true`,
		"observability.metrics.endpoint":    `"http://127.0.0.1:8080/metrics"`,
		"observability.tracing.enabled":     `true`,
		"observability.tracing.endpoint":    `"https://collector.database.example/v1/traces"`,
		"observability.tracing.protocol":    `"grpc"`,
		"observability.tracing.tls_verify":  `false`,
		"observability.tracing.sample_rate": `0.5`,
		"observability.otlp.api_key":        `"stored-secret"`,
	} {
		if _, err := repository.Append(context.Background(), StoredSetting{Key: key, RawValue: []byte(value)}); err != nil {
			t.Fatalf("Append(%q) error = %v", key, err)
		}
	}

	fallback := domainobs.DefaultConfig()
	fallback.OTLPEndpoint = "https://collector.explicit.example/v1/traces"
	resolved, err := ResolveObservabilityConfig(context.Background(), repository, fallback, func(key string) bool {
		return key != "observability.tracing.endpoint"
	})
	if err != nil {
		t.Fatalf("ResolveObservabilityConfig() error = %v", err)
	}
	if !resolved.MetricsEnabled || resolved.MetricsEndpoint != "http://127.0.0.1:8080/metrics" {
		t.Fatalf("metrics config = %#v", resolved)
	}
	if !resolved.TracingEnabled || resolved.OTLPEndpoint != fallback.OTLPEndpoint || resolved.OTLPProtocol != "grpc" || resolved.TLSVerify || resolved.SampleRate != 0.5 || resolved.OTLPAPIKey != "stored-secret" {
		t.Fatalf("tracing config = %#v", resolved.SafeSummary())
	}
}

func TestResolveObservabilityConfigRejectsMalformedSecretWithoutDisclosure(t *testing.T) {
	repository := NewMemoryRepository()
	if _, err := repository.Append(context.Background(), StoredSetting{
		Key:      "observability.otlp.api_key",
		RawValue: []byte("TOP_SECRET_NOT_JSON"),
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	_, err := ResolveObservabilityConfig(context.Background(), repository, domainobs.DefaultConfig(), func(string) bool { return true })
	if err == nil {
		t.Fatal("ResolveObservabilityConfig() error = nil")
	}
	if strings.Contains(err.Error(), "TOP_SECRET_NOT_JSON") {
		t.Fatalf("error disclosed stored secret: %v", err)
	}
}
