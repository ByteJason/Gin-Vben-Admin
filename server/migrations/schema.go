// Package migrations contains the single fresh-install GORM schema.
//
// Every table shape is owned by the persistence model package. This package
// keeps one ordered registry and the fresh-install seed/rollback workflow.
// Relationship IDs are plain scalar fields; relation descriptors used by
// queries are deliberately kept outside the migration registry.
package migrations

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ByteJason/Gin-Vben-Admin/server/global"
	persistencemodel "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Compatibility aliases keep existing migration callers source-compatible
// while making persistence/model the single definition owner.
type JSONValue = persistencemodel.JSONValue
type BinaryValue = persistencemodel.BinaryValue
type JSONData = persistencemodel.JSONData
type BinaryData = persistencemodel.BinaryData
type AppMetadata = persistencemodel.AppMetadata
type Tenant = persistencemodel.Tenant
type Organization = persistencemodel.Organization
type User = persistencemodel.User
type Role = persistencemodel.Role
type UserRole = persistencemodel.UserRole
type Menu = persistencemodel.Menu
type Permission = persistencemodel.Permission
type IAMPolicy = persistencemodel.IAMPolicy
type IAMDataScope = persistencemodel.IAMDataScope
type AuthSession = persistencemodel.AuthSession
type AuthAuditEvent = persistencemodel.AuthAuditEvent
type SettingVersion = persistencemodel.SettingVersion
type FileObject = persistencemodel.FileObject
type SMTPAccount = persistencemodel.SMTPAccount
type EmailMessage = persistencemodel.EmailMessage
type EmailRecipient = persistencemodel.EmailRecipient
type EmailDeliveryAttempt = persistencemodel.EmailDeliveryAttempt
type DictionaryType = persistencemodel.DictionaryType
type DictionaryItem = persistencemodel.DictionaryItem
type DictionaryCacheVersion = persistencemodel.DictionaryCacheVersion
type TaskDefinition = persistencemodel.TaskDefinition
type TaskRun = persistencemodel.TaskRun
type TaskRunLog = persistencemodel.TaskRunLog
type ImportExportJob = persistencemodel.ImportExportJob
type ImportExportError = persistencemodel.ImportExportError
type ImportExportArtifact = persistencemodel.ImportExportArtifact
type ModelModule = persistencemodel.Module
type Relation = persistencemodel.Relation
type RelationKind = persistencemodel.RelationKind

const (
	ModuleShared = persistencemodel.ModuleShared
	ModuleAuth   = persistencemodel.ModuleAuth
	ModuleAdmin  = persistencemodel.ModuleAdmin
	ModuleAudit  = persistencemodel.ModuleAudit
	ModuleClient = persistencemodel.ModuleClient
)

// schemaModels is intentionally flat: scalar *_id columns describe business
// relationships without GORM association metadata (and therefore without
// implicit foreign-key constraints or indexes). Any indexes on these columns
// are explicit query/tenant indexes, never indexes generated for a foreign key.
func schemaModels() []any {
	return persistencemodel.All()
}

// Models returns fresh persistence models in deterministic creation order.
// The values contain no associations and are safe to pass to a GORM Migrator.
func Models() []any { return schemaModels() }

// ModelDefinitions exposes module ownership for tooling and future versioned
// migrations while keeping the initial CreateTable registry in one file.
func ModelDefinitions() []persistencemodel.Definition {
	return persistencemodel.Definitions()
}

// ModelsFor returns the fresh models owned by one module. The initial install
// uses Models so all modules are created in one transaction; this view is for
// future module-scoped upgrade planning and tooling.
func ModelsFor(module persistencemodel.Module) []any {
	return persistencemodel.ModelsFor(module)
}

// Relations returns the application-level relationship catalog. It is metadata
// for query/read models and is never passed to a GORM Migrator.
func Relations() []persistencemodel.Relation { return persistencemodel.Relations() }

var tableComments = persistencemodel.TableComments()

// TableComments returns a copy of the table comment registry for migration
// tooling and schema documentation.
func TableComments() map[string]string { return persistencemodel.TableComments() }

// TableComment returns the short comment associated with one schema table.
func TableComment(table string) string { return persistencemodel.TableComment(table) }

// TableNames exposes the canonical table list for contract checks and tooling.
func TableNames() []string {
	models := schemaModels()
	result := make([]string, 0, len(models))
	for _, model := range models {
		if named, ok := model.(interface{ TableName() string }); ok {
			result = append(result, named.TableName())
		}
	}
	return result
}

// CreateSchema creates every missing table in one fresh-install operation and
// inserts the small set of deterministic system rows. Existing tables are
// never altered, so rerunning the installer does not issue ADD/ALTER calls.
func CreateSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("migration database is not initialized")
	}
	if db.Dialector == nil {
		return errors.New("migration database dialect is not initialized")
	}

	// Keep the process-wide compatibility handle synchronized even when callers
	// pass a directly opened GORM database rather than a gormdb.Store.
	global.SetDatabase(db, db.Dialector.Name())

	// Clone the handle so the no-FK policy is local to schema work while still
	// sharing the same connection pool and resolver state.
	db = db.Session(&gorm.Session{})
	db.DisableForeignKeyConstraintWhenMigrating = true
	return db.Transaction(func(tx *gorm.DB) error {
		tx.DisableForeignKeyConstraintWhenMigrating = true
		for _, model := range schemaModels() {
			if tx.Migrator().HasTable(model) {
				continue
			}
			if err := createTable(tx, model); err != nil {
				return err
			}
		}
		if err := seedSchema(tx); err != nil {
			return err
		}
		return nil
	})
}

