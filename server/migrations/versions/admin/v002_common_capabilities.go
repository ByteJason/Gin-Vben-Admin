// Package admin contains explicit, reviewable admin schema upgrades.
package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	persistencemodel "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Version identifies the explicit common-capabilities upgrade. It remains
// outside application startup; the migration runner/CLI must invoke Up/Down
// as part of a reviewed versioned upgrade rather than relying on AutoMigrate.
const Version = "v002_common_capabilities"

var newModels = []any{
	&persistencemodel.MediaCategory{}, &persistencemodel.MediaUsage{},
	&persistencemodel.NotificationCaller{}, &persistencemodel.NotificationCallerAccount{},
	&persistencemodel.NotificationTemplate{}, &persistencemodel.NotificationTemplateLocale{},
	&persistencemodel.NotificationTemplateVersion{}, &persistencemodel.VerificationPolicy{},
	&persistencemodel.VerificationChallenge{},
}

// additiveColumnSet describes columns that were added to the v001 file/mail
// tables after the common-capabilities models were introduced. The final
// models remain the source of truth; these sets only make the old-table
// upgrade explicit and reviewable.
type additiveColumnSet struct {
	model   any
	columns []string
}

var fileObjectColumns = []additiveColumnSet{
	{model: &persistencemodel.FileObject{}, columns: []string{
		"scope_type", "category_id", "provider_id", "lifecycle_status", "metadata_json",
		"original_extension", "detected_mime", "reconcile_key", "pending_at", "ready_at",
	}},
	{model: &persistencemodel.SMTPAccount{}, columns: []string{"scope_type"}},
	{model: &persistencemodel.EmailMessage{}, columns: []string{
		"scope_type", "caller_key", "template_key", "template_generation", "policy_generation",
		"locale", "is_test", "challenge_id", "relay_status", "idempotency_scope_hash",
	}},
}

// legacyModels is the v001 baseline that this version extends. Keeping the
// list explicit prevents an empty or partially-installed database from being
// mistaken for an already-upgraded database when HasTable checks below would
// otherwise silently skip every compatibility column.
var legacyModels = []any{
	&persistencemodel.FileObject{},
	&persistencemodel.SMTPAccount{},
	&persistencemodel.EmailMessage{},
}

// metadataColumn is deliberately nullable while it is introduced. Existing
// v001 file rows have no value for the column, so adding the final NOT NULL
// shape in one DDL statement would fail on PostgreSQL and strict MySQL.
type metadataColumn struct {
	MetadataJSON *persistencemodel.JSONValue `gorm:"column:metadata_json;comment:媒体元数据"`
}

func (metadataColumn) TableName() string { return "file_objects" }

type indexSpec struct {
	model any
	name  string
}

// These indexes contain at least one v002 column and therefore need to be
// created when upgrading an existing v001 table. Scope indexes that already
// existed in v001 are intentionally omitted so Down never removes a
// pre-existing index. Index discovery uses GetIndexes (rather than HasIndex),
// because the latter intentionally suppresses catalog query errors.
var additiveIndexes = []indexSpec{
	{model: &persistencemodel.FileObject{}, name: "idx_file_objects_scope_category_created"},
	{model: &persistencemodel.FileObject{}, name: "idx_file_objects_scope_mime_created"},
	{model: &persistencemodel.FileObject{}, name: "idx_file_objects_scope_owner_created"},
	{model: &persistencemodel.FileObject{}, name: "idx_file_objects_status_created"},
	{model: &persistencemodel.FileObject{}, name: "uq_file_objects_reconcile_key"},
	{model: &persistencemodel.EmailMessage{}, name: "idx_email_messages_caller"},
	{model: &persistencemodel.EmailMessage{}, name: "idx_email_messages_template"},
	{model: &persistencemodel.EmailMessage{}, name: "idx_email_messages_test"},
	{model: &persistencemodel.EmailMessage{}, name: "idx_email_messages_challenge"},
	{model: &persistencemodel.EmailMessage{}, name: "idx_email_messages_relay"},
	{model: &persistencemodel.EmailMessage{}, name: "uq_email_messages_idempotency_scope"},
}

