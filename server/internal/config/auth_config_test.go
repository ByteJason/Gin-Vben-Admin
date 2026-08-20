package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAuthConfigDefaultsAndValidation(t *testing.T) {
	cfg := Default()
	if cfg.Auth.Enabled {
		t.Fatal("auth must be disabled by default")
	}
	if cfg.Auth.AccessTTL <= 0 || cfg.Auth.RefreshTTL <= cfg.Auth.AccessTTL {
		t.Fatalf("invalid auth TTL defaults: %#v", cfg.Auth)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default().Validate() error = %v", err)
	}
	if err := (AuthConfig{}).validate(); err != nil {
		t.Fatalf("disabled auth with no policy should validate: %v", err)
	}

	cfg.Auth.Enabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabled auth without secret should fail validation")
	}
	cfg.Auth.JWTSecret = strings.Repeat("s", 32)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid enabled auth rejected: %v", err)
	}
}

func TestAuthConfigEnvironmentOverridesAndSafeSummary(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "true")
	t.Setenv("AUTH_JWT_SECRET", strings.Repeat("e", 32))
	t.Setenv("AUTH_ACCESS_TTL", "2m")
	t.Setenv("AUTH_REFRESH_TTL", "2h")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Auth.Enabled || cfg.Auth.AccessTTL != 2*time.Minute || cfg.Auth.RefreshTTL != 2*time.Hour {
		t.Fatalf("auth environment overrides not applied: %#v", cfg.Auth)
	}
	payload, err := json.Marshal(cfg.SafeSummary())
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if strings.Contains(string(payload), strings.Repeat("e", 32)) {
		t.Fatalf("auth secret leaked in summary: %s", payload)
	}
}
