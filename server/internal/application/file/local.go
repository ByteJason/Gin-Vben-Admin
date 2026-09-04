package file

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// LocalStore is a private filesystem provider. Uploads are first written to
// .staging and atomically renamed into the object tree; existing keys are
// never overwritten and all path components are checked for symlinks.
type LocalStore struct {
	root       string
	baseURL    string
	signingKey []byte
}

// NewLocalStore constructs a provider. The optional key enables deterministic
// HMAC URL verification while preserving the historical two-argument API.
func NewLocalStore(root, baseURL string, signingKey ...[]byte) (*LocalStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("%w: local root is required", ErrInvalidUpload)
	}
	// A configured root follows the host filesystem syntax. Windows drive and
	// relative paths are valid on Windows, while foreign Windows syntax remains
	// invalid on Unix. UNC/device paths stay outside this local-only provider.
	if invalidConfiguredRootSyntax(root, runtime.GOOS) {
		return nil, fmt.Errorf("%w: windows-style local root is not allowed", ErrInvalidUpload)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("local root: %w", err)
	}
	if normalized, normalizeErr := normalizeConfiguredRoot(abs); normalizeErr != nil {
		return nil, normalizeErr
	} else {
		abs = normalized
	}
	if info, lstatErr := os.Lstat(abs); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: local root symlink is not allowed", ErrInvalidUpload)
	}
	if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
		return nil, fmt.Errorf("%w: local root is not a directory", ErrInvalidUpload)
	}
	clean := filepath.Clean(abs)
	if unsafeStorageRoot(clean) || isSensitiveStorageRoot(clean) {
		return nil, fmt.Errorf("%w: unsafe local root", ErrInvalidUpload)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create local root: %w", err)
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, fmt.Errorf("secure local root: %w", err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: local root is not a directory", ErrInvalidUpload)
	}
	// Re-check every component after creation. In contrast to rejectSymlink on
	// the root itself, this walk starts at the filesystem root and therefore
	// also inspects every configured parent directory.
	postCreateRoot, postCreateErr := normalizeConfiguredRoot(abs)
	if postCreateErr != nil || filepath.Clean(postCreateRoot) != filepath.Clean(abs) {
		return nil, fmt.Errorf("%w: local root path changed", ErrInvalidUpload)
	}
	if unsafeStorageRoot(filepath.Clean(abs)) || isSensitiveStorageRoot(filepath.Clean(abs)) {
		return nil, fmt.Errorf("%w: unsafe local root", ErrInvalidUpload)
	}
	key := []byte(nil)
	if len(signingKey) > 0 {
		key = append(key, signingKey[0]...)
	}
	if len(key) == 0 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("initialize signed URL key: %w", err)
		}
	}
	return &LocalStore{root: abs, baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), signingKey: key}, nil
}

func invalidConfiguredRootSyntax(root, goos string) bool {
	if strings.ContainsRune(root, '\x00') || strings.ContainsAny(root, "\r\n") {
		return true
	}
	unc := strings.HasPrefix(root, `\\`) || strings.HasPrefix(root, "//")
	drivePrefix := len(root) >= 2 && ((root[0] >= 'A' && root[0] <= 'Z') || (root[0] >= 'a' && root[0] <= 'z')) && root[1] == ':'
	if goos == "windows" {
		if unc {
			return true
		}
		if drivePrefix {
			// Reject drive-relative forms such as C:storage. A configured drive
			// root must be fully qualified before filepath.Abs inspects it.
			return len(root) < 3 || (root[2] != '\\' && root[2] != '/')
		}
		return strings.Contains(root, ":")
	}
	return unc || strings.Contains(root, "\\") || drivePrefix
}

