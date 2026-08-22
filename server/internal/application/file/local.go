package file

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalStore is the 0.10 provider. It writes atomically below a canonical
// private root and never follows a key outside that root.
type LocalStore struct {
	root    string
	baseURL string
}

func NewLocalStore(root, baseURL string) (*LocalStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("%w: local root is required", ErrInvalidUpload)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("local root: %w", err)
	}
	if err = os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create local root: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	return &LocalStore{root: abs, baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/")}, nil
}

func (s *LocalStore) pathFor(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || filepath.IsAbs(key) || strings.ContainsRune(key, '\x00') {
		return "", ErrInvalidUpload
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

func (s *LocalStore) Put(_ context.Context, object Object) error {
	path, err := s.pathFor(object.Key)
	if err != nil {
		return err
	}
	if object.Size < 0 || (object.Data != nil && int64(len(object.Data)) != object.Size) {
		return fmt.Errorf("%w: size does not match data", ErrInvalidUpload)
	}
	if err = s.rejectSymlink(path); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create object directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return fmt.Errorf("create object temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(object.Data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write object: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("publish object: %w", err)
	}
	return nil
}

func (s *LocalStore) Get(_ context.Context, key string) (Object, error) {
	path, err := s.pathFor(key)
	if err != nil {
		return Object{}, err
	}
	if err = s.rejectSymlink(path); err != nil {
		return Object{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Object{}, ErrFileNotFound
	}
	if err != nil {
		return Object{}, fmt.Errorf("read object: %w", err)
	}
	return Object{Key: key, Size: int64(len(data)), Data: data}, nil
}

func (s *LocalStore) Delete(_ context.Context, key string) error {
	path, err := s.pathFor(key)
	if err != nil {
		return err
	}
	if err = s.rejectSymlink(path); err != nil {
		return err
	}
	if err = os.Remove(path); os.IsNotExist(err) {
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

func (s *LocalStore) SignURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", ErrInvalidUpload
	}
	if _, err := s.Get(context.Background(), key); err != nil {
		return "", err
	}
	base := s.baseURL
	if base == "" {
		base = "http://localhost/files"
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse local store URL: %w", err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + url.PathEscape(key)
	q := u.Query()
	q.Set("expires_in", ttl.String())
	u.RawQuery = q.Encode()
	return u.String(), nil
}
