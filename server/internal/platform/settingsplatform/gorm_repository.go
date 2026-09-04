// Package settingsplatform contains persistent adapters for versioned settings.
package settingsplatform

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
)

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

type settingVersionRecord = model.SettingVersion

func moduleKeyPredicate(module string) (string, []any) {
	module = strings.ToLower(strings.TrimSpace(module))
	if module == "basic" {
		return "(key LIKE ? OR key = ?)", []any{module + ".%", "branding"}
	}
	return "key LIKE ?", []any{module + ".%"}
}

// settingScopePredicate limits reads to the current tenant and organization
// boundary.  An organization-scoped request may read the tenant-wide value as
// a fallback, but it must never see a sibling organization's override.  A
// tenant-wide request only reads rows with a NULL org_id so an organization
// override cannot bleed into the tenant default view.
func settingScopePredicate(scope tenant.Context) (string, []any) {
	if strings.TrimSpace(scope.Organization) == "" {
		return "tenant_id = ? AND org_id IS NULL", []any{scope.TenantID}
	}
	return "tenant_id = ? AND (org_id = ? OR org_id IS NULL)", []any{scope.TenantID, scope.Organization}
}

// settingWritePredicate selects exactly the scope that a write/reset is
// allowed to mutate.  Organization requests may inherit a tenant-wide row,
// but reset must only remove their own override and leave that fallback intact.
func settingWritePredicate(scope tenant.Context) (string, []any) {
	if strings.TrimSpace(scope.Organization) == "" {
		return "tenant_id = ? AND org_id IS NULL", []any{scope.TenantID}
	}
	return "tenant_id = ? AND org_id = ?", []any{scope.TenantID, scope.Organization}
}

func appendPredicate(base string, baseArgs []any, extra string, extraArgs []any) (string, []any) {
	args := make([]any, 0, len(baseArgs)+len(extraArgs))
	args = append(args, baseArgs...)
	args = append(args, extraArgs...)
	return base + " AND " + extra, args
}

func organizationOf(record settingVersionRecord) string {
	if record.OrgID == nil {
		return ""
	}
	return strings.TrimSpace(*record.OrgID)
}

// chooseCurrentRow applies organization precedence after the SQL scope
// predicate has excluded sibling organizations.  Rows are normally ordered by
// version descending, but the comparison keeps the helper correct for callers
// that provide an unordered slice in tests.
func chooseCurrentRow(rows []settingVersionRecord, scope tenant.Context) (settingVersionRecord, bool) {
	var global, organization *settingVersionRecord
	for index := range rows {
		row := rows[index]
		if row.DeletedAt != nil {
			continue
		}
		org := organizationOf(row)
		if scope.Organization != "" && org == strings.TrimSpace(scope.Organization) {
			if organization == nil || row.Version > organization.Version {
				copy := row
				organization = &copy
			}
			continue
		}
		if org == "" && (global == nil || row.Version > global.Version) {
			copy := row
			global = &copy
		}
	}
	if organization != nil {
		return *organization, true
	}
	if global != nil {
		return *global, true
	}
	return settingVersionRecord{}, false
}

// chooseModuleRows computes the effective latest row per key.  Unscoped
// reads include reset tombstones; a tombstone suppresses an older override and
// allows an organization request to fall back to the tenant-wide row.
func chooseModuleRows(rows []settingVersionRecord, scope tenant.Context) (map[string]settingVersionRecord, int64, time.Time) {
	type scopedRow struct {
		row settingVersionRecord
		ok  bool
	}
	global := map[string]scopedRow{}
	organization := map[string]scopedRow{}
	var revision int64
	var updated time.Time
	orgID := strings.TrimSpace(scope.Organization)
	for _, row := range rows {
		if row.Version > revision {
			revision = row.Version
		}
		if row.UpdatedAt.After(updated) {
			updated = row.UpdatedAt
		}
		org := organizationOf(row)
		target := global
		if orgID != "" && org == orgID {
			target = organization
		} else if org != "" {
			// The SQL predicate should already exclude this branch. Keeping the
			// guard makes the helper safe when called directly by tests.
			continue
		}
		previous, exists := target[row.Key]
		if !exists || row.Version > previous.row.Version {
			target[row.Key] = scopedRow{row: row, ok: true}
		}
	}
	keys := make(map[string]struct{}, len(global)+len(organization))
	for key := range global {
		keys[key] = struct{}{}
	}
	for key := range organization {
		keys[key] = struct{}{}
	}
	values := make(map[string]settingVersionRecord, len(keys))
	for key := range keys {
		if orgID != "" {
			if selected, ok := organization[key]; ok && selected.row.DeletedAt == nil {
				values[key] = selected.row
				continue
			}
		}
		if selected, ok := global[key]; ok && selected.row.DeletedAt == nil {
			values[key] = selected.row
		}
	}
	return values, revision, updated
}

