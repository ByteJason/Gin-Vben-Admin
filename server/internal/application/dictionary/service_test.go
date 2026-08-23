package dictionary

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

func dictionaryContext(t *testing.T, tenantID, orgID string) context.Context {
	t.Helper()
	scope, err := tenant.NewContext(tenantID, orgID, false)
	if err != nil {
		t.Fatalf("tenant context: %v", err)
	}
	return WithActor(tenant.WithContext(context.Background(), scope), "admin-1")
}

func TestServiceMergesSystemItemsWithTenantOverridesAndBumpsVersion(t *testing.T) {
	repo := NewMemoryRepository()
	now := time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC)
	repo.SeedType(DictionaryType{ID: "sys-order", Code: "order.status", NameZhCN: "订单状态", NameEnUS: "Order status", SystemOwned: true, CreatedAt: now, UpdatedAt: now})
	repo.SeedItem(DictionaryItem{ID: "sys-paid", TypeCode: "order.status", Value: "paid", LabelZhCN: "已支付", LabelEnUS: "Paid", SystemOwned: true, SortOrder: 1, CreatedAt: now, UpdatedAt: now})
	repo.SeedItem(DictionaryItem{ID: "sys-pending", TypeCode: "order.status", Value: "pending", LabelZhCN: "待支付", LabelEnUS: "Pending", SystemOwned: true, SortOrder: 2, CreatedAt: now, UpdatedAt: now})
	audit := &MemoryAuditSink{}
	service := NewService(repo, audit)
	ctx := dictionaryContext(t, "tenant-a", "org-a")

	created, err := service.CreateItem(ctx, "order.status", ItemInput{Value: "paid", LabelZhCN: "付款完成", LabelEnUS: "Payment complete", Enabled: true, SortOrder: 1})
	if err != nil {
		t.Fatalf("CreateItem() error = %v", err)
	}
	if created.SystemOwned || created.TenantID != "tenant-a" {
		t.Fatalf("tenant override metadata = %+v", created)
	}
	items, err := service.ListItems(ctx, "order.status", ListOptions{Locale: "en-US"})
	if err != nil {
		t.Fatalf("ListItems() error = %v", err)
	}
	if len(items) != 2 || items[0].Label != "Payment complete" || items[0].Value != "paid" {
		t.Fatalf("effective localized items = %+v", items)
	}
	if items[0].CacheVersion == 0 || items[1].CacheVersion != items[0].CacheVersion {
		t.Fatalf("cache version not shared = %+v", items)
	}
	if len(audit.Events) != 1 || audit.Events[0].Action != "dictionary.item.create" {
		t.Fatalf("audit events = %+v", audit.Events)
	}
}

func TestServiceKeepsSystemDefinitionsReadOnlyAndBulkImportAtomic(t *testing.T) {
	repo := NewMemoryRepository()
	repo.SeedType(DictionaryType{ID: "sys-status", Code: "common.status", SystemOwned: true})
	service := NewService(repo, nil)
	ctx := dictionaryContext(t, "tenant-a", "")
	if _, err := service.UpdateType(ctx, "common.status", TypeInput{NameZhCN: "覆盖"}); !errors.Is(err, ErrSystemReadOnly) {
		t.Fatalf("UpdateType() error = %v, want ErrSystemReadOnly", err)
	}
	if _, err := service.ImportItems(ctx, "common.status", []ItemInput{{Value: "a", LabelZhCN: "A"}, {Value: "", LabelZhCN: "invalid"}}); !errors.Is(err, ErrInvalidItem) {
		t.Fatalf("ImportItems() error = %v, want ErrInvalidItem", err)
	}
	items, err := service.ListItems(ctx, "common.status", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("failed import partially wrote items: %+v", items)
	}
}
