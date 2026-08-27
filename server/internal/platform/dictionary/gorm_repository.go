// Package dictionaryplatform contains the durable dictionary adapters.
package dictionaryplatform

import (
	"context"
	"errors"
	"strings"
	"time"

	dictionaryapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/dictionary"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

type dictionaryTypeRecord = model.DictionaryType
type dictionaryItemRecord = model.DictionaryItem
type cacheVersionRecord = model.DictionaryCacheVersion

func (r *GORMRepository) ListTypes(ctx context.Context, tenantID, orgID string, includeDisabled bool) ([]dictionaryapp.DictionaryType, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("dictionary repository is not initialized")
	}
	query := gorm.G[dictionaryTypeRecord](r.db.Read(ctx)).Where("deleted_at IS NULL AND (tenant_id = '' OR (tenant_id = ? AND (org_id = '' OR org_id = ?)))", tenantID, orgID)
	if !includeDisabled {
		query = query.Where("status = ?", "active")
	}
	rows, err := query.Order("sort_order ASC, code ASC").Find(ctx)
	if err != nil {
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
	row, err := gorm.G[dictionaryTypeRecord](r.db.Read(ctx)).Where("id = ? AND deleted_at IS NULL", strings.TrimSpace(id)).First(ctx)
	if err != nil {
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
	query := gorm.G[dictionaryTypeRecord](r.db.Read(ctx)).Where("code = ? AND (tenant_id = '' OR (tenant_id = ? AND (org_id = '' OR org_id = ?)))", strings.TrimSpace(code), tenantID, orgID)
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	rows, err := query.Find(ctx)
	if err != nil {
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
	if err := gorm.G[dictionaryTypeRecord](r.db.Write(ctx)).Create(ctx, &record); err != nil {
		return dictionaryapp.DictionaryType{}, dictionaryapp.ErrTypeConflict
	}
	return toType(record), nil
}

func (r *GORMRepository) UpdateType(ctx context.Context, row dictionaryapp.DictionaryType) (dictionaryapp.DictionaryType, error) {
	if r == nil || r.db == nil {
		return dictionaryapp.DictionaryType{}, errors.New("dictionary repository is not initialized")
	}
	rows, updateErr := gorm.G[dictionaryTypeRecord](r.db.Write(ctx)).Where("id = ? AND tenant_id = ? AND org_id = ? AND system_owned = FALSE AND deleted_at IS NULL", row.ID, row.TenantID, row.OrgID).Set(clause.Assignments(map[string]any{"name_zh_cn": row.NameZhCN, "name_en_us": row.NameEnUS, "description": row.Description, "status": row.Status, "sort_order": row.SortOrder, "updated_at": row.UpdatedAt})).Update(ctx)
	if updateErr != nil {
		return dictionaryapp.DictionaryType{}, updateErr
	}
	if rows == 0 {
		return dictionaryapp.DictionaryType{}, dictionaryapp.ErrTypeNotFound
	}
	return r.FindType(ctx, row.ID)
}

func (r *GORMRepository) SoftDeleteType(ctx context.Context, id, tenantID, orgID string, at time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("dictionary repository is not initialized")
	}
	rows, updateErr := gorm.G[dictionaryTypeRecord](r.db.Write(ctx)).Where("id = ? AND tenant_id = ? AND org_id = ? AND system_owned = FALSE AND deleted_at IS NULL", strings.TrimSpace(id), tenantID, orgID).Set(clause.Assignments(map[string]any{"deleted_at": at, "updated_at": at})).Update(ctx)
	if updateErr != nil {
		return updateErr
	}
	if rows == 0 {
		return dictionaryapp.ErrTypeNotFound
	}
	return nil
}

func (r *GORMRepository) ListItems(ctx context.Context, typeCode, tenantID, orgID string, includeDisabled bool) ([]dictionaryapp.DictionaryItem, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("dictionary repository is not initialized")
	}
	query := gorm.G[dictionaryItemRecord](r.db.Read(ctx)).Where("type_code = ? AND deleted_at IS NULL AND (tenant_id = '' OR (tenant_id = ? AND (org_id = '' OR org_id = ?)))", strings.TrimSpace(typeCode), tenantID, orgID)
	if !includeDisabled {
		query = query.Where("status = ?", "active")
	}
	rows, err := query.Order("sort_order ASC, item_value ASC").Find(ctx)
	if err != nil {
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
	query := gorm.G[dictionaryItemRecord](r.db.Read(ctx)).Where("id = ? AND tenant_id = ? AND org_id = ?", strings.TrimSpace(id), tenantID, orgID)
	if typeCode != "" {
		query = query.Where("type_code = ?", typeCode)
	}
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	} else {
		query = query.Where("deleted_at IS NULL")
	}
	row, err := query.First(ctx)
	if err != nil {
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
		records := make([]dictionaryItemRecord, 0, len(rows))
		for _, row := range rows {
			records = append(records, fromItem(row))
		}
		if err := gorm.G[dictionaryItemRecord](tx).CreateInBatches(ctx, &records, 100); err != nil {
			return dictionaryapp.ErrItemConflict
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
	rows, updateErr := gorm.G[dictionaryItemRecord](r.db.Write(ctx)).Where("id = ? AND tenant_id = ? AND org_id = ? AND system_owned = FALSE AND deleted_at IS NULL", row.ID, row.TenantID, row.OrgID).Set(clause.Assignments(map[string]any{"item_value": row.Value, "label_zh_cn": row.LabelZhCN, "label_en_us": row.LabelEnUS, "description": row.Description, "tag": row.Tag, "sort_order": row.SortOrder, "status": row.Status, "updated_at": row.UpdatedAt})).Update(ctx)
	if updateErr != nil {
		return dictionaryapp.DictionaryItem{}, updateErr
	}
	if rows == 0 {
		return dictionaryapp.DictionaryItem{}, dictionaryapp.ErrItemNotFound
	}
	return r.FindItem(ctx, row.ID, row.TenantID, row.OrgID, row.TypeCode, false)
}

func (r *GORMRepository) SoftDeleteItem(ctx context.Context, id, tenantID, orgID string, at time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("dictionary repository is not initialized")
	}
	rows, updateErr := gorm.G[dictionaryItemRecord](r.db.Write(ctx)).Where("id = ? AND tenant_id = ? AND org_id = ? AND system_owned = FALSE AND deleted_at IS NULL", strings.TrimSpace(id), tenantID, orgID).Set(clause.Assignments(map[string]any{"deleted_at": at, "updated_at": at})).Update(ctx)
	if updateErr != nil {
		return updateErr
	}
	if rows == 0 {
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
		row, findErr := gorm.G[cacheVersionRecord](tx).Where("tenant_id = ? AND org_id = ? AND type_code = ? AND deleted_at IS NULL", tenantID, orgID, typeCode).First(ctx)
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			row = cacheVersionRecord{ID: "cache-" + dictionaryapp.NewVersionID(), TenantID: tenantID, OrgID: orgID, TypeCode: typeCode, Version: 1, CreatedAt: at, UpdatedAt: at}
			version = int64(row.Version)
			return gorm.G[cacheVersionRecord](tx).Create(ctx, &row)
		}
		if findErr != nil {
			return findErr
		}
		row.Version++
		row.UpdatedAt = at
		version = int64(row.Version)
		_, updateErr := gorm.G[cacheVersionRecord](tx).Where("id = ?", row.ID).Set(clause.Assignments(map[string]any{"version": row.Version, "updated_at": at})).Update(ctx)
		return updateErr
	})
	return version, err
}