// Migrate is a concise alias used by callers that prefer migration terminology.
func Migrate(db *gorm.DB) error { return CreateSchema(db) }

// DropSchema removes tables created by CreateSchema in reverse order. It is
// intended for local fresh-install rollback and does not inspect or mutate
// unrelated tables.
func DropSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("migration database is not initialized")
	}
	db = db.Session(&gorm.Session{})
	db.DisableForeignKeyConstraintWhenMigrating = true
	models := schemaModels()
	for left, right := 0, len(models)-1; left < right; left, right = left+1, right-1 {
		models[left], models[right] = models[right], models[left]
	}
	return db.Migrator().DropTable(models...)
}

// SchemaStatus reports whether every table in the single schema exists.
// GetTables is used instead of HasTable so a database connection error is
// returned to the caller and a partially completed MySQL DDL run is not
// reported as fully applied.
func SchemaStatus(db *gorm.DB) (bool, error) {
	if db == nil {
		return false, errors.New("migration database is not initialized")
	}
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return false, err
	}
	present := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		present[strings.ToLower(table)] = struct{}{}
	}
	for _, table := range TableNames() {
		if _, ok := present[strings.ToLower(table)]; !ok {
			return false, nil
		}
	}
	return true, nil
}

func modelTableName(model any) string {
	if named, ok := model.(interface{ TableName() string }); ok {
		return named.TableName()
	}
	return ""
}

func createTable(tx *gorm.DB, model any) error {
	table := modelTableName(model)
	createDB := tx
	if tx.Dialector.Name() == "mysql" {
		options := " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"
		if comment := tableComments[table]; comment != "" {
			options += " COMMENT='" + escapeSQLLiteral(comment) + "'"
		}
		createDB = tx.Set("gorm:table_options", options)
	}
	if err := createDB.Migrator().CreateTable(model); err != nil {
		return fmt.Errorf("create table %s: %w", table, err)
	}
	if tx.Dialector.Name() == "postgres" {
		if comment := tableComments[table]; comment != "" {
			if err := commentPostgresTable(tx, table, comment); err != nil {
				return fmt.Errorf("comment table %s: %w", table, err)
			}
		}
	}
	return nil
}

func commentPostgresTable(tx *gorm.DB, table, comment string) error {
	statement := "COMMENT ON TABLE ? IS '" + escapeSQLLiteral(comment) + "'"
	return tx.Exec(statement, clause.Table{Name: table}).Error
}

func escapeSQLLiteral(value string) string { return strings.ReplaceAll(value, "'", "''") }

func seedSchema(tx *gorm.DB) error {
	if err := insertSeed(tx, &AppMetadata{
		MetadataKey:   "product",
		MetadataValue: JSONValue(`{"name":"gin-vben-admin"}`),
		Version:       1,
	}); err != nil {
		return fmt.Errorf("seed app metadata: %w", err)
	}
	if err := insertSeed(tx, &Tenant{ID: "default", Name: "Default tenant", Status: "active"}); err != nil {
		return fmt.Errorf("seed default tenant: %w", err)
	}
	if err := insertSeed(tx, &Organization{ID: "default-org", TenantID: "default", Name: "Default organization", Status: "active"}); err != nil {
		return fmt.Errorf("seed default organization: %w", err)
	}
	if err := insertSeed(tx, &DictionaryType{
		ID: "system-common-status", TenantID: "", OrgID: "", Code: "common.status", NameZhCN: "通用状态", NameEnUS: "Common status",
		Description: "系统预置状态字典", Status: "active", SortOrder: 0, SystemOwned: true,
	}); err != nil {
		return fmt.Errorf("seed dictionary type: %w", err)
	}
	for _, item := range []DictionaryItem{
		{ID: "system-common-status-active", TenantID: "", OrgID: "", TypeCode: "common.status", Value: "active", LabelZhCN: "启用", LabelEnUS: "Active", Status: "active", SortOrder: 1, SystemOwned: true},
		{ID: "system-common-status-disabled", TenantID: "", OrgID: "", TypeCode: "common.status", Value: "disabled", LabelZhCN: "停用", LabelEnUS: "Disabled", Status: "active", SortOrder: 2, SystemOwned: true},
	} {
		if err := insertSeed(tx, &item); err != nil {
			return fmt.Errorf("seed dictionary item %s: %w", item.ID, err)
		}
	}
	if err := insertSeed(tx, &DictionaryCacheVersion{ID: "system-common-status-cache", TenantID: "", OrgID: "", TypeCode: "common.status", Version: 1}); err != nil {
		return fmt.Errorf("seed dictionary cache version: %w", err)
	}
	return nil
}

// insertSeed keeps deterministic bootstrap rows on the same typed GORM
// generics path as the runtime repositories.  The conflict clause makes the
// fresh-install seed idempotent without issuing schema alterations.
func insertSeed[T any](tx *gorm.DB, value *T) error {
	if tx == nil {
		return errors.New("migration database is not initialized")
	}
	return gorm.G[T](tx, clause.OnConflict{DoNothing: true}).Create(seedContext(tx), value)
}

func seedContext(tx *gorm.DB) context.Context {
	if tx != nil && tx.Statement != nil && tx.Statement.Context != nil {
		return tx.Statement.Context
	}
	return context.Background()
}