// Up applies the additive common-capabilities schema. Every operation is
// idempotent; no AutoMigrate or startup registration is used.
func Up(db *gorm.DB) error {
	if db == nil || db.Dialector == nil {
		return errors.New("common capabilities migration database is not initialized")
	}
	if err := validateDialect(db); err != nil {
		return err
	}
	db = db.Session(&gorm.Session{NewDB: true})
	db.DisableForeignKeyConstraintWhenMigrating = true
	return db.Transaction(func(tx *gorm.DB) error {
		tx.DisableForeignKeyConstraintWhenMigrating = true
		existing, err := existingTables(tx)
		if err != nil {
			return err
		}
		if err := validateLegacyTables(existing); err != nil {
			return err
		}
		for _, model := range newModels {
			if hasTable(existing, model) {
				continue
			}
			if err := createCapabilityTable(tx, model); err != nil {
				return err
			}
			existing[strings.ToLower(tableName(model))] = struct{}{}
		}
		for _, set := range fileObjectColumns {
			if !hasTable(existing, set.model) {
				return fmt.Errorf("common capabilities migration baseline table disappeared: %s", tableName(set.model))
			}
			columns, err := existingColumns(tx, set.model)
			if err != nil {
				return err
			}
			for _, column := range set.columns {
				if hasColumn(columns, column) {
					continue
				}
				addModel := set.model
				if _, ok := set.model.(*persistencemodel.FileObject); ok && column == "metadata_json" {
					addModel = &metadataColumn{}
				}
				if err := tx.Migrator().AddColumn(addModel, column); err != nil {
					return fmt.Errorf("add %s.%s: %w", tableName(set.model), column, err)
				}
				columns[strings.ToLower(column)] = struct{}{}
			}
			if _, ok := set.model.(*persistencemodel.FileObject); ok && hasColumn(columns, "metadata_json") {
				if err := finalizeMetadataColumn(tx); err != nil {
					return err
				}
			}
		}
		if err := backfillLegacyRelayStatus(tx); err != nil {
			return err
		}
		if err := ensureAdditiveIndexes(tx, existing); err != nil {
			return err
		}
		return nil
	})
}

// Down removes the objects listed by this version and the additive compatibility
// columns; it is intended for an explicit local rollback of a database that was
// upgraded from the v001 shape, and is safe to run repeatedly in that context.
func Down(db *gorm.DB) error {
	if db == nil || db.Dialector == nil {
		return errors.New("common capabilities migration database is not initialized")
	}
	if err := validateDialect(db); err != nil {
		return err
	}
	db = db.Session(&gorm.Session{NewDB: true})
	db.DisableForeignKeyConstraintWhenMigrating = true
	return db.Transaction(func(tx *gorm.DB) error {
		tx.DisableForeignKeyConstraintWhenMigrating = true
		existing, err := existingTables(tx)
		if err != nil {
			return err
		}
		if err := validateLegacyTables(existing); err != nil {
			return err
		}
		for i := len(newModels) - 1; i >= 0; i-- {
			if hasTable(existing, newModels[i]) {
				if err := tx.Migrator().DropTable(newModels[i]); err != nil {
					return fmt.Errorf("drop %s: %w", tableName(newModels[i]), err)
				}
				delete(existing, strings.ToLower(tableName(newModels[i])))
			}
		}
		if err := dropAdditiveIndexes(tx, existing); err != nil {
			return err
		}
		for _, item := range []struct {
			model   any
			columns []string
		}{
			{&persistencemodel.EmailMessage{}, []string{"scope_type", "caller_key", "template_key", "template_generation", "policy_generation", "locale", "is_test", "challenge_id", "relay_status", "idempotency_scope_hash"}},
			{&persistencemodel.SMTPAccount{}, []string{"scope_type"}},
			{&persistencemodel.FileObject{}, []string{"scope_type", "category_id", "provider_id", "lifecycle_status", "metadata_json", "original_extension", "detected_mime", "reconcile_key", "pending_at", "ready_at"}},
		} {
			model := item.model
			if hasTable(existing, model) {
				columns, err := existingColumns(tx, model)
				if err != nil {
					return err
				}
				for i := len(item.columns) - 1; i >= 0; i-- {
					column := item.columns[i]
					if hasColumn(columns, column) {
						if err := tx.Migrator().DropColumn(model, column); err != nil {
							return fmt.Errorf("drop %s.%s: %w", tableName(model), column, err)
						}
					}
				}
			}
		}
		return nil
	})
}

func validateDialect(db *gorm.DB) error {
	name := strings.ToLower(strings.TrimSpace(db.Dialector.Name()))
	if name != "mysql" && name != "postgres" {
		return fmt.Errorf("common capabilities migration unsupported database dialect %q", name)
	}
	return nil
}

