package auditplatform

import (
	"testing"

	"example.com/gin-vben-admin/server/internal/application/audit"
)

func TestLoginFilterUsesPersistedAuthEventType(t *testing.T) {
	if got := persistedEventType(audit.Filter{Action: "login", Resource: "auth"}); got != "auth.login" {
		t.Fatalf("persistedEventType() = %q, want auth.login", got)
	}
}