func (r *GORMRepository) Current(ctx context.Context, key string) (settings.StoredSetting, error) {
	if r == nil || r.db == nil {
		return settings.StoredSetting{}, errors.New("settings repository is not initialized")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return settings.StoredSetting{}, err
	}
	scopeSQL, scopeArgs := settingScopePredicate(scope)
	rows, err := gorm.G[settingVersionRecord](r.db.Read(ctx)).Where(scopeSQL+" AND key = ?", append(scopeArgs, key)...).Order("version DESC").Find(ctx)
	if err != nil {
		return settings.StoredSetting{}, err
	}
	record, ok := chooseCurrentRow(rows, scope)
	if !ok {
		return settings.StoredSetting{}, settings.ErrSettingNotFound
	}
	return toStored(record), nil
}

func (r *GORMRepository) Append(ctx context.Context, value settings.StoredSetting) (settings.StoredSetting, error) {
	if r == nil || r.db == nil {
		return settings.StoredSetting{}, errors.New("settings repository is not initialized")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return settings.StoredSetting{}, err
	}
	var record settingVersionRecord
	err = r.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var aggregate struct {
			Current int64 `gorm:"column:current"`
		}
		scopeSQL, scopeArgs := settingScopePredicate(scope)
		if err := gorm.G[settingVersionRecord](tx).Select("COALESCE(MAX(version), 0) AS current").Where(scopeSQL+" AND key = ?", append(scopeArgs, value.Key)...).Scan(ctx, &aggregate); err != nil {
			return err
		}
		// The legacy unique index is keyed by tenant/key/version (without
		// org_id). Allocate the next physical row version from every scope in
		// this tenant so two organizations cannot race into the same unique
		// tuple even though their logical module revisions remain independent.
		var physical struct {
			Current int64 `gorm:"column:current"`
		}
		if err := gorm.G[settingVersionRecord](tx.Unscoped()).Select("COALESCE(MAX(version), 0) AS current").Where("tenant_id = ? AND key = ?", scope.TenantID, value.Key).Scan(ctx, &physical); err != nil {
			return err
		}
		record = fromStored(value)
		record.Version = physical.Current + 1
		record.TenantID = scope.TenantID
		if scope.Organization != "" {
			orgID := scope.Organization
			record.OrgID = &orgID
		}
		return gorm.G[settingVersionRecord](tx).Create(ctx, &record)
	})
	if err != nil {
		return settings.StoredSetting{}, err
	}
	return toStored(record), nil
}

