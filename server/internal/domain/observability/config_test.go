package observability

import "testing"

func TestConfigDefaultsDisableExternalCollection(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MetricsEnabled || cfg.TracingEnabled {
		t.Fatalf("defaults enabled metrics/tracing: %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestConfigValidatesExternalTargetsWithoutCreatingCollectors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MetricsEnabled = true
	cfg.MetricsEndpoint = "https://metrics.example.invalid/api"
	cfg.TracingEnabled = true
	cfg.OTLPEndpoint = "https://collector.example.invalid"
	cfg.OTLPProtocol = "http/protobuf"
	cfg.SampleRate = 0.25
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.CollectorCount() != 0 {
		t.Fatalf("CollectorCount() = %d, want 0 before B8", cfg.CollectorCount())
	}
}

func TestConfigRejectsEnabledTargetWithoutEndpoint(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MetricsEnabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want endpoint error")
	}
}
