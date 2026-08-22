// Package dictionaryplatform contains the durable dictionary adapters.
package dictionaryplatform

import (
	"context"
	"errors"
	"strings"
	"time"

	dictionaryapp "example.com/gin-vben-admin/server/internal/application/dictionary"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
)

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

type dictionaryTypeRecord struct {
	ID          string     `gorm:"column:id;primaryKey"`
	TenantID    string     `gorm:"column:tenant_id"`
	OrgID       string     `gorm:"column:org_id"`
	Code        string     `gorm:"column:code"`
	NameZhCN    string     `gorm:"column:name_zh_cn"`
	NameEnUS    string     `gorm:"column:name_en_us"`
	Description string     `gorm:"column:description"`
	Status      string     `gorm:"column:status"`
	SortOrder   int        `gorm:"column:sort_order"`
	SystemOwned bool       `gorm:"column:system_owned"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
}

func (dictionaryTypeRecord) TableName() string { return "dictionary_types" }

type dictionaryItemRecord struct {
	ID          string     `gorm:"column:id;primaryKey"`
	TenantID    string     `gorm:"column:tenant_id"`
	OrgID       string     `gorm:"column:org_id"`
	TypeCode    string     `gorm:"column:type_code"`
	Value       string     `gorm:"column:item_value"`
	LabelZhCN   string     `gorm:"column:label_zh_cn"`
	LabelEnUS   string     `gorm:"column:label_en_us"`
	Description string     `gorm:"column:description"`
	Tag         string     `gorm:"column:tag"`
	Status      string     `gorm:"column:status"`
	SortOrder   int        `gorm:"column:sort_order"`
	SystemOwned bool       `gorm:"column:system_owned"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
}

func (dictionaryItemRecord) TableName() string { return "dictionary_items" }

