package dictionary

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

// MemoryRepository is used by unit tests and the dependency-free local
// bootstrap. It mirrors the durable scope and uniqueness rules.
type MemoryRepository struct {
	mu       sync.RWMutex
	types    map[string]DictionaryType
	items    map[string]DictionaryItem
	versions map[string]int64
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{types: map[string]DictionaryType{}, items: map[string]DictionaryItem{}, versions: map[string]int64{}}
}

func (r *MemoryRepository) SeedType(row DictionaryType) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if row.Status == "" {
		row.Status = "active"
	}
	if row.ID == "" {
		row.ID = newID("seed-type")
	}
	r.types[row.ID] = cloneType(row)
}

func (r *MemoryRepository) SeedItem(row DictionaryItem) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if row.Status == "" {
		row.Status = "active"
	}
	if row.ID == "" {
		row.ID = newID("seed-item")
	}
	r.items[row.ID] = cloneItem(row)
}

func (r *MemoryRepository) ListTypes(ctx context.Context, tenantID, orgID string, includeDisabled bool) ([]DictionaryType, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := make([]DictionaryType, 0, len(r.types))
	for _, row := range r.types {
		if row.DeletedAt != nil || precedence(row.TenantID, row.OrgID, tenant.Context{TenantID: tenantID, Organization: orgID}) < 0 {
			continue
		}
		if !includeDisabled && row.Status == "disabled" {
			continue
		}
		rows = append(rows, cloneType(row))
	}
	return rows, nil
}

