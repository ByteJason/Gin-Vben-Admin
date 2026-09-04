package settingsplatform

import (
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
)

func TestStoredSettingMappingCopiesValueAndMetadata(t *testing.T) {
	now := time.Now().UTC()
	value := settings.StoredSetting{Key: "site.name", RawValue: []byte(`"APP"`), Version: 2, UpdatedBy: "admin", UpdatedAt: now}
	record := fromStored(value)
	value.RawValue[0] = 'X'
	got := toStored(record)
	if string(got.RawValue) != `"APP"` || got.Version != 2 || got.UpdatedBy != "admin" || !got.UpdatedAt.Equal(now) {
		t.Fatalf("mapping result = %+v", got)
	}
}

func TestNilRepositoryReturnsCredentialFreeError(t *testing.T) {
	_, err := (*GORMRepository)(nil).Current(nil, "secret")
	if err == nil || err.Error() != "settings repository is not initialized" {
		t.Fatalf("error = %v", err)
	}
}

func TestSettingScopePredicatesKeepOrganizationBoundaries(t *testing.T) {
	global, globalArgs := settingScopePredicate(tenant.Context{TenantID: "tenant-a"})
	if global != "tenant_id = ? AND org_id IS NULL" || len(globalArgs) != 1 || globalArgs[0] != "tenant-a" {
		t.Fatalf("tenant predicate = %q %#v", global, globalArgs)
	}
	org, orgArgs := settingScopePredicate(tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	if org != "tenant_id = ? AND (org_id = ? OR org_id IS NULL)" || len(orgArgs) != 2 || orgArgs[1] != "org-a" {
		t.Fatalf("organization predicate = %q %#v", org, orgArgs)
	}
	write, writeArgs := settingWritePredicate(tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	if write != "tenant_id = ? AND org_id = ?" || len(writeArgs) != 2 || writeArgs[1] != "org-a" {
		t.Fatalf("organization write predicate = %q %#v", write, writeArgs)
	}
}

func TestChooseModuleRowsPrefersOrganizationAndFallsBackAfterTombstone(t *testing.T) {
	org := "org-a"
	deletedAt := time.Now().UTC()
	rows := []model.SettingVersion{
		{Key: "basic.site_name", Version: 1, Value: model.JSONValue(`"tenant"`)},
		{Key: "basic.site_name", Version: 2, OrgID: &org, Value: model.JSONValue(`"org"`)},
	}
	selected, revision, _ := chooseModuleRows(rows, tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	if revision != 2 || string(selected["basic.site_name"].Value) != `"org"` {
		t.Fatalf("organization selection = %#v revision=%d", selected, revision)
	}
	rows[1].DeletedAt = &deletedAt
	selected, revision, _ = chooseModuleRows(rows, tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	if revision != 2 || string(selected["basic.site_name"].Value) != `"tenant"` {
		t.Fatalf("fallback selection = %#v revision=%d", selected, revision)
	}
}
