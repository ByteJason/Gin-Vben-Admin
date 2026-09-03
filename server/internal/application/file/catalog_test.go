package file

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

func catalogContext() context.Context {
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	return auth.WithCapabilityMetadata(ctx, "media.logo", "zh-CN", "trace-media", "owner-a")
}

func TestCatalogAdapterSupportsReaderCursorAndControlledURLs(t *testing.T) {
	legacy := NewService(NewMemoryStore("http://memory.invalid/objects"), Config{AllowedMIMEs: []string{"image/png", "text/plain"}})
	catalog := NewCatalog(legacy)
	ctx := catalogContext()
	first, err := catalog.Upload(ctx, UploadInput{Reader: strings.NewReader("png-data"), Size: 8, Name: "logo.png", MIME: "image/png", ACL: ACLPrivate, IdempotencyKey: "logo-1"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != MediaReady || first.ScopeType != ScopeOrg || !first.Selectable || first.URLHints["preview"] == false {
		t.Fatalf("resource=%+v", first)
	}
	duplicate, err := catalog.Upload(ctx, UploadInput{Data: []byte("png-data"), Size: 8, Name: "logo.png", MIME: "image/png", ACL: ACLPrivate, IdempotencyKey: "logo-1"})
	if err != nil || duplicate.ID != first.ID {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
	page, err := catalog.List(ctx, MediaFilter{MIMEFamily: "image/*", Limit: 1})
	if err != nil || len(page.Items) != 1 || page.HasMore {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	reader, err := catalog.Open(ctx, first.ID, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(data) != "png-data" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	urlRef, err := catalog.SignedURL(ctx, first.ID, URLRequest{Purpose: URLPurpose("preview"), TTL: 60})
	if err != nil || urlRef.URL == "" || urlRef.ExpiresAt.IsZero() {
		t.Fatalf("url=%+v err=%v", urlRef, err)
	}
	if _, err := catalog.SignedURL(ctx, first.ID, URLRequest{TTL: time.Minute}); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("missing purpose error=%v", err)
	}
	if _, err := catalog.SignedURL(ctx, first.ID, URLRequest{Purpose: URLPurposeDownload, TTL: maxMediaURLTTL + time.Second}); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("long TTL error=%v", err)
	}
}

func TestCatalogListUsesIDAsDeterministicTimestampTieBreaker(t *testing.T) {
	clock := func() time.Time { return time.Unix(1700000000, 0).UTC() }
	legacy := NewService(NewMemoryStore("http://memory.invalid/objects"), Config{AllowedMIMEs: []string{"text/plain"}, Clock: clock})
	catalog := NewCatalog(legacy)
	ctx := catalogContext()
	first, err := catalog.Upload(ctx, UploadInput{Data: []byte("a"), Size: 1, Name: "a.txt", MIME: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := catalog.Upload(ctx, UploadInput{Data: []byte("b"), Size: 1, Name: "b.txt", MIME: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := catalog.List(ctx, MediaFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items=%+v", page.Items)
	}
	wantFirst, wantSecond := second.ID, first.ID
	if wantSecond > wantFirst {
		wantFirst, wantSecond = wantSecond, wantFirst
	}
	if page.Items[0].ID != wantFirst || page.Items[1].ID != wantSecond {
		t.Fatalf("tie order=%v want=[%s %s]", []string{page.Items[0].ID, page.Items[1].ID}, wantFirst, wantSecond)
	}
}

func TestCatalogListHonorsLegacyOffsetButCursorWins(t *testing.T) {
	clock := func() time.Time { return time.Unix(1700000000, 0).UTC() }
	legacy := NewService(NewMemoryStore("http://memory.invalid/objects"), Config{AllowedMIMEs: []string{"text/plain"}, Clock: clock})
	catalog := NewCatalog(legacy)
	ctx := catalogContext()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := catalog.Upload(ctx, UploadInput{Data: []byte(name), Size: int64(len(name)), Name: name, MIME: "text/plain"}); err != nil {
			t.Fatal(err)
		}
	}
	offsetPage, err := catalog.List(ctx, MediaFilter{Offset: 1, Limit: 1})
	if err != nil || offsetPage.Offset != 1 || len(offsetPage.Items) != 1 {
		t.Fatalf("offset page=%+v err=%v", offsetPage, err)
	}
	cursorPage, err := catalog.List(ctx, MediaFilter{Offset: 0, Cursor: encodeCursor(2), Limit: 1})
	if err != nil || cursorPage.Offset != 2 || len(cursorPage.Items) != 1 {
		t.Fatalf("cursor precedence page=%+v err=%v", cursorPage, err)
	}
}

func TestCatalogUsageProtectsReferencedResourceAndSupportsIdempotentAttach(t *testing.T) {
	legacy := NewService(NewMemoryStore("http://memory.invalid/objects"), Config{AllowedMIMEs: []string{"image/png"}})
	catalog := NewCatalog(legacy)
	ctx := catalogContext()
	resource, err := catalog.Upload(ctx, UploadInput{Data: []byte("x"), Size: 1, Name: "logo.png", MIME: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := catalog.Attach(ctx, UsageInput{ResourceID: resource.ID, Module: "branding", EntityType: "site", EntityID: "site-a", Field: "logo", IdempotencyKey: "usage-1"})
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := catalog.Attach(ctx, UsageInput{ResourceID: resource.ID, Module: "branding", EntityType: "site", EntityID: "site-a", Field: "logo", IdempotencyKey: "usage-1"})
	if err != nil || duplicate.ID != usage.ID {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
	if err := catalog.Delete(ctx, resource.ID, DeleteOptions{Reason: "replace"}); !errors.Is(err, ErrMediaInUse) {
		t.Fatalf("delete referenced error=%v", err)
	}
	if err := catalog.Detach(ctx, DetachRequest{UsageID: usage.ID}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Delete(ctx, resource.ID, DeleteOptions{Reason: "replace"}); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogRejectsCrossScopeAndNonImageSelection(t *testing.T) {
	legacy := NewService(NewMemoryStore("http://memory.invalid/objects"), Config{AllowedMIMEs: []string{"text/plain"}})
	catalog := NewCatalog(legacy)
	ctx := catalogContext()
	textFile, err := catalog.Upload(ctx, UploadInput{Data: []byte("text"), Size: 4, Name: "readme.txt", MIME: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	if textFile.Selectable || textFile.DisabledReason != "media_type_not_allowed" {
		t.Fatalf("non-image resource=%+v", textFile)
	}
	other := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-b", Organization: "org-b"})
	if _, err := catalog.Get(other, textFile.ID); !errors.Is(err, ErrAccessDenied) && !errors.Is(err, tenant.ErrCrossTenant) {
		t.Fatalf("cross-scope get error=%v", err)
	}
}

func TestCatalogListHidesAnotherPrincipalsPrivateResource(t *testing.T) {
	legacy := NewService(NewMemoryStore("http://memory.invalid/objects"), Config{AllowedMIMEs: []string{"image/png"}})
	catalog := NewCatalog(legacy)
	ownerCtx := auth.WithCapabilityMetadata(
		tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"}),
		"media.logo", "zh-CN", "trace-owner", "owner-a",
	)
	private, err := catalog.Upload(ownerCtx, UploadInput{Data: []byte("private"), Size: 7, Name: "private.png", MIME: "image/png", ACL: ACLPrivate})
	if err != nil {
		t.Fatal(err)
	}
	otherCtx := auth.WithCapabilityMetadata(
		tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"}),
		"media.logo", "zh-CN", "trace-other", "owner-b",
	)
	page, err := catalog.List(otherCtx, MediaFilter{MIMEFamily: "image/*", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.ID == private.ID {
			t.Fatalf("private resource leaked in list: %+v", item)
		}
	}
}

func TestCatalogListInheritsOrgTenantSystemAndCategoryDescendants(t *testing.T) {
	legacy := NewService(NewMemoryStore("http://memory.invalid/objects"), Config{AllowedMIMEs: []string{"image/png"}})
	catalog := NewCatalog(legacy)
	ctx := catalogContext()
	root, err := legacy.CreateCategory(ctx, CategoryInput{Name: "Brand"}, "tenant-a", "org-a")
	if err != nil {
		t.Fatal(err)
	}
	child, err := legacy.CreateCategory(ctx, CategoryInput{Name: "Logos", ParentID: root.ID}, "tenant-a", "org-a")
	if err != nil {
		t.Fatal(err)
	}
	orgFile, err := catalog.Upload(ctx, UploadInput{Data: []byte("org"), Size: 3, Name: "org.png", MIME: "image/png", CategoryID: child.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Tenant and system resources are created by the provider-facing service in
	// this fixture; the catalog must still expose them through inheritance.
	tenantFile, err := legacy.Upload(ctx, UploadInput{Data: []byte("tenant"), Size: 6, Name: "tenant.png", MIME: "image/png", TenantID: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	systemFile, err := legacy.Upload(ctx, UploadInput{Data: []byte("system"), Size: 6, Name: "system.png", MIME: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := catalog.List(ctx, MediaFilter{MIMEFamily: "image/*", CategoryID: root.ID, IncludeDescendants: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != orgFile.ID {
		t.Fatalf("category descendants page=%+v", page)
	}
	all, err := catalog.List(ctx, MediaFilter{MIMEFamily: "image/*", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, item := range all.Items {
		ids[item.ID] = true
	}
	if !ids[orgFile.ID] || !ids[tenantFile.ID] || !ids[systemFile.ID] {
		t.Fatalf("inherited resources=%v", ids)
	}
}

func TestCatalogDeleteIdempotencyConflict(t *testing.T) {
	legacy := NewService(NewMemoryStore("http://memory.invalid/objects"), Config{AllowedMIMEs: []string{"image/png"}})
	catalog := NewCatalog(legacy)
	ctx := catalogContext()
	resource, err := catalog.Upload(ctx, UploadInput{Data: []byte("x"), Size: 1, Name: "x.png", MIME: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Delete(ctx, resource.ID, DeleteOptions{Reason: "replace", IdempotencyKey: "delete-1"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Delete(ctx, resource.ID, DeleteOptions{Reason: "replace", IdempotencyKey: "delete-1"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Delete(ctx, resource.ID, DeleteOptions{Reason: "other", IdempotencyKey: "delete-1"}); !errors.Is(err, ErrMediaConflict) {
		t.Fatalf("conflict error=%v", err)
	}
}

func TestCatalogReconcilesBundledPresetIdempotently(t *testing.T) {
	legacy := NewService(NewMemoryStore("http://memory.invalid/objects"), Config{AllowedMIMEs: []string{"image/svg+xml"}})
	catalog := NewCatalog(legacy)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", PlatformAdmin: true})
	asset := DefaultPresetAsset()
	first, err := catalog.ReconcilePreset(ctx, asset)
	if err != nil {
		t.Fatal(err)
	}
	second, err := catalog.ReconcilePreset(ctx, asset)
	if err != nil || first.ID != second.ID || second.ReconcileKey != asset.Key {
		t.Fatalf("first=%+v second=%+v err=%v", first, second, err)
	}
	if _, err := catalog.ReconcilePreset(catalogContext(), asset); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("tenant reconcile error=%v", err)
	}
}

func TestCatalogCategoryMutationsAreIdempotentAndExposePaths(t *testing.T) {
	legacy := NewService(NewMemoryStore("http://memory.invalid/objects"), Config{AllowedMIMEs: []string{"image/png"}})
	catalog := NewCatalog(legacy)
	ctx := catalogContext()
	root, err := catalog.CreateCategory(ctx, CategoryInput{Name: "Brand", IdempotencyKey: "category-create"})
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := catalog.CreateCategory(ctx, CategoryInput{Name: "Brand", IdempotencyKey: "category-create"})
	if err != nil || duplicate.ID != root.ID {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
	name := "Logos"
	child, err := catalog.CreateCategory(ctx, CategoryInput{Name: name, ParentID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	updatedName := "Brand assets"
	updated, err := catalog.UpdateCategory(ctx, root.ID, CategoryPatch{Name: &updatedName, IdempotencyKey: "category-update"})
	if err != nil || updated.Path != "Brand assets" {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	updatedAgain, err := catalog.UpdateCategory(ctx, root.ID, CategoryPatch{Name: &updatedName, IdempotencyKey: "category-update"})
	if err != nil || updatedAgain.ID != updated.ID {
		t.Fatalf("updatedAgain=%+v err=%v", updatedAgain, err)
	}
	items, err := catalog.ListCategories(ctx, CategoryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var childRef CategoryRef
	for _, item := range items {
		if item.ID == child.ID {
			childRef = item
		}
	}
	if childRef.Path != "Brand assets/Logos" || !childRef.Enabled {
		t.Fatalf("child ref=%+v", childRef)
	}
	if err := catalog.DeleteCategory(ctx, CategoryDeleteRequest{ID: child.ID, IdempotencyKey: "category-delete"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.DeleteCategory(ctx, CategoryDeleteRequest{ID: child.ID, IdempotencyKey: "category-delete"}); err != nil {
		t.Fatalf("idempotent delete=%v", err)
	}
}

func TestCatalogPlatformAdminCanTargetSystemAndTenantCategories(t *testing.T) {
	legacy := NewService(NewMemoryStore("http://memory.invalid/objects"), Config{AllowedMIMEs: []string{"image/png"}})
	catalog := NewCatalog(legacy)
	adminCtx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", PlatformAdmin: true})
	system, err := catalog.CreateCategory(adminCtx, CategoryInput{Name: "System"})
	if err != nil || system.ScopeType != ScopeSystem {
		t.Fatalf("system=%+v err=%v", system, err)
	}
	tenantCategory, err := catalog.CreateCategory(adminCtx, CategoryInput{Name: "Tenant", TenantID: "tenant-b"})
	if err != nil || tenantCategory.ScopeType != ScopeTenant {
		t.Fatalf("tenant=%+v err=%v", tenantCategory, err)
	}
	name := "Tenant renamed"
	updated, err := catalog.UpdateCategory(adminCtx, tenantCategory.ID, CategoryPatch{Name: &name})
	if err != nil || updated.Name != name {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	if err := catalog.DeleteCategory(adminCtx, CategoryDeleteRequest{ID: system.ID}); err != nil {
		t.Fatalf("delete system=%v", err)
	}
}

func TestCatalogPlatformAdminCanInspectPrivateTenantResourceButCannotDeleteSystemPreset(t *testing.T) {
	legacy := NewService(NewMemoryStore("http://memory.invalid/objects"), Config{AllowedMIMEs: []string{"image/png", "image/svg+xml"}})
	catalog := NewCatalog(legacy)
	ownerCtx := auth.WithCapabilityMetadata(
		tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"}),
		"media.logo", "zh-CN", "trace-owner", "owner-a",
	)
	privateResource, err := catalog.Upload(ownerCtx, UploadInput{Data: []byte("private"), Size: 7, Name: "private.png", MIME: "image/png", ACL: ACLPrivate})
	if err != nil {
		t.Fatal(err)
	}
	adminCtx := auth.WithCapabilityMetadata(
		tenant.WithContext(context.Background(), tenant.Context{TenantID: "platform", PlatformAdmin: true}),
		"system.admin", "zh-CN", "trace-admin", "admin-a",
	)
	if _, err := catalog.Get(adminCtx, privateResource.ID); err != nil {
		t.Fatalf("platform admin get private tenant resource: %v", err)
	}
	reader, err := catalog.Open(adminCtx, privateResource.ID, OpenOptions{})
	if err != nil {
		t.Fatalf("platform admin open private tenant resource: %v", err)
	}
	_ = reader.Close()
	preset, err := catalog.ReconcilePreset(adminCtx, DefaultPresetAsset())
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Delete(adminCtx, preset.ID, DeleteOptions{Reason: "remove preset"}); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("system preset delete error=%v", err)
	}
}
