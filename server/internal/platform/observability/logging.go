// Package observabilityplatform provides B6 structured logging primitives.
package observabilityplatform

import (
	"io"
	"log/slog"
	"strings"
)

func NewJSONLogger(writer io.Writer, level slog.Level, service, environment string) *slog.Logger {
	if writer == nil {
		writer = io.Discard
	}
	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})
	return slog.New(handler).With("service", service, "environment", environment)
}

func Redact(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	return "[REDACTED]"
}

func SensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, token := range []string{"password", "secret", "token", "api_key", "apikey", "authorization"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}
