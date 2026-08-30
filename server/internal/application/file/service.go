// Package file defines the provider-independent file-center application seam.
// Concrete object stores (for example, S3-compatible providers) can implement
// Store without changing validation, authorization, or lifecycle policy.
package file

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrFileTooLarge         = errors.New("file exceeds configured size limit")
	ErrMIMETypeNotAllowed   = errors.New("file MIME type is not allowed")
	ErrFileNotFound         = errors.New("file not found")
	ErrAccessDenied         = errors.New("file access denied")
	ErrInvalidUpload        = errors.New("invalid file upload")
	ErrStorageRead          = errors.New("file storage does not support reads")
	ErrCategoryNotFound     = errors.New("category not found")
	ErrCategoryAccessDenied = errors.New("category access denied")
	ErrCategoryNotEmpty     = errors.New("category is not empty")
	ErrInvalidCategory      = errors.New("invalid category")
)

type ACL string

const (
	ACLPrivate    ACL = "private"
	ACLPublicRead ACL = "public-read"
)

// Object is the provider payload. Data is optional for remote providers and
// is retained by the memory provider solely for deterministic local tests.
type Object struct {
	Key        string
	Name       string
	MIME       string
	Size       int64
	OwnerID    string
	TenantID   string
	OrgID      string
	ACL        ACL
	CreatedAt  time.Time
	SHA256     string
	Data       []byte
	CategoryID string
}

// Store is the only object-storage dependency required by Service.
type Store interface {
	Put(context.Context, Object) error
	Delete(context.Context, string) error
	SignURL(context.Context, string, time.Duration) (string, error)
}

type Config struct {
	MaxBytes     int64
	AllowedMIMEs []string
	Clock        func() time.Time
}

type UploadInput struct {
	Name       string
	MIME       string
	Size       int64
	OwnerID    string
	TenantID   string
	OrgID      string
	ACL        ACL
	Data       []byte
	CategoryID string
}

type File struct {
	ID         string
	Key        string
	Name       string
	MIME       string
	Size       int64
	OwnerID    string
	TenantID   string
	OrgID      string
	ACL        ACL
	CreatedAt  time.Time
	SHA256     string
	CategoryID string `json:"categoryId,omitempty"`
}

type ListFilter struct {
	TenantID   string
	OrgID      string
	OwnerID    string
	Limit      int
	Offset     int
	CategoryID string
}

type Category struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentID  string    `json:"parentId,omitempty"`
	TenantID  string    `json:"tenantId,omitempty"`
	OrgID     string    `json:"orgId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CategoryInput struct {
	Name     string `json:"name"`
	ParentID string `json:"parentId,omitempty"`
}

type Page struct {
	Items  []File `json:"items"`
	Total  int    `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type CleanupReport struct {
	Cutoff        time.Time `json:"cutoff"`
	MatchingCount int       `json:"matchingCount"`
	Bytes         int64     `json:"bytes"`
}

type readableStore interface {
	Get(context.Context, string) (Object, error)
}

type Service struct {
	store      Store
	maxBytes   int64
	allowed    map[string]struct{}
	clock      func() time.Time
	mu         sync.RWMutex
	files      map[string]File
	categories map[string]Category
}

