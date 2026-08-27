// Package organizationplatform persists tenant-owned organization trees.
package organizationplatform

import (
	"context"
	"errors"
	"strings"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/organization"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
)

var ErrStoreUnavailable = errors.New("organization persistence store is unavailable")

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

type organizationRow struct {
	ID       string  `gorm:"column:id;primaryKey"`
	TenantID string  `gorm:"column:tenant_id"`
	ParentID *string `gorm:"column:parent_id"`
	Name     string  `gorm:"column:name"`
	Status   string  `gorm:"column:status"`
}

func (organizationRow) TableName() string { return "organizations" }

func (r *GORMRepository) Create(ctx context.Context, value organization.Organization) error {
	if err := organization.Validate(value); err != nil {
		return err
	}
	scope, err := organization.RequireTenant(ctx, value)
	if err != nil {
		return err
	}
	if r == nil || r.db == nil {
		return ErrStoreUnavailable
	}
	if value.Status == "" {
		value.Status = "active"
	}
	if value.ParentID != "" {
		_, err := gorm.G[organizationRow](r.db.Read(ctx)).Where("tenant_id = ? AND id = ?", scope.TenantID, value.ParentID).Take(ctx)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return organization.ErrOrganizationNotFound
			}
			return ErrStoreUnavailable
		}
	}
	row := organizationRow{ID: value.ID, TenantID: scope.TenantID, ParentID: nullable(value.ParentID), Name: value.Name, Status: value.Status}
	if err := gorm.G[organizationRow](r.db.Write(ctx)).Create(ctx, &row); err != nil {
		return ErrStoreUnavailable
	}
	return nil
}

func (r *GORMRepository) Get(ctx context.Context, id string) (organization.Organization, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return organization.Organization{}, err
	}
	if r == nil || r.db == nil {
		return organization.Organization{}, ErrStoreUnavailable
	}
	var row organizationRow
	if row, err = gorm.G[organizationRow](r.db.Read(ctx)).Where("tenant_id = ? AND id = ?", scope.TenantID, strings.TrimSpace(id)).Take(ctx); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return organization.Organization{}, organization.ErrOrganizationNotFound
		}
		return organization.Organization{}, ErrStoreUnavailable
	}
	return toDomain(row), nil
}

func (r *GORMRepository) List(ctx context.Context, parentID string) ([]organization.Organization, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if r == nil || r.db == nil {
		return nil, ErrStoreUnavailable
	}
	query := gorm.G[organizationRow](r.db.Read(ctx)).Where("tenant_id = ?", scope.TenantID).Order("name ASC")
	if strings.TrimSpace(parentID) == "" {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", strings.TrimSpace(parentID))
	}
	rows, err := query.Find(ctx)
	if err != nil {
		return nil, ErrStoreUnavailable
	}
	out := make([]organization.Organization, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomain(row))
	}
	return out, nil
}

func nullable(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	value = strings.TrimSpace(value)
	return &value
}

func toDomain(row organizationRow) organization.Organization {
	parent := ""
	if row.ParentID != nil {
		parent = *row.ParentID
	}
	return organization.Organization{ID: row.ID, TenantID: row.TenantID, ParentID: parent, Name: row.Name, Status: row.Status}
}

var _ organization.Repository = (*GORMRepository)(nil)