func unsafeStorageRoot(root string) bool {
	root = filepath.Clean(root)
	if runtime.GOOS == "windows" {
		return windowsSensitiveStorageRoot(root)
	}
	// macOS exposes per-process temporary directories below /var/folders;
	// those are safe test/runtime roots even though /var itself is protected.
	if strings.HasPrefix(root, "/private/var/folders/") {
		return false
	}
	for _, forbidden := range []string{string(filepath.Separator), "/etc", "/usr", "/var", "/bin", "/sbin", "/System", "/Library", "/private/etc", "/private/var"} {
		if root == forbidden || strings.HasPrefix(root, forbidden+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// isSensitiveStorageRoot rejects the aliases themselves while allowing a
// private application directory below an otherwise shared temporary parent.
// In particular, constructing a provider must never chmod /tmp or /var.
func isSensitiveStorageRoot(root string) bool {
	clean := filepath.Clean(root)
	if runtime.GOOS == "windows" {
		return windowsSensitiveStorageRoot(clean)
	}
	for _, sensitive := range []string{"/", "/tmp", "/private/tmp", "/var", "/private/var", "/etc", "/usr", "/bin", "/sbin", "/System", "/Library", "/private/etc"} {
		if clean == sensitive {
			return true
		}
	}
	return false
}

func windowsSensitiveStorageRoot(root string) bool {
	clean := filepath.Clean(root)
	volume := filepath.VolumeName(clean)
	if volume != "" && strings.EqualFold(clean, filepath.Clean(volume+string(filepath.Separator))) {
		return true
	}
	for _, environment := range []string{"SystemRoot", "WINDIR", "ProgramFiles", "ProgramFiles(x86)", "ProgramData"} {
		forbidden := strings.TrimSpace(os.Getenv(environment))
		if forbidden == "" {
			continue
		}
		forbidden = filepath.Clean(forbidden)
		if strings.EqualFold(clean, forbidden) || strings.HasPrefix(strings.ToLower(clean), strings.ToLower(forbidden+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

// normalizeConfiguredRoot resolves only the platform's well-known temporary
// aliases before creation. Any user-controlled symlink component is rejected
// so MkdirAll cannot silently create the provider outside the configured tree.
func normalizeConfiguredRoot(abs string) (string, error) {
	abs = filepath.Clean(abs)
	volume := filepath.VolumeName(abs)
	root := volume + string(filepath.Separator)
	if volume == "" {
		root = string(filepath.Separator)
	}
	relative, err := filepath.Rel(root, abs)
	if err != nil {
		return "", fmt.Errorf("%w: local root cannot be inspected", ErrInvalidUpload)
	}
	parts := make([]string, 0, 8)
	if relative != "." && relative != "" {
		parts = strings.Split(relative, string(filepath.Separator))
	}
	current := root
	for index, part := range parts {
		if part == "" || part == "." {
			continue
		}
		candidate := filepath.Join(current, part)
		info, statErr := os.Lstat(candidate)
		if errors.Is(statErr, os.ErrNotExist) {
			// Descendants below the first missing component do not exist yet.
			// Rebuild the lexical suffix under the already-validated parent.
			for _, remaining := range parts[index:] {
				if remaining != "" && remaining != "." {
					current = filepath.Join(current, remaining)
				}
			}
			return current, nil
		}
		if statErr != nil {
			return "", fmt.Errorf("%w: local root cannot be inspected", ErrInvalidUpload)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, resolveErr := resolveAllowedRootAlias(candidate)
			if resolveErr != nil {
				return "", resolveErr
			}
			current = resolved
			continue
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%w: local root component is not a directory", ErrInvalidUpload)
		}
		current = candidate
	}
	return current, nil
}

func allowedRootAlias(path string) bool {
	clean := filepath.Clean(path)
	return clean == "/tmp" || clean == "/var"
}

func resolveAllowedRootAlias(path string) (string, error) {
	clean := filepath.Clean(path)
	if !allowedRootAlias(clean) {
		return "", fmt.Errorf("%w: local root symlink is not allowed", ErrInvalidUpload)
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("%w: local root alias cannot be resolved", ErrInvalidUpload)
	}
	resolved = filepath.Clean(resolved)
	// Only the operating system's conventional aliases are accepted. A
	// retargeted /tmp or /var must not redirect storage into an arbitrary tree.
	valid := (clean == "/tmp" && (resolved == "/tmp" || resolved == "/private/tmp")) ||
		(clean == "/var" && (resolved == "/var" || resolved == "/private/var"))
	if !valid {
		return "", fmt.Errorf("%w: local root alias target is not allowed", ErrInvalidUpload)
	}
	return resolved, nil
}

func (s *LocalStore) pathFor(key string) (string, error) {
	return s.pathForInternal(key, false)
}

func isStagingKey(key string) bool {
	key = strings.TrimSpace(key)
	return strings.HasPrefix(key, ".staging/") && key != ".staging/"
}

func (s *LocalStore) pathForInternal(key string, allowInternal bool) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || filepath.IsAbs(key) || strings.ContainsRune(key, '\x00') || strings.Contains(key, "\\") {
		return "", ErrInvalidUpload
	}
	// Reject Windows drive and UNC forms even on Unix hosts.
	if strings.HasPrefix(key, "//") || (len(key) >= 2 && key[1] == ':') {
		return "", ErrInvalidUpload
	}
	for _, part := range strings.Split(key, "/") {
		if part == ".." {
			return "", ErrInvalidUpload
		}
		if !allowInternal && part == ".staging" {
			return "", ErrInvalidUpload
		}
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrInvalidUpload
	}
	path := filepath.Join(s.root, clean)
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidUpload
	}
	return path, nil
}

func (s *LocalStore) Put(ctx context.Context, object Object) error {
	if object.Data == nil && object.Size != 0 {
		return ErrInvalidUpload
	}
	declared := object.Size
	if object.Data != nil && declared == 0 {
		declared = int64(len(object.Data))
	}
	return s.PutReader(ctx, object.Key, bytes.NewReader(object.Data), declared)
}

// PutReader streams a payload to a staging file and atomically publishes it.
// declaredSize may be -1 when the caller enforces a limit independently.
func (s *LocalStore) PutReader(ctx context.Context, key string, src io.Reader, declaredSize int64) error {
	return s.putReader(ctx, key, src, declaredSize, false)
}

// PutStaging writes a provider-owned staging object. It is deliberately not
// reachable through PutReader/PutStream, which reserve the `.staging` prefix
// from application-supplied object keys.
func (s *LocalStore) PutStaging(ctx context.Context, key string, src io.Reader, object Object) error {
	if !isStagingKey(key) {
		return ErrInvalidUpload
	}
	return s.putReader(ctx, key, src, object.Size, true)
}

func (s *LocalStore) putReader(ctx context.Context, key string, src io.Reader, declaredSize int64, allowInternal bool) error {
	if src == nil || declaredSize < -1 {
		return ErrInvalidUpload
	}
	// CopyN below requests one extra byte to detect overflow; avoid the
	// signed-integer wraparound when a caller supplies the absolute maximum.
	if declaredSize == int64(^uint64(0)>>1) {
		return ErrFileTooLarge
	}
	destination, err := s.pathForInternal(key, allowInternal)
	if err != nil {
		return err
	}
	if err := s.rejectSymlink(destination); err != nil {
		return err
	}
	if _, statErr := os.Lstat(destination); statErr == nil {
		return ErrObjectExists
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	stagingDir := filepath.Join(s.root, ".staging")
	if err := s.rejectSymlink(stagingDir); err != nil {
		return err
	}
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	if err := s.ensurePrivateDir(stagingDir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(stagingDir, ".upload-*")
	if err != nil {
		return fmt.Errorf("create object temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure object temp: %w", err)
	}
	reader := &contextReader{ctx: ctx, reader: src}
	var written int64
	if declaredSize >= 0 {
		written, err = io.CopyN(tmp, reader, declaredSize+1)
		if written > declaredSize {
			_ = tmp.Close()
			return ErrFileTooLarge
		}
		if err == io.EOF {
			err = nil
		}
	} else {
		written, err = io.Copy(tmp, reader)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write object: %w", err)
	}
	if declaredSize >= 0 && written != declaredSize {
		return fmt.Errorf("%w: size does not match data", ErrInvalidUpload)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create object directory: %w", err)
	}
	if err := s.ensurePrivateDir(filepath.Dir(destination)); err != nil {
		return err
	}
	if _, statErr := os.Lstat(destination); statErr == nil {
		return ErrObjectExists
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if err := os.Link(tmpName, destination); err != nil {
		if os.IsExist(err) {
			return ErrObjectExists
		}
		return fmt.Errorf("publish object: %w", err)
	}
	_ = os.Remove(tmpName)
	return nil
}

// PutStream is the streaming Store extension used by the application layer.
func (s *LocalStore) PutStream(ctx context.Context, key string, src io.Reader, object Object) error {
	return s.PutReader(ctx, key, src, object.Size)
}

// Promote atomically moves a staged object to its final key after validation.
func (s *LocalStore) Promote(ctx context.Context, from, to string) error {
	if !isStagingKey(from) {
		return ErrInvalidUpload
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	src, err := s.pathForInternal(from, true)
	if err != nil {
		return err
	}
	dst, err := s.pathFor(to)
	if err != nil {
		return err
	}
	if err := s.rejectSymlink(src); err != nil {
		return err
	}
	if err := s.rejectSymlink(dst); err != nil {
		return err
	}
	if _, statErr := os.Lstat(dst); statErr == nil {
		return ErrObjectExists
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := s.ensurePrivateDir(filepath.Dir(dst)); err != nil {
		return err
	}
	if err := os.Link(src, dst); err != nil {
		if os.IsExist(err) {
			return ErrObjectExists
		}
		return err
	}
	_ = os.Remove(src)
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if r.ctx == nil {
		return r.reader.Read(p)
	}
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(p)
	}
}

func (s *LocalStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	path, err := s.pathFor(key)
	if err != nil {
		return nil, err
	}
	if err := s.rejectSymlink(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, ErrFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open object: %w", err)
	}
	info, statErr := f.Stat()
	if statErr != nil {
		_ = f.Close()
		return nil, statErr
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, ErrInvalidUpload
	}
	if ctx == nil {
		return f, nil
	}
	return &contextFile{File: f, ctx: ctx}, nil
}

type contextFile struct {
	*os.File
	ctx context.Context
}

func (f *contextFile) Read(p []byte) (int, error) {
	select {
	case <-f.ctx.Done():
		return 0, f.ctx.Err()
	default:
		return f.File.Read(p)
	}
}

func (s *LocalStore) Get(ctx context.Context, key string) (Object, error) {
	f, err := s.Open(ctx, key)
	if err != nil {
		return Object{}, err
	}
	defer f.Close()
	var data bytes.Buffer
	_, err = io.Copy(&data, f)
	if err != nil {
		return Object{}, fmt.Errorf("read object: %w", err)
	}
	return Object{Key: key, Size: int64(data.Len()), Data: append([]byte(nil), data.Bytes()...)}, nil
}

func (s *LocalStore) Delete(ctx context.Context, key string) error {
	return s.deleteObject(ctx, key, false)
}

// DeleteStaging removes only a provider-owned staging object.
func (s *LocalStore) DeleteStaging(ctx context.Context, key string) error {
	if !isStagingKey(key) {
		return ErrInvalidUpload
	}
	return s.deleteObject(ctx, key, true)
}

func (s *LocalStore) deleteObject(ctx context.Context, key string, allowInternal bool) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	path, err := s.pathForInternal(key, allowInternal)
	if err != nil {
		return err
	}
	if err := s.rejectSymlink(path); err != nil {
		return err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return ErrInvalidUpload
		}
	} else if os.IsNotExist(statErr) {
		return ErrFileNotFound
	} else {
		return statErr
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return ErrFileNotFound
	}
	return err
}

func (s *LocalStore) rejectSymlink(path string) error {
	current := s.root
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return ErrInvalidUpload
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return nil
			}
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidUpload
		}
	}
	return nil
}

func (s *LocalStore) ensurePrivateDir(dir string) error {
	rel, err := filepath.Rel(s.root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrInvalidUpload
	}
	current := s.root
	if err := os.Chmod(current, 0o700); err != nil {
		return err
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return statErr
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidUpload
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (s *LocalStore) SignURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", ErrInvalidUpload
	}
	reader, err := s.Open(context.Background(), key)
	if err != nil {
		return "", err
	}
	if err := reader.Close(); err != nil {
		return "", err
	}
	base := s.baseURL
	if base == "" {
		base = "http://localhost/files"
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimRight(u.Path, "/")
	// Path is kept decoded while RawPath carries one (not double) escaping of
	// the sharded key. This makes the URL usable by ordinary HTTP servers while
	// preserving slash boundaries for verification.
	u.Path = basePath + "/" + key
	u.RawPath = basePath + "/" + url.PathEscape(key)
	expires := time.Now().Add(ttl).Unix()
	q := u.Query()
	q.Set("expires", strconv.FormatInt(expires, 10))
	q.Set("sig", s.signature(key, expires))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// SignURLForID emits an opaque URL whose path contains only the application
// file ID. The provider object key is included solely in the HMAC input and is
// never serialized into the URL.
func (s *LocalStore) SignURLForID(ctx context.Context, id, key string, ttl time.Duration) (string, error) {
	if !validOpaqueID(id) || strings.TrimSpace(key) == "" || ttl <= 0 {
		return "", ErrInvalidUpload
	}
	reader, err := s.Open(ctx, key)
	if err != nil {
		return "", err
	}
	if err := reader.Close(); err != nil {
		return "", err
	}
	base := s.baseURL
	if base == "" {
		base = "http://localhost/api/admin/v1/files"
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimRight(u.Path, "/")
	u.Path = basePath + "/" + id + "/download"
	u.RawPath = basePath + "/" + url.PathEscape(id) + "/download"
	expires := time.Now().Add(ttl).Unix()
	q := u.Query()
	q.Set("expires", strconv.FormatInt(expires, 10))
	q.Set("sig", s.idSignature(id, key, expires))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *LocalStore) signature(key string, expires int64) string {
	h := hmac.New(sha256.New, s.signingKey)
	_, _ = io.WriteString(h, key+"|"+strconv.FormatInt(expires, 10))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *LocalStore) idSignature(id, key string, expires int64) string {
	h := hmac.New(sha256.New, s.signingKey)
	_, _ = io.WriteString(h, id+"\x00"+key+"|"+strconv.FormatInt(expires, 10))
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyIDURL validates an opaque ID URL against the object key resolved by
// the application repository. It is the server-side counterpart of
// SignURLForID and is intentionally separate from the legacy key URL parser.
func (s *LocalStore) VerifyIDURL(raw, id, key string) error {
	if !validOpaqueID(id) || strings.TrimSpace(key) == "" {
		return ErrInvalidUpload
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidUpload
	}
	if got, err := s.idFromURL(u); err != nil || got != id {
		return ErrAccessDenied
	}
	exp, err := strconv.ParseInt(u.Query().Get("expires"), 10, 64)
	if err != nil || exp <= time.Now().Unix() {
		return ErrAccessDenied
	}
	if !hmac.Equal([]byte(u.Query().Get("sig")), []byte(s.idSignature(id, key, exp))) {
		return ErrAccessDenied
	}
	f, err := s.Open(context.Background(), key)
	if err != nil {
		return err
	}
	return f.Close()
}

// VerifyKeyURL validates expiry, signature, and object existence.
func (s *LocalStore) VerifyKeyURL(raw, key string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidUpload
	}
	urlKey, keyErr := s.keyFromURL(u)
	if keyErr != nil || urlKey != key {
		return ErrAccessDenied
	}
	exp, err := strconv.ParseInt(u.Query().Get("expires"), 10, 64)
	if err != nil || exp <= time.Now().Unix() {
		return ErrAccessDenied
	}
	if !hmac.Equal([]byte(u.Query().Get("sig")), []byte(s.signature(key, exp))) {
		return ErrAccessDenied
	}
	f, err := s.Open(context.Background(), key)
	if err != nil {
		return err
	}
	return f.Close()
}

// VerifySignedURL derives the key from the configured base URL and validates
// the same server-side signature contract.
func (s *LocalStore) VerifySignedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidUpload
	}
	// Opaque application URLs end in /{file_id}/download and are verified
	// against the key derived below only for this provider convenience method.
	// The HTTP application uses VerifyIDURL with the repository-resolved key.
	if id, idErr := s.idFromURL(u); idErr == nil {
		resolved, resolveErr := s.keyForOpaqueID(id)
		if resolveErr != nil {
			return resolveErr
		}
		return s.VerifyIDURL(raw, id, resolved)
	}
	key, keyErr := s.keyFromURL(u)
	if keyErr != nil {
		return keyErr
	}
	if err := s.VerifyKeyURL(raw, key); err == nil {
		return nil
	}
	return ErrAccessDenied
}

func validOpaqueID(id string) bool {
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "/\\\x00\r\n") {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *LocalStore) idFromURL(u *url.URL) (string, error) {
	if u == nil {
		return "", ErrInvalidUpload
	}
	base := ""
	if s.baseURL != "" {
		if b, parseErr := url.Parse(s.baseURL); parseErr == nil {
			base = strings.TrimRight(b.Path, "/")
		}
	}
	if base == "" {
		// SignURLForID uses the public file-center route when no externally
		// configured base URL is supplied. Keep verification on that same route;
		// legacy key URLs still pass an explicit baseURL when they are used.
		base = "/api/admin/v1/files"
	}
	prefix := base + "/"
	if !strings.HasPrefix(u.Path, prefix) {
		return "", ErrAccessDenied
	}
	relative := strings.TrimPrefix(u.Path, prefix)
	if strings.HasSuffix(relative, "/download") {
		relative = strings.TrimSuffix(relative, "/download")
	}
	id, err := url.PathUnescape(relative)
	if err != nil || !validOpaqueID(id) || strings.Contains(id, "/") {
		return "", ErrInvalidUpload
	}
	return id, nil
}

func (s *LocalStore) keyForOpaqueID(id string) (string, error) {
	if !validOpaqueID(id) || len(id) < 4 {
		return "", ErrAccessDenied
	}
	pattern := filepath.Join(s.root, "v1", id[:2], id[2:4], id+"*")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) != 1 {
		return "", ErrAccessDenied
	}
	match := matches[0]
	if err := s.rejectSymlink(match); err != nil {
		return "", ErrAccessDenied
	}
	info, err := os.Stat(match)
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrAccessDenied
	}
	rel, err := filepath.Rel(s.root, match)
	if err != nil {
		return "", ErrAccessDenied
	}
	return filepath.ToSlash(rel), nil
}

func (s *LocalStore) keyFromURL(u *url.URL) (string, error) {
	if u == nil {
		return "", ErrInvalidUpload
	}
	base := ""
	if s.baseURL != "" {
		if b, parseErr := url.Parse(s.baseURL); parseErr == nil {
			base = strings.TrimRight(b.Path, "/")
		}
	}
	if base == "" {
		base = "/files"
	}
	prefix := base + "/"
	if !strings.HasPrefix(u.Path, prefix) {
		return "", ErrAccessDenied
	}
	encoded := strings.TrimPrefix(u.Path, prefix)
	key, err := url.PathUnescape(encoded)
	if err != nil || key == "" {
		return "", ErrInvalidUpload
	}
	return key, nil
}

var _ StreamStore = (*LocalStore)(nil)
var _ OpaqueURLSigner = (*LocalStore)(nil)