func NewService(store Store, config Config) *Service {
	allowed := make(map[string]struct{}, len(config.AllowedMIMEs))
	for _, mime := range config.AllowedMIMEs {
		allowed[strings.ToLower(strings.TrimSpace(mime))] = struct{}{}
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Service{store: store, maxBytes: config.MaxBytes, allowed: allowed, clock: clock, files: make(map[string]File), categories: make(map[string]Category)}
}

func (s *Service) Upload(ctx context.Context, input UploadInput) (File, error) {
	if s.store == nil || strings.TrimSpace(input.Name) == "" || input.Size < 0 {
		return File{}, ErrInvalidUpload
	}
	if s.maxBytes > 0 && input.Size > s.maxBytes {
		return File{}, ErrFileTooLarge
	}
	if input.Data != nil && int64(len(input.Data)) != input.Size {
		return File{}, fmt.Errorf("%w: size does not match data", ErrInvalidUpload)
	}
	if len(s.allowed) > 0 {
		if _, ok := s.allowed[strings.ToLower(strings.TrimSpace(input.MIME))]; !ok {
			return File{}, ErrMIMETypeNotAllowed
		}
	}
	acl := input.ACL
	if acl == "" {
		acl = ACLPrivate
	}
	if acl != ACLPrivate && acl != ACLPublicRead {
		return File{}, ErrInvalidUpload
	}
	id, err := newID()
	if err != nil {
		return File{}, fmt.Errorf("create file id: %w", err)
	}
	now := s.clock().UTC()
	data := append([]byte(nil), input.Data...)
	hash := ""
	if data != nil {
		sum := sha256.Sum256(data)
		hash = hex.EncodeToString(sum[:])
	}
	categoryID := strings.TrimSpace(input.CategoryID)
	if categoryID != "" {
		s.mu.RLock()
		category, exists := s.categories[categoryID]
		s.mu.RUnlock()
		if !exists {
			return File{}, ErrCategoryNotFound
		}
		if category.TenantID != strings.TrimSpace(input.TenantID) || category.OrgID != strings.TrimSpace(input.OrgID) {
			return File{}, ErrCategoryAccessDenied
		}
	}
	object := Object{Key: id, Name: strings.TrimSpace(input.Name), MIME: strings.TrimSpace(input.MIME), Size: input.Size, OwnerID: strings.TrimSpace(input.OwnerID), TenantID: strings.TrimSpace(input.TenantID), OrgID: strings.TrimSpace(input.OrgID), ACL: acl, CreatedAt: now, SHA256: hash, Data: data, CategoryID: categoryID}
	if err = s.store.Put(ctx, object); err != nil {
		return File{}, fmt.Errorf("put file: %w", err)
	}
	file := File{ID: id, Key: id, Name: object.Name, MIME: object.MIME, Size: object.Size, OwnerID: object.OwnerID, TenantID: object.TenantID, OrgID: object.OrgID, ACL: object.ACL, CreatedAt: now, SHA256: object.SHA256, CategoryID: categoryID}
	s.mu.Lock()
	s.files[id] = file
	s.mu.Unlock()
	return file, nil
}

// SetClock is used by deterministic lifecycle checks and does not alter the
// provider contract.
func (s *Service) SetClock(clock func() time.Time) {
	if clock == nil {
		return
	}
	s.mu.Lock()
	s.clock = clock
	s.mu.Unlock()
}

func (s *Service) List(_ context.Context, filter ListFilter) (Page, error) {
	if filter.Limit < 0 || filter.Offset < 0 {
		return Page{}, ErrInvalidUpload
	}
	s.mu.RLock()
	items := make([]File, 0, len(s.files))
	for _, item := range s.files {
		if filter.TenantID != "" && item.TenantID != filter.TenantID {
			continue
		}
		if filter.OrgID != "" && item.OrgID != filter.OrgID {
			continue
		}
		if filter.OwnerID != "" && item.OwnerID != filter.OwnerID {
			continue
		}
		if filter.CategoryID != "" && item.CategoryID != filter.CategoryID {
			continue
		}
		items = append(items, item)
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	total := len(items)
	start := minInt(filter.Offset, total)
	end := total
	if filter.Limit > 0 {
		end = minInt(start+filter.Limit, total)
	}
	return Page{Items: items[start:end], Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (s *Service) CreateCategory(_ context.Context, input CategoryInput, tenantID, orgID string) (Category, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Category{}, ErrInvalidCategory
	}
	parentID := strings.TrimSpace(input.ParentID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if parentID != "" {
		p, ok := s.categories[parentID]
		if !ok {
			return Category{}, ErrCategoryNotFound
		}
		if p.TenantID != tenantID || p.OrgID != orgID {
			return Category{}, ErrCategoryAccessDenied
		}
	}
	id, err := newID()
	if err != nil {
		return Category{}, err
	}
	now := s.clock().UTC()
	c := Category{ID: id, Name: name, ParentID: parentID, TenantID: strings.TrimSpace(tenantID), OrgID: strings.TrimSpace(orgID), CreatedAt: now, UpdatedAt: now}
	s.categories[id] = c
	return c, nil
}

func (s *Service) ListCategories(_ context.Context, tenantID, orgID string) []Category {
	s.mu.RLock()
	out := make([]Category, 0)
	for _, c := range s.categories {
		if c.TenantID == tenantID && c.OrgID == orgID {
			out = append(out, c)
		}
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].ParentID == out[j].ParentID {
			return out[i].Name < out[j].Name
		}
		return out[i].ParentID < out[j].ParentID
	})
	return out
}

func (s *Service) UpdateCategory(_ context.Context, id string, input CategoryInput, tenantID, orgID string) (Category, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.categories[strings.TrimSpace(id)]
	if !ok {
		return Category{}, ErrCategoryNotFound
	}
	if c.TenantID != tenantID || c.OrgID != orgID {
		return Category{}, ErrCategoryAccessDenied
	}
	if strings.TrimSpace(input.Name) != "" {
		c.Name = strings.TrimSpace(input.Name)
	}
	if input.ParentID != "" {
		p, exists := s.categories[strings.TrimSpace(input.ParentID)]
		if !exists {
			return Category{}, ErrCategoryNotFound
		}
		if p.TenantID != tenantID || p.OrgID != orgID || p.ID == c.ID {
			return Category{}, ErrCategoryAccessDenied
		}
		for ancestor := p; ancestor.ParentID != ""; {
			if ancestor.ParentID == c.ID {
				return Category{}, ErrInvalidCategory
			}
			next, ok := s.categories[ancestor.ParentID]
			if !ok {
				break
			}
			ancestor = next
		}
		c.ParentID = p.ID
	}
	c.UpdatedAt = s.clock().UTC()
	s.categories[c.ID] = c
	return c, nil
}

func (s *Service) DeleteCategory(_ context.Context, id, tenantID, orgID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	c, ok := s.categories[id]
	if !ok {
		return ErrCategoryNotFound
	}
	if c.TenantID != tenantID || c.OrgID != orgID {
		return ErrCategoryAccessDenied
	}
	for _, child := range s.categories {
		if child.ParentID == id {
			return ErrCategoryNotEmpty
		}
	}
	for _, f := range s.files {
		if f.CategoryID == id {
			return ErrCategoryNotEmpty
		}
	}
	delete(s.categories, id)
	return nil
}

func (s *Service) Download(ctx context.Context, id, subject, tenantID, orgID string) (File, Object, error) {
	file, err := s.authorize(id, subject, tenantID, orgID)
	if err != nil {
		return File{}, Object{}, err
	}
	reader, ok := s.store.(readableStore)
	if !ok {
		return File{}, Object{}, ErrStorageRead
	}
	object, err := reader.Get(ctx, file.Key)
	if err != nil {
		if errors.Is(err, ErrFileNotFound) {
			return File{}, Object{}, err
		}
		return File{}, Object{}, fmt.Errorf("get file: %w", err)
	}
	object.Name, object.MIME, object.Size, object.OwnerID, object.TenantID, object.OrgID, object.ACL, object.CreatedAt, object.SHA256 = file.Name, file.MIME, file.Size, file.OwnerID, file.TenantID, file.OrgID, file.ACL, file.CreatedAt, file.SHA256
	return file, object, nil
}

func (s *Service) DeleteFile(ctx context.Context, id, subject, tenantID, orgID string) error {
	file, err := s.authorize(id, subject, tenantID, orgID)
	if err != nil {
		return err
	}
	if err = s.store.Delete(ctx, file.Key); err != nil && !errors.Is(err, ErrFileNotFound) {
		return fmt.Errorf("delete file: %w", err)
	}
	s.mu.Lock()
	delete(s.files, id)
	s.mu.Unlock()
	return nil
}

func (s *Service) CleanupDryRun(_ context.Context, age time.Duration, scopes ...string) (CleanupReport, error) {
	if age < 0 {
		return CleanupReport{}, ErrInvalidUpload
	}
	now := s.clock().UTC()
	report := CleanupReport{Cutoff: now.Add(-age)}
	tenantID, orgID := "", ""
	if len(scopes) > 0 {
		tenantID = strings.TrimSpace(scopes[0])
	}
	if len(scopes) > 1 {
		orgID = strings.TrimSpace(scopes[1])
	}
	s.mu.RLock()
	for _, item := range s.files {
		if tenantID != "" && item.TenantID != tenantID {
			continue
		}
		if orgID != "" && item.OrgID != orgID {
			continue
		}
		if !item.CreatedAt.After(report.Cutoff) {
			report.MatchingCount++
			report.Bytes += item.Size
		}
	}
	s.mu.RUnlock()
	return report, nil
}

func (s *Service) authorize(id, subject, tenantID, orgID string) (File, error) {
	s.mu.RLock()
	file, ok := s.files[strings.TrimSpace(id)]
	s.mu.RUnlock()
	if !ok {
		return File{}, ErrFileNotFound
	}
	if tenantID != "" && file.TenantID != "" && file.TenantID != tenantID {
		return File{}, ErrAccessDenied
	}
	if orgID != "" && file.OrgID != "" && file.OrgID != orgID {
		return File{}, ErrAccessDenied
	}
	if file.ACL != ACLPublicRead && file.OwnerID != "" && file.OwnerID != subject {
		return File{}, ErrAccessDenied
	}
	return file, nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func (s *Service) SignedURL(ctx context.Context, id, subject string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", ErrInvalidUpload
	}
	file, err := s.authorize(id, subject, "", "")
	if err != nil {
		return "", err
	}
	url, err := s.store.SignURL(ctx, file.Key, ttl)
	if err != nil {
		return "", fmt.Errorf("sign file URL: %w", err)
	}
	return url, nil
}

// SignedURLFor applies tenant and organization checks before provider signing.
func (s *Service) SignedURLFor(ctx context.Context, id, subject, tenantID, orgID string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", ErrInvalidUpload
	}
	file, err := s.authorize(id, subject, tenantID, orgID)
	if err != nil {
		return "", err
	}
	url, err := s.store.SignURL(ctx, file.Key, ttl)
	if err != nil {
		return "", fmt.Errorf("sign file URL: %w", err)
	}
	return url, nil
}

// Authorize checks metadata ACL without requiring the provider to support a
// read operation, keeping remote provider signing compatible with this seam.
func (s *Service) Authorize(id, subject, tenantID, orgID string) (File, error) {
	return s.authorize(id, subject, tenantID, orgID)
}

func (s *Service) Cleanup(ctx context.Context, age time.Duration) error {
	if age < 0 {
		return ErrInvalidUpload
	}
	cutoff := s.clock().UTC().Add(-age)
	s.mu.RLock()
	expired := make([]File, 0)
	for _, file := range s.files {
		if !file.CreatedAt.After(cutoff) {
			expired = append(expired, file)
		}
	}
	s.mu.RUnlock()
	for _, file := range expired {
		if err := s.store.Delete(ctx, file.Key); err != nil {
			return fmt.Errorf("delete file %s: %w", file.ID, err)
		}
		s.mu.Lock()
		delete(s.files, file.ID)
		s.mu.Unlock()
	}
	return nil
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
