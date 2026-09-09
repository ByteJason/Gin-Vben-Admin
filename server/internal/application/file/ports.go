package file

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"time"
)

type ResourceID = string
type CategoryID = string
type URLPurpose string
type ScopeType string
type MediaStatus string
type MediaSelectionRole string

const (
	URLPurposePreview  URLPurpose         = "preview"
	URLPurposeDownload URLPurpose         = "download"
	ScopeSystem        ScopeType          = "system"
	ScopeTenant        ScopeType          = "tenant"
	ScopeOrg           ScopeType          = "org"
	MediaPending       MediaStatus        = "pending"
	MediaReady         MediaStatus        = "ready"
	MediaFailed        MediaStatus        = "failed"
	MediaDeleting      MediaStatus        = "deleting"
	MediaDeleted       MediaStatus        = "deleted"
	MediaDamaged       MediaStatus        = "damaged"
	MediaRoleCover     MediaSelectionRole = "cover"
	MediaRoleGallery   MediaSelectionRole = "gallery"
)

// MediaSelection is the provider-neutral value passed from a picker to a
// business use case. Preview URLs intentionally do not belong here: callers
// persist the resource ID and resolve a short-lived URL only at read time.
type MediaSelection struct {
	ResourceID ResourceID
	SortOrder  int
	Role       MediaSelectionRole
}

// NormalizeMediaSelections validates and canonicalizes an ordered selection
// before a business module persists it. Ordering ties are stable, IDs are
// unique, sortOrder is rewritten to a contiguous zero-based sequence, and at
// most one explicit cover is accepted. The function does not infer a cover;
// choosing one is a product decision owned by the caller.
func NormalizeMediaSelections(input []MediaSelection) ([]MediaSelection, error) {
	if len(input) == 0 {
		return []MediaSelection{}, nil
	}
	items := append([]MediaSelection(nil), input...)
	seen := make(map[string]struct{}, len(items))
	coverCount := 0
	for i := range items {
		items[i].ResourceID = strings.TrimSpace(items[i].ResourceID)
		if items[i].ResourceID == "" {
			return nil, errors.New("media selection resource id is required")
		}
		if items[i].SortOrder < 0 {
			return nil, errors.New("media selection sort order must be non-negative")
		}
		if _, exists := seen[items[i].ResourceID]; exists {
			return nil, errors.New("media selection resource id must be unique")
		}
		seen[items[i].ResourceID] = struct{}{}
		switch items[i].Role {
		case "", MediaRoleGallery:
			if items[i].Role == "" {
				items[i].Role = MediaRoleGallery
			}
		case MediaRoleCover:
			coverCount++
		default:
			return nil, errors.New("media selection role is invalid")
		}
	}
	if coverCount > 1 {
		return nil, errors.New("media selection allows only one cover")
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].SortOrder < items[j].SortOrder })
	for i := range items {
		items[i].SortOrder = i
	}
	return items, nil
}

func ValidMediaStatus(status MediaStatus) bool {
	switch status {
	case MediaPending, MediaReady, MediaFailed, MediaDeleting, MediaDeleted, MediaDamaged:
		return true
	default:
		return false
	}
}

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

// URLImportResult is the per-item outcome of a remote media import. Partial
// success is intentional: callers can retry only failed rows without
// duplicating successful resources.
type URLImportResult struct {
	Index     int          `json:"index"`
	SourceURL string       `json:"sourceUrl"`
	Name      string       `json:"name,omitempty"`
	Resource  *ResourceRef `json:"resource,omitempty"`
	Success   bool         `json:"success"`
	Error     string       `json:"error,omitempty"`
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
type DeleteOptions struct {
	Reason, IdempotencyKey string
	// Force is accepted only with the exact confirmation phrase below. This
	// keeps the safe default (409 when media_usages still reference a file)
	// while making an intentional destructive action explicit.
	Force        bool
	Confirmation string
}

const ForceDeleteConfirmation = "I confirm force delete"

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
	ObjectKey      string            `json:"-"`
	Extension      string            `json:"extension,omitempty"`
	ETag           string            `json:"etag,omitempty"`
	FailureReason  string            `json:"failureReason,omitempty"`
	ScanStatus     string            `json:"scanStatus,omitempty"`
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

// MediaURLImporter is an optional capability implemented by catalog adapters
// that support server-side remote ingestion. Keeping it separate preserves the
// provider-neutral catalog port for stores that intentionally disable remote
// fetching.
type MediaURLImporter interface {
	ImportURLs(context.Context, []string, string, string, string) []URLImportResult
}

type MediaUsageService interface {
	Attach(context.Context, UsageInput) (UsageRef, error)
	Detach(context.Context, DetachRequest) error
	ListByResource(context.Context, ResourceID) ([]UsageRef, error)
}