func (r *GORMRepository) CurrentVersion(ctx context.Context, tenantID, orgID, typeCode string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("dictionary repository is not initialized")
	}
	row, err := gorm.G[cacheVersionRecord](r.db.Read(ctx)).Where("tenant_id = ? AND org_id = ? AND type_code = ? AND deleted_at IS NULL", tenantID, orgID, typeCode).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return int64(row.Version), nil
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
	return dictionaryapp.DictionaryType{ID: row.ID, TenantID: row.TenantID, OrgID: row.OrgID, Code: row.Code, NameZhCN: row.NameZhCN, NameEnUS: row.NameEnUS, Description: row.Description, Status: row.Status, SortOrder: int(row.SortOrder), SystemOwned: row.SystemOwned, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt}
}
func fromType(row dictionaryapp.DictionaryType) dictionaryTypeRecord {
	return dictionaryTypeRecord{ID: row.ID, TenantID: row.TenantID, OrgID: row.OrgID, Code: row.Code, NameZhCN: row.NameZhCN, NameEnUS: row.NameEnUS, Description: row.Description, Status: row.Status, SortOrder: int32(row.SortOrder), SystemOwned: row.SystemOwned, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt}
}
func toItem(row dictionaryItemRecord) dictionaryapp.DictionaryItem {
	return dictionaryapp.DictionaryItem{ID: row.ID, TenantID: row.TenantID, OrgID: row.OrgID, TypeCode: row.TypeCode, Value: row.Value, LabelZhCN: row.LabelZhCN, LabelEnUS: row.LabelEnUS, Description: row.Description, Tag: row.Tag, Status: row.Status, SortOrder: int(row.SortOrder), SystemOwned: row.SystemOwned, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt}
}
func fromItem(row dictionaryapp.DictionaryItem) dictionaryItemRecord {
	return dictionaryItemRecord{ID: row.ID, TenantID: row.TenantID, OrgID: row.OrgID, TypeCode: row.TypeCode, Value: row.Value, LabelZhCN: row.LabelZhCN, LabelEnUS: row.LabelEnUS, Description: row.Description, Tag: row.Tag, Status: row.Status, SortOrder: int32(row.SortOrder), SystemOwned: row.SystemOwned, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, DeletedAt: row.DeletedAt}
}

var _ dictionaryapp.Repository = (*GORMRepository)(nil)
