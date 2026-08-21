package settingsplatform

import (
	"context"
	"testing"

	settingsapp "example.com/gin-vben-admin/server/internal/application/settings"
)

func TestGORMSettingsAuditSinkExposesApplicationPort(t *testing.T) {
	var _ settingsapp.AuditSink = NewGORMAuditSink(nil)
	if err := NewGORMAuditSink(nil).Record(context.Background(), settingsapp.AuditEvent{ActorID: "1", Action: "update", Key: "site.name"}); err == nil {
		t.Fatal("expected unavailable sink error")
	}
}