func (r *GORMRepository) History(ctx context.Context, key string) ([]settings.StoredSetting, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("settings repository is not initialized")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	scopeSQL, scopeArgs := settingScopePredicate(scope)
	records, err := gorm.G[settingVersionRecord](r.db.Read(ctx)).Where(scopeSQL+" AND key = ?", append(scopeArgs, key)...).Order("version ASC").Find(ctx)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, settings.ErrSettingNotFound
	}
	// If an organization has its own history, do not expose the tenant-wide
	// history alongside it.  Otherwise return the tenant-wide fallback history.
	if scope.Organization != "" {
		hasOrganization := false
		for _, record := range records {
			if organizationOf(record) == strings.TrimSpace(scope.Organization) {
				hasOrganization = true
				break
			}
		}
		filtered := records[:0]
		for _, record := range records {
			if (hasOrganization && organizationOf(record) == strings.TrimSpace(scope.Organization)) || (!hasOrganization && organizationOf(record) == "") {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	if len(records) == 0 {
		return nil, settings.ErrSettingNotFound
	}
	out := make([]settings.StoredSetting, 0, len(records))
	for _, record := range records {
		out = append(out, toStored(record))
	}
	return out, nil
}

// CurrentModule reads the latest row for each key in a module. The legacy
// setting_versions table may retain old rows, but this adapter exposes only
// the current final state to module callers.
func (r *GORMRepository) CurrentModule(ctx context.Context, module string) (settings.StoredModule, error) {
	if r == nil || r.db == nil {
		return settings.StoredModule{}, errors.New("settings repository is not initialized")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return settings.StoredModule{}, err
	}
	predicate, args := moduleKeyPredicate(module)
	// Read historical/tombstone rows unscoped so the aggregate revision remains
	// monotonic after a restore-default operation. Deleted rows are filtered out
	// of Values below, but still contribute to Revision.
	scopeSQL, scopeArgs := settingScopePredicate(scope)
	querySQL, queryArgs := appendPredicate(scopeSQL, scopeArgs, predicate, args)
	query := gorm.G[settingVersionRecord](r.db.Read(ctx).Unscoped()).Where(querySQL, queryArgs...)
	rows, err := query.Order("version ASC").Find(ctx)
	if err != nil {
		return settings.StoredModule{}, err
	}
	selected, revision, updated := chooseModuleRows(rows, scope)
	values := make(map[string]settings.StoredSetting, len(selected))
	for key, row := range selected {
		values[key] = toStored(row)
	}
	if len(values) == 0 {
		return settings.StoredModule{Module: strings.ToLower(strings.TrimSpace(module)), Values: values, Revision: revision}, settings.ErrSettingNotFound
	}
	return settings.StoredModule{Module: strings.ToLower(strings.TrimSpace(module)), Values: values, Revision: revision, UpdatedAt: updated}, nil
}

// SaveModule appends a complete candidate in one DB transaction. The table is
// retained for backwards-compatible migrations; no caller needs to inspect
// its historical rows, and module revision is derived from the committed
// aggregate's latest version.
func (r *GORMRepository) SaveModule(ctx context.Context, module string, values map[string]settings.StoredSetting, expectedRevision int64) (settings.StoredModule, error) {
	if r == nil || r.db == nil {
		return settings.StoredModule{}, errors.New("settings repository is not initialized")
	}
	if strings.TrimSpace(module) == "" || len(values) == 0 {
		return settings.StoredModule{}, settings.ErrInvalidSetting
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return settings.StoredModule{}, err
	}
	module = strings.ToLower(strings.TrimSpace(module))
	predicate, args := moduleKeyPredicate(module)
	var result settings.StoredModule
	err = r.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var aggregate struct {
			Current int64 `gorm:"column:current"`
		}
		scopeSQL, scopeArgs := settingScopePredicate(scope)
		querySQL, queryArgs := appendPredicate(scopeSQL, scopeArgs, predicate, args)
		query := gorm.G[settingVersionRecord](tx.Unscoped()).Where(querySQL, queryArgs...)
		if err := query.Select("COALESCE(MAX(version), 0) AS current").Scan(ctx, &aggregate); err != nil {
			return err
		}
		if aggregate.Current != expectedRevision {
			return errors.Join(settings.ErrVersionConflict, settings.ErrModuleRevisionConflict)
		}
		// A module revision advances once per atomic save, independently of the
		// number of keys changed.  Every row written by this transaction carries
		// that same next revision; deriving it from the maximum per-key version
		// would allow two successive saves that touch different keys to reuse the
		// same module revision.
		// See Append: physical row versions must remain unique across
		// organization scopes under the legacy tenant/key/version index. The
		// logical compare-and-swap still uses the scoped aggregate above.
		var physical struct {
			Current int64 `gorm:"column:current"`
		}
		if err := gorm.G[settingVersionRecord](tx.Unscoped()).Select("COALESCE(MAX(version), 0) AS current").Where("tenant_id = ? AND "+predicate, append([]any{scope.TenantID}, args...)...).Scan(ctx, &physical); err != nil {
			return err
		}
		nextRevision := physical.Current + 1
		result = settings.StoredModule{Module: module, Values: make(map[string]settings.StoredSetting, len(values)), Revision: nextRevision, UpdatedAt: time.Now().UTC()}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := values[key]
			if len(value.RawValue) == 0 || !json.Valid(value.RawValue) {
				return settings.ErrInvalidSetting
			}
			// Keep the aggregate revision as the row version.  Per-key legacy
			// updates still use their own monotonically increasing versions through
			// Append; module writes are the canonical atomic contract.
			value.Version = nextRevision
			if value.UpdatedAt.IsZero() {
				value.UpdatedAt = result.UpdatedAt
			}
			record := fromStored(value)
			record.TenantID = scope.TenantID
			if scope.Organization != "" {
				orgID := scope.Organization
				record.OrgID = &orgID
			}
			if err := gorm.G[settingVersionRecord](tx).Create(ctx, &record); err != nil {
				return err
			}
			result.Values[key] = toStored(record)
		}
		return nil
	})
	if err != nil {
		return settings.StoredModule{}, err
	}
	return result, nil
}

