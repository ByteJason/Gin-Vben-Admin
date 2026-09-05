// Package admin contains explicit, reviewable admin schema/data upgrades.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	persistencemodel "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
)

// V003Version is the immutable migration identifier. The cleanup is a data
// migration rather than an application-startup hook and is safe to rerun.
const V003Version = "v003_settings_mail_cleanup"

// SettingsMailCleanupVersion is a descriptive alias for migration tooling.
const SettingsMailCleanupVersion = V003Version

// LegacyMailCacheCleaner is implemented by a cache adapter that knows the
// configured namespace. The migration never receives Redis credentials or
// scans an arbitrary keyspace; the adapter owns its narrowly scoped patterns.
type LegacyMailCacheCleaner interface {
	DeleteLegacyMailSettings(context.Context) error
}

// CleanupReport gives operators an auditable count without exposing deleted
// values. Independent mail tables (gvba_notify_smtp_accounts, gvba_notify_email_messages and related
// outbox tables) are intentionally absent from this report and untouched.
type CleanupReport struct {
	SettingRows    int64 `json:"settingRows"`
	AuditRows      int64 `json:"auditRows"`
	PermissionRows int64 `json:"permissionRows"`
	PolicyRows     int64 `json:"policyRows"`
	CacheCleaned   bool  `json:"cacheCleaned"`
}

// retiredSettingsPermissionIDs is intentionally an exact allow-list. The
// independent mail service uses system:mail:* and notification:* permissions;
// neither family is owned by this migration and must remain untouched.
var retiredSettingsPermissionIDs = []string{
	"system:parameters:read",
	"system:parameters:manage",
	"system:observability:read",
	"system:observability:manage",
}

// UpV003 executes the idempotent database cleanup. Cache cleanup is supplied
// by UpV003WithCache when a deployment has a Redis adapter available.
func UpV003(db *gorm.DB) error {
	_, err := UpV003WithCache(context.Background(), db, nil)
	return err
}

// UpSettingsMailCleanup is a descriptive alias for migration runners.
func UpSettingsMailCleanup(db *gorm.DB) error { return UpV003(db) }