type cacheVersionRecord struct {
	ID        string     `gorm:"column:id;primaryKey"`
	TenantID  string     `gorm:"column:tenant_id"`
	OrgID     string     `gorm:"column:org_id"`
	TypeCode  string     `gorm:"column:type_code"`
	Version   int64      `gorm:"column:version"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
	DeletedAt *time.Time `gorm:"column:deleted_at"`
}

func (cacheVersionRecord) TableName() string { return "dictionary_cache_versions" }

func (r *GORMRepository) ListTypes(ctx context.Context, tenantID, orgID string, includeDisabled bool) ([]dictionaryapp.DictionaryType, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("dictionary repository is not initialized")
	}
	query := r.db.Read(ctx).Where("deleted_at IS NULL AND (tenant_id = '' OR (tenant_id = ? AND (org_id = '' OR org_id = ?)))", tenantID, orgID)
	if !includeDisabled {
		query = query.Where("status = ?", "active")
	}
	var rows []dictionaryTypeRecord
	if err := query.Order("sort_order ASC, code ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]dictionaryapp.DictionaryType, 0, len(rows))
	for _, row := range rows {
		items = append(items, toType(row))
	}
	return items, nil
}

func (r *GORMRepository) FindType(ctx context.Context, id string) (dictionaryapp.DictionaryType, error) {
	if r == nil || r.db == nil {
		return dictionaryapp.DictionaryType{}, errors.New("dictionary repository is not initialized")
	}
	var row dictionaryTypeRecord
	if err := r.db.Read(ctx).Where("id = ? AND deleted_at IS NULL", strings.TrimSpace(id)).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dictionaryapp.DictionaryType{}, dictionaryapp.ErrTypeNotFound
		}
		return dictionaryapp.DictionaryType{}, err
	}
	return toType(row), nil
}

func (r *GORMRepository) FindTypeByScope(ctx context.Context, code, tenantID, orgID string, includeDeleted bool) (dictionaryapp.DictionaryType, error) {
	if r == nil || r.db == nil {
		return dictionaryapp.DictionaryType{}, errors.New("dictionary repository is not initialized")
	}
	query := r.db.Read(ctx).Where("code = ? AND (tenant_id = '' OR (tenant_id = ? AND (org_id = '' OR org_id = ?)))", strings.TrimSpace(code), tenantID, orgID)
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	var rows []dictionaryTypeRecord
	if err := query.Find(&rows).Error; err != nil {
		return dictionaryapp.DictionaryType{}, err
	}
	var chosen dictionaryapp.DictionaryType
	best := -1
	for _, row := range rows {
		candidate := toType(row)
		p := precedence(candidate.TenantID, candidate.OrgID, tenantID, orgID)
		if p > best {
			chosen, best = candidate, p
		}
	}
	if best < 0 {
		return dictionaryapp.DictionaryType{}, dictionaryapp.ErrTypeNotFound
	}
	return chosen, nil
}

func (r *GORMRepository) CreateType(ctx context.Context, row dictionaryapp.DictionaryType) (dictionaryapp.DictionaryType, error) {
	if r == nil || r.db == nil {
		return dictionaryapp.DictionaryType{}, errors.New("dictionary repository is not initialized")
	}
	record := fromType(row)
	if err := r.db.Write(ctx).Create(&record).Error; err != nil {
		return dictionaryapp.DictionaryType{}, dictionaryapp.ErrTypeConflict
	}
	return toType(record), nil
}

func (r *GORMRepository) UpdateType(ctx context.Context, row dictionaryapp.DictionaryType) (dictionaryapp.DictionaryType, error) {
	if r == nil || r.db == nil {
		return dictionaryapp.DictionaryType{}, errors.New("dictionary repository is not initialized")
	}
	result := r.db.Write(ctx).Model(&dictionaryTypeRecord{}).Where("id = ? AND tenant_id = ? AND org_id = ? AND system_owned = FALSE AND deleted_at IS NULL", row.ID, row.TenantID, row.OrgID).Updates(map[string]any{"name_zh_cn": row.NameZhCN, "name_en_us": row.NameEnUS, "description": row.Description, "status": row.Status, "sort_order": row.SortOrder, "updated_at": row.UpdatedAt})
	if result.Error != nil {
		return dictionaryapp.DictionaryType{}, result.Error
	}
	if result.RowsAffected == 0 {
		return dictionaryapp.DictionaryType{}, dictionaryapp.ErrTypeNotFound
	}
	return r.FindType(ctx, row.ID)
}

func (r *GORMRepository) SoftDeleteType(ctx context.Context, id, tenantID, orgID string, at time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("dictionary repository is not initialized")
	}
	result := r.db.Write(ctx).Model(&dictionaryTypeRecord{}).Where("id = ? AND tenant_id = ? AND org_id = ? AND system_owned = FALSE AND deleted_at IS NULL", strings.TrimSpace(id), tenantID, orgID).Updates(map[string]any{"deleted_at": at, "updated_at": at})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return dictionaryapp.ErrTypeNotFound
	}
	return nil
}

func (r *GORMRepository) ListItems(ctx context.Context, typeCode, tenantID, orgID string, includeDisabled bool) ([]dictionaryapp.DictionaryItem, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("dictionary repository is not initialized")
	}
	query := r.db.Read(ctx).Where("type_code = ? AND deleted_at IS NULL AND (tenant_id = '' OR (tenant_id = ? AND (org_id = '' OR org_id = ?)))", strings.TrimSpace(typeCode), tenantID, orgID)
	if !includeDisabled {
		query = query.Where("status = ?", "active")
	}
	var rows []dictionaryItemRecord
	if err := query.Order("sort_order ASC, item_value ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]dictionaryapp.DictionaryItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, toItem(row))
	}
	return items, nil
}

func (r *GORMRepository) FindItem(ctx context.Context, id, tenantID, orgID, typeCode string, includeDeleted bool) (dictionaryapp.DictionaryItem, error) {
	if r == nil || r.db == nil {
		return dictionaryapp.DictionaryItem{}, errors.New("dictionary repository is not initialized")
	}
	query := r.db.Read(ctx).Where("id = ? AND tenant_id = ? AND org_id = ?", strings.TrimSpace(id), tenantID, orgID)
	if typeCode != "" {
		query = query.Where("type_code = ?", typeCode)
	}
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	} else {
		query = query.Where("deleted_at IS NULL")
	}
	var row dictionaryItemRecord
	if err := query.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dictionaryapp.DictionaryItem{}, dictionaryapp.ErrItemNotFound
		}
		return dictionaryapp.DictionaryItem{}, err
	}
	return toItem(row), nil
}

func (r *GORMRepository) CreateItems(ctx context.Context, rows []dictionaryapp.DictionaryItem) ([]dictionaryapp.DictionaryItem, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("dictionary repository is not initialized")
	}
	if len(rows) == 0 {
		return nil, dictionaryapp.ErrInvalidItem
	}
	err := r.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		for _, row := range rows {
			record := fromItem(row)
			if err := tx.Create(&record).Error; err != nil {
				return dictionaryapp.ErrItemConflict
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result := make([]dictionaryapp.DictionaryItem, len(rows))
	for i, row := range rows {
		result[i] = row
	}
	return result, nil
}

func (r *GORMRepository) UpdateItem(ctx context.Context, row dictionaryapp.DictionaryItem) (dictionaryapp.DictionaryItem, error) {
	if r == nil || r.db == nil {
		return dictionaryapp.DictionaryItem{}, errors.New("dictionary repository is not initialized")
	}
	result := r.db.Write(ctx).Model(&dictionaryItemRecord{}).Where("id = ? AND tenant_id = ? AND org_id = ? AND system_owned = FALSE AND deleted_at IS NULL", row.ID, row.TenantID, row.OrgID).Updates(map[string]any{"item_value": row.Value, "label_zh_cn": row.LabelZhCN, "label_en_us": row.LabelEnUS, "description": row.Description, "tag": row.Tag, "status": row.Status, "sort_order": row.SortOrder, "updated_at": row.UpdatedAt})
	if result.Error != nil {
		return dictionaryapp.DictionaryItem{}, result.Error
	}
	if result.RowsAffected == 0 {
		return dictionaryapp.DictionaryItem{}, dictionaryapp.ErrItemNotFound
	}
	return r.FindItem(ctx, row.ID, row.TenantID, row.OrgID, row.TypeCode, false)
}

func (r *GORMRepository) SoftDeleteItem(ctx context.Context, id, tenantID, orgID string, at time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("dictionary repository is not initialized")
	}
	result := r.db.Write(ctx).Model(&dictionaryItemRecord{}).Where("id = ? AND tenant_id = ? AND org_id = ? AND system_owned = FALSE AND deleted_at IS NULL", strings.TrimSpace(id), tenantID, orgID).Updates(map[string]any{"deleted_at": at, "updated_at": at})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return dictionaryapp.ErrItemNotFound
	}
	return nil
}

func (r *GORMRepository) BumpVersion(ctx context.Context, tenantID, orgID, typeCode string, at time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("dictionary repository is not initialized")
	}
	var version int64
	err := r.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var row cacheVersionRecord
		err := tx.Where("tenant_id = ? AND org_id = ? AND type_code = ? AND deleted_at IS NULL", tenantID, orgID, typeCode).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = cacheVersionRecord{ID: "cache-" + dictionaryapp.NewVersionID(), TenantID: tenantID, OrgID: orgID, TypeCode: typeCode, Version: 1, CreatedAt: at, UpdatedAt: at}
			version = row.Version
			return tx.Create(&row).Error
		}
		if err != nil {
			return err
		}
		row.Version++
		row.UpdatedAt = at
		version = row.Version
		return tx.Model(&cacheVersionRecord{}).Where("id = ?", row.ID).Updates(map[string]any{"version": row.Version, "updated_at": at}).Error
	})
	return version, err
}

func (r *GORMRepository) CurrentVersion(ctx context.Context, tenantID, orgID, typeCode string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("dictionary repository is not initialized")
	}
	var row cacheVersionRecord
	if err := r.db.Read(ctx).Where("tenant_id = ? AND org_id = ? AND type_code = ? AND deleted_at IS NULL", tenantID, orgID, typeCode).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return row.Version, nil
}

func precedence(tenantID, orgID, scopeTenant, scopeOrg string) int {
	if tenantID == "" && orgID == "" {
		return 1
	}
	if tenantID != scopeTenant {
		return -1
	}
	if orgID == scopeOrg && orgID != "" {
		return 3
	}
	if orgID == "" {
		return 2
	}
	return -1
}
func toType(row dictionaryTypeRecord) dictionaryapp.DictionaryType {
	return dictionaryapp.DictionaryType{ID: row.ID, TenantID: row.TenantID, OrgID: row.OrgID, Code: row.Code, NameZhCN: row.NameZhCN, NameEnUS: row.NameEnUS, Description: row.Description, Status: row.Status, SortOrder: row.SortOrder, SystemOwned: row.SystemOwned, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt}
}
func fromType(row dictionaryapp.DictionaryType) dictionaryTypeRecord {
	return dictionaryTypeRecord{ID: row.ID, TenantID: row.TenantID, OrgID: row.OrgID, Code: row.Code, NameZhCN: row.NameZhCN, NameEnUS: row.NameEnUS, Description: row.Description, Status: row.Status, SortOrder: row.SortOrder, SystemOwned: row.SystemOwned, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt}
}
func toItem(row dictionaryItemRecord) dictionaryapp.DictionaryItem {
	return dictionaryapp.DictionaryItem{ID: row.ID, TenantID: row.TenantID, OrgID: row.OrgID, TypeCode: row.TypeCode, Value: row.Value, LabelZhCN: row.LabelZhCN, LabelEnUS: row.LabelEnUS, Description: row.Description, Tag: row.Tag, Status: row.Status, SortOrder: row.SortOrder, SystemOwned: row.SystemOwned, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt}
}
func fromItem(row dictionaryapp.DictionaryItem) dictionaryItemRecord {
	return dictionaryItemRecord{ID: row.ID, TenantID: row.TenantID, OrgID: row.OrgID, TypeCode: row.TypeCode, Value: row.Value, LabelZhCN: row.LabelZhCN, LabelEnUS: row.LabelEnUS, Description: row.Description, Tag: row.Tag, Status: row.Status, SortOrder: row.SortOrder, SystemOwned: row.SystemOwned, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt}
}

var _ dictionaryapp.Repository = (*GORMRepository)(nil)