// existingTables reads the table catalog once and turns driver errors into an
// actionable migration error. This avoids relying on GORM's HasTable bool,
// which deliberately suppresses catalog query errors.
func existingTables(tx *gorm.DB) (map[string]struct{}, error) {
	if tx == nil || tx.Dialector == nil {
		return nil, errors.New("common capabilities migration database is not initialized")
	}
	tables, err := tx.Migrator().GetTables()
	if err != nil {
		return nil, fmt.Errorf("inspect v001 schema tables: %w", err)
	}
	present := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		present[strings.ToLower(strings.TrimSpace(table))] = struct{}{}
	}
	return present, nil
}

// requireLegacyTables validates the v001 precondition before any DDL is
// issued. It remains a small wrapper for tests/callers that only need the
// precondition check; Up/Down retain the catalog map to avoid later silent
// HasTable failures.
func requireLegacyTables(tx *gorm.DB) error {
	present, err := existingTables(tx)
	if err != nil {
		return err
	}
	return validateLegacyTables(present)
}

func validateLegacyTables(present map[string]struct{}) error {
	missing := missingLegacyTables(present)
	if len(missing) > 0 {
		return fmt.Errorf("common capabilities migration requires v001 tables: %s", strings.Join(missing, ", "))
	}
	return nil
}

func hasTable(present map[string]struct{}, model any) bool {
	_, ok := present[strings.ToLower(strings.TrimSpace(tableName(model)))]
	return ok
}

func existingColumns(tx *gorm.DB, model any) (map[string]struct{}, error) {
	columnTypes, err := tx.Migrator().ColumnTypes(model)
	if err != nil {
		return nil, fmt.Errorf("inspect %s columns: %w", tableName(model), err)
	}
	columns := make(map[string]struct{}, len(columnTypes))
	for _, column := range columnTypes {
		columns[strings.ToLower(strings.TrimSpace(column.Name()))] = struct{}{}
	}
	return columns, nil
}