// UpV003WithCache removes only configuration-centre mail rows and old settings
// audit events, then asks the optional namespaced cache adapter to remove stale
// settings keys after the database transaction commits.
func UpV003WithCache(ctx context.Context, db *gorm.DB, cache LegacyMailCacheCleaner) (CleanupReport, error) {
	if db == nil || db.Dialector == nil {
		return CleanupReport{}, errors.New("settings mail cleanup migration database is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	db = db.Session(&gorm.Session{NewDB: true})
	var report CleanupReport
	err := db.Transaction(func(tx *gorm.DB) error {
		tables, err := migrationTables(tx)
		if err != nil {
			return err
		}
		if tables["gvba_sys_setting_versions"] {
			keyColumn := `"key"`
			if strings.EqualFold(tx.Dialector.Name(), "mysql") {
				keyColumn = "`key`"
			}
			result := tx.WithContext(ctx).Unscoped().Where(
				"LOWER("+keyColumn+") IN (?, ?, ?) OR LOWER("+keyColumn+") LIKE ? OR LOWER("+keyColumn+") LIKE ? OR LOWER("+keyColumn+") LIKE ?",
				"mail", "email", "smtp", "mail.%", "email.%", "smtp.%",
			).Delete(&persistencemodel.SettingVersion{})
			if result.Error != nil {
				return fmt.Errorf("remove legacy settings mail rows: %w", result.Error)
			}
			report.SettingRows = result.RowsAffected
		}
		if tables["gvba_audit_auth_events"] {
			// Iterate by primary-key batches so a large audit table never has to be
			// materialized in memory. We intentionally inspect every row in each
			// batch because event-type and metadata formats varied across retired
			// settings-center releases. The final legacyMailAudit predicate remains
			// narrow and preserves independent mail/notification events.
			var lastID uint64
			for {
				rows, err := loadLegacyMailAuditBatch(tx.WithContext(ctx), lastID, legacyAuditBatchSize)
				if err != nil {
					return err
				}
				if len(rows) == 0 {
					break
				}
				ids := make([]uint64, 0, len(rows))
				for _, row := range rows {
					if legacyMailAudit(row) {
						ids = append(ids, row.ID)
					}
					if row.ID > lastID {
						lastID = row.ID
					}
				}
				if len(ids) > 0 {
					deleteResult := tx.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&persistencemodel.AuthAuditEvent{})
					if deleteResult.Error != nil {
						return fmt.Errorf("remove legacy settings mail audit rows: %w", deleteResult.Error)
					}
					report.AuditRows += deleteResult.RowsAffected
				}
				if len(rows) < legacyAuditBatchSize {
					break
				}
			}
		}
		// IAMPolicy stores the method/path pair rather than a permission_id
		// foreign key.  Resolve the exact retired permission routes before
		// deleting permission rows, then remove only policies carrying one of
		// those routes.  This keeps the cleanup compatible with the actual
		// persistence model and leaves the independent mail/notification routes
		// untouched.
		var retiredRoutes []permissionRoute
		if tables["gvba_iam_permissions"] {
			rows, queryErr := loadRetiredPermissionRoutes(tx.WithContext(ctx), retiredSettingsPermissionIDs)
			if queryErr != nil {
				return queryErr
			}
			retiredRoutes = rows
		}
		if tables["gvba_iam_policies"] && len(retiredRoutes) > 0 {
			predicate, args := permissionRoutePredicate(retiredRoutes)
			result := tx.WithContext(ctx).Unscoped().Where(predicate, args...).Delete(&persistencemodel.IAMPolicy{})
			if result.Error != nil {
				return fmt.Errorf("remove retired settings permission policies: %w", result.Error)
			}
			report.PolicyRows = result.RowsAffected
		}
		if tables["gvba_iam_permissions"] {
			result := tx.WithContext(ctx).Unscoped().Where("id IN ?", retiredSettingsPermissionIDs).Delete(&persistencemodel.Permission{})
			if result.Error != nil {
				return fmt.Errorf("remove retired settings permissions: %w", result.Error)
			}
			report.PermissionRows = result.RowsAffected
		}
		return nil
	})
	if err != nil {
		return report, err
	}
	if cache != nil {
		if err := cache.DeleteLegacyMailSettings(ctx); err != nil {
			return report, fmt.Errorf("remove legacy settings mail cache: %w", err)
		}
		report.CacheCleaned = true
	}
	return report, nil
}

type permissionRoute struct {
	Method string `gorm:"column:method"`
	Path   string `gorm:"column:path"`
}

func loadRetiredPermissionRoutes(tx *gorm.DB, ids []string) ([]permissionRoute, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []permissionRoute
	if err := tx.Unscoped().Model(&persistencemodel.Permission{}).
		Select("method", "path").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("inspect retired settings permission routes: %w", err)
	}
	return rows, nil
}

func permissionRoutePredicate(routes []permissionRoute) (string, []any) {
	parts := make([]string, 0, len(routes))
	args := make([]any, 0, len(routes)*2)
	seen := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		method := strings.TrimSpace(route.Method)
		path := strings.TrimSpace(route.Path)
		if method == "" || path == "" {
			continue
		}
		key := method + "\x00" + path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		parts = append(parts, "(method = ? AND path = ?)")
		args = append(args, method, path)
	}
	if len(parts) == 0 {
		return "1 = 0", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// UpSettingsMailCleanupWithCache is a descriptive alias for callers that need
// the cleanup report and cache hook.
func UpSettingsMailCleanupWithCache(ctx context.Context, db *gorm.DB, cache LegacyMailCacheCleaner) (CleanupReport, error) {
	return UpV003WithCache(ctx, db, cache)
}

// UpWithContext is retained as a convenient spelling for migration runners
// that already carry a context but do not need a cache adapter.
func UpWithContext(ctx context.Context, db *gorm.DB) (CleanupReport, error) {
	return UpV003WithCache(ctx, db, nil)
}

// DownV003 is intentionally a no-op. Deleting obsolete rows is irreversible by
// design; a rollback must restore a backup rather than manufacture stale mail
// configuration. Keeping the entry point idempotent lets generic migration
// tooling invoke Down without mutating independent mail data.
func DownV003(_ *gorm.DB) error { return nil }

// DownSettingsMailCleanup is the descriptive rollback alias.
func DownSettingsMailCleanup(db *gorm.DB) error { return DownV003(db) }

type legacyMailAuditRow struct {
	ID        uint64                      `gorm:"column:id"`
	EventType string                      `gorm:"column:event_type"`
	Metadata  *persistencemodel.JSONValue `gorm:"column:metadata"`
}

const legacyAuditBatchSize = 500

func (legacyMailAuditRow) TableName() string { return "gvba_audit_auth_events" }

func loadLegacyMailAuditRows(tx *gorm.DB) ([]legacyMailAuditRow, error) {
	var rows []legacyMailAuditRow
	if err := tx.Unscoped().Select("id", "event_type", "metadata").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("inspect settings audit rows: %w", err)
	}
	return rows, nil
}