// ListCurrent returns one latest row per key for snapshot reconstruction.
func (r *GORMRepository) ListCurrent(ctx context.Context) ([]settings.StoredSetting, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("settings repository is not initialized")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	scopeSQL, scopeArgs := settingScopePredicate(scope)
	rows, err := gorm.G[settingVersionRecord](r.db.Read(ctx).Unscoped()).Where(scopeSQL, scopeArgs...).Order("key ASC, version ASC").Find(ctx)
	if err != nil {
		return nil, err
	}
	selected, _, _ := chooseModuleRows(rows, scope)
	latest := make(map[string]settings.StoredSetting, len(selected))
	for key, row := range selected {
		latest[key] = toStored(row)
	}
	keys := make([]string, 0, len(latest))
	for key := range latest {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]settings.StoredSetting, 0, len(keys))
	for _, key := range keys {
		result = append(result, latest[key])
	}
	if len(result) == 0 {
		return nil, settings.ErrSettingNotFound
	}
	return result, nil
}

// Delete removes the active database override while retaining the table's
// soft-delete semantics. The effective resolver then falls back to YAML or
// the compiled definition default.
func (r *GORMRepository) Delete(ctx context.Context, key string) error {
	if r == nil || r.db == nil {
		return errors.New("settings repository is not initialized")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	scopeSQL, scopeArgs := settingWritePredicate(scope)
	rows, err := gorm.G[settingVersionRecord](r.db.Write(ctx)).Where(scopeSQL+" AND key = ?", append(scopeArgs, key)...).Delete(ctx)
	if err != nil {
		return err
	}
	if rows == 0 {
		return settings.ErrSettingNotFound
	}
	return nil
}

// ResetModule atomically soft-deletes all active overrides in a module and
// records a deleted tombstone carrying the next aggregate revision. Keeping a
// tombstone means a subsequent save cannot reuse an old per-key version after
// reset, while Current/CurrentModule continue to expose inherited defaults.
func (r *GORMRepository) ResetModule(ctx context.Context, module string, expectedRevision int64) (settings.StoredModule, error) {
	if r == nil || r.db == nil {
		return settings.StoredModule{}, errors.New("settings repository is not initialized")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return settings.StoredModule{}, err
	}
	module = strings.ToLower(strings.TrimSpace(module))
	if module == "" {
		return settings.StoredModule{}, settings.ErrInvalidSetting
	}
	predicate, args := moduleKeyPredicate(module)
	var result settings.StoredModule
	err = r.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		scopeSQL, scopeArgs := settingScopePredicate(scope)
		querySQL, queryArgs := appendPredicate(scopeSQL, scopeArgs, predicate, args)
		query := gorm.G[settingVersionRecord](tx.Unscoped()).Where(querySQL, queryArgs...)
		rows, findErr := query.Order("version ASC").Find(ctx)
		if findErr != nil {
			return findErr
		}
		selected, current, _ := chooseModuleRows(rows, scope)
		// A reset only removes an override at the exact request scope.  An
		// organization that merely inherits tenant-wide values has no carrier
		// and therefore remains an idempotent no-op.
		var carrier *settingVersionRecord
		for key := range selected {
			row := selected[key]
			if organizationOf(row) == strings.TrimSpace(scope.Organization) {
				copy := row
				carrier = &copy
				break
			}
		}
		if current != expectedRevision {
			return errors.Join(settings.ErrVersionConflict, settings.ErrModuleRevisionConflict)
		}
		if carrier == nil {
			// Nothing is persisted for this module. A reset is idempotent and
			// does not manufacture a database override or revision row.
			result = settings.StoredModule{Module: module, Values: map[string]settings.StoredSetting{}, Revision: current, UpdatedAt: time.Now().UTC()}
			return nil
		}
		writeSQL, writeArgs := settingWritePredicate(scope)
		deleteSQL, deleteArgs := appendPredicate(writeSQL, writeArgs, predicate, args)
		if _, deleteErr := gorm.G[settingVersionRecord](tx).Where(deleteSQL, deleteArgs...).Delete(ctx); deleteErr != nil {
			return deleteErr
		}
		now := time.Now().UTC()
		tombstone := *carrier
		tombstone.ID = 0
		// Keep the physical version unique across organization scopes while
		// preserving the scoped revision check above.
		var physical struct {
			Current int64 `gorm:"column:current"`
		}
		if err := gorm.G[settingVersionRecord](tx.Unscoped()).Select("COALESCE(MAX(version), 0) AS current").Where("tenant_id = ? AND "+predicate, append([]any{scope.TenantID}, args...)...).Scan(ctx, &physical); err != nil {
			return err
		}
		tombstone.Version = physical.Current + 1
		tombstone.Value = model.JSONValue([]byte("null"))
		tombstone.Source = "reset"
		tombstone.UpdatedBy = ""
		tombstone.CreatedAt = now
		tombstone.UpdatedAt = now
		tombstone.DeletedAt = &now
		if scope.Organization != "" {
			orgID := scope.Organization
			tombstone.OrgID = &orgID
		} else {
			tombstone.OrgID = nil
		}
		if createErr := gorm.G[settingVersionRecord](tx.Unscoped()).Create(ctx, &tombstone); createErr != nil {
			return createErr
		}
		result = settings.StoredModule{Module: module, Values: map[string]settings.StoredSetting{}, Revision: tombstone.Version, UpdatedAt: now}
		return nil
	})
	if err != nil {
		return settings.StoredModule{}, err
	}
	return result, nil
}

