package file

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MemoryStore is a deterministic provider for unit tests. It implements the
// same streaming and signed-URL contract as LocalStore.
type MemoryStore struct {
	mu         sync.RWMutex
	objects    map[string]Object
	urlIDs     map[string]string
	BaseURL    string
	signingKey []byte
}

func NewMemoryStore(baseURL string, signingKey ...[]byte) *MemoryStore {
	key := []byte("memory-store-secret")
	if len(signingKey) > 0 && len(signingKey[0]) > 0 {
		key = append([]byte(nil), signingKey[0]...)
	}
	return &MemoryStore{objects: make(map[string]Object), urlIDs: make(map[string]string), BaseURL: baseURL, signingKey: key}
}

func (s *MemoryStore) Get(_ context.Context, key string) (Object, error) {
	s.mu.RLock()
	object, ok := s.objects[key]
	s.mu.RUnlock()
	if !ok {
		return Object{}, ErrFileNotFound
	}
	object.Data = append([]byte(nil), object.Data...)
	return object, nil
}
func (s *MemoryStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := s.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(object.Data)), nil
}
func (s *MemoryStore) Put(ctx context.Context, object Object) error {
	// A nil payload is an empty stream, not permission to silently replace a
	// caller-declared non-zero size. PutStream performs the authoritative size
	// check before publishing the object.
	if object.Data != nil && object.Size == 0 {
		object.Size = int64(len(object.Data))
	}
	return s.PutStream(ctx, object.Key, bytes.NewReader(object.Data), object)
}
func (s *MemoryStore) PutStream(ctx context.Context, key string, src io.Reader, object Object) error {
	if key == "" || src == nil {
		return ErrInvalidUpload
	}
	var data bytes.Buffer
	_, err := io.Copy(&data, &contextReader{ctx: ctx, reader: src})
	if err != nil {
		return err
	}
	if object.Size >= 0 && int64(data.Len()) != object.Size {
		return fmt.Errorf("%w: size does not match data", ErrInvalidUpload)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.objects == nil {
		s.objects = map[string]Object{}
	}
	if _, exists := s.objects[key]; exists {
		return ErrObjectExists
	}
	object.Key = key
	object.Size = int64(data.Len())
	object.Data = append([]byte(nil), data.Bytes()...)
	s.objects[key] = object
	return nil
}
func (s *MemoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.objects[key]; !ok {
		return ErrFileNotFound
	}
	delete(s.objects, key)
	return nil
}
func (s *MemoryStore) SignURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", ErrInvalidUpload
	}
	if _, err := s.Get(context.Background(), key); err != nil {
		return "", err
	}
	base := s.BaseURL
	if base == "" {
		base = "http://memory.invalid/objects"
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = path.Join(u.Path, key)
	exp := time.Now().Add(ttl).Unix()
	q := u.Query()
	q.Set("expires", strconv.FormatInt(exp, 10))
	q.Set("sig", s.signature(key, exp))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *MemoryStore) SignURLForID(_ context.Context, id, key string, ttl time.Duration) (string, error) {
	if !validOpaqueID(id) || strings.TrimSpace(key) == "" || ttl <= 0 {
		return "", ErrInvalidUpload
	}
	if _, err := s.Get(context.Background(), key); err != nil {
		return "", err
	}
	s.mu.Lock()
	if s.urlIDs == nil {
		s.urlIDs = map[string]string{}
	}
	s.urlIDs[id] = key
	s.mu.Unlock()
	base := s.BaseURL
	if base == "" {
		base = "http://memory.invalid/objects"
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimRight(u.Path, "/")
	u.Path = basePath + "/" + id + "/download"
	u.RawPath = basePath + "/" + url.PathEscape(id) + "/download"
	exp := time.Now().Add(ttl).Unix()
	q := u.Query()
	q.Set("expires", strconv.FormatInt(exp, 10))
	q.Set("sig", s.idSignature(id, key, exp))
	u.RawQuery = q.Encode()
	return u.String(), nil
}
func (s *MemoryStore) signature(key string, exp int64) string {
	h := hmac.New(sha256.New, s.signingKey)
	_, _ = io.WriteString(h, key+"|"+strconv.FormatInt(exp, 10))
	return hex.EncodeToString(h.Sum(nil))
}
func (s *MemoryStore) idSignature(id, key string, exp int64) string {
	h := hmac.New(sha256.New, s.signingKey)
	_, _ = io.WriteString(h, id+"\x00"+key+"|"+strconv.FormatInt(exp, 10))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *MemoryStore) VerifyIDURL(raw, id, key string) error {
	if !validOpaqueID(id) || strings.TrimSpace(key) == "" {
		return ErrInvalidUpload
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidUpload
	}
	base := s.BaseURL
	if base == "" {
		base = "http://memory.invalid/objects"
	}
	parsedBase, parseErr := url.Parse(base)
	if parseErr != nil {
		return ErrInvalidUpload
	}
	prefix := strings.TrimRight(parsedBase.Path, "/") + "/"
	relative := strings.TrimPrefix(u.Path, prefix)
	if strings.HasSuffix(relative, "/download") {
		relative = strings.TrimSuffix(relative, "/download")
	}
	got, decodeErr := url.PathUnescape(relative)
	if !strings.HasPrefix(u.Path, prefix) || decodeErr != nil || got != id {
		return ErrAccessDenied
	}
	exp, parseErr := strconv.ParseInt(u.Query().Get("expires"), 10, 64)
	if parseErr != nil || exp <= time.Now().Unix() {
		return ErrAccessDenied
	}
	if !hmac.Equal([]byte(u.Query().Get("sig")), []byte(s.idSignature(id, key, exp))) {
		return ErrAccessDenied
	}
	_, err = s.Get(context.Background(), key)
	return err
}
func (s *MemoryStore) VerifyKeyURL(raw, key string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidUpload
	}
	base := s.BaseURL
	if base == "" {
		base = "http://memory.invalid/objects"
	}
	parsedBase, parseErr := url.Parse(base)
	if parseErr != nil {
		return ErrInvalidUpload
	}
	basePath := strings.TrimRight(parsedBase.Path, "/")
	urlKey, decodeErr := url.PathUnescape(strings.TrimPrefix(u.Path, basePath+"/"))
	if !strings.HasPrefix(u.Path, basePath+"/") || decodeErr != nil || urlKey != key {
		return ErrAccessDenied
	}
	exp, e := strconv.ParseInt(u.Query().Get("expires"), 10, 64)
	if e != nil || exp <= time.Now().Unix() {
		return ErrAccessDenied
	}
	if !hmac.Equal([]byte(u.Query().Get("sig")), []byte(s.signature(key, exp))) {
		return ErrAccessDenied
	}
	_, err = s.Get(context.Background(), key)
	return err
}
func (s *MemoryStore) VerifySignedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidUpload
	}
	base := s.BaseURL
	if base == "" {
		base = "http://memory.invalid/objects"
	}
	parsedBase, parseErr := url.Parse(base)
	if parseErr != nil {
		return ErrInvalidUpload
	}
	basePath := strings.TrimRight(parsedBase.Path, "/")
	key := strings.TrimPrefix(u.Path, basePath+"/")
	key, err = url.PathUnescape(key)
	if err != nil || key == "" {
		return ErrInvalidUpload
	}
	if err := s.VerifyKeyURL(raw, key); err == nil {
		return nil
	}
	id := key
	if strings.HasSuffix(id, "/download") {
		id = strings.TrimSuffix(id, "/download")
	}
	if !validOpaqueID(id) {
		return ErrAccessDenied
	}
	s.mu.RLock()
	resolved := s.urlIDs[id]
	s.mu.RUnlock()
	if resolved == "" {
		return ErrAccessDenied
	}
	return s.VerifyIDURL(raw, id, resolved)
}

var _ StreamStore = (*MemoryStore)(nil)
var _ OpaqueURLSigner = (*MemoryStore)(nil)
