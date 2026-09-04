package admin

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	persistencemodel "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"github.com/DATA-DOG/go-sqlmock"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestCommonCapabilitiesVersionAndModels(t *testing.T) {
	if Version != "v002_common_capabilities" {
		t.Fatalf("version = %q", Version)
	}
	want := map[string]bool{
		"media_categories": true, "media_usages": true,
		"notification_callers": true, "notification_caller_accounts": true,
		"notification_templates": true, "notification_template_locales": true,
		"notification_template_versions": true, "verification_policies": true,
		"verification_challenges": true,
	}
	for _, value := range newModels {
		parsed, err := schema.Parse(value, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse %T: %v", value, err)
		}
		if !want[parsed.Table] {
			t.Fatalf("unexpected migration table %q", parsed.Table)
		}
		delete(want, parsed.Table)
		for _, field := range parsed.Fields {
			if strings.TrimSpace(field.TagSettings["COMMENT"]) == "" {
				t.Fatalf("%s.%s has no comment", parsed.Table, field.DBName)
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing migration tables: %v", want)
	}
}

func TestRetiredSettingsPermissionAllowListDoesNotTouchIndependentMailAccess(t *testing.T) {
	for _, id := range retiredSettingsPermissionIDs {
		if id == "system:mail:read" || id == "system:mail:manage" || strings.HasPrefix(id, "notification:") {
			t.Fatalf("cleanup allow-list crosses independent mail boundary: %q", id)
		}
	}
	for _, retained := range []string{"system:mail:read", "system:mail:manage", "system:mail:test", "notification:accounts:manage"} {
		for _, id := range retiredSettingsPermissionIDs {
			if id == retained {
				t.Fatalf("independent permission %q is scheduled for cleanup", retained)
			}
		}
	}
}

func TestCommonCapabilitiesMigrationRejectsNilDatabase(t *testing.T) {
	if err := Up(nil); err == nil {
		t.Fatal("Up(nil) returned nil")
	}
	if err := Down(nil); err == nil {
		t.Fatal("Down(nil) returned nil")
	}
}

func TestMetadataCompatibilityColumnStartsNullableForExistingRows(t *testing.T) {
	parsed, err := schema.Parse(&metadataColumn{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse metadata compatibility model: %v", err)
	}
	field := parsed.FieldsByDBName["metadata_json"]
	if field == nil {
		t.Fatal("metadata compatibility model is missing metadata_json")
	}
	if field.NotNull {
		t.Fatal("metadata compatibility column must be nullable before backfill")
	}
}

func TestAdditiveColumnsExistOnTheirCanonicalModels(t *testing.T) {
	for _, set := range fileObjectColumns {
		parsed, err := schema.Parse(set.model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse %T: %v", set.model, err)
		}
		for _, column := range set.columns {
			if parsed.FieldsByDBName[column] == nil {
				t.Fatalf("table %s migration column %s is missing from model", parsed.Table, column)
			}
		}
	}
}

func TestAdditiveNonNullColumnsHaveCompatibilityDefaults(t *testing.T) {
	for _, set := range fileObjectColumns {
		parsed, err := schema.Parse(set.model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse %T: %v", set.model, err)
		}
		for _, column := range set.columns {
			field := parsed.FieldsByDBName[column]
			if field == nil || !field.NotNull || (parsed.Table == "file_objects" && column == "metadata_json") {
				continue
			}
			if !field.HasDefaultValue && field.DefaultValue == "" && field.DefaultValueInterface == nil {
				t.Fatalf("table %s non-null compatibility column %s has no default", parsed.Table, column)
			}
		}
	}
}

func TestMigrationTableNameErrorsUseCanonicalNames(t *testing.T) {
	for _, model := range []any{&persistencemodel.FileObject{}, &persistencemodel.SMTPAccount{}, &persistencemodel.EmailMessage{}} {
		if got := tableName(model); got == "" || strings.HasPrefix(got, "*") {
			t.Fatalf("tableName(%T) = %q", model, got)
		}
	}
}

func TestLegacySchemaPreconditionReportsAllMissingTables(t *testing.T) {
	// Keep this contract test independent of a live database; requireLegacyTables
	// obtains the same canonical names from legacyModels after reading GetTables.
	want := []string{"file_objects", "smtp_accounts", "email_messages"}
	if got := missingLegacyTables(nil); strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Fatalf("missing legacy tables = %v, want %v", got, want)
	}
	present := map[string]struct{}{"file_objects": {}, "SMTP_ACCOUNTS": {}}
	got := missingLegacyTables(present)
	if strings.Join(got, ", ") != "email_messages" {
		t.Fatalf("partial legacy schema missing = %v, want [email_messages]", got)
	}
}

func TestUpRejectsMissingLegacyTableBeforeIssuingDDL(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT table_name FROM information_schema.tables WHERE table_schema = CURRENT_SCHEMA\(\) AND table_type = \$1`).
		WithArgs("BASE TABLE").
		WillReturnRows(sqlmock.NewRows([]string{"table_name"}).AddRow("file_objects").AddRow("smtp_accounts"))
	mock.ExpectRollback()
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	got := Up(db)
	if got == nil || !strings.Contains(got.Error(), "email_messages") {
		t.Fatalf("Up() error = %v, want missing email_messages", got)
	}
	if errors.Is(got, sql.ErrNoRows) {
		t.Fatalf("Up() leaked driver row error instead of migration precondition: %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("preflight executed unexpected SQL: %v", err)
	}
}

func TestBackfillLegacyRelayStatusRepairsTerminalRows(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	// Unscoped is intentional: soft-deleted messages are still part of the
	// audit/outbox history and must not be left with the synthetic pending
	// default after the compatibility column is introduced.
	for _, status := range []string{"sent", "failed"} {
		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "email_messages" SET "relay_status"=\$1 WHERE status = \$2 AND \(relay_status IS NULL OR relay_status = \$3\)`).
			WithArgs(status, status, "pending").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
	}
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	if err := backfillLegacyRelayStatus(db); err != nil {
		t.Fatalf("backfillLegacyRelayStatus() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("relay status backfill SQL mismatch: %v", err)
	}
}

func TestExistingIndexesPropagatesCatalogErrors(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	wantErr := errors.New("index catalog unavailable")
	mock.ExpectQuery(`SELECT`).WithArgs("email_messages").WillReturnError(wantErr)
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	_, got := existingIndexes(db, &persistencemodel.EmailMessage{})
	if got == nil || !errors.Is(got, wantErr) || !strings.Contains(got.Error(), "email_messages") {
		t.Fatalf("existingIndexes() error = %v, want wrapped catalog error", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("index catalog expectations: %v", err)
	}
}
