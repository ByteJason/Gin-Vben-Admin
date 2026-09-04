package fileplatform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	fileapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/file"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	model "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GORMRepository is the durable file_objects authority used by production
// file operations. Object bytes remain in the provider; every metadata query
// (list/get/status) is resolved from this repository.
type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

func (r *GORMRepository) Create(ctx context.Context, f fileapp.File) error {
	if r == nil || r.db == nil {
		return errors.New("file repository is not initialized")
	}
	status := string(f.Status)
	if status == "" {
		status = string(fileapp.MediaPending)
	}
	now := f.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	name := f.Name
	ext := f.Extension
	objectKey := f.ObjectKey
	if objectKey == "" {
		objectKey = f.Key
	}
	providerID := f.ProviderID
	if providerID == "" {
		providerID = "local"
	}
	scopeType := "system"
	if f.TenantID != "" {
		scopeType = "tenant"
	}
	if f.OrgID != "" {
		scopeType = "org"
	}
	scanStatus := f.ScanStatus
	if scanStatus == "" {
		scanStatus = "pending"
	}
	metadata := []byte("{}")
	if f.Metadata != nil {
		if encoded, marshalErr := json.Marshal(f.Metadata); marshalErr == nil {
			metadata = encoded
		}
	}
	row := model.FileObject{ID: f.ID, TenantID: f.TenantID, ProviderID: providerID, Bucket: f.Bucket, ScopeType: scopeType, LifecycleStatus: status, MetadataJSON: model.JSONValue(metadata), ObjectKey: objectKey, Name: name, OriginalExtension: ext, MIME: f.MIME, Size: f.Size, OwnerID: f.OwnerID, ACL: string(f.ACL), ScanStatus: scanStatus, CreatedAt: now, UpdatedAt: now, PendingAt: &now}
	if f.MIME != "" {
		detected := f.MIME
		row.DetectedMIME = &detected
	}
	if f.OrgID != "" {
		row.OrgID = &f.OrgID
	}
	if f.CategoryID != "" {
		row.CategoryID = &f.CategoryID
	}
	if f.SHA256 != "" {
		row.SHA256 = &f.SHA256
	}
	if f.ETag != "" {
		row.ETag = &f.ETag
	}
	if f.FailureReason != "" {
		row.FailureReason = &f.FailureReason
	}
	return r.db.Write(ctx).Create(&row).Error
}

func (r *GORMRepository) Get(ctx context.Context, id string) (fileapp.File, error) {
	if r == nil || r.db == nil {
		return fileapp.File{}, fileapp.ErrFileNotFound
	}
	var row model.FileObject
	err := r.db.Write(ctx).Where("id = ?", strings.TrimSpace(id)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fileapp.File{}, fileapp.ErrFileNotFound
	}
	if err != nil {
		return fileapp.File{}, err
	}
	return toFile(row), nil
}

