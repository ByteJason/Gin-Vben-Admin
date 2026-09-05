package file

// CatalogAdapter exposes the target MediaCatalog/MediaUsageService ports on
// top of the existing, thoroughly tested file.Service.  It is intentionally a
// separate type: legacy HTTP handlers keep their offset-based API while new
// modules get context-scoped IDs, cursor pagination, controlled reads and
// reference protection.

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

var (
	ErrMediaInUse          = errors.New("media resource is in use")
	ErrMediaNotReady       = errors.New("media resource is not ready")
	ErrMediaTypeNotAllowed = errors.New("media type is not allowed")
	ErrInvalidMediaCursor  = errors.New("invalid media cursor")
	ErrMediaConflict       = errors.New("media idempotency conflict")
	ErrUsageNotFound       = errors.New("media usage not found")
	ErrInvalidUsage        = errors.New("invalid media usage")
)

const maxMediaURLTTL = time.Hour

// CatalogAdapter holds only metadata that the legacy in-memory service does
// not yet persist (scope/status/metadata). A future database-backed catalog
// can replace this adapter without changing callers.
type CatalogAdapter struct {
	service *Service
	usage   MediaUsageService
	clock   func() time.Time

	mu           sync.RWMutex
	metadata     map[string]map[string]string
	status       map[string]MediaStatus
	updated      map[string]time.Time
	presets      map[string]ResourceRef
	presetMu     sync.Mutex
	idem         map[string]catalogIdempotency
	categoryIdem map[string]categoryIdempotency
	idemMu       sync.Mutex
}

type catalogIdempotency struct {
	payload     string
	contentSHA  string
	contentSize int64
	ref         ResourceRef
}

type categoryIdempotency struct {
	payload string
	ref     CategoryRef
	deleted bool
}

func NewCatalog(service *Service) *CatalogAdapter {
	adapter := &CatalogAdapter{service: service, clock: time.Now, metadata: map[string]map[string]string{}, status: map[string]MediaStatus{}, updated: map[string]time.Time{}, presets: map[string]ResourceRef{}, idem: map[string]catalogIdempotency{}, categoryIdem: map[string]categoryIdempotency{}}
	if service != nil && service.usageService != nil {
		adapter.usage = service.usageService
		return adapter
	}
	adapter.usage = NewMemoryUsageService(func(ctx context.Context, id string) error {
		if adapter.service == nil {
			return ErrFileNotFound
		}
		scope, _ := tenant.FromContext(ctx)
		item, err := adapter.service.authorizeAccessWithContext(ctx, id, catalogFileAccess(ctx, scope))
		if err != nil {
			return err
		}
		if item.Status != "" && item.Status != MediaReady {
			return ErrMediaNotReady
		}
		return nil
	})
	return adapter
}

// NewMediaCatalog is a descriptive alias used by bootstrap wiring.
func NewMediaCatalog(service *Service) *CatalogAdapter { return NewCatalog(service) }

func (c *CatalogAdapter) UsageService() MediaUsageService {
	if c == nil {
		return nil
	}
	return c.usage
}

func (c *CatalogAdapter) SetUsageService(usage MediaUsageService) {
	if c != nil && usage != nil {
		c.usage = usage
	}
}

func (c *CatalogAdapter) Upload(ctx context.Context, input UploadInput) (ResourceRef, error) {
	if c == nil || c.service == nil {
		return ResourceRef{}, ErrInvalidUpload
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		// Scope is an authority boundary, not a payload field. Requiring the
		// validated context prevents a caller from spoofing tenant/org values in
		// UploadInput when it is used outside the HTTP adapter.
		return ResourceRef{}, err
	}
	if input.TenantID != "" && input.TenantID != scope.TenantID {
		return ResourceRef{}, ErrAccessDenied
	}
	if input.OrgID != "" && input.OrgID != scope.Organization {
		return ResourceRef{}, ErrAccessDenied
	}
	if input.Reader == nil {
		data, readErr := readUploadData(nil, input.Data, input.Size)
		if readErr != nil {
			return ResourceRef{}, readErr
		}
		input.Data = data
		input.Size = int64(len(data))
	} else if input.Size < -1 {
		return ResourceRef{}, ErrInvalidUpload
	}
	input.TenantID = scope.TenantID
	input.OrgID = scope.Organization
	if strings.TrimSpace(input.OwnerID) == "" {
		input.OwnerID = auth.PrincipalIDFromContext(ctx)
	}
	if input.ACL == "" {
		input.ACL = ACLPrivate
	}
	if err := validateMetadata(input.Metadata); err != nil {
		return ResourceRef{}, err
	}
	idemKey := strings.TrimSpace(input.IdempotencyKey)
	payload := uploadPayloadHash(input)
	if idemKey != "" {
		c.idemMu.Lock()
		defer c.idemMu.Unlock()
		key := scope.TenantID + ":" + scope.Organization + ":" + idemKey
		c.mu.RLock()
		previous, exists := c.idem[key]
		c.mu.RUnlock()
		if exists {
			if previous.payload != payload {
				return ResourceRef{}, ErrMediaConflict
			}
			// A streaming reader is deliberately not included in the cheap
			// metadata payload hash.  On a retry, consume it once into a digest
			// (without buffering the body) before deciding whether it is the same
			// idempotent request; otherwise two different streams with identical
			// metadata could incorrectly return the first resource.
			if input.Reader != nil {
				contentSHA, contentSize, hashErr := c.hashReaderForIdempotency(ctx, input.Reader, input.Size)
				if hashErr != nil {
					return ResourceRef{}, hashErr
				}
				expectedSHA := previous.contentSHA
				if expectedSHA == "" {
					expectedSHA = previous.ref.SHA256
				}
				if expectedSHA == "" || contentSHA != expectedSHA || (previous.contentSize >= 0 && contentSize != previous.contentSize) {
					return ResourceRef{}, ErrMediaConflict
				}
				return cloneResource(previous.ref), nil
			}
			expectedSHA := previous.contentSHA
			if expectedSHA == "" {
				expectedSHA = previous.ref.SHA256
			}
			if expectedSHA != "" {
				sum := sha256.Sum256(input.Data)
				if hex.EncodeToString(sum[:]) != expectedSHA || (previous.contentSize >= 0 && int64(len(input.Data)) != previous.contentSize) {
					return ResourceRef{}, ErrMediaConflict
				}
			}
			return cloneResource(previous.ref), nil
		}
	}
	created, err := c.service.Upload(ctx, input)
	if err != nil {
		return ResourceRef{}, err
	}
	now := c.now()
	c.mu.Lock()
	c.metadata[created.ID] = cloneStringMap(input.Metadata)
	c.status[created.ID] = MediaReady
	c.updated[created.ID] = now
	c.mu.Unlock()
	ref := c.toResource(created, scope)
	if idemKey != "" {
		c.mu.Lock()
		contentSHA := created.SHA256
		contentSize := created.Size
		c.idem[scope.TenantID+":"+scope.Organization+":"+idemKey] = catalogIdempotency{payload: payload, contentSHA: contentSHA, contentSize: contentSize, ref: cloneResource(ref)}
		c.mu.Unlock()
	}
	return ref, nil
}