func (r *MemoryRepository) FindType(ctx context.Context, id string) (DictionaryType, error) {
	if err := contextErr(ctx); err != nil {
		return DictionaryType{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	row, ok := r.types[strings.TrimSpace(id)]
	if !ok || row.DeletedAt != nil {
		return DictionaryType{}, ErrTypeNotFound
	}
	return cloneType(row), nil
}

func (r *MemoryRepository) FindTypeByScope(ctx context.Context, code, tenantID, orgID string, includeDeleted bool) (DictionaryType, error) {
	if err := contextErr(ctx); err != nil {
		return DictionaryType{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var chosen DictionaryType
	best := -1
	scope := tenant.Context{TenantID: tenantID, Organization: orgID}
	for _, row := range r.types {
		if row.Code != strings.TrimSpace(code) || (!includeDeleted && row.DeletedAt != nil) {
			continue
		}
		p := precedence(row.TenantID, row.OrgID, scope)
		if p > best {
			chosen, best = row, p
		}
	}
	if best < 0 {
		return DictionaryType{}, ErrTypeNotFound
	}
	return cloneType(chosen), nil
}

func (r *MemoryRepository) CreateType(ctx context.Context, row DictionaryType) (DictionaryType, error) {
	if err := contextErr(ctx); err != nil {
		return DictionaryType{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.types {
		if existing.DeletedAt == nil && existing.TenantID == row.TenantID && existing.OrgID == row.OrgID && strings.EqualFold(existing.Code, row.Code) {
			return DictionaryType{}, ErrTypeConflict
		}
	}
	r.types[row.ID] = cloneType(row)
	return cloneType(row), nil
}

func (r *MemoryRepository) UpdateType(ctx context.Context, row DictionaryType) (DictionaryType, error) {
	if err := contextErr(ctx); err != nil {
		return DictionaryType{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.types[row.ID]; !ok || existing.DeletedAt != nil {
		return DictionaryType{}, ErrTypeNotFound
	}
	for id, existing := range r.types {
		if id == row.ID || existing.DeletedAt != nil {
			continue
		}
		if existing.TenantID == row.TenantID && existing.OrgID == row.OrgID && strings.EqualFold(existing.Code, row.Code) {
			return DictionaryType{}, ErrTypeConflict
		}
	}
	r.types[row.ID] = cloneType(row)
	return cloneType(row), nil
}

func (r *MemoryRepository) SoftDeleteType(ctx context.Context, id, tenantID, orgID string, at time.Time) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.types[strings.TrimSpace(id)]
	if !ok || row.DeletedAt != nil || row.TenantID != tenantID || row.OrgID != orgID {
		return ErrTypeNotFound
	}
	row.DeletedAt, row.UpdatedAt = &at, at
	r.types[row.ID] = row
	return nil
}

func (r *MemoryRepository) ListItems(ctx context.Context, typeCode, tenantID, orgID string, includeDisabled bool) ([]DictionaryItem, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	scope := tenant.Context{TenantID: tenantID, Organization: orgID}
	rows := make([]DictionaryItem, 0, len(r.items))
	for _, row := range r.items {
		if row.TypeCode != strings.TrimSpace(typeCode) || row.DeletedAt != nil || precedence(row.TenantID, row.OrgID, scope) < 0 {
			continue
		}
		if !includeDisabled && row.Status == "disabled" {
			continue
		}
		rows = append(rows, cloneItem(row))
	}
	return rows, nil
}

func (r *MemoryRepository) FindItem(ctx context.Context, id, tenantID, orgID, typeCode string, includeDeleted bool) (DictionaryItem, error) {
	if err := contextErr(ctx); err != nil {
		return DictionaryItem{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	row, ok := r.items[strings.TrimSpace(id)]
	if !ok || (!includeDeleted && row.DeletedAt != nil) || row.DeletedAt != nil || row.TenantID != tenantID || row.OrgID != orgID || typeCode != "" && row.TypeCode != typeCode {
		return DictionaryItem{}, ErrItemNotFound
	}
	return cloneItem(row), nil
}

func (r *MemoryRepository) CreateItems(ctx context.Context, rows []DictionaryItem) ([]DictionaryItem, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrInvalidItem
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		key := scopeItemKey(row.TenantID, row.OrgID, row.TypeCode, row.Value)
		if _, ok := seen[key]; ok {
			return nil, ErrItemConflict
		}
		seen[key] = struct{}{}
		for _, existing := range r.items {
			if existing.DeletedAt == nil && scopeItemKey(existing.TenantID, existing.OrgID, existing.TypeCode, existing.Value) == key {
				return nil, ErrItemConflict
			}
		}
	}
	for _, row := range rows {
		r.items[row.ID] = cloneItem(row)
	}
	result := make([]DictionaryItem, len(rows))
	for i, row := range rows {
		result[i] = cloneItem(row)
	}
	return result, nil
}

func (r *MemoryRepository) UpdateItem(ctx context.Context, row DictionaryItem) (DictionaryItem, error) {
	if err := contextErr(ctx); err != nil {
		return DictionaryItem{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.items[row.ID]
	if !ok || existing.DeletedAt != nil {
		return DictionaryItem{}, ErrItemNotFound
	}
	for id, candidate := range r.items {
		if id == row.ID || candidate.DeletedAt != nil {
			continue
		}
		if scopeItemKey(candidate.TenantID, candidate.OrgID, candidate.TypeCode, candidate.Value) == scopeItemKey(row.TenantID, row.OrgID, row.TypeCode, row.Value) {
			return DictionaryItem{}, ErrItemConflict
		}
	}
	r.items[row.ID] = cloneItem(row)
	return cloneItem(row), nil
}

func (r *MemoryRepository) SoftDeleteItem(ctx context.Context, id, tenantID, orgID string, at time.Time) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.items[strings.TrimSpace(id)]
	if !ok || row.DeletedAt != nil || row.TenantID != tenantID || row.OrgID != orgID {
		return ErrItemNotFound
	}
	row.DeletedAt, row.UpdatedAt = &at, at
	r.items[row.ID] = row
	return nil
}

func (r *MemoryRepository) BumpVersion(ctx context.Context, tenantID, orgID, typeCode string, at time.Time) (int64, error) {
	if err := contextErr(ctx); err != nil {
		return 0, err
	}
	if at.IsZero() {
		return r.CurrentVersion(ctx, tenantID, orgID, typeCode)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := scopeVersionKey(tenantID, orgID, typeCode)
	r.versions[key]++
	return r.versions[key], nil
}

func (r *MemoryRepository) CurrentVersion(ctx context.Context, tenantID, orgID, typeCode string) (int64, error) {
	if err := contextErr(ctx); err != nil {
		return 0, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.versions[scopeVersionKey(tenantID, orgID, typeCode)], nil
}

type MemoryAuditSink struct {
	mu     sync.Mutex
	Events []AuditEvent
}

func (s *MemoryAuditSink) Record(ctx context.Context, event AuditEvent) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Events = append(s.Events, event)
	return nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return context.Canceled
	}
	return ctx.Err()
}
func scopeItemKey(tenantID, orgID, typeCode, value string) string {
	return tenantID + "\x00" + orgID + "\x00" + typeCode + "\x00" + value
}
func scopeVersionKey(tenantID, orgID, typeCode string) string {
	return tenantID + "\x00" + orgID + "\x00" + typeCode
}
func cloneType(row DictionaryType) DictionaryType {
	if row.DeletedAt != nil {
		value := *row.DeletedAt
		row.DeletedAt = &value
	}
	return row
}
func cloneItem(row DictionaryItem) DictionaryItem {
	if row.DeletedAt != nil {
		value := *row.DeletedAt
		row.DeletedAt = &value
	}
	return row
}

var _ Repository = (*MemoryRepository)(nil)
