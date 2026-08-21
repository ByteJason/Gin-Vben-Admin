package installplatform

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var (
	ErrEnvironmentExists = errors.New("environment file already exists")
	ErrEnvironmentBusy   = errors.New("environment file is being written")
)

// EnvWriteReceipt is a credential-free summary of a published environment
// file. It is safe to persist in an installation transaction manifest.
type EnvWriteReceipt struct {
	Digest   string
	Replaced bool
}

// AtomicEnvStore publishes the root environment file without exposing its
// values through return types or logs.
type AtomicEnvStore struct {
	path string
}

func NewAtomicEnvStore(path string) *AtomicEnvStore {
	return &AtomicEnvStore{path: filepath.Clean(path)}
}

// Write creates a private dotenv file using an fsync + atomic rename sequence.
// An existing file is preserved until the separate backup/rollback workflow is
// explicitly used.
func (s *AtomicEnvStore) Write(ctx context.Context, values map[string]string) (EnvWriteReceipt, error) {
	if err := contextError(ctx); err != nil {
		return EnvWriteReceipt{}, err
	}
	if s == nil || s.path == "" || s.path == "." {
		return EnvWriteReceipt{}, errors.New("environment file path is required")
	}
	contents, err := renderEnvironment(values)
	if err != nil {
		return EnvWriteReceipt{}, err
	}
	if _, err := os.Lstat(s.path); err == nil {
		return EnvWriteReceipt{}, ErrEnvironmentExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return EnvWriteReceipt{}, fmt.Errorf("inspect environment file: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return EnvWriteReceipt{}, fmt.Errorf("create environment directory: %w", err)
	}
	lockPath := s.path + ".install.lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return EnvWriteReceipt{}, ErrEnvironmentBusy
	}
	if err != nil {
		return EnvWriteReceipt{}, fmt.Errorf("acquire environment file lock: %w", err)
	}
	if err := lock.Close(); err != nil {
		_ = os.Remove(lockPath)
		return EnvWriteReceipt{}, fmt.Errorf("close environment file lock: %w", err)
	}
	defer os.Remove(lockPath)

	if _, err := os.Lstat(s.path); err == nil {
		return EnvWriteReceipt{}, ErrEnvironmentExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return EnvWriteReceipt{}, fmt.Errorf("recheck environment file: %w", err)
	}

	temp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return EnvWriteReceipt{}, fmt.Errorf("create temporary environment file: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return EnvWriteReceipt{}, fmt.Errorf("set environment file permissions: %w", err)
	}
	writer := bufio.NewWriter(temp)
	if _, err := writer.Write(contents); err != nil {
		_ = temp.Close()
		return EnvWriteReceipt{}, fmt.Errorf("write environment file: %w", err)
	}
	if err := writer.Flush(); err != nil {
		_ = temp.Close()
		return EnvWriteReceipt{}, fmt.Errorf("flush environment file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return EnvWriteReceipt{}, fmt.Errorf("sync environment file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return EnvWriteReceipt{}, fmt.Errorf("close environment file: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return EnvWriteReceipt{}, err
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return EnvWriteReceipt{}, fmt.Errorf("publish environment file: %w", err)
	}
	removeTemp = false
	if err := syncDirectory(dir); err != nil {
		return EnvWriteReceipt{}, fmt.Errorf("sync environment directory: %w", err)
	}

	digest := sha256.Sum256(contents)
	return EnvWriteReceipt{Digest: hex.EncodeToString(digest[:])}, nil
}

func renderEnvironment(values map[string]string) ([]byte, error) {
	if len(values) == 0 {
		return nil, errors.New("environment values are required")
	}
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if !validEnvironmentKey(key) {
			return nil, errors.New("environment key is invalid")
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return nil, fmt.Errorf("environment value for %s contains a line break", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(strconv.Quote(values[key]))
		builder.WriteByte('\n')
	}
	return []byte(builder.String()), nil
}

func validEnvironmentKey(key string) bool {
	if key == "" || key[0] < 'A' || key[0] > 'Z' {
		return false
	}
	for _, character := range key {
		if character == '_' || unicode.IsDigit(character) || (character >= 'A' && character <= 'Z') {
			continue
		}
		return false
	}
	return true
}
