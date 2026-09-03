package file

import (
	"context"
	"io"
	"time"
)

type ResourceID = string
type CategoryID = string
type URLPurpose string
type ScopeType string
type MediaStatus string

const (
	URLPurposePreview  URLPurpose  = "preview"
	URLPurposeDownload URLPurpose  = "download"
	ScopeSystem        ScopeType   = "system"
	ScopeTenant        ScopeType   = "tenant"
	ScopeOrg           ScopeType   = "org"
	MediaPending       MediaStatus = "pending"
	MediaReady         MediaStatus = "ready"
	MediaFailed        MediaStatus = "failed"
	MediaDeleting      MediaStatus = "deleting"
	MediaDeleted       MediaStatus = "deleted"
)

type UploadInput struct {
	// Reader is the port form. Data and scope fields remain for compatibility
	// with the legacy HTTP/application service during migration.
	Reader         io.Reader
	Data           []byte
	Size           int64
	Name           string
	MIME           string
	OwnerID        string
	TenantID       string
	OrgID          string
	ACL            ACL
	CategoryID     CategoryID
	Metadata       map[string]string
	IdempotencyKey string
}
type OpenOptions struct{ RangeStart, RangeEnd *int64 }
type URLRequest struct {
	Purpose URLPurpose
	TTL     time.Duration
}
type URLRef struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type DeleteOptions struct{ Reason, IdempotencyKey string }

// ResourcePatch changes metadata that is safe to edit after upload. A nil
// pointer leaves the field unchanged; system-scoped resources remain read-only
// in the catalog adapter.
type ResourcePatch struct {
	Name           *string
	CategoryID     *CategoryID
	Status         *MediaStatus
	Metadata       map[string]string
	IdempotencyKey string
}
type MediaFilter struct {
	MIMEExact, MIMEFamily string
	CategoryID            CategoryID
	ScopeType             ScopeType
	IncludeDescendants    bool
	OwnerID, Cursor       string
	// Offset is retained for the legacy management clients. New callers should
	// prefer Cursor; when both are supplied, the opaque cursor wins so a retry
	// cannot silently jump to a different page.
	Offset int
	Limit  int
	Status MediaStatus
}
type ResourceRef struct {
	ID             ResourceID        `json:"id"`
	Name           string            `json:"name"`
	MIME           string            `json:"mime"`
	Size           int64             `json:"size"`
	SHA256         string            `json:"sha256"`
	CategoryID     CategoryID        `json:"categoryId,omitempty"`
	ScopeType      ScopeType         `json:"scopeType"`
	ACL            ACL               `json:"acl"`
	Status         MediaStatus       `json:"status"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
	URLHints       map[string]bool   `json:"urlHints,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Selectable     bool              `json:"selectable"`
	DisabledReason string            `json:"disabledReason,omitempty"`
	ReconcileKey   string            `json:"reconcileKey,omitempty"`
}
type MediaPage struct {
	Items      []ResourceRef `json:"items"`
	Total      int           `json:"total"`
	Limit      int           `json:"limit"`
	Offset     int           `json:"offset"`
	NextCursor string        `json:"nextCursor,omitempty"`
	HasMore    bool          `json:"hasMore"`
}
type CategoryFilter struct {
	ParentID           CategoryID
	ScopeType          ScopeType
	IncludeDescendants bool
}
type CategoryRef struct {
	ID        CategoryID `json:"id"`
	Name      string     `json:"name"`
	Path      string     `json:"path"`
	ScopeType ScopeType  `json:"scopeType"`
	Enabled   bool       `json:"enabled"`
}
type CategoryInput struct {
	ParentID       CategoryID `json:"parentId,omitempty"`
	Name           string     `json:"name"`
	TenantID       string     `json:"tenantId,omitempty"`
	OrgID          string     `json:"orgId,omitempty"`
	Enabled        *bool      `json:"enabled,omitempty"`
	IdempotencyKey string     `json:"-"`
}
type CategoryPatch struct {
	Name           *string `json:"name"`
	Enabled        *bool   `json:"enabled"`
	IdempotencyKey string  `json:"-"`
}
type CategoryDeleteRequest struct {
	ID             CategoryID
	IdempotencyKey string
}
type UsageRef struct {
	ID         string     `json:"id"`
	ResourceID ResourceID `json:"resourceId"`
	Module     string     `json:"module"`
	EntityType string     `json:"entityType"`
	EntityID   string     `json:"entityId"`
	Field      string     `json:"field"`
}
type UsageInput struct {
	ResourceID                                          ResourceID
	Module, EntityType, EntityID, Field, IdempotencyKey string
}
type DetachRequest struct{ UsageID, IdempotencyKey string }

type MediaCatalog interface {
	Upload(context.Context, UploadInput) (ResourceRef, error)
	Get(context.Context, ResourceID) (ResourceRef, error)
	List(context.Context, MediaFilter) (MediaPage, error)
	Open(context.Context, ResourceID, OpenOptions) (io.ReadCloser, error)
	SignedURL(context.Context, ResourceID, URLRequest) (URLRef, error)
	Delete(context.Context, ResourceID, DeleteOptions) error
	ListCategories(context.Context, CategoryFilter) ([]CategoryRef, error)
	CreateCategory(context.Context, CategoryInput) (CategoryRef, error)
	UpdateCategory(context.Context, CategoryID, CategoryPatch) (CategoryRef, error)
	DeleteCategory(context.Context, CategoryDeleteRequest) error
}

type MediaUsageService interface {
	Attach(context.Context, UsageInput) (UsageRef, error)
	Detach(context.Context, DetachRequest) error
	ListByResource(context.Context, ResourceID) ([]UsageRef, error)
}
