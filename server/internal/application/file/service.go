// Package file defines the provider-independent file-center application seam.
// Concrete object stores (for example, S3-compatible providers) can implement
// Store without changing validation, authorization, or lifecycle policy.
package file

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrFileTooLarge       = errors.New("file exceeds configured size limit")
	ErrMIMETypeNotAllowed = errors.New("file MIME type is not allowed")
	ErrFileNotFound       = errors.New("file not found")
	ErrAccessDenied       = errors.New("file access denied")
	ErrInvalidUpload      = errors.New("invalid file upload")
)

type ACL string

const (
	ACLPrivate    ACL = "private"
	ACLPublicRead ACL = "public-read"
)

// Object is the provider payload. Data is optional for remote providers and
// is retained by the memory provider solely for deterministic local tests.
type Object struct {
	Key       string
	Name      string
	MIME      string
	Size      int64
	OwnerID   string
	ACL       ACL
	CreatedAt time.Time
	Data      []byte
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
	Name    string
	MIME    string
	Size    int64
	OwnerID string
	ACL     ACL
	Data    []byte
}

type File struct {
	ID        string
	Key       string
	Name      string
	MIME      string
	Size      int64
	OwnerID   string
	ACL       ACL
	CreatedAt time.Time
}

type Service struct {
	store    Store
	maxBytes int64
	allowed  map[string]struct{}
	clock    func() time.Time
	mu       sync.RWMutex
	files    map[string]File
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
	return &Service{store: store, maxBytes: config.MaxBytes, allowed: allowed, clock: clock, files: make(map[string]File)}
}

func (s *Service) Upload(ctx context.Context, input UploadInput) (File, error) {
	if s.store == nil || strings.TrimSpace(input.Name) == "" || input.Size < 0 {
		return File{}, ErrInvalidUpload
	}
	if s.maxBytes > 0 && input.Size > s.maxBytes {
		return File{}, ErrFileTooLarge
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
	object := Object{Key: id, Name: strings.TrimSpace(input.Name), MIME: input.MIME, Size: input.Size, OwnerID: input.OwnerID, ACL: acl, CreatedAt: now, Data: input.Data}
	if err = s.store.Put(ctx, object); err != nil {
		return File{}, fmt.Errorf("put file: %w", err)
	}
	file := File{ID: id, Key: id, Name: object.Name, MIME: object.MIME, Size: object.Size, OwnerID: object.OwnerID, ACL: object.ACL, CreatedAt: now}
	s.mu.Lock()
	s.files[id] = file
	s.mu.Unlock()
	return file, nil
}

func (s *Service) SignedURL(ctx context.Context, id, subject string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", ErrInvalidUpload
	}
	s.mu.RLock()
	file, ok := s.files[id]
	s.mu.RUnlock()
	if !ok {
		return "", ErrFileNotFound
	}
	if file.ACL != ACLPublicRead && file.OwnerID != subject {
		return "", ErrAccessDenied
	}
	url, err := s.store.SignURL(ctx, file.Key, ttl)
	if err != nil {
		return "", fmt.Errorf("sign file URL: %w", err)
	}
	return url, nil
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
