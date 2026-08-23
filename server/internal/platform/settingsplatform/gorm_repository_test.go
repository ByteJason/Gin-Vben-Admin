package settingsplatform

import (
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
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