func hasColumn(columns map[string]struct{}, name string) bool {
	_, ok := columns[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// backfillLegacyRelayStatus preserves the terminal state of messages that
// predate the relay_status column. New columns default to pending, but a
// v001 row that is already sent or failed must not be re-enqueued by a relay
// after the upgrade. Unscoped includes soft-deleted audit rows as well.
func backfillLegacyRelayStatus(tx *gorm.DB) error {
	for _, status := range []string{"sent", "failed"} {
		if _, err := gorm.G[persistencemodel.EmailMessage](tx.Unscoped()).
			Where("status = ? AND (relay_status IS NULL OR relay_status = ?)", status, "pending").
			Set(clause.Assignments(map[string]any{"relay_status": status})).
			Update(context.Background()); err != nil {
			return fmt.Errorf("backfill email_messages.relay_status=%s: %w", status, err)
		}
	}
	return nil
}

func missingLegacyTables(present map[string]struct{}) []string {
	normalized := make(map[string]struct{}, len(present))
	for table := range present {
		normalized[strings.ToLower(strings.TrimSpace(table))] = struct{}{}
	}
	missing := make([]string, 0)
	for _, model := range legacyModels {
		table := tableName(model)
		if _, ok := normalized[strings.ToLower(table)]; !ok {
			missing = append(missing, table)
		}
	}
	return missing
}

func tableName(model any) string {
	if named, ok := model.(interface{ TableName() string }); ok {
		return named.TableName()
	}
	return fmt.Sprintf("%T", model)
}

func createCapabilityTable(tx *gorm.DB, model any) error {
	table := tableName(model)
	createDB := tx
	if tx.Dialector.Name() == "mysql" {
		options := " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"
		if comment := persistencemodel.TableComment(table); comment != "" {
			options += " COMMENT='" + strings.ReplaceAll(comment, "'", "''") + "'"
		}
		createDB = tx.Set("gorm:table_options", options)
	}
	if err := createDB.Migrator().CreateTable(model); err != nil {
		return fmt.Errorf("create %s: %w", table, err)
	}
	if tx.Dialector.Name() == "postgres" {
		if comment := persistencemodel.TableComment(table); comment != "" {
			// PostgreSQL accepts a quoted identifier through the clause.Table
			// placeholder, but COMMENT's string literal must be rendered as a
			// literal. Binding it as a second parameter produces invalid SQL on
			// real PostgreSQL connections (and differs from the fresh-schema
			// helper), so escape only the literal and keep the identifier bound.
			statement := "COMMENT ON TABLE ? IS '" + escapeSQLLiteral(comment) + "'"
			if err := tx.Exec(statement, clause.Table{Name: table}).Error; err != nil {
				return fmt.Errorf("comment table %s: %w", table, err)
			}
		}
	}
	return nil
}

func escapeSQLLiteral(value string) string { return strings.ReplaceAll(value, "'", "''") }

func finalizeMetadataColumn(tx *gorm.DB) error {
	const table = "file_objects"
	// Backfill every row, including soft-deleted rows, before tightening the
	// constraint. This makes a retry after a partially completed migration safe.
	if err := tx.Unscoped().Model(&persistencemodel.FileObject{}).
		Where("metadata_json IS NULL").
		Update("metadata_json", persistencemodel.JSONValue([]byte("{}"))).Error; err != nil {
		return fmt.Errorf("backfill %s.metadata_json: %w", table, err)
	}
	columnTypes, err := tx.Migrator().ColumnTypes(&persistencemodel.FileObject{})
	if err != nil {
		return fmt.Errorf("inspect %s.metadata_json: %w", table, err)
	}
	for _, column := range columnTypes {
		if !strings.EqualFold(column.Name(), "metadata_json") {
			continue
		}
		nullable, known := column.Nullable()
		if known && !nullable {
			return nil
		}
		if err := tx.Migrator().AlterColumn(&persistencemodel.FileObject{}, "metadata_json"); err != nil {
			return fmt.Errorf("set %s.metadata_json NOT NULL: %w", table, err)
		}
		return nil
	}
	return fmt.Errorf("inspect %s.metadata_json: column is missing", table)
}

func ensureAdditiveIndexes(tx *gorm.DB, knownTables ...map[string]struct{}) error {
	indexCache := make(map[string]map[string]struct{}, 2)
	for _, index := range additiveIndexes {
		if !tablePresent(tx, index.model, knownTables) {
			continue
		}
		table := strings.ToLower(strings.TrimSpace(tableName(index.model)))
		indexes, ok := indexCache[table]
		if !ok {
			var err error
			indexes, err = existingIndexes(tx, index.model)
			if err != nil {
				return err
			}
			indexCache[table] = indexes
		}
		if _, ok := indexes[strings.ToLower(index.name)]; ok {
			continue
		}
		if err := tx.Migrator().CreateIndex(index.model, index.name); err != nil {
			return fmt.Errorf("create %s.%s: %w", tableName(index.model), index.name, err)
		}
		indexes[strings.ToLower(index.name)] = struct{}{}
	}
	return nil
}

func dropAdditiveIndexes(tx *gorm.DB, knownTables ...map[string]struct{}) error {
	indexCache := make(map[string]map[string]struct{}, 2)
	for i := len(additiveIndexes) - 1; i >= 0; i-- {
		index := additiveIndexes[i]
		if !tablePresent(tx, index.model, knownTables) {
			continue
		}
		table := strings.ToLower(strings.TrimSpace(tableName(index.model)))
		indexes, ok := indexCache[table]
		if !ok {
			var err error
			indexes, err = existingIndexes(tx, index.model)
			if err != nil {
				return err
			}
			indexCache[table] = indexes
		}
		if _, ok := indexes[strings.ToLower(index.name)]; !ok {
			continue
		}
		if err := tx.Migrator().DropIndex(index.model, index.name); err != nil {
			return fmt.Errorf("drop %s.%s: %w", tableName(index.model), index.name, err)
		}
		delete(indexes, strings.ToLower(index.name))
	}
	return nil
}

// existingIndexes reads one table's index catalog and preserves driver errors
// for the caller. GORM's HasIndex helper returns only a bool and intentionally
// discards those errors, which is unsafe for a migration retry/rollback path.
func existingIndexes(tx *gorm.DB, model any) (map[string]struct{}, error) {
	if tx == nil || tx.Dialector == nil {
		return nil, errors.New("common capabilities migration database is not initialized")
	}
	indexes, err := tx.Migrator().GetIndexes(model)
	if err != nil {
		return nil, fmt.Errorf("inspect %s indexes: %w", tableName(model), err)
	}
	present := make(map[string]struct{}, len(indexes))
	for _, index := range indexes {
		present[strings.ToLower(strings.TrimSpace(index.Name()))] = struct{}{}
	}
	return present, nil
}

func tablePresent(tx *gorm.DB, model any, knownTables []map[string]struct{}) bool {
	if len(knownTables) > 0 {
		return hasTable(knownTables[0], model)
	}
	return tx != nil && tx.Migrator().HasTable(model)
}
