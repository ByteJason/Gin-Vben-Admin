package auditplatform

import (
	"context"
	"testing"

	"example.com/gin-vben-admin/server/internal/application/audit"
)

func TestGORMAuditRepositoryExposesPagePortWithoutDatabase(t *testing.T) {
	var _ audit.PageRepository = NewGORMRepository(nil)
	if _, _, err := NewGORMRepository(nil).QueryPage(context.Background(), audit.Filter{}); err == nil {
		t.Fatal("expected unavailable repository error")
	}
}