func (r *GORMRepository) List(ctx context.Context, filter fileapp.ListFilter) (fileapp.Page, error) {
	if r == nil || r.db == nil {
		return fileapp.Page{}, fileapp.ErrFileNotFound
	}
	// lifecycle_status is the pre-existing status column. Keep the query
	// compatible with rows written before the canonical status vocabulary was
	// introduced; soft-deleted rows are hidden, while damaged rows remain
	// visible for reconciliation and operator repair.
	q := r.db.Write(ctx).Model(&model.FileObject{}).Where("(lifecycle_status IS NULL OR lifecycle_status <> ?)", string(fileapp.MediaDeleted))
	if filter.TenantID != "" {
		q = q.Where("tenant_id = ?", filter.TenantID)
	}
	if filter.OrgID != "" {
		q = q.Where("(org_id = ? OR org_id IS NULL)", filter.OrgID)
	}
	if filter.OwnerID != "" {
		q = q.Where("owner_id = ?", filter.OwnerID)
	}
	if filter.CategoryID != "" {
		q = q.Where("category_id = ?", filter.CategoryID)
	}
	if filter.Status != "" {
		q = q.Where("lifecycle_status = ?", string(filter.Status))
	}
	if filter.MIME != "" {
		q = q.Where("COALESCE(detected_mime, mime) = ?", filter.MIME)
	}
	if filter.MIMEFamily != "" {
		family := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(filter.MIMEFamily)), "*") + "%"
		q = q.Where("LOWER(COALESCE(detected_mime, mime)) LIKE ?", family)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return fileapp.Page{}, err
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	var rows []model.FileObject
	if err := q.Order("created_at DESC").Order("id DESC").Find(&rows).Error; err != nil {
		return fileapp.Page{}, err
	}
	items := make([]fileapp.File, 0, len(rows))
	for _, row := range rows {
		items = append(items, toFile(row))
	}
	return fileapp.Page{Items: items, Total: int(total), Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (r *GORMRepository) ListByStatus(ctx context.Context, status fileapp.MediaStatus, limit int) ([]fileapp.File, error) {
	if r == nil || r.db == nil {
		return nil, fileapp.ErrFileNotFound
	}
	q := r.db.Write(ctx).Where("lifecycle_status = ?", string(status)).Limit(limit)
	var rows []model.FileObject
	if err := q.Order("updated_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]fileapp.File, 0, len(rows))
	for _, row := range rows {
		items = append(items, toFile(row))
	}
	return items, nil
}

func (r *GORMRepository) CountByResource(ctx context.Context, resourceID string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, fileapp.ErrFileNotFound
	}
	var count int64
	// Deletion decisions must observe the primary immediately after a usage
	// attach; a lagging replica could otherwise allow a referenced object to be
	// marked deleting.
	err := r.db.Write(ctx).Model(&model.MediaUsage{}).Where("resource_id = ? AND deleted_at IS NULL", resourceID).Count(&count).Error
	return count, err
}

func (r *GORMRepository) RequestDeletion(ctx context.Context, id string, force bool, at time.Time) error {
	if r == nil || r.db == nil {
		return fileapp.ErrFileNotFound
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fileapp.ErrFileNotFound
	}
	return r.db.Write(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.FileObject
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fileapp.ErrFileNotFound
		}
		if err != nil {
			return err
		}
		status := fileapp.MediaStatus(row.LifecycleStatus)
		if status == fileapp.MediaDeleted {
			return fileapp.ErrFileNotFound
		}
		if status == fileapp.MediaDeleting {
			return nil
		}
		if !force {
			var count int64
			if err := tx.Model(&model.MediaUsage{}).Where("resource_id = ? AND deleted_at IS NULL", id).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return fileapp.ErrMediaInUse
			}
		}
		at = at.UTC()
		if at.IsZero() {
			at = time.Now().UTC()
		}
		result := tx.Model(&model.FileObject{}).
			Where("id = ? AND lifecycle_status <> ?", id, string(fileapp.MediaDeleted)).
			Updates(map[string]any{"lifecycle_status": string(fileapp.MediaDeleting), "failure_reason": nil, "deleted_at": &at, "updated_at": at})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fileapp.ErrFileNotFound
		}
		return nil
	})
}

func (r *GORMRepository) Update(ctx context.Context, f fileapp.File) error {
	if r == nil || r.db == nil {
		return fileapp.ErrFileNotFound
	}
	var current model.FileObject
	err := r.db.Write(ctx).Select("id", "object_key", "lifecycle_status").Where("id = ?", f.ID).First(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fileapp.ErrFileNotFound
	}
	if err != nil {
		return err
	}
	status := string(f.Status)
	if status == "" {
		status = current.LifecycleStatus
	}
	if status == "" {
		status = string(fileapp.MediaReady)
	}
	objectKey := f.ObjectKey
	if objectKey == "" {
		objectKey = f.Key
	}
	if objectKey == "" {
		objectKey = current.ObjectKey
	}
	if current.LifecycleStatus != string(fileapp.MediaPending) && current.ObjectKey != "" && objectKey != current.ObjectKey {
		return fileapp.ErrInvalidUpload
	}
	if !repositoryUpdateTransition(fileapp.MediaStatus(current.LifecycleStatus), fileapp.MediaStatus(status)) {
		return fileapp.ErrInvalidUpload
	}
	updatedAt := f.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	updates := map[string]any{"object_key": objectKey, "name": f.Name, "mime": f.MIME, "detected_mime": f.MIME, "size": f.Size, "original_extension": f.Extension, "owner_id": f.OwnerID, "acl": string(f.ACL), "lifecycle_status": status, "updated_at": updatedAt}
	if f.Metadata != nil {
		if encoded, marshalErr := json.Marshal(f.Metadata); marshalErr == nil {
			updates["metadata_json"] = encoded
		}
	}
	if f.ProviderID != "" {
		updates["provider_id"] = f.ProviderID
	}
	if f.Bucket != "" {
		updates["bucket"] = f.Bucket
	}
	if f.SHA256 != "" {
		updates["sha256"] = f.SHA256
	}
	if f.ETag != "" {
		updates["etag"] = f.ETag
	}
	if f.ScanStatus != "" {
		updates["scan_status"] = f.ScanStatus
	}
	if f.FailureReason != "" {
		updates["failure_reason"] = f.FailureReason
	}
	if f.CategoryID != "" {
		updates["category_id"] = f.CategoryID
	} else {
		updates["category_id"] = nil
	}
	if status == string(fileapp.MediaReady) {
		updates["ready_at"] = updatedAt
	}
	if status == string(fileapp.MediaPending) {
		updates["pending_at"] = updatedAt
	}
	if f.DeletedAt != nil {
		updates["deleted_at"] = f.DeletedAt
	}
	result := r.db.Write(ctx).Model(&model.FileObject{}).Where("id = ?", f.ID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fileapp.ErrFileNotFound
	}
	return nil
}