// loadLegacyMailAuditBatch uses a keyset predicate rather than OFFSET. Rows
// selected by an earlier batch may be deleted before the next query, while the
// strictly increasing primary key still guarantees progress and bounded
// memory use on all supported SQL dialects.
func loadLegacyMailAuditBatch(tx *gorm.DB, afterID uint64, limit int) ([]legacyMailAuditRow, error) {
	if limit <= 0 {
		limit = legacyAuditBatchSize
	}
	var rows []legacyMailAuditRow
	query := tx.Unscoped().Select("id", "event_type", "metadata").Order("id ASC").Limit(limit)
	if afterID > 0 {
		query = query.Where("id > ?", afterID)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("inspect settings audit rows: %w", err)
	}
	return rows, nil
}

func legacyMailAudit(row legacyMailAuditRow) bool {
	event := strings.ToLower(strings.TrimSpace(row.EventType))
	// A few early builds encoded the module directly in the event type. This
	// branch is deliberately narrow and cannot match ordinary auth/mail events.
	for _, marker := range []string{"settings.mail", "settings.email", "settings.smtp", "configuration.mail", "configuration.smtp"} {
		if strings.HasPrefix(event, marker) || strings.Contains(event, "."+marker) {
			return true
		}
	}
	// Metadata from the independent mail service may legitimately contain
	// words such as "email" or "smtp".  Only inspect generic settings audit
	// events below; a mail/notification event is outside this migration's
	// ownership boundary and must remain untouched.
	if !strings.HasPrefix(event, "settings.") && !strings.HasPrefix(event, "configuration.") {
		return false
	}
	if row.Metadata == nil || len(*row.Metadata) == 0 {
		return false
	}
	var value any
	if json.Unmarshal([]byte(*row.Metadata), &value) != nil {
		return false
	}
	return metadataContainsLegacyMail(value)
}

func metadataContainsLegacyMail(value any) bool {
	switch item := value.(type) {
	case string:
		candidate := strings.ToLower(strings.TrimSpace(item))
		if candidate == "mail" || candidate == "email" || candidate == "smtp" {
			return true
		}
		for _, prefix := range []string{"mail.", "email.", "smtp."} {
			if strings.HasPrefix(candidate, prefix) {
				return true
			}
		}
	case []any:
		for _, child := range item {
			if metadataContainsLegacyMail(child) {
				return true
			}
		}
	case map[string]any:
		for key, child := range item {
			key = strings.ToLower(strings.TrimSpace(key))
			// These are the fields emitted by the retired settings adapter. Do
			// not recursively inspect arbitrary metadata such as message subjects,
			// provider names, or notification payloads.
			if key == "module" || key == "category" || key == "key" || key == "changedkeys" || key == "setting" || key == "settings" {
				if metadataContainsLegacyMail(child) {
					return true
				}
			}
		}
	}
	return false
}

func migrationTables(tx *gorm.DB) (map[string]bool, error) {
	tables, err := tx.Migrator().GetTables()
	if err != nil {
		return nil, fmt.Errorf("inspect settings mail cleanup tables: %w", err)
	}
	present := make(map[string]bool, len(tables))
	for _, table := range tables {
		present[strings.ToLower(strings.TrimSpace(table))] = true
	}
	return present, nil
}