// hashReaderForIdempotency computes a retry digest in bounded chunks.  It is
// only called after an idempotency key already exists, so consuming this
// reader does not affect a subsequent upload attempt.
func (c *CatalogAdapter) hashReaderForIdempotency(ctx context.Context, reader io.Reader, declared int64) (string, int64, error) {
	if reader == nil {
		return "", 0, ErrInvalidUpload
	}
	maxBytes := int64(100 << 20)
	if c != nil && c.service != nil && c.service.maxBytes > 0 {
		maxBytes = c.service.maxBytes
	}
	if declared >= 0 && declared > maxBytes {
		return "", 0, ErrFileTooLarge
	}
	h := sha256.New()
	buf := make([]byte, 32*1024)
	var total int64
	for {
		select {
		case <-ctx.Done():
			return "", total, ctx.Err()
		default:
		}
		n, readErr := reader.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > maxBytes {
				return "", total, ErrFileTooLarge
			}
			_, _ = h.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", total, readErr
		}
	}
	if declared >= 0 && total != declared {
		return "", total, ErrInvalidUpload
	}
	return hex.EncodeToString(h.Sum(nil)), total, nil
}

func (c *CatalogAdapter) Get(ctx context.Context, id ResourceID) (ResourceRef, error) {
	if c == nil || c.service == nil {
		return ResourceRef{}, ErrFileNotFound
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return ResourceRef{}, err
	}
	item, err := c.service.authorizeAccessWithContext(ctx, string(id), catalogFileAccess(ctx, scope))
	if err != nil {
		return ResourceRef{}, err
	}
	return c.toResource(item, scope), nil
}

// UpdateResource is an optional extension to MediaCatalog used by the
// management PATCH route. It keeps the original MediaCatalog interface source
// compatible for external adapters while giving this implementation a
// concrete final-state mutation seam.
func (c *CatalogAdapter) UpdateResource(ctx context.Context, id ResourceID, patch ResourcePatch) (ResourceRef, error) {
	if c == nil || c.service == nil {
		return ResourceRef{}, ErrFileNotFound
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return ResourceRef{}, err
	}
	item, err := c.service.authorizeAccessWithContext(ctx, string(id), catalogFileAccess(ctx, scope))
	if err != nil {
		return ResourceRef{}, err
	}
	if item.TenantID == "" { // system resources are immutable; copy before editing
		return ResourceRef{}, ErrAccessDenied
	}
	// Lifecycle transitions are owned by upload/reconciliation/deletion jobs.
	// Management PATCH may echo the current value, but cannot bypass usage
	// checks or move a row directly between operational states.
	currentStatus := item.Status
	if currentStatus == "" {
		currentStatus = MediaReady
	}
	if patch.Status != nil && *patch.Status != currentStatus {
		return ResourceRef{}, ErrInvalidUpload
	}
	key := "update:" + scope.TenantID + ":" + scope.Organization + ":" + string(id) + ":" + strings.TrimSpace(patch.IdempotencyKey)
	payload := resourcePatchHash(patch)
	if strings.TrimSpace(patch.IdempotencyKey) != "" {
		c.idemMu.Lock()
		defer c.idemMu.Unlock()
		c.mu.RLock()
		previous, exists := c.idem[key]
		c.mu.RUnlock()
		if exists {
			if previous.payload != payload {
				return ResourceRef{}, ErrMediaConflict
			}
			return cloneResource(previous.ref), nil
		}
	}
	updated, err := c.service.updateFileWithAccess(ctx, string(id), catalogFileAccess(ctx, scope), patch)
	if err != nil {
		return ResourceRef{}, err
	}
	c.mu.Lock()
	if patch.Metadata != nil {
		c.metadata[updated.ID] = cloneStringMap(patch.Metadata)
	}
	if patch.Status != nil {
		c.status[updated.ID] = *patch.Status
	}
	c.updated[updated.ID] = c.now()
	c.mu.Unlock()
	ref := c.toResource(updated, scope)
	if strings.TrimSpace(patch.IdempotencyKey) != "" {
		c.mu.Lock()
		c.idem[key] = catalogIdempotency{payload: payload, ref: cloneResource(ref)}
		c.mu.Unlock()
	}
	return ref, nil
}

// Update is a short adapter alias for callers that prefer CRUD naming.
func (c *CatalogAdapter) Update(ctx context.Context, id ResourceID, patch ResourcePatch) (ResourceRef, error) {
	return c.UpdateResource(ctx, id, patch)
}