func repositoryUpdateTransition(from, to fileapp.MediaStatus) bool {
	if from == "" || from == to {
		return true
	}
	return from == fileapp.MediaPending && to == fileapp.MediaReady
}

func (r *GORMRepository) MarkStatus(ctx context.Context, id string, status fileapp.MediaStatus, reason string, deletedAt *time.Time) error {
	if r == nil || r.db == nil {
		return fileapp.ErrFileNotFound
	}
	if !fileapp.ValidMediaStatus(status) {
		return fileapp.ErrInvalidUpload
	}
	var current model.FileObject
	err := r.db.Write(ctx).Select("id", "lifecycle_status").Where("id = ?", id).First(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fileapp.ErrFileNotFound
	}
	if err != nil {
		return err
	}
	from := fileapp.MediaStatus(current.LifecycleStatus)
	if !repositoryStatusTransition(from, status) {
		return fileapp.ErrInvalidUpload
	}
	updates := map[string]any{"lifecycle_status": string(status), "updated_at": time.Now().UTC()}
	if reason != "" {
		updates["failure_reason"] = reason
	} else if status == fileapp.MediaReady || status == fileapp.MediaDeleted {
		updates["failure_reason"] = nil
	}
	if status == fileapp.MediaReady {
		updates["ready_at"] = updates["updated_at"]
	}
	if deletedAt != nil {
		updates["deleted_at"] = deletedAt
	}
	result := r.db.Write(ctx).Model(&model.FileObject{}).Where("id = ? AND lifecycle_status = ?", id, current.LifecycleStatus).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var latest model.FileObject
		if latestErr := r.db.Write(ctx).Select("lifecycle_status").Where("id = ?", id).First(&latest).Error; errors.Is(latestErr, gorm.ErrRecordNotFound) {
			return fileapp.ErrFileNotFound
		} else if latestErr == nil && latest.LifecycleStatus == string(status) {
			return nil
		}
		return fileapp.ErrInvalidUpload
	}
	return nil
}

func repositoryStatusTransition(from, to fileapp.MediaStatus) bool {
	if from == "" || from == to {
		return true
	}
	switch from {
	case fileapp.MediaPending:
		return to == fileapp.MediaReady || to == fileapp.MediaFailed || to == fileapp.MediaDeleting
	case fileapp.MediaReady:
		return to == fileapp.MediaDamaged || to == fileapp.MediaDeleting
	case fileapp.MediaFailed, fileapp.MediaDamaged:
		return to == fileapp.MediaPending || to == fileapp.MediaDeleting
	case fileapp.MediaDeleting:
		return to == fileapp.MediaDeleted
	default:
		return false
	}
}

func toFile(row model.FileObject) fileapp.File {
	name := row.Name
	ext := row.OriginalExtension
	mimeType := row.MIME
	if row.DetectedMIME != nil && strings.TrimSpace(*row.DetectedMIME) != "" {
		mimeType = *row.DetectedMIME
	}
	size := row.Size
	status := row.LifecycleStatus
	if status == "" {
		status = string(fileapp.MediaReady)
	}
	var org, cat, sha, etag, failure string
	metadata := map[string]string{}
	if len(row.MetadataJSON) > 0 {
		_ = json.Unmarshal([]byte(row.MetadataJSON), &metadata)
	}
	if row.OrgID != nil {
		org = *row.OrgID
	}
	if row.CategoryID != nil {
		cat = *row.CategoryID
	}
	if row.SHA256 != nil {
		sha = *row.SHA256
	}
	if row.ETag != nil {
		etag = *row.ETag
	}
	if row.FailureReason != nil {
		failure = *row.FailureReason
	}
	return fileapp.File{ID: row.ID, Key: row.ID, ObjectKey: row.ObjectKey, ProviderID: row.ProviderID, Bucket: row.Bucket, Name: name, MIME: mimeType, Size: size, OwnerID: row.OwnerID, TenantID: row.TenantID, OrgID: org, ACL: fileapp.ACL(row.ACL), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, SHA256: sha, CategoryID: cat, Metadata: metadata, Extension: ext, ETag: etag, Status: fileapp.MediaStatus(status), ScanStatus: row.ScanStatus, FailureReason: failure, DeletedAt: row.DeletedAt}
}

var _ fileapp.FileRepository = (*GORMRepository)(nil)
var _ fileapp.StatusRepository = (*GORMRepository)(nil)
var _ fileapp.UsageRepository = (*GORMRepository)(nil)
var _ fileapp.DeletionRepository = (*GORMRepository)(nil)
