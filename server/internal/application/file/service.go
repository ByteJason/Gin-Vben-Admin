// Package file defines the provider-independent file-center application seam.
// Concrete object stores (for example, S3-compatible providers) can implement
// Store without changing validation, authorization, or lifecycle policy.
package file

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
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
	ErrObjectExists         = errors.New("file object already exists")
	ErrStorageRead          = errors.New("file storage does not support reads")
	ErrSignedURLUnsupported = errors.New("file storage does not support verified signed URLs")
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
	Extension  string
	ETag       string
}

// Store is the only object-storage dependency required by Service.
type Store interface {
	Put(context.Context, Object) error
	Delete(context.Context, string) error
	SignURL(context.Context, string, time.Duration) (string, error)
}

type StreamStore interface {
	Store
	PutStream(context.Context, string, io.Reader, Object) error
	Open(context.Context, string) (io.ReadCloser, error)
}

type StagingStore interface {
	StreamStore
	Promote(context.Context, string, string) error
}

// privateStagingStore separates provider-owned staging names from ordinary
// object keys. LocalStore implements this interface so callers cannot use the
// public PutStream/Delete methods to write or remove another upload's staging
// object merely by choosing a key below `.staging`.
type privateStagingStore interface {
	StagingStore
	PutStaging(context.Context, string, io.Reader, Object) error
	DeleteStaging(context.Context, string) error
}

// OpaqueURLSigner lets the application expose an opaque file ID while the
// provider keeps its object key internal. The signature binds both values, so
// a URL cannot be retargeted by changing either the ID or the provider key.
// Store.SignURL remains for source compatibility with older providers.
type OpaqueURLSigner interface {
	SignURLForID(context.Context, string, string, time.Duration) (string, error)
	VerifyIDURL(string, string, string) error
}

type Config struct {
	MaxBytes        int64
	AllowedMIMEs    []string
	Clock           func() time.Time
	Repository      FileRepository
	UsageRepository UsageRepository
	UsageService    MediaUsageService
}

type File struct {
	ID            string            `json:"id"`
	Key           string            `json:"key"`
	ObjectKey     string            `json:"-"`
	ProviderID    string            `json:"-"`
	Bucket        string            `json:"-"`
	Name          string            `json:"name"`
	MIME          string            `json:"mime"`
	Size          int64             `json:"size"`
	OwnerID       string            `json:"ownerId"`
	TenantID      string            `json:"tenantId"`
	OrgID         string            `json:"orgId"`
	ACL           ACL               `json:"acl"`
	CreatedAt     time.Time         `json:"createdAt"`
	SHA256        string            `json:"sha256"`
	CategoryID    string            `json:"categoryId,omitempty"`
	Metadata      map[string]string `json:"-"`
	Extension     string            `json:"extension,omitempty"`
	ETag          string            `json:"etag,omitempty"`
	Status        MediaStatus       `json:"status,omitempty"`
	ScanStatus    string            `json:"scanStatus,omitempty"`
	FailureReason string            `json:"failureReason,omitempty"`
	UpdatedAt     time.Time         `json:"updatedAt"`
	DeletedAt     *time.Time        `json:"deletedAt,omitempty"`
}

type ListFilter struct {
	TenantID   string
	OrgID      string
	OwnerID    string
	Limit      int
	Offset     int
	CategoryID string
	Status     MediaStatus
	MIME       string
	MIMEFamily string
}

