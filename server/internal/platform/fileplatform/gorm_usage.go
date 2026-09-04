package fileplatform

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	fileapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/file"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	model "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GORMUsageService struct {
	db    *gormdb.Store
	files fileapp.FileRepository
}

func NewGORMUsageService(db *gormdb.Store, files fileapp.FileRepository) *GORMUsageService {
	return &GORMUsageService{db: db, files: files}
}

func (u *GORMUsageService) Attach(ctx context.Context, in fileapp.UsageInput) (fileapp.UsageRef, error) {
	if u == nil || u.db == nil || strings.TrimSpace(string(in.ResourceID)) == "" || strings.TrimSpace(in.Module) == "" || strings.TrimSpace(in.EntityType) == "" || strings.TrimSpace(in.EntityID) == "" || strings.TrimSpace(in.Field) == "" {
		return fileapp.UsageRef{}, fileapp.ErrInvalidUsage
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return fileapp.UsageRef{}, err
	}
	idBytes := make([]byte, 16)
	if _, err = crand.Read(idBytes); err != nil {
		return fileapp.UsageRef{}, err
	}
	id := hex.EncodeToString(idBytes)
	now := time.Now().UTC()
	row := model.MediaUsage{ID: id, ResourceID: string(in.ResourceID), Module: strings.TrimSpace(in.Module), EntityType: strings.TrimSpace(in.EntityType), EntityID: strings.TrimSpace(in.EntityID), Field: strings.TrimSpace(in.Field), CallerKey: strings.TrimSpace(in.Module), ScopeType: "system", CreatedAt: now, UpdatedAt: now}
	err = u.db.Write(ctx).Transaction(func(tx *gorm.DB) error {
		// Deletion takes the same row lock before counting usages. Holding it
		// through the usage insert makes attach-vs-delete deterministic.
		var object model.FileObject
		getErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", string(in.ResourceID)).First(&object).Error
		if errors.Is(getErr, gorm.ErrRecordNotFound) {
			return fileapp.ErrFileNotFound
		}
		if getErr != nil {
			return getErr
		}
		if object.LifecycleStatus == string(fileapp.MediaDeleted) {
			return fileapp.ErrFileNotFound
		}
		if object.LifecycleStatus != "" && object.LifecycleStatus != string(fileapp.MediaReady) {
			return fileapp.ErrMediaNotReady
		}
		objectOrg := ""
		if object.OrgID != nil {
			objectOrg = *object.OrgID
		}
		if !scope.PlatformAdmin && (object.TenantID != "" && object.TenantID != scope.TenantID || objectOrg != "" && objectOrg != scope.Organization) {
			return fileapp.ErrAccessDenied
		}
		if object.TenantID != "" {
			tenantID := object.TenantID
			row.TenantID = &tenantID
			row.ScopeType = "tenant"
		}
		if objectOrg != "" {
			orgID := objectOrg
			row.OrgID = &orgID
			row.ScopeType = "org"
		}
		var existing model.MediaUsage
		find := tx.Where("resource_id = ? AND caller_key = ? AND module = ? AND entity_type = ? AND entity_id = ? AND field = ?", row.ResourceID, row.CallerKey, row.Module, row.EntityType, row.EntityID, row.Field).First(&existing).Error
		if find == nil {
			row.ID = existing.ID
			if existing.DeletedAt == nil {
				return nil
			}
			return tx.Model(&model.MediaUsage{}).Where("id = ?", existing.ID).Updates(map[string]any{"deleted_at": nil, "updated_at": now}).Error
		}
		if !errors.Is(find, gorm.ErrRecordNotFound) {
			return find
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		return fileapp.UsageRef{}, err
	}
	return fileapp.UsageRef{ID: row.ID, ResourceID: in.ResourceID, Module: row.Module, EntityType: row.EntityType, EntityID: row.EntityID, Field: row.Field}, nil
}
func (u *GORMUsageService) Detach(ctx context.Context, req fileapp.DetachRequest) error {
	if u == nil || u.db == nil {
		return fileapp.ErrUsageNotFound
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	q := u.db.Write(ctx).Model(&model.MediaUsage{}).Where("id = ? AND deleted_at IS NULL", strings.TrimSpace(req.UsageID))
	if !scope.PlatformAdmin {
		q = q.Where("tenant_id = ?", scope.TenantID)
		if scope.Organization != "" {
			q = q.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
		}
	}
	res := q.Updates(map[string]any{"deleted_at": &now, "updated_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fileapp.ErrUsageNotFound
	}
	return nil
}
func (u *GORMUsageService) ListByResource(ctx context.Context, id fileapp.ResourceID) ([]fileapp.UsageRef, error) {
	if u == nil || u.db == nil {
		return nil, fileapp.ErrUsageNotFound
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	q := u.db.Write(ctx).Where("resource_id = ? AND deleted_at IS NULL", string(id))
	if !scope.PlatformAdmin {
		q = q.Where("tenant_id = ?", scope.TenantID)
		if scope.Organization != "" {
			q = q.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
		}
	}
	var rows []model.MediaUsage
	if err := q.Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]fileapp.UsageRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, fileapp.UsageRef{ID: row.ID, ResourceID: row.ResourceID, Module: row.Module, EntityType: row.EntityType, EntityID: row.EntityID, Field: row.Field})
	}
	return out, nil
}

var _ fileapp.MediaUsageService = (*GORMUsageService)(nil)
