package observabilityplatform

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestNewJSONLoggerWritesStableContextAndRedactionHelpers(t *testing.T) {
	var output bytes.Buffer
	logger := NewJSONLogger(&output, slog.LevelInfo, "api", "test")
	logger.Info("request", "traceId", "req-1", "password", Redact("TOKEN"))
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("JSON log error = %v", err)
	}
	if record["service"] != "api" || record["environment"] != "test" || record["traceId"] != "req-1" || record["password"] != "[REDACTED]" {
		t.Fatalf("record = %#v", record)
	}
	if !SensitiveKey("Authorization") || !SensitiveKey("otlp_api_key") || SensitiveKey("status") {
		t.Fatal("sensitive key classification mismatch")
	}
}