type Category struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentID  string    `json:"parentId,omitempty"`
	TenantID  string    `json:"tenantId,omitempty"`
	OrgID     string    `json:"orgId,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
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

// fileAccess is the application-internal authorization input. Legacy public
// methods always construct a non-privileged value; CatalogAdapter derives the
// platformAdmin bit only from a validated tenant context.
type fileAccess struct {
	subject       string
	tenantID      string
	orgID         string
	platformAdmin bool
}

type Service struct {
	store        Store
	maxBytes     int64
	allowed      map[string]struct{}
	clock        func() time.Time
	repo         FileRepository
	usageRepo    UsageRepository
	usageService MediaUsageService
	mu           sync.RWMutex
	files        map[string]File
	// deleted retains tombstones until the object-store cleanup worker removes
	// the provider object. The durable repository is authoritative when it is
	// configured; this map exists only for dependency-free provider fixtures.
	deleted    map[string]time.Time
	categories map[string]Category
}

func NewService(store Store, config Config) *Service {
	if config.MaxBytes <= 0 {
		config.MaxBytes = 100 << 20
	}
	allowed := make(map[string]struct{}, len(config.AllowedMIMEs))
	for _, mime := range config.AllowedMIMEs {
		allowed[strings.ToLower(strings.TrimSpace(mime))] = struct{}{}
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	usageRepo := config.UsageRepository
	if usageRepo == nil {
		if candidate, ok := config.Repository.(UsageRepository); ok {
			usageRepo = candidate
		}
	}
	return &Service{store: store, repo: config.Repository, usageRepo: usageRepo, usageService: config.UsageService, maxBytes: config.MaxBytes, allowed: allowed, clock: clock, files: make(map[string]File), deleted: make(map[string]time.Time), categories: make(map[string]Category)}
}

// SetRepository installs the durable metadata authority after dependency
// construction (bootstrap opens the database after the local provider).
func (s *Service) SetRepository(repo FileRepository) {
	if s != nil {
		s.repo = repo
	}
}

func (s *Service) SetUsageRepository(repo UsageRepository) {
	if s != nil {
		s.usageRepo = repo
	}
}

func (s *Service) SetUsageService(usage MediaUsageService) {
	if s != nil {
		s.usageService = usage
	}
}

func (s *Service) Upload(ctx context.Context, input UploadInput) (File, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.store == nil || strings.TrimSpace(input.Name) == "" || input.Size < -1 {
		return File{}, ErrInvalidUpload
	}
	if s.maxBytes > 0 && input.Size >= 0 && input.Size > s.maxBytes {
		return File{}, ErrFileTooLarge
	}
	if input.Reader == nil && input.Data != nil && int64(len(input.Data)) != input.Size {
		return File{}, fmt.Errorf("%w: size does not match data", ErrInvalidUpload)
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
	name := strings.TrimSpace(input.Name)
	if name == "" || strings.ContainsRune(name, '\x00') || strings.ContainsAny(name, "\r\n/\\") || len(name) > 255 {
		return File{}, ErrInvalidUpload
	}
	reader := input.Reader
	if reader == nil {
		reader = bytes.NewReader(input.Data)
	}
	// A stable, versioned key with two shard levels keeps directory fan-out
	// bounded while preserving the real extension inferred from MIME.
	ext := inferredExtension(strings.TrimSpace(input.MIME), name)
	key := fmt.Sprintf("v1/%s/%s/%s%s", id[:2], id[2:4], id, ext)
	file := File{ID: id, Key: id, ObjectKey: key, Name: name, MIME: strings.TrimSpace(input.MIME), Size: input.Size, OwnerID: strings.TrimSpace(input.OwnerID), TenantID: strings.TrimSpace(input.TenantID), OrgID: strings.TrimSpace(input.OrgID), ACL: acl, CreatedAt: now, UpdatedAt: now, SHA256: "", CategoryID: categoryID, Metadata: cloneStringMap(input.Metadata), Extension: strings.TrimPrefix(ext, "."), Status: MediaPending}
	if s.repo != nil {
		if err := s.repo.Create(ctx, file); err != nil {
			return File{}, err
		}
	}
	stream := &observedReader{ctx: ctx, reader: reader, hash: sha256.New(), sample: make([]byte, 0, 512)}
	stageKey := ".staging/" + id
	var writeErr error
	published := false
	providerAttempted := false
	if staged, ok := s.store.(StagingStore); ok {
		providerAttempted = true
		if privateStage, privateOK := staged.(privateStagingStore); privateOK {
			writeErr = privateStage.PutStaging(ctx, stageKey, limitedReader(stream, s.maxBytes), Object{Key: stageKey, Size: -1})
		} else {
			writeErr = staged.PutStream(ctx, stageKey, limitedReader(stream, s.maxBytes), Object{Key: stageKey, Size: -1})
		}
		if writeErr == nil {
			file.Size, file.SHA256, file.MIME, file.Extension = stream.n, hex.EncodeToString(stream.hash.Sum(nil)), detectedMIME(stream.sample, input.MIME), normalizedExtension(detectedMIME(stream.sample, input.MIME), name)
			file.ETag = file.SHA256
			if err := s.validateObserved(input, file); err != nil {
				_ = s.deleteStaging(context.Background(), stageKey)
				s.markFailed(ctx, file, err)
				return File{}, err
			}
			file.ObjectKey = fmt.Sprintf("v1/%s/%s/%s%s", id[:2], id[2:4], id, extensionSuffix(file.Extension))
			key = file.ObjectKey
			// Publish the final key while the row is still pending. If the
			// subsequent ready update fails, reconciliation can still locate the
			// promoted object without guessing an extension.
			if s.repo != nil {
				file.UpdatedAt = s.clock().UTC()
				writeErr = s.repo.Update(ctx, file)
			}
			if writeErr == nil {
				providerAttempted = true
				writeErr = staged.Promote(ctx, stageKey, file.ObjectKey)
				if writeErr == nil {
					published = true
				}
			}
			if writeErr == nil {
				file.Status = MediaReady
				file.UpdatedAt = s.clock().UTC()
				writeErr = s.persistReady(ctx, file)
			}
		}
	} else if streamStore, ok := s.store.(StreamStore); ok {
		// A streaming provider without a native staging/promote primitive still
		// gets the same validate-before-publish semantics. The request is copied
		// to a private OS temporary file (bounded and context-aware), then that
		// file is streamed once to the provider under the MIME-derived key. This
		// avoids buffering a large request in the Go heap and prevents a client
		// filename extension from becoming authoritative.
		var temporary *os.File
		temporary, stream, writeErr = stageToTemp(ctx, stream, s.maxBytes)
		if writeErr == nil {
			file.Size, file.SHA256, file.MIME, file.Extension = stream.n, hex.EncodeToString(stream.hash.Sum(nil)), detectedMIME(stream.sample, input.MIME), normalizedExtension(detectedMIME(stream.sample, input.MIME), name)
			file.ETag = file.SHA256
			if err := s.validateObserved(input, file); err != nil {
				_ = temporary.Close()
				_ = os.Remove(temporary.Name())
				s.markFailed(ctx, file, err)
				return File{}, err
			}
			file.ObjectKey = fmt.Sprintf("v1/%s/%s/%s%s", id[:2], id[2:4], id, extensionSuffix(file.Extension))
			key = file.ObjectKey
			// Persist the final key and observed metadata while the row remains
			// pending. If the provider write succeeds but the ready transition
			// later fails, reconciliation has an exact key to probe.
			if s.repo != nil {
				file.Status = MediaPending
				file.UpdatedAt = s.clock().UTC()
				writeErr = s.repo.Update(ctx, file)
			}
			if writeErr != nil {
				// The outer failure path marks the pending row failed and removes
				// the private temporary file; no provider object was published.
			} else if _, err := temporary.Seek(0, io.SeekStart); err != nil {
				writeErr = err
			} else {
				providerAttempted = true
				writeErr = streamStore.PutStream(ctx, file.ObjectKey, io.LimitReader(temporary, file.Size), Object{Key: file.ObjectKey, Name: name, MIME: file.MIME, Size: file.Size, OwnerID: file.OwnerID, TenantID: file.TenantID, OrgID: file.OrgID, ACL: acl, CreatedAt: now, SHA256: file.SHA256, Extension: file.Extension, ETag: file.ETag, CategoryID: categoryID})
			}
			closeErr := temporary.Close()
			removeErr := os.Remove(temporary.Name())
			if writeErr == nil && closeErr != nil {
				writeErr = closeErr
			}
			if writeErr == nil && removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				writeErr = removeErr
			}
			if writeErr == nil {
				published = true
				file.Status = MediaReady
				file.UpdatedAt = s.clock().UTC()
				writeErr = s.persistReady(ctx, file)
			}
		} else if temporary != nil {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}
	} else {
		// Legacy providers only expose []byte; preserve compatibility for
		// existing fixtures while all production providers use StreamStore.
		var buffer bytes.Buffer
		written, readErr := io.Copy(&buffer, limitedReader(reader, s.maxBytes))
		data := buffer.Bytes()
		if readErr != nil {
			writeErr = readErr
		} else if s.maxBytes > 0 && written > s.maxBytes {
			writeErr = ErrFileTooLarge
		} else {
			stream.n = int64(len(data))
			sum := sha256.Sum256(data)
			stream.hash = sha256.New()
			_, _ = stream.hash.Write(data)
			file.Size, file.SHA256, file.MIME, file.Extension = stream.n, hex.EncodeToString(sum[:]), detectedMIME(data[:minInt(len(data), 512)], input.MIME), normalizedExtension(detectedMIME(data[:minInt(len(data), 512)], input.MIME), name)
			file.ETag = file.SHA256
			if writeErr == nil {
				writeErr = s.validateObserved(input, file)
			}
			if writeErr == nil {
				file.ObjectKey = fmt.Sprintf("v1/%s/%s/%s%s", id[:2], id[2:4], id, extensionSuffix(file.Extension))
				key = file.ObjectKey
				if s.repo != nil {
					file.Status = MediaPending
					file.UpdatedAt = s.clock().UTC()
					writeErr = s.repo.Update(ctx, file)
				}
			}
			if writeErr == nil {
				providerAttempted = true
				writeErr = s.store.Put(ctx, Object{Key: key, Name: name, MIME: file.MIME, Size: file.Size, OwnerID: file.OwnerID, TenantID: file.TenantID, OrgID: file.OrgID, ACL: acl, CreatedAt: now, SHA256: file.SHA256, Data: data, CategoryID: categoryID, Extension: file.Extension, ETag: file.ETag})
				if writeErr == nil {
					published = true
				}
			}
			if writeErr == nil {
				file.Status = MediaReady
				writeErr = s.persistReady(ctx, file)
			}
		}
	}
	if writeErr != nil {
		if !published {
			if _, staged := s.store.(StagingStore); staged && !errors.Is(writeErr, ErrObjectExists) {
				_ = s.deleteStaging(context.Background(), stageKey)
			} else if providerAttempted && !errors.Is(writeErr, ErrObjectExists) {
				_ = s.store.Delete(context.Background(), key)
			}
			s.markFailed(ctx, file, writeErr)
		} else {
			// The provider object is already durable. Keep the repository row in
			// pending state for ReconcilePending instead of deleting the object or
			// overwriting a transient metadata-write failure with failed.
			slog.Default().Warn("file object published before metadata update", "file_id", file.ID)
		}
		return File{}, fmt.Errorf("put file: %w", writeErr)
	}
	s.mu.Lock()
	s.files[id] = file
	s.mu.Unlock()
	return file, nil
}

func (s *Service) deleteStaging(ctx context.Context, stageKey string) error {
	if privateStage, ok := s.store.(privateStagingStore); ok {
		return privateStage.DeleteStaging(ctx, stageKey)
	}
	return s.store.Delete(ctx, stageKey)
}

func (s *Service) persistReady(ctx context.Context, file File) error {
	if s.repo != nil {
		return s.repo.Update(ctx, file)
	}
	return nil
}
func (s *Service) markFailed(ctx context.Context, file File, cause error) {
	file.Status = MediaFailed
	file.FailureReason = failureReason(cause)
	file.UpdatedAt = s.clock().UTC()
	if s.repo != nil {
		// A cancelled request must still leave a durable terminal failure state;
		// retain context values for tenancy/trace fields while detaching the
		// cancellation signal from this best-effort lifecycle write.
		if ctx == nil {
			ctx = context.Background()
		}
		persistCtx := context.WithoutCancel(ctx)
		_ = s.repo.MarkStatus(persistCtx, file.ID, MediaFailed, file.FailureReason, nil)
		return
	}
	s.mu.Lock()
	s.files[file.ID] = file
	s.mu.Unlock()
}

func failureReason(cause error) string {
	switch {
	case cause == nil:
		return "upload failed"
	case errors.Is(cause, ErrFileTooLarge):
		return "file exceeds configured size limit"
	case errors.Is(cause, ErrMIMETypeNotAllowed):
		return "file MIME type is not allowed"
	case errors.Is(cause, context.Canceled), errors.Is(cause, context.DeadlineExceeded):
		return "upload canceled"
	case errors.Is(cause, ErrInvalidUpload):
		return "invalid file upload"
	default:
		// Provider errors can contain absolute paths or connection details;
		// keep those out of durable metadata and structured logs.
		return "provider write failed"
	}
}

type observedReader struct {
	ctx    context.Context
	reader io.Reader
	hash   hash.Hash
	sample []byte
	n      int64
}

func (r *observedReader) Read(p []byte) (int, error) {
	if r.ctx != nil {
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		default:
		}
	}
	n, err := r.reader.Read(p)
	if n > 0 {
		r.n += int64(n)
		_, _ = r.hash.Write(p[:n])
		if len(r.sample) < 512 {
			take := minInt(512-len(r.sample), n)
			r.sample = append(r.sample, p[:take]...)
		}
	}
	return n, err
}
func limitedReader(r io.Reader, max int64) io.Reader {
	if max <= 0 {
		return r
	}
	// Avoid wrapping at the signed integer boundary. A caller-provided limit
	// of MaxInt64 is already an effective unlimited stream for io.LimitReader.
	if max == int64(^uint64(0)>>1) {
		return r
	}
	return io.LimitReader(r, max+1)
}

// stageToTemp copies an observed stream to a mode-0600 temporary file. The
// observed reader remains the single source of byte count, digest and MIME
// sample; the returned file is rewound by the caller before provider upload.
func stageToTemp(ctx context.Context, observed *observedReader, max int64) (*os.File, *observedReader, error) {
	if observed == nil || observed.reader == nil {
		return nil, observed, ErrInvalidUpload
	}
	tmp, err := os.CreateTemp("", "file-upload-*")
	if err != nil {
		return nil, observed, fmt.Errorf("create upload staging file: %w", err)
	}
	cleanup := func(cause error) (*os.File, *observedReader, error) {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, observed, cause
	}
	if err := tmp.Chmod(0o600); err != nil {
		return cleanup(fmt.Errorf("secure upload staging file: %w", err))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_, err = io.Copy(tmp, limitedReader(observed, max))
	if err != nil {
		return cleanup(fmt.Errorf("stage upload: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		return cleanup(fmt.Errorf("flush upload staging file: %w", err))
	}
	return tmp, observed, nil
}

func detectedMIME(sample []byte, _ string) string {
	// net/http intentionally classifies XML/SVG as text/plain. Recognize only
	// bounded, unambiguous signatures before falling back to its sniffing table;
	// the caller-provided MIME is never used to relabel bytes.
	trimmed := strings.TrimSpace(strings.TrimPrefix(string(sample), "\ufeff"))
	if looksLikeSVG(trimmed) {
		return "image/svg+xml"
	}
	if looksLikeJSON(trimmed, sample) {
		return "application/json"
	}
	detected := http.DetectContentType(sample)
	if parsed, _, err := mime.ParseMediaType(detected); err == nil {
		detected = parsed
	}
	return strings.ToLower(detected)
}

func looksLikeSVG(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "<?xml") {
		if end := strings.Index(lower, "?>"); end >= 0 {
			lower = strings.TrimSpace(lower[end+2:])
		}
	}
	if !strings.HasPrefix(lower, "<svg") {
		return false
	}
	if len(lower) == 4 {
		return true
	}
	next := lower[4]
	return next == ' ' || next == '\t' || next == '\r' || next == '\n' || next == '>'
}

func looksLikeJSON(trimmed string, sample []byte) bool {
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return false
	}
	// json.Valid is deliberately bounded by observedReader's 512-byte sample;
	// a valid prefix alone is not enough, so only classify complete samples.
	return json.Valid(sample)
}

func inferredExtension(mimeType, name string) string {
	common := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "image/svg+xml": ".svg", "text/plain": ".txt", "application/pdf": ".pdf", "application/json": ".json", "application/zip": ".zip"}
	if ext, ok := common[strings.ToLower(strings.TrimSpace(mimeType))]; ok {
		return ext
	}
	if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
		return exts[0]
	}
	// An unknown or client-only MIME has no trustworthy suffix.  Never copy a
	// user supplied filename extension into the object key.
	return ""
}

func normalizedExtension(mimeType, name string) string {
	return strings.TrimPrefix(inferredExtension(mimeType, name), ".")
}
func extensionSuffix(ext string) string {
	ext = strings.TrimPrefix(strings.TrimSpace(ext), ".")
	if ext == "" {
		return ""
	}
	return "." + ext
}
func (s *Service) validateObserved(input UploadInput, file File) error {
	if s.maxBytes > 0 && file.Size > s.maxBytes {
		return ErrFileTooLarge
	}
	if input.Size >= 0 && file.Size != input.Size {
		return fmt.Errorf("%w: size does not match data", ErrInvalidUpload)
	}
	if len(s.allowed) > 0 {
		if _, ok := s.allowed[strings.ToLower(file.MIME)]; !ok {
			return ErrMIMETypeNotAllowed
		}
	}
	return nil
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

func (s *Service) List(ctx context.Context, filter ListFilter) (Page, error) {
	if filter.Limit < 0 || filter.Offset < 0 {
		return Page{}, ErrInvalidUpload
	}
	if s.repo != nil {
		return s.repo.List(ctx, filter)
	}
	s.mu.RLock()
	items := make([]File, 0, len(s.files))
	for _, item := range s.files {
		if _, removed := s.deleted[item.ID]; removed {
			continue
		}
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
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if filter.MIME != "" && !strings.EqualFold(item.MIME, filter.MIME) {
			continue
		}
		if filter.MIMEFamily != "" && !mimeMatchesFamily(item.MIME, filter.MIMEFamily) {
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
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	c := Category{ID: id, Name: name, ParentID: parentID, TenantID: strings.TrimSpace(tenantID), OrgID: strings.TrimSpace(orgID), Enabled: enabled, CreatedAt: now, UpdatedAt: now}
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

// ListAllCategories is reserved for an explicitly resolved platform
// administrator.  The legacy ListCategories method keeps its exact-scope
// semantics for tenant handlers, while the catalog adapter can still inspect
// every scope when it is building an administrative picker or mutating a
// cross-scope category.
func (s *Service) ListAllCategories(_ context.Context) []Category {
	s.mu.RLock()
	out := make([]Category, 0, len(s.categories))
	for _, category := range s.categories {
		out = append(out, category)
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

// GetCategory returns a detached category for catalog adapters that have
// already passed a platform-admin authorization check.
func (s *Service) GetCategory(_ context.Context, id string) (Category, bool) {
	s.mu.RLock()
	category, ok := s.categories[strings.TrimSpace(id)]
	s.mu.RUnlock()
	return category, ok
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
	if input.Enabled != nil {
		c.Enabled = *input.Enabled
	}
	c.UpdatedAt = s.clock().UTC()
	s.categories[c.ID] = c
	return c, nil
}

func (s *Service) DeleteCategory(ctx context.Context, id, tenantID, orgID string) error {
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
	if s.repo != nil {
		page, err := s.repo.List(ctx, ListFilter{TenantID: tenantID, OrgID: orgID, CategoryID: id, Limit: 1})
		if err != nil {
			return err
		}
		if page.Total > 0 {
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
	return s.downloadWithAccess(ctx, id, fileAccess{subject: subject, tenantID: tenantID, orgID: orgID})
}

// DownloadStream is the preferred download API. It returns a bounded,
// context-aware reader and never materializes the object payload in the
// application heap. Download remains as a compatibility adapter for callers
// that explicitly require Object.Data.
func (s *Service) DownloadStream(ctx context.Context, id, subject, tenantID, orgID string) (File, io.ReadCloser, error) {
	return s.Open(ctx, id, subject, tenantID, orgID)
}

// Get returns durable metadata without opening the provider object.  HTTP
// detail/list callers use this path so a streaming-only provider never has to
// buffer file bytes merely to render metadata.
func (s *Service) Get(ctx context.Context, id, subject, tenantID, orgID string) (File, error) {
	return s.authorizeAccessWithContext(ctx, id, fileAccess{subject: subject, tenantID: tenantID, orgID: orgID})
}

func (s *Service) downloadWithAccess(ctx context.Context, id string, access fileAccess) (File, Object, error) {
	file, reader, err := s.openWithAccess(ctx, id, access)
	if err != nil {
		return File{}, Object{}, err
	}
	defer reader.Close()
	limit := file.Size
	if limit < 0 {
		return File{}, Object{}, ErrInvalidUpload
	}
	if s.maxBytes > 0 && limit > s.maxBytes {
		limit = s.maxBytes
	}
	var data bytes.Buffer
	if _, err := io.Copy(&data, io.LimitReader(reader, limit)); err != nil {
		return File{}, Object{}, fmt.Errorf("read file: %w", err)
	}
	object := Object{Key: storageKey(file), Data: data.Bytes()}
	object.Name, object.MIME, object.Size, object.OwnerID, object.TenantID, object.OrgID, object.ACL, object.CreatedAt, object.SHA256, object.Extension, object.ETag = file.Name, file.MIME, file.Size, file.OwnerID, file.TenantID, file.OrgID, file.ACL, file.CreatedAt, file.SHA256, file.Extension, file.ETag
	return file, object, nil
}

// Open returns a streaming object reader. Callers must close it; no payload
// is buffered in application memory for providers implementing StreamStore.
func (s *Service) Open(ctx context.Context, id, subject, tenantID, orgID string) (File, io.ReadCloser, error) {
	return s.openWithAccess(ctx, id, fileAccess{subject: subject, tenantID: tenantID, orgID: orgID})
}

func (s *Service) openWithAccess(ctx context.Context, id string, access fileAccess) (File, io.ReadCloser, error) {
	file, err := s.authorizeAccessWithContext(ctx, id, access)
	if err != nil {
		return File{}, nil, err
	}
	if file.Status != "" && file.Status != MediaReady {
		return File{}, nil, ErrMediaNotReady
	}
	if stream, ok := s.store.(interface {
		Open(context.Context, string) (io.ReadCloser, error)
	}); ok {
		reader, err := stream.Open(ctx, storageKey(file))
		if err != nil {
			if errors.Is(err, ErrFileNotFound) && s.repo != nil {
				s.markDamaged(ctx, file, "object missing")
			}
			return File{}, nil, err
		}
		return file, s.checkedReader(file, reader), nil
	}
	reader, ok := s.store.(readableStore)
	if !ok {
		return File{}, nil, ErrStorageRead
	}
	object, err := reader.Get(ctx, storageKey(file))
	if err != nil {
		return File{}, nil, err
	}
	return file, s.checkedReader(file, io.NopCloser(bytes.NewReader(object.Data))), nil
}

func (s *Service) checkedReader(file File, reader io.ReadCloser) io.ReadCloser {
	if reader == nil || file.Size < 0 {
		return reader
	}
	return &sizeCheckedReader{ReadCloser: reader, remain: file.Size, onShort: func() {
		if s.repo != nil {
			s.markDamaged(context.Background(), file, "object shorter than metadata")
		}
	}}
}

type sizeCheckedReader struct {
	io.ReadCloser
	remain  int64
	onShort func()
	marked  bool
}

func (r *sizeCheckedReader) Read(p []byte) (int, error) {
	if r.remain <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remain {
		p = p[:r.remain]
	}
	n, err := r.ReadCloser.Read(p)
	r.remain -= int64(n)
	if err == io.EOF && r.remain > 0 && !r.marked {
		r.marked = true
		if r.onShort != nil {
			r.onShort()
		}
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

func (s *Service) markDamaged(ctx context.Context, file File, reason string) {
	if s.repo != nil {
		_ = s.repo.MarkStatus(ctx, file.ID, MediaDamaged, reason, nil)
	}
	// Keep diagnostics metadata-only: IDs and a bounded reason never include
	// object contents, credentials, or the provider's full filesystem path.
	slog.Default().Warn("file object is damaged", "file_id", file.ID, "reason", reason)
}

func (s *Service) DeleteFile(ctx context.Context, id, subject, tenantID, orgID string) error {
	file, err := s.authorizeMutationWithContext(ctx, id, fileAccess{subject: subject, tenantID: tenantID, orgID: orgID})
	if err != nil {
		return err
	}
	if s.repo != nil {
		if file.Status == MediaDeleted {
			return ErrFileNotFound
		}
		if file.Status == MediaDeleting {
			return nil
		}
		if deletionRepo, ok := s.repo.(DeletionRepository); ok {
			return deletionRepo.RequestDeletion(ctx, file.ID, false, s.clock().UTC())
		}
		if s.usageRepo != nil {
			count, countErr := s.usageRepo.CountByResource(ctx, file.ID)
			if countErr != nil {
				return countErr
			}
			if count > 0 {
				return ErrMediaInUse
			}
		}
		return s.repo.MarkStatus(ctx, file.ID, MediaDeleting, "", nil)
	}
	return s.softDeleteFileWithAccess(ctx, id, fileAccess{subject: subject, tenantID: tenantID, orgID: orgID}, s.clock().UTC())
}

// SoftDeleteFile marks a resource deleted while leaving its provider object
// available for an asynchronous cleanup worker. Reads and listings treat the
// tombstone as absent, and the operation remains scope/ACL checked.
func (s *Service) SoftDeleteFile(ctx context.Context, id, subject, tenantID, orgID string, at time.Time) error {
	return s.softDeleteFileWithAccess(ctx, id, fileAccess{subject: subject, tenantID: tenantID, orgID: orgID}, at)
}

// ForceDeleteFile is the explicit-confirmation path used by the catalog when
// an operator has acknowledged that existing business references may break.
func (s *Service) ForceDeleteFile(ctx context.Context, id, subject, tenantID, orgID string, at time.Time) error {
	return s.softDeleteFileWithAccess(ctx, id, fileAccess{subject: subject, tenantID: tenantID, orgID: orgID}, at, true)
}

func (s *Service) softDeleteFileWithAccess(ctx context.Context, id string, access fileAccess, at time.Time, force ...bool) error {
	if s == nil {
		return ErrFileNotFound
	}
	file, err := s.authorizeMutationWithContext(ctx, id, access)
	if err != nil {
		return err
	}
	if file.Status == MediaDeleted {
		return ErrFileNotFound
	}
	if file.Status == MediaDeleting {
		return nil
	}
	forced := len(force) > 0 && force[0]
	if s.repo != nil {
		if deletionRepo, ok := s.repo.(DeletionRepository); ok {
			return deletionRepo.RequestDeletion(ctx, file.ID, forced, at.UTC())
		}
		if s.usageRepo != nil && !forced {
			if count, countErr := s.usageRepo.CountByResource(ctx, file.ID); countErr != nil {
				return countErr
			} else if count > 0 {
				return ErrMediaInUse
			}
		}
		return s.repo.MarkStatus(ctx, file.ID, MediaDeleting, "", &at)
	}
	s.mu.Lock()
	if _, already := s.deleted[file.ID]; already {
		s.mu.Unlock()
		return nil
	}
	if s.deleted == nil {
		s.deleted = make(map[string]time.Time)
	}
	deletedAt := at.UTC()
	file.Status = MediaDeleting
	file.UpdatedAt = deletedAt
	file.DeletedAt = &deletedAt
	s.files[file.ID] = file
	s.deleted[file.ID] = deletedAt
	s.mu.Unlock()
	return nil
}

// CleanupDeleted permanently removes tombstoned objects older than age and
// returns the number of provider objects reclaimed. It is a narrow seam for a
// jobs worker; callers keep authorization and scheduling outside this method.
func (s *Service) CleanupDeleted(ctx context.Context, age time.Duration) (int, error) {
	if s == nil || s.store == nil || age < 0 {
		return 0, ErrInvalidUpload
	}
	cutoff := s.clock().UTC().Add(-age)
	s.mu.RLock()
	ids := make([]string, 0)
	for id, deletedAt := range s.deleted {
		if !deletedAt.After(cutoff) {
			ids = append(ids, id)
		}
	}
	s.mu.RUnlock()
	removed := 0
	for _, id := range ids {
		s.mu.RLock()
		file, exists := s.files[id]
		s.mu.RUnlock()
		if !exists {
			continue
		}
		if err := s.store.Delete(ctx, storageKey(file)); err != nil && !errors.Is(err, ErrFileNotFound) {
			return removed, fmt.Errorf("delete file %s: %w", id, err)
		}
		deletedAt := s.clock().UTC()
		file.Status = MediaDeleted
		file.UpdatedAt = deletedAt
		file.DeletedAt = &deletedAt
		s.mu.Lock()
		s.files[id] = file
		s.deleted[id] = deletedAt
		s.mu.Unlock()
		removed++
	}
	return removed, nil
}

// ProcessDeleting performs an idempotent deletion pass for durable rows in
// deleting state. It is suitable for a cron/command entrypoint and never
// removes file_objects metadata rows.
func (s *Service) ProcessDeleting(ctx context.Context, limit int) (int, error) {
	if s == nil || s.store == nil {
		return 0, ErrFileNotFound
	}
	if limit <= 0 {
		limit = 100
	}
	if s.repo == nil {
		s.mu.RLock()
		items := make([]File, 0, limit)
		for _, item := range s.files {
			if item.Status == MediaDeleting {
				items = append(items, item)
				if len(items) == limit {
					break
				}
			}
		}
		s.mu.RUnlock()
		removed := 0
		for _, item := range items {
			if err := s.store.Delete(ctx, storageKey(item)); err != nil && !errors.Is(err, ErrFileNotFound) {
				continue
			}
			_ = s.deleteStaging(ctx, ".staging/"+item.ID)
			deletedAt := s.clock().UTC()
			item.Status = MediaDeleted
			item.UpdatedAt = deletedAt
			item.DeletedAt = &deletedAt
			s.mu.Lock()
			s.files[item.ID] = item
			s.deleted[item.ID] = deletedAt
			s.mu.Unlock()
			removed++
		}
		return removed, nil
	}
	statusRepo, ok := s.repo.(StatusRepository)
	if !ok {
		return 0, ErrStorageRead
	}
	items, err := statusRepo.ListByStatus(ctx, MediaDeleting, limit)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, item := range items {
		if err := s.store.Delete(ctx, storageKey(item)); err != nil && !errors.Is(err, ErrFileNotFound) {
			continue
		}
		// A delete request can race an interrupted upload. Remove the named
		// staging object as well; a missing stage is already the desired state.
		if err := s.deleteStaging(ctx, ".staging/"+item.ID); err != nil && !errors.Is(err, ErrFileNotFound) {
			continue
		}
		deletedAt := s.clock().UTC()
		if err := s.repo.MarkStatus(ctx, item.ID, MediaDeleted, "", &deletedAt); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// CleanupPending marks stale pending rows failed. The operation is repeatable
// and safe to run from a lightweight scheduled command.
func (s *Service) CleanupPending(ctx context.Context, age time.Duration, limit int) (int, error) {
	if s == nil || s.repo == nil {
		return 0, ErrFileNotFound
	}
	statusRepo, ok := s.repo.(StatusRepository)
	if !ok {
		return 0, ErrStorageRead
	}
	if age < 0 {
		return 0, ErrInvalidUpload
	}
	if limit <= 0 {
		limit = 100
	}
	items, err := statusRepo.ListByStatus(ctx, MediaPending, limit)
	if err != nil {
		return 0, err
	}
	cutoff := s.clock().UTC().Add(-age)
	changed := 0
	for _, item := range items {
		if !item.CreatedAt.After(cutoff) {
			reason := "pending upload timeout"
			if err := s.deleteStaging(ctx, ".staging/"+item.ID); err != nil && !errors.Is(err, ErrFileNotFound) {
				// Keep the row pending when cleanup itself is transiently
				// unavailable so a later pass can retry the staging removal.
				continue
			}
			if err := s.repo.MarkStatus(ctx, item.ID, MediaFailed, reason, nil); err != nil {
				return changed, err
			}
			changed++
		}
	}
	return changed, nil
}

// ReconcilePending promotes rows whose provider object was published before a
// process interruption could persist the final ready state. The repository
// remains authoritative: only a confirmed provider object changes the row,
// and a missing object is left pending for CleanupPending to expire.
func (s *Service) ReconcilePending(ctx context.Context, limit int) (int, error) {
	if s == nil || s.repo == nil {
		return 0, ErrFileNotFound
	}
	statusRepo, ok := s.repo.(StatusRepository)
	if !ok {
		return 0, ErrStorageRead
	}
	if limit <= 0 {
		limit = 100
	}
	items, err := statusRepo.ListByStatus(ctx, MediaPending, limit)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, item := range items {
		key := storageKey(item)
		if strings.TrimSpace(key) == "" {
			continue
		}
		if !s.providerObjectExists(ctx, key) {
			continue
		}
		item.Status = MediaReady
		item.UpdatedAt = s.clock().UTC()
		if err := s.repo.Update(ctx, item); err != nil {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}

func (s *Service) providerObjectExists(ctx context.Context, key string) bool {
	if stream, ok := s.store.(interface {
		Open(context.Context, string) (io.ReadCloser, error)
	}); ok {
		reader, err := stream.Open(ctx, key)
		if err != nil {
			return false
		}
		return reader.Close() == nil
	}
	reader, ok := s.store.(readableStore)
	if !ok {
		return false
	}
	_, err := reader.Get(ctx, key)
	return err == nil
}

// UpdateFile changes the mutable metadata owned by the application catalog.
// Object bytes and provider keys stay immutable; callers can rename a file or
// move it to another category after the same scope/ACL check used by reads.
func (s *Service) UpdateFile(ctx context.Context, id, subject, tenantID, orgID string, patch ResourcePatch) (File, error) {
	return s.updateFileWithAccess(ctx, id, fileAccess{subject: subject, tenantID: tenantID, orgID: orgID}, patch)
}

func (s *Service) updateFileWithAccess(ctx context.Context, id string, access fileAccess, patch ResourcePatch) (File, error) {
	if s == nil {
		return File{}, ErrFileNotFound
	}
	file, err := s.authorizeMutationWithContext(ctx, id, access)
	if err != nil {
		return File{}, err
	}
	if strings.TrimSpace(file.TenantID) == "" && !access.platformAdmin && access.tenantID != "" {
		return File{}, ErrAccessDenied
	}
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if name == "" || strings.ContainsAny(name, "\r\n/\\") || len(name) > 255 {
			return File{}, ErrInvalidUpload
		}
		file.Name = name
	}
	if patch.CategoryID != nil {
		categoryID := strings.TrimSpace(*patch.CategoryID)
		if categoryID != "" {
			s.mu.RLock()
			category, exists := s.categories[categoryID]
			s.mu.RUnlock()
			if !exists {
				return File{}, ErrCategoryNotFound
			}
			if category.TenantID != file.TenantID || category.OrgID != file.OrgID {
				return File{}, ErrCategoryAccessDenied
			}
		}
		file.CategoryID = categoryID
	}
	if patch.Metadata != nil {
		if err := validateMetadata(patch.Metadata); err != nil {
			return File{}, err
		}
		file.Metadata = cloneStringMap(patch.Metadata)
	}
	if patch.Status != nil {
		// Lifecycle state is controlled by upload, reconciliation and deletion
		// services. A metadata PATCH may echo the current value for optimistic
		// clients, but it cannot advance or rewind the state machine.
		if !ValidMediaStatus(*patch.Status) || *patch.Status != file.Status {
			return File{}, ErrInvalidUpload
		}
	}
	file.UpdatedAt = s.clock().UTC()
	if s.repo != nil {
		if err := s.repo.Update(ctx, file); err != nil {
			return File{}, err
		}
		return file, nil
	}
	s.mu.Lock()
	s.files[file.ID] = file
	s.mu.Unlock()
	return file, nil
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
	if s.repo != nil {
		page, err := s.repo.List(context.Background(), ListFilter{TenantID: tenantID, OrgID: orgID})
		if err != nil {
			return CleanupReport{}, err
		}
		for _, item := range page.Items {
			if !item.CreatedAt.After(report.Cutoff) {
				report.MatchingCount++
				report.Bytes += item.Size
			}
		}
		return report, nil
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
	return s.authorizeAccess(id, fileAccess{subject: subject, tenantID: tenantID, orgID: orgID})
}

func (s *Service) authorizeAccess(id string, access fileAccess) (File, error) {
	return s.authorizeAccessWithContext(context.Background(), id, access)
}

func (s *Service) authorizeMutationWithContext(ctx context.Context, id string, access fileAccess) (File, error) {
	file, err := s.authorizeAccessWithContext(ctx, id, access)
	if err != nil {
		return File{}, err
	}
	if access.platformAdmin {
		return file, nil
	}
	// Public-read controls downloads only. Mutating metadata or lifecycle
	// state still requires an authenticated owner, preventing any anonymous
	// reader from deleting a shared object.
	if strings.TrimSpace(access.subject) == "" || strings.TrimSpace(file.OwnerID) == "" || file.OwnerID != access.subject {
		return File{}, ErrAccessDenied
	}
	return file, nil
}

func (s *Service) authorizeAccessWithContext(ctx context.Context, id string, access fileAccess) (File, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.repo != nil {
		file, err := s.repo.Get(ctx, strings.TrimSpace(id))
		if err != nil {
			return File{}, err
		}
		if file.Status == MediaDeleted {
			return File{}, ErrFileNotFound
		}
		if access.platformAdmin {
			return file, nil
		}
		if file.TenantID != "" && file.TenantID != access.tenantID {
			return File{}, ErrAccessDenied
		}
		if file.OrgID != "" && file.OrgID != access.orgID {
			return File{}, ErrAccessDenied
		}
		if file.ACL != ACLPublicRead {
			if strings.TrimSpace(access.subject) == "" {
				return File{}, ErrAccessDenied
			}
			if file.OwnerID != "" && file.OwnerID != access.subject {
				return File{}, ErrAccessDenied
			}
		}
		return file, nil
	}
	s.mu.RLock()
	file, ok := s.files[strings.TrimSpace(id)]
	_, removed := s.deleted[strings.TrimSpace(id)]
	s.mu.RUnlock()
	if !ok || removed {
		return File{}, ErrFileNotFound
	}
	if file.Status == MediaDeleted {
		return File{}, ErrFileNotFound
	}
	if access.platformAdmin {
		return file, nil
	}
	if file.TenantID != "" && file.TenantID != access.tenantID {
		return File{}, ErrAccessDenied
	}
	if file.OrgID != "" && file.OrgID != access.orgID {
		return File{}, ErrAccessDenied
	}
	if file.ACL != ACLPublicRead {
		if strings.TrimSpace(access.subject) == "" {
			return File{}, ErrAccessDenied
		}
		if file.OwnerID != "" && file.OwnerID != access.subject {
			return File{}, ErrAccessDenied
		}
	}
	return file, nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func storageKey(file File) string {
	if file.ObjectKey != "" {
		return file.ObjectKey
	}
	return file.Key
}

func (s *Service) SignedURL(ctx context.Context, id, subject string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", ErrInvalidUpload
	}
	file, err := s.authorizeAccessWithContext(ctx, id, fileAccess{subject: subject})
	if err != nil {
		return "", err
	}
	if file.Status != "" && file.Status != MediaReady {
		return "", ErrMediaNotReady
	}
	url, err := signFileURL(ctx, s.store, file.ID, storageKey(file), ttl)
	if err != nil {
		return "", fmt.Errorf("sign file URL: %w", err)
	}
	return url, nil
}

// SignedURLFor applies tenant and organization checks before provider signing.
func (s *Service) SignedURLFor(ctx context.Context, id, subject, tenantID, orgID string, ttl time.Duration) (string, error) {
	return s.signedURLWithAccess(ctx, id, fileAccess{subject: subject, tenantID: tenantID, orgID: orgID}, ttl)
}

func (s *Service) signedURLWithAccess(ctx context.Context, id string, access fileAccess, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", ErrInvalidUpload
	}
	file, err := s.authorizeAccessWithContext(ctx, id, access)
	if err != nil {
		return "", err
	}
	if file.Status != "" && file.Status != MediaReady {
		return "", ErrMediaNotReady
	}
	url, err := signFileURL(ctx, s.store, file.ID, storageKey(file), ttl)
	if err != nil {
		return "", fmt.Errorf("sign file URL: %w", err)
	}
	return url, nil
}

func signFileURL(ctx context.Context, store Store, id, key string, ttl time.Duration) (string, error) {
	if signer, ok := store.(OpaqueURLSigner); ok {
		return signer.SignURLForID(ctx, id, key, ttl)
	}
	// A legacy URL generator has no application-level verification contract and
	// may merely concatenate a provider path. Fail closed rather than presenting
	// such a URL as signed. Remote providers can implement OpaqueURLSigner or a
	// dedicated adapter that verifies their native presigned request.
	return "", ErrSignedURLUnsupported
}

// OpenSignedURL verifies an opaque, expiring capability against metadata
// resolved by file ID, then streams the provider object. The object key never
// comes from the request URL.
func (s *Service) OpenSignedURL(ctx context.Context, id, rawURL string) (File, io.ReadCloser, error) {
	if s == nil || s.store == nil {
		return File{}, nil, ErrFileNotFound
	}
	file, err := s.lookupFile(ctx, id)
	if err != nil {
		return File{}, nil, err
	}
	if file.Status != "" && file.Status != MediaReady {
		return File{}, nil, ErrMediaNotReady
	}
	signer, ok := s.store.(OpaqueURLSigner)
	if !ok {
		return File{}, nil, ErrSignedURLUnsupported
	}
	if err := signer.VerifyIDURL(rawURL, file.ID, storageKey(file)); err != nil {
		return File{}, nil, err
	}
	stream, ok := s.store.(interface {
		Open(context.Context, string) (io.ReadCloser, error)
	})
	if !ok {
		return File{}, nil, ErrStorageRead
	}
	reader, err := stream.Open(ctx, storageKey(file))
	if err != nil {
		if errors.Is(err, ErrFileNotFound) && s.repo != nil {
			s.markDamaged(ctx, file, "object missing")
		}
		return File{}, nil, err
	}
	return file, s.checkedReader(file, reader), nil
}

func (s *Service) lookupFile(ctx context.Context, id string) (File, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return File{}, ErrFileNotFound
	}
	if s.repo != nil {
		file, err := s.repo.Get(ctx, id)
		if err != nil {
			return File{}, err
		}
		if file.Status == MediaDeleted {
			return File{}, ErrFileNotFound
		}
		return file, nil
	}
	s.mu.RLock()
	file, ok := s.files[id]
	_, removed := s.deleted[id]
	s.mu.RUnlock()
	if !ok || removed || file.Status == MediaDeleted {
		return File{}, ErrFileNotFound
	}
	return file, nil
}

// Authorize checks metadata ACL without requiring the provider to support a
// read operation, keeping remote provider signing compatible with this seam.
func (s *Service) Authorize(id, subject, tenantID, orgID string) (File, error) {
	return s.authorize(id, subject, tenantID, orgID)
}

func (s *Service) Cleanup(ctx context.Context, age time.Duration) error {
	if s == nil || s.store == nil || age < 0 {
		return ErrInvalidUpload
	}
	cutoff := s.clock().UTC().Add(-age)
	// Cleanup is a worker entrypoint, not a second delete implementation. Move
	// candidates through the same deleting state and let ProcessDeleting own
	// provider removal; file_objects rows are never physically removed.
	if s.repo != nil {
		page, err := s.repo.List(ctx, ListFilter{Status: MediaReady})
		if err != nil {
			return err
		}
		for _, file := range page.Items {
			if file.CreatedAt.After(cutoff) {
				continue
			}
			if deletionRepo, ok := s.repo.(DeletionRepository); ok {
				if err := deletionRepo.RequestDeletion(ctx, file.ID, false, s.clock().UTC()); err != nil {
					return err
				}
				continue
			}
			if s.usageRepo != nil {
				count, countErr := s.usageRepo.CountByResource(ctx, file.ID)
				if countErr != nil {
					return countErr
				}
				if count > 0 {
					return ErrMediaInUse
				}
			}
			if err := s.repo.MarkStatus(ctx, file.ID, MediaDeleting, "", nil); err != nil {
				return err
			}
		}
		_, err = s.ProcessDeleting(ctx, len(page.Items))
		return err
	}
	s.mu.RLock()
	expired := make([]File, 0)
	for _, file := range s.files {
		if (file.Status == "" || file.Status == MediaReady) && !file.CreatedAt.After(cutoff) {
			expired = append(expired, file)
		}
	}
	s.mu.RUnlock()
	for _, file := range expired {
		s.mu.Lock()
		file.Status = MediaDeleting
		deletedAt := s.clock().UTC()
		file.DeletedAt = &deletedAt
		file.UpdatedAt = deletedAt
		s.files[file.ID] = file
		if s.deleted == nil {
			s.deleted = make(map[string]time.Time)
		}
		s.deleted[file.ID] = deletedAt
		s.mu.Unlock()
	}
	_, err := s.ProcessDeleting(ctx, len(expired))
	return err
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
