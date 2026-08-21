// Package observability contains the B6 configuration contract for external
// metrics and tracing. It deliberately does not construct exporters; that
// runtime collection work is reserved for B8.
package observability

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type Config struct {
	MetricsEnabled  bool    `json:"metricsEnabled" yaml:"metrics_enabled"`
	MetricsEndpoint string  `json:"metricsEndpoint" yaml:"metrics_endpoint"`
	TracingEnabled  bool    `json:"tracingEnabled" yaml:"tracing_enabled"`
	OTLPEndpoint    string  `json:"otlpEndpoint" yaml:"otlp_endpoint"`
	OTLPProtocol    string  `json:"otlpProtocol" yaml:"otlp_protocol"`
	TLSVerify       bool    `json:"tlsVerify" yaml:"tls_verify"`
	SampleRate      float64 `json:"sampleRate" yaml:"sample_rate"`
	OTLPAPIKey      string  `json:"-" yaml:"otlp_api_key"`
}

func DefaultConfig() Config {
	return Config{
		OTLPProtocol: "http/protobuf",
		TLSVerify:    true,
	}
}

func (c Config) Validate() error {
	if c.SampleRate < 0 || c.SampleRate > 1 {
		return errors.New("sample_rate must be between 0 and 1")
	}
	if c.MetricsEnabled {
		if err := validateEndpoint(c.MetricsEndpoint, "metrics_endpoint"); err != nil {
			return err
		}
	}
	if c.TracingEnabled {
		if err := validateEndpoint(c.OTLPEndpoint, "otlp_endpoint"); err != nil {
			return err
		}
		switch c.OTLPProtocol {
		case "grpc", "http/protobuf":
		default:
			return fmt.Errorf("otlp_protocol must be grpc or http/protobuf, got %q", c.OTLPProtocol)
		}
	}
	return nil
}

func validateEndpoint(value, field string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute http or https URL", field)
	}
	return nil
}

// CollectorCount intentionally remains zero until B8 wires actual exporters.
func (c Config) CollectorCount() int { return 0 }

func (c Config) SafeSummary() map[string]any {
	return map[string]any{
		"metricsEnabled":  c.MetricsEnabled,
		"metricsEndpoint": c.MetricsEndpoint,
		"tracingEnabled":  c.TracingEnabled,
		"otlpEndpoint":    c.OTLPEndpoint,
		"otlpProtocol":    c.OTLPProtocol,
		"tlsVerify":       c.TLSVerify,
		"sampleRate":      c.SampleRate,
		"collectorCount":  c.CollectorCount(),
	}
}