func (c *CatalogAdapter) List(ctx context.Context, filter MediaFilter) (MediaPage, error) {
	if c == nil || c.service == nil {
		return MediaPage{}, ErrInvalidUpload
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return MediaPage{}, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		return MediaPage{}, ErrInvalidMediaCursor
	}
	if strings.TrimSpace(filter.Cursor) != "" {
		offset, err = decodeCursor(filter.Cursor)
		if err != nil {
			return MediaPage{}, err
		}
	}
	if filter.Status == "" {
		filter.Status = MediaReady
	}
	// Visibility inherits org -> tenant -> system. The legacy list API cannot
	// express an empty OrgID predicate, so fetch the tenant view and apply the
	// inheritance predicate here, then merge the system view by ID.
	queries := []ListFilter{{TenantID: scope.TenantID, OwnerID: strings.TrimSpace(filter.OwnerID), Status: filter.Status, MIME: filter.MIMEExact, MIMEFamily: filter.MIMEFamily}}
	if scope.PlatformAdmin {
		queries = []ListFilter{{OwnerID: strings.TrimSpace(filter.OwnerID), Status: filter.Status, MIME: filter.MIMEExact, MIMEFamily: filter.MIMEFamily}}
	}
	if !scope.PlatformAdmin {
		queries = append(queries, ListFilter{OwnerID: strings.TrimSpace(filter.OwnerID), Status: filter.Status, MIME: filter.MIMEExact, MIMEFamily: filter.MIMEFamily})
	}
	seen := map[string]struct{}{}
	allowedCategories := map[string]struct{}{}
	if filter.CategoryID != "" && filter.IncludeDescendants {
		categories := c.visibleCategories(scope)
		for _, category := range categories {
			if category.ID == string(filter.CategoryID) {
				allowedCategories[category.ID] = struct{}{}
			}
		}
		for changed := true; changed; {
			changed = false
			for _, category := range categories {
				if _, exists := allowedCategories[category.ID]; exists {
					continue
				}
				if _, parent := allowedCategories[category.ParentID]; parent {
					allowedCategories[category.ID] = struct{}{}
					changed = true
				}
			}
		}
	}
	items := make([]ResourceRef, 0)
	for _, query := range queries {
		page, listErr := c.service.List(ctx, query)
		if listErr != nil {
			return MediaPage{}, listErr
		}
		for _, item := range page.Items {
			if !scope.PlatformAdmin {
				if item.TenantID != "" && item.TenantID != scope.TenantID {
					continue
				}
				if scope.Organization != "" && item.OrgID != "" && item.OrgID != scope.Organization {
					continue
				}
				// Listing is an authorization boundary too. The legacy service's
				// offset list intentionally returns metadata only, so apply the
				// catalog ACL here and never expose another principal's private
				// resource merely because it shares a tenant or organization.
				if item.ACL != ACLPublicRead {
					principal := auth.PrincipalIDFromContext(ctx)
					if principal == "" || (item.OwnerID != "" && item.OwnerID != principal) {
						continue
					}
				}
			}
			if _, duplicate := seen[item.ID]; duplicate {
				continue
			}
			seen[item.ID] = struct{}{}
			ref := c.toResource(item, scope)
			if filter.Status != "" && ref.Status != filter.Status {
				continue
			}
			if filter.MIMEExact != "" && !strings.EqualFold(ref.MIME, strings.TrimSpace(filter.MIMEExact)) {
				continue
			}
			if filter.MIMEFamily != "" && !mimeMatchesFamily(ref.MIME, filter.MIMEFamily) {
				continue
			}
			if filter.ScopeType != "" && ref.ScopeType != filter.ScopeType {
				continue
			}
			if filter.CategoryID != "" {
				if filter.IncludeDescendants {
					if _, ok := allowedCategories[ref.CategoryID]; !ok {
						continue
					}
				} else if ref.CategoryID != filter.CategoryID {
					continue
				}
			}
			items = append(items, ref)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			// Keep cursor pages deterministic when several provider rows share
			// the same timestamp (the documented secondary sort key).
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	// The legacy service already sorts newest first. Cursor is an offset over
	// the filtered result, not a provider key, so it remains opaque to callers.
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	result := MediaPage{Items: cloneResources(items[offset:end]), Total: total, Limit: limit, Offset: offset, HasMore: end < total}
	if result.HasMore {
		result.NextCursor = encodeCursor(end)
	}
	return result, nil
}

func (c *CatalogAdapter) Open(ctx context.Context, id ResourceID, options OpenOptions) (io.ReadCloser, error) {
	if c == nil || c.service == nil {
		return nil, ErrFileNotFound
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	item, reader, err := c.service.openWithAccess(ctx, string(id), catalogFileAccess(ctx, scope))
	if err != nil {
		return nil, err
	}
	if item.Status != "" && item.Status != MediaReady {
		_ = reader.Close()
		return nil, ErrMediaNotReady
	}
	start, end, err := normalizeRange(options, item.Size)
	if err != nil {
		_ = reader.Close()
		return nil, err
	}
	if start == 0 && int64(end) == item.Size {
		return reader, nil
	}
	return &rangeReadCloser{ReadCloser: reader, skip: int64(start), remain: int64(end - start)}, nil
}

type rangeReadCloser struct {
	io.ReadCloser
	skip, remain int64
}

func (r *rangeReadCloser) Read(p []byte) (int, error) {
	for r.skip > 0 {
		n, err := io.CopyN(io.Discard, r.ReadCloser, r.skip)
		r.skip -= n
		if err != nil {
			return 0, err
		}
	}
	if r.remain <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remain {
		p = p[:r.remain]
	}
	n, err := r.ReadCloser.Read(p)
	r.remain -= int64(n)
	return n, err
}

func (c *CatalogAdapter) SignedURL(ctx context.Context, id ResourceID, request URLRequest) (URLRef, error) {
	if request.TTL <= 0 || request.TTL > maxMediaURLTTL || (request.Purpose != URLPurposePreview && request.Purpose != URLPurposeDownload) {
		return URLRef{}, ErrInvalidUpload
	}
	if c == nil || c.service == nil {
		return URLRef{}, ErrFileNotFound
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return URLRef{}, err
	}
	// Authorize first so an unknown or cross-scope ID never becomes a
	// lifecycle oracle. The provider is only asked to sign a ready object.
	item, err := c.service.authorizeAccessWithContext(ctx, string(id), catalogFileAccess(ctx, scope))
	if err != nil {
		return URLRef{}, err
	}
	if item.Status != "" && item.Status != MediaReady {
		return URLRef{}, ErrMediaNotReady
	}
	urlValue, err := c.service.signedURLWithAccess(ctx, item.ID, catalogFileAccess(ctx, scope), request.TTL)
	if err != nil {
		return URLRef{}, err
	}
	return URLRef{URL: urlValue, ExpiresAt: c.now().Add(request.TTL)}, nil
}

func (c *CatalogAdapter) Delete(ctx context.Context, id ResourceID, options DeleteOptions) error {
	if c == nil || c.service == nil {
		return ErrFileNotFound
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	idemKey := strings.TrimSpace(options.IdempotencyKey)
	idem := "delete:" + scope.TenantID + ":" + scope.Organization + ":" + string(id) + ":" + idemKey
	if idemKey != "" {
		c.idemMu.Lock()
		defer c.idemMu.Unlock()
		payload := strings.TrimSpace(options.Reason) + "\x00" + fmt.Sprintf("%t", options.Force) + "\x00" + strings.TrimSpace(options.Confirmation)
		c.mu.RLock()
		previous, exists := c.idem[idem]
		c.mu.RUnlock()
		if exists {
			if previous.payload != payload {
				return ErrMediaConflict
			}
			return nil
		}
	}
	item, err := c.service.authorizeMutationWithContext(ctx, string(id), catalogFileAccess(ctx, scope))
	if err != nil {
		return err
	}
	if item.TenantID == "" {
		return ErrAccessDenied
	}
	if item.Status == MediaDeleting {
		return nil
	}
	if c.usage != nil {
		refs, usageErr := c.usage.ListByResource(ctx, id)
		if usageErr != nil && !errors.Is(usageErr, ErrUsageNotFound) {
			return usageErr
		}
		forced := options.Force && strings.TrimSpace(options.Confirmation) == ForceDeleteConfirmation
		if len(refs) > 0 && !forced {
			return ErrMediaInUse
		}
		if forced {
			deleteAt := c.now()
			if err := c.service.ForceDeleteFile(ctx, string(id), auth.PrincipalIDFromContext(ctx), scope.TenantID, scope.Organization, deleteAt); err != nil {
				return err
			}
			c.mu.Lock()
			c.status[string(id)] = MediaDeleting
			c.updated[string(id)] = deleteAt
			c.mu.Unlock()
			return nil
		}
	}
	deleteAt := c.now()
	if err := c.service.softDeleteFileWithAccess(ctx, string(id), catalogFileAccess(ctx, scope), deleteAt); err != nil {
		return err
	}
	c.mu.Lock()
	c.status[string(id)] = MediaDeleting
	c.updated[string(id)] = deleteAt
	if idemKey != "" {
		c.idem[idem] = catalogIdempotency{payload: strings.TrimSpace(options.Reason) + "\x00" + fmt.Sprintf("%t", options.Force) + "\x00" + strings.TrimSpace(options.Confirmation)}
	}
	c.mu.Unlock()
	return nil
}

func (c *CatalogAdapter) ListCategories(ctx context.Context, filter CategoryFilter) ([]CategoryRef, error) {
	if c == nil || c.service == nil {
		return nil, ErrInvalidCategory
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	items := c.visibleCategories(scope)
	refs := make([]CategoryRef, 0, len(items))
	byID := make(map[string]Category, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	allowed := map[string]struct{}{}
	filterActive := strings.TrimSpace(string(filter.ParentID)) != ""
	if filterActive {
		for _, item := range items {
			if item.ParentID == string(filter.ParentID) {
				allowed[item.ID] = struct{}{}
			}
		}
		if filter.IncludeDescendants && filter.ParentID != "" {
			changed := true
			for changed {
				changed = false
				for _, item := range items {
					if _, ok := allowed[item.ID]; ok {
						continue
					}
					if _, ok := allowed[item.ParentID]; ok {
						allowed[item.ID] = struct{}{}
						changed = true
					}
				}
			}
		}
	}
	for _, item := range items {
		if filter.ScopeType != "" && scopeTypeFor(item.TenantID, item.OrgID) != filter.ScopeType {
			continue
		}
		if filterActive {
			if _, ok := allowed[item.ID]; !ok {
				continue
			}
		}
		refs = append(refs, categoryRef(item, byID))
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}

func (c *CatalogAdapter) visibleCategories(scope tenant.Context) []Category {
	if c == nil || c.service == nil {
		return nil
	}
	if scope.PlatformAdmin {
		return c.service.ListAllCategories(context.Background())
	}
	queries := [][2]string{{scope.TenantID, scope.Organization}, {scope.TenantID, ""}, {"", ""}}
	seen := map[string]struct{}{}
	items := make([]Category, 0)
	for _, query := range queries {
		for _, item := range c.service.ListCategories(context.Background(), query[0], query[1]) {
			if !scope.PlatformAdmin {
				if item.TenantID != "" && item.TenantID != scope.TenantID {
					continue
				}
				if scope.Organization != "" && item.OrgID != "" && item.OrgID != scope.Organization {
					continue
				}
			}
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			items = append(items, item)
		}
	}
	return items
}

func (c *CatalogAdapter) CreateCategory(ctx context.Context, input CategoryInput) (CategoryRef, error) {
	if c == nil || c.service == nil {
		return CategoryRef{}, ErrInvalidCategory
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return CategoryRef{}, err
	}
	targetTenant, targetOrg, err := categoryTargetScope(scope, input.TenantID, input.OrgID)
	if err != nil {
		return CategoryRef{}, err
	}
	idemKey := strings.TrimSpace(input.IdempotencyKey)
	payload := categoryCreatePayload(input, targetTenant, targetOrg)
	if idemKey != "" {
		c.idemMu.Lock()
		defer c.idemMu.Unlock()
		c.mu.RLock()
		previous, exists := c.categoryIdem[categoryIdemKey(targetTenant, targetOrg, "create", "", idemKey)]
		c.mu.RUnlock()
		if exists {
			if previous.payload != payload {
				return CategoryRef{}, ErrMediaConflict
			}
			return previous.ref, nil
		}
	}
	created, err := c.service.CreateCategory(ctx, CategoryInput{Name: input.Name, ParentID: input.ParentID, Enabled: input.Enabled}, targetTenant, targetOrg)
	if err != nil {
		return CategoryRef{}, err
	}
	ref := categoryRef(created, map[string]Category{created.ID: created})
	if created.ParentID != "" {
		all := c.visibleCategories(scope)
		byID := make(map[string]Category, len(all)+1)
		for _, item := range all {
			byID[item.ID] = item
		}
		byID[created.ID] = created
		ref = categoryRef(created, byID)
	}
	if idemKey != "" {
		c.mu.Lock()
		if c.categoryIdem == nil {
			c.categoryIdem = make(map[string]categoryIdempotency)
		}
		c.categoryIdem[categoryIdemKey(targetTenant, targetOrg, "create", "", idemKey)] = categoryIdempotency{payload: payload, ref: ref}
		c.mu.Unlock()
	}
	return ref, nil
}

func (c *CatalogAdapter) UpdateCategory(ctx context.Context, id CategoryID, patch CategoryPatch) (CategoryRef, error) {
	if c == nil || c.service == nil {
		return CategoryRef{}, ErrInvalidCategory
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return CategoryRef{}, err
	}
	targetTenant, targetOrg := repositoryTenantForCatalog(scope), repositoryOrgForCatalog(scope)
	if scope.PlatformAdmin {
		category, exists := c.service.GetCategory(ctx, string(id))
		if !exists {
			return CategoryRef{}, ErrCategoryNotFound
		}
		targetTenant, targetOrg = category.TenantID, category.OrgID
	}
	idemKey := strings.TrimSpace(patch.IdempotencyKey)
	payload := categoryPatchPayload(patch)
	if idemKey != "" {
		c.idemMu.Lock()
		defer c.idemMu.Unlock()
		c.mu.RLock()
		previous, exists := c.categoryIdem[categoryIdemKey(targetTenant, targetOrg, "update", string(id), idemKey)]
		c.mu.RUnlock()
		if exists {
			if previous.payload != payload {
				return CategoryRef{}, ErrMediaConflict
			}
			return previous.ref, nil
		}
	}
	input := CategoryInput{Enabled: patch.Enabled}
	if patch.Name != nil {
		input.Name = *patch.Name
	}
	updated, err := c.service.UpdateCategory(ctx, string(id), input, targetTenant, targetOrg)
	if err != nil {
		return CategoryRef{}, err
	}
	all := c.visibleCategories(scope)
	byID := make(map[string]Category, len(all)+1)
	for _, item := range all {
		byID[item.ID] = item
	}
	byID[updated.ID] = updated
	ref := categoryRef(updated, byID)
	if idemKey != "" {
		c.mu.Lock()
		if c.categoryIdem == nil {
			c.categoryIdem = make(map[string]categoryIdempotency)
		}
		c.categoryIdem[categoryIdemKey(targetTenant, targetOrg, "update", string(id), idemKey)] = categoryIdempotency{payload: payload, ref: ref}
		c.mu.Unlock()
	}
	return ref, nil
}

func (c *CatalogAdapter) DeleteCategory(ctx context.Context, request CategoryDeleteRequest) error {
	if c == nil || c.service == nil {
		return ErrInvalidCategory
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	targetTenant, targetOrg := repositoryTenantForCatalog(scope), repositoryOrgForCatalog(scope)
	if scope.PlatformAdmin {
		category, exists := c.service.GetCategory(ctx, string(request.ID))
		if !exists {
			if idemKey := strings.TrimSpace(request.IdempotencyKey); idemKey != "" {
				if previous, found := c.categoryIdempotencyForAnyScope("delete", string(request.ID), idemKey); found && previous.deleted {
					return nil
				}
			}
			return ErrCategoryNotFound
		}
		targetTenant, targetOrg = category.TenantID, category.OrgID
	}
	idemKey := strings.TrimSpace(request.IdempotencyKey)
	key := categoryIdemKey(targetTenant, targetOrg, "delete", string(request.ID), idemKey)
	if idemKey != "" {
		c.idemMu.Lock()
		defer c.idemMu.Unlock()
		c.mu.RLock()
		previous, exists := c.categoryIdem[key]
		c.mu.RUnlock()
		if exists {
			if previous.payload != "delete" {
				return ErrMediaConflict
			}
			return nil
		}
	}
	if err := c.service.DeleteCategory(ctx, string(request.ID), targetTenant, targetOrg); err != nil {
		return err
	}
	if idemKey != "" {
		c.mu.Lock()
		if c.categoryIdem == nil {
			c.categoryIdem = make(map[string]categoryIdempotency)
		}
		c.categoryIdem[key] = categoryIdempotency{payload: "delete", deleted: true}
		c.mu.Unlock()
	}
	return nil
}

func (c *CatalogAdapter) Attach(ctx context.Context, input UsageInput) (UsageRef, error) {
	if c == nil || c.usage == nil {
		return UsageRef{}, ErrInvalidUsage
	}
	return c.usage.Attach(ctx, input)
}

func (c *CatalogAdapter) Detach(ctx context.Context, request DetachRequest) error {
	if c == nil || c.usage == nil {
		return ErrInvalidUsage
	}
	return c.usage.Detach(ctx, request)
}

func (c *CatalogAdapter) ListByResource(ctx context.Context, id ResourceID) ([]UsageRef, error) {
	if c == nil || c.usage == nil {
		return nil, ErrInvalidUsage
	}
	return c.usage.ListByResource(ctx, id)
}

// MemoryUsageService is a concurrency-safe usage index. It is deliberately
// independent from a storage provider and can later be replaced by the
// media_usages repository without changing the port.
type MemoryUsageService struct {
	mu       sync.RWMutex
	items    map[string]UsageRef
	byKey    map[string]string
	idem     map[string]usageIdempotency
	scopes   map[string]string
	resource func(context.Context, string) error
}

type usageIdempotency struct {
	payload string
	ref     UsageRef
}

func NewMemoryUsageService(resourceExists func(context.Context, string) error) *MemoryUsageService {
	return &MemoryUsageService{items: map[string]UsageRef{}, byKey: map[string]string{}, idem: map[string]usageIdempotency{}, scopes: map[string]string{}, resource: resourceExists}
}

// NewMemoryMediaUsageService is a compatibility-friendly constructor name.
func NewMemoryMediaUsageService(resourceExists func(context.Context, string) error) *MemoryUsageService {
	return NewMemoryUsageService(resourceExists)
}

func (m *MemoryUsageService) Attach(ctx context.Context, input UsageInput) (UsageRef, error) {
	if m == nil || strings.TrimSpace(string(input.ResourceID)) == "" || strings.TrimSpace(input.Module) == "" || strings.TrimSpace(input.EntityType) == "" || strings.TrimSpace(input.EntityID) == "" || strings.TrimSpace(input.Field) == "" {
		return UsageRef{}, ErrInvalidUsage
	}
	if _, err := tenant.RequireContext(ctx); err != nil {
		return UsageRef{}, err
	}
	if m.resource != nil {
		if err := m.resource(ctx, string(input.ResourceID)); err != nil {
			return UsageRef{}, err
		}
	}
	scopeKey := usageScopeKey(ctx)
	key := scopeKey + "\x00" + usageKey(input.ResourceID, input.Module, input.EntityType, input.EntityID, input.Field)
	payload := key
	idem := strings.TrimSpace(input.IdempotencyKey)
	m.mu.Lock()
	defer m.mu.Unlock()
	if id := m.byKey[key]; id != "" {
		return m.items[id], nil
	}
	if idem != "" {
		if previous, ok := m.idem[scopeKey+"\x00"+idem]; ok {
			if previous.payload != payload {
				return UsageRef{}, ErrMediaConflict
			}
			return previous.ref, nil
		}
	}
	id, err := randomID()
	if err != nil {
		return UsageRef{}, err
	}
	ref := UsageRef{ID: id, ResourceID: input.ResourceID, Module: strings.TrimSpace(input.Module), EntityType: strings.TrimSpace(input.EntityType), EntityID: strings.TrimSpace(input.EntityID), Field: strings.TrimSpace(input.Field)}
	m.items[id], m.byKey[key] = ref, id
	m.scopes[id] = scopeKey
	if idem != "" {
		m.idem[scopeKey+"\x00"+idem] = usageIdempotency{payload: payload, ref: ref}
	}
	return ref, nil
}

func (m *MemoryUsageService) Detach(ctx context.Context, request DetachRequest) error {
	if m == nil {
		return ErrUsageNotFound
	}
	if _, err := tenant.RequireContext(ctx); err != nil {
		return err
	}
	id := strings.TrimSpace(request.UsageID)
	m.mu.Lock()
	defer m.mu.Unlock()
	scopeKey := usageScopeKey(ctx)
	idem := strings.TrimSpace(request.IdempotencyKey)
	if idem != "" {
		idemKey := scopeKey + "\x00detach:" + idem
		if previous, ok := m.idem[idemKey]; ok {
			if previous.payload != id {
				return ErrMediaConflict
			}
			return nil
		}
	}
	ref, ok := m.items[id]
	if !ok {
		if idem != "" {
			// A retry after a successful detach is still idempotent. The
			// operation is recorded only after scope authorization below.
			if storedScope, exists := m.scopes[id]; exists && storedScope == scopeKey {
				m.idem[scopeKey+"\x00detach:"+idem] = usageIdempotency{payload: id}
				return nil
			}
		}
		return ErrUsageNotFound
	}
	storedScope := m.scopes[id]
	if storedScope != "" && storedScope != scopeKey && scopeKey != "*" {
		return ErrAccessDenied
	}
	delete(m.items, id)
	delete(m.byKey, storedScope+"\x00"+usageKey(ref.ResourceID, ref.Module, ref.EntityType, ref.EntityID, ref.Field))
	// Retain the tombstone scope so a keyed retry can be recognized without
	// exposing a detached usage to another scope.
	if idem != "" {
		m.idem[scopeKey+"\x00detach:"+idem] = usageIdempotency{payload: id}
	}
	return nil
}

func (m *MemoryUsageService) ListByResource(ctx context.Context, id ResourceID) ([]UsageRef, error) {
	if m == nil {
		return nil, ErrInvalidUsage
	}
	if strings.TrimSpace(string(id)) == "" {
		return nil, ErrInvalidUsage
	}
	if _, err := tenant.RequireContext(ctx); err != nil {
		return nil, err
	}
	if m.resource != nil {
		if err := m.resource(ctx, string(id)); err != nil {
			return nil, err
		}
	}
	scopeKey := usageScopeKey(ctx)
	m.mu.RLock()
	result := make([]UsageRef, 0)
	for _, ref := range m.items {
		if ref.ResourceID == id {
			if storedScope := m.scopes[ref.ID]; storedScope != "" && storedScope != scopeKey && scopeKey != "*" {
				continue
			}
			result = append(result, ref)
		}
	}
	m.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func usageScopeKey(ctx context.Context) string {
	if scope, ok := tenant.FromContext(ctx); ok {
		if scope.PlatformAdmin {
			return "*"
		}
		return strings.TrimSpace(scope.TenantID) + ":" + strings.TrimSpace(scope.Organization)
	}
	return ""
}

func (c *CatalogAdapter) toResource(item File, scope tenant.Context) ResourceRef {
	status := item.Status
	if status == "" {
		// Only dependency-free fixtures use the sidecar. Durable catalog reads
		// already carry lifecycle_status from gvba_storage_file_objects in item.Status.
		if c.service == nil || c.service.repo == nil {
			status = c.statusFor(item.ID)
		}
		if status == "" {
			status = MediaReady
		}
	}
	updated := item.CreatedAt
	if !item.UpdatedAt.IsZero() {
		updated = item.UpdatedAt
	}
	c.mu.RLock()
	if value, ok := c.updated[item.ID]; ok {
		updated = value
	}
	metadata := cloneStringMap(item.Metadata)
	if metadata == nil {
		metadata = cloneStringMap(c.metadata[item.ID])
	}
	c.mu.RUnlock()
	reconcileKey := strings.TrimSpace(metadata["reconcile_key"])
	resourceScope := scopeTypeFor(item.TenantID, item.OrgID)
	selectable := strings.HasPrefix(strings.ToLower(item.MIME), "image/") && status == MediaReady
	ref := ResourceRef{ID: item.ID, Name: item.Name, MIME: item.MIME, Size: item.Size, SHA256: item.SHA256, CategoryID: item.CategoryID, ScopeType: resourceScope, ACL: item.ACL, Status: status, CreatedAt: item.CreatedAt, UpdatedAt: updated, URLHints: map[string]bool{"preview": selectable, "download": status == MediaReady}, Metadata: metadata, Selectable: selectable, ReconcileKey: reconcileKey, ObjectKey: item.ObjectKey, Extension: item.Extension, ETag: item.ETag, FailureReason: item.FailureReason, ScanStatus: item.ScanStatus}
	if !selectable {
		ref.DisabledReason = "media_type_not_allowed"
	}
	return ref
}

func (c *CatalogAdapter) statusFor(id string) MediaStatus {
	c.mu.RLock()
	status := c.status[id]
	c.mu.RUnlock()
	return status
}

func (c *CatalogAdapter) now() time.Time {
	if c.clock == nil {
		return time.Now().UTC()
	}
	return c.clock().UTC()
}

func readUploadData(reader io.Reader, data []byte, declared int64) ([]byte, error) {
	if reader == nil {
		if declared < 0 || (data == nil && declared != 0) || (data != nil && declared != int64(len(data))) {
			return nil, ErrInvalidUpload
		}
		return append([]byte(nil), data...), nil
	}
	if declared < 0 {
		return nil, ErrInvalidUpload
	}
	limited := io.LimitReader(reader, declared+1)
	var buffer bytes.Buffer
	_, err := io.Copy(&buffer, limited)
	if err != nil {
		return nil, fmt.Errorf("read media upload: %w", err)
	}
	if int64(buffer.Len()) != declared {
		return nil, ErrInvalidUpload
	}
	return append([]byte(nil), buffer.Bytes()...), nil
}

func normalizeRange(options OpenOptions, size int64) (int, int, error) {
	start, end := int64(0), size
	if options.RangeStart != nil {
		start = *options.RangeStart
	}
	if options.RangeEnd != nil {
		end = *options.RangeEnd + 1
	}
	if start < 0 || end < start || end > size {
		return 0, 0, ErrInvalidUpload
	}
	return int(start), int(end), nil
}

func mimeMatchesFamily(mimeValue, family string) bool {
	family = strings.ToLower(strings.TrimSpace(family))
	mimeValue = strings.ToLower(strings.TrimSpace(mimeValue))
	if family == "" {
		return true
	}
	if strings.HasSuffix(family, "/*") {
		return strings.HasPrefix(mimeValue, strings.TrimSuffix(family, "*"))
	}
	return mimeValue == family
}

func scopeTypeFor(tenantID, orgID string) ScopeType {
	if strings.TrimSpace(orgID) != "" {
		return ScopeOrg
	}
	if strings.TrimSpace(tenantID) != "" {
		return ScopeTenant
	}
	return ScopeSystem
}

func categoryTargetScope(scope tenant.Context, tenantID, orgID string) (string, string, error) {
	tenantID, orgID = strings.TrimSpace(tenantID), strings.TrimSpace(orgID)
	if !scope.PlatformAdmin {
		if (tenantID != "" && tenantID != scope.TenantID) || (orgID != "" && orgID != scope.Organization) {
			return "", "", ErrCategoryAccessDenied
		}
		return scope.TenantID, scope.Organization, nil
	}
	// A platform administrator may explicitly target a tenant/org or the
	// system scope (both fields empty). An organization without a tenant is not
	// a valid scope and is rejected before the legacy service is called.
	if orgID != "" && tenantID == "" {
		return "", "", ErrCategoryAccessDenied
	}
	if tenantID != "" {
		if _, err := tenant.NewContext(tenantID, orgID, false); err != nil {
			return "", "", ErrInvalidCategory
		}
	}
	return tenantID, orgID, nil
}

func categoryIdemKey(tenantID, orgID, operation, id, key string) string {
	return strings.Join([]string{strings.TrimSpace(tenantID), strings.TrimSpace(orgID), operation, strings.TrimSpace(id), strings.TrimSpace(key)}, "\x00")
}

func (c *CatalogAdapter) categoryIdempotencyForAnyScope(operation, id, key string) (categoryIdempotency, bool) {
	if c == nil {
		return categoryIdempotency{}, false
	}
	suffix := strings.Join([]string{operation, strings.TrimSpace(id), strings.TrimSpace(key)}, "\x00")
	c.mu.RLock()
	defer c.mu.RUnlock()
	for storedKey, value := range c.categoryIdem {
		if strings.HasSuffix(storedKey, "\x00"+suffix) {
			return value, true
		}
	}
	return categoryIdempotency{}, false
}

func categoryCreatePayload(input CategoryInput, tenantID, orgID string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s", strings.TrimSpace(input.Name), strings.TrimSpace(input.ParentID), strings.TrimSpace(tenantID), strings.TrimSpace(orgID))
	if input.Enabled != nil {
		fmt.Fprintf(h, "\x00enabled=%t", *input.Enabled)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func categoryPatchPayload(patch CategoryPatch) string {
	h := sha256.New()
	if patch.Name != nil {
		fmt.Fprintf(h, "name=%s", *patch.Name)
	}
	if patch.Enabled != nil {
		fmt.Fprintf(h, "\x00enabled=%t", *patch.Enabled)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func categoryRef(item Category, byID map[string]Category) CategoryRef {
	parts := make([]string, 0, 4)
	current := item
	seen := map[string]struct{}{}
	for {
		if _, exists := seen[current.ID]; exists {
			// A malformed legacy cycle should not make a read loop forever; the
			// current node is still useful to the caller.
			break
		}
		seen[current.ID] = struct{}{}
		parts = append(parts, current.Name)
		if strings.TrimSpace(current.ParentID) == "" {
			break
		}
		parent, exists := byID[current.ParentID]
		if !exists {
			break
		}
		current = parent
	}
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	return CategoryRef{ID: item.ID, Name: item.Name, Path: strings.Join(parts, "/"), ScopeType: scopeTypeFor(item.TenantID, item.OrgID), Enabled: item.Enabled}
}

func repositoryTenantForCatalog(scope tenant.Context) string {
	if scope.PlatformAdmin {
		return ""
	}
	return scope.TenantID
}
func repositoryOrgForCatalog(scope tenant.Context) string {
	if scope.PlatformAdmin {
		return ""
	}
	return scope.Organization
}

func catalogFileAccess(ctx context.Context, scope tenant.Context) fileAccess {
	return fileAccess{
		subject:       auth.PrincipalIDFromContext(ctx),
		tenantID:      repositoryTenantForCatalog(scope),
		orgID:         repositoryOrgForCatalog(scope),
		platformAdmin: scope.PlatformAdmin,
	}
}

func uploadPayloadHash(input UploadInput) string {
	// This is intentionally metadata-only; content identity is stored as the
	// resulting SHA-256 so byte-backed and streaming retries share one key
	// without buffering a reader just to construct the idempotency token.
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s", input.Name, input.MIME, input.Size, input.ACL, input.CategoryID, input.OwnerID, input.TenantID, input.OrgID)
	keys := make([]string, 0, len(input.Metadata))
	for key := range input.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(h, "\x00meta:%s=%s", key, input.Metadata[key])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func validateMetadata(values map[string]string) error {
	if len(values) > 64 {
		return ErrInvalidUpload
	}
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" || len(key) > 128 || len(value) > 2048 || strings.ContainsAny(key+value, "\r\n") {
			return ErrInvalidUpload
		}
	}
	return nil
}

func resourcePatchHash(patch ResourcePatch) string {
	h := sha256.New()
	if patch.Name != nil {
		_, _ = io.WriteString(h, "name=")
		_, _ = io.WriteString(h, *patch.Name)
	}
	if patch.CategoryID != nil {
		_, _ = io.WriteString(h, "\x00category=")
		_, _ = io.WriteString(h, string(*patch.CategoryID))
	}
	if patch.Status != nil {
		_, _ = io.WriteString(h, "\x00status=")
		_, _ = io.WriteString(h, string(*patch.Status))
	}
	keys := make([]string, 0, len(patch.Metadata))
	for key := range patch.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(h, "\x00meta:%s=%s", key, patch.Metadata[key])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func usageKey(resource ResourceID, module, entityType, entityID, field string) string {
	return strings.Join([]string{string(resource), strings.TrimSpace(module), strings.TrimSpace(entityType), strings.TrimSpace(entityID), strings.TrimSpace(field)}, "\x00")
}

func encodeCursor(offset int) string {
	value, _ := json.Marshal(struct {
		Offset int `json:"offset"`
	}{Offset: offset})
	return base64.RawURLEncoding.EncodeToString(value)
}

func decodeCursor(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, ErrInvalidMediaCursor
	}
	var value struct {
		Offset int `json:"offset"`
	}
	if err := json.Unmarshal(raw, &value); err != nil || value.Offset < 0 {
		return 0, ErrInvalidMediaCursor
	}
	return value.Offset, nil
}

func cloneResource(value ResourceRef) ResourceRef {
	if value.URLHints != nil {
		hints := make(map[string]bool, len(value.URLHints))
		for key, enabled := range value.URLHints {
			hints[key] = enabled
		}
		value.URLHints = hints
	}
	value.Metadata = cloneStringMap(value.Metadata)
	return value
}
func cloneResources(values []ResourceRef) []ResourceRef {
	result := make([]ResourceRef, len(values))
	for i, value := range values {
		result[i] = cloneResource(value)
	}
	return result
}
func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
func randomID() (string, error) {
	var bytesValue [16]byte
	if _, err := crand.Read(bytesValue[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytesValue[:]), nil
}

var _ MediaCatalog = (*CatalogAdapter)(nil)
var _ MediaUsageService = (*CatalogAdapter)(nil)
var _ MediaUsageService = (*MemoryUsageService)(nil)