// ClearSensitiveKeys atomically removes only the requested credential rows in
// the caller's exact scope. Soft-delete tombstones suppress historical values
// and advance the module revision, while leaving unrelated settings intact.
func (r *GORMRepository) ClearSensitiveKeys(ctx context.Context, module string, keys []string, expectedRevision int64) (settings.StoredModule, error) {
	if r == nil || r.db == nil {
		return settings.StoredModule{}, errors.New("settings repository is not initialized")
	}
	module = strings.ToLower(strings.TrimSpace(module))
	if module == "" || len(keys) == 0 {
		return settings.StoredModule{}, settings.ErrInvalidSetting
	}
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			wanted[key] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return settings.StoredModule{}, settings.ErrInvalidSetting
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return settings.StoredModule{}, err
	}
	predicate, args := moduleKeyPredicate(module)
	var result settings.StoredModule
	err = r.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		scopeSQL, scopeArgs := settingScopePredicate(scope)
		querySQL, queryArgs := appendPredicate(scopeSQL, scopeArgs, predicate, args)
		rows, findErr := gorm.G[settingVersionRecord](tx.Unscoped()).Where(querySQL, queryArgs...).Order("version ASC").Find(ctx)
		if findErr != nil {
			return findErr
		}
		var current int64
		for _, row := range rows {
			if row.Version > current {
				current = row.Version
			}
		}
		if current != expectedRevision {
			return errors.Join(settings.ErrVersionConflict, settings.ErrModuleRevisionConflict)
		}
		// Select active database rows at the exact write scope. A deployment
		// source cannot be cleared by this adapter; the service rejects it before
		// entering the transaction.
		selected := make(map[string]settingVersionRecord, len(wanted))
		orgID := strings.TrimSpace(scope.Organization)
		for _, row := range rows {
			if row.DeletedAt != nil || organizationOf(row) != orgID {
				continue
			}
			source := strings.ToLower(strings.TrimSpace(row.Source))
			if source != "" && source != string(settings.SourceDatabase) {
				continue
			}
			if _, ok := wanted[row.Key]; !ok {
				continue
			}
			previous, exists := selected[row.Key]
			if !exists || row.Version > previous.Version {
				selected[row.Key] = row
			}
		}
		if len(selected) == 0 {
			result = settings.StoredModule{Module: module, Values: map[string]settings.StoredSetting{}, Revision: current, UpdatedAt: time.Now().UTC()}
			return nil
		}
		writeSQL, writeArgs := settingWritePredicate(scope)
		deleteSQL, deleteArgs := appendPredicate(writeSQL, writeArgs, predicate, args)
		selectedKeys := make([]string, 0, len(selected))
		for key := range selected {
			selectedKeys = append(selectedKeys, key)
		}
		sort.Strings(selectedKeys)
		deleteSQL += " AND key IN ?"
		deleteArgs = append(deleteArgs, selectedKeys)
		if _, deleteErr := gorm.G[settingVersionRecord](tx).Where(deleteSQL, deleteArgs...).Delete(ctx); deleteErr != nil {
			return deleteErr
		}
		now := time.Now().UTC()
		nextRevision := current + 1
		// The legacy unique index is tenant/key/version. Keep the aggregate
		// revision above every physical version for every selected key.
		for _, key := range selectedKeys {
			var physical struct {
				Current int64 `gorm:"column:current"`
			}
			if scanErr := gorm.G[settingVersionRecord](tx.Unscoped()).Select("COALESCE(MAX(version), 0) AS current").Where("tenant_id = ? AND key = ?", scope.TenantID, key).Scan(ctx, &physical); scanErr != nil {
				return scanErr
			}
			if physical.Current+1 > nextRevision {
				nextRevision = physical.Current + 1
			}
		}
		for _, key := range selectedKeys {
			carrier := selected[key]
			org := strings.TrimSpace(scope.Organization)
			tombstone := settingVersionRecord{TenantID: scope.TenantID, Key: key, Value: model.JSONValue([]byte("null")), Version: nextRevision, Sensitive: carrier.Sensitive, Encrypted: false, Source: "clear", UpdatedBy: "", CreatedAt: now, UpdatedAt: now, DeletedAt: &now}
			if org != "" {
				tombstone.OrgID = &org
			}
			if createErr := gorm.G[settingVersionRecord](tx.Unscoped()).Create(ctx, &tombstone); createErr != nil {
				return createErr
			}
		}
		result = settings.StoredModule{Module: module, Values: map[string]settings.StoredSetting{}, Revision: nextRevision, UpdatedAt: now}
		return nil
	})
	if err != nil {
		return settings.StoredModule{}, err
	}
	return result, nil
}

func toStored(record settingVersionRecord) settings.StoredSetting {
	return settings.StoredSetting{Key: record.Key, RawValue: append([]byte(nil), record.Value...), Version: record.Version, Sensitive: record.Sensitive, Encrypted: record.Encrypted, Source: settings.Source(record.Source), UpdatedBy: record.UpdatedBy, UpdatedAt: record.UpdatedAt, TenantID: strings.TrimSpace(record.TenantID), Organization: organizationOf(record)}
}

func fromStored(value settings.StoredSetting) settingVersionRecord {
	return settingVersionRecord{Key: value.Key, Value: model.JSONValue(append([]byte(nil), value.RawValue...)), Version: value.Version, Sensitive: value.Sensitive, Encrypted: value.Encrypted, Source: string(value.Source), UpdatedBy: value.UpdatedBy, UpdatedAt: value.UpdatedAt}
}

var _ settings.Repository = (*GORMRepository)(nil)
var _ settings.AtomicModuleResetRepository = (*GORMRepository)(nil)
var _ settings.AtomicCredentialClearRepository = (*GORMRepository)(nil)
