package installplatform

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var (
	ErrEnvironmentExists  = errors.New("environment file already exists")
	ErrEnvironmentBusy    = errors.New("environment file is being written")
	ErrEnvironmentChanged = errors.New("environment file changed after installation write")
)

const maxEnvironmentBytes = 1 << 20

var allowedInstallerEnvironmentKeys = map[string]struct{}{
	"APP_UI_ACTIVE":                {},
	"APP_UI_MODE":                  {},
	"AUTH_ACCESS_TTL":              {},
	"AUTH_AUDIENCE":                {},
	"AUTH_BCRYPT_COST":             {},
	"AUTH_ENABLED":                 {},
	"AUTH_ISSUER":                  {},
	"AUTH_JWT_SECRET":              {},
	"AUTH_LOCKOUT_DURATION":        {},
	"AUTH_LOCKOUT_THRESHOLD":       {},
	"AUTH_RATE_LIMIT_MAX_ATTEMPTS": {},
	"AUTH_RATE_LIMIT_WINDOW":       {},
	"AUTH_REFRESH_COOKIE_NAME":     {},
	"AUTH_REFRESH_TTL":             {},
	"AUTH_REGISTRATION_ENABLED":    {},
	"AUTH_SECURE_COOKIE":           {},
	"DATABASE_CONN_MAX_IDLE_TIME":  {},
	"DATABASE_CONN_MAX_LIFETIME":   {},
	"DATABASE_DRIVER":              {},
	"DATABASE_DSN":                 {},
	"DATABASE_ENABLED":             {},
	"DATABASE_MAX_IDLE_CONNS":      {},
	"DATABASE_MAX_OPEN_CONNS":      {},
	"DATABASE_MODE":                {},
	"DATABASE_PING_TIMEOUT":        {},
	"DATABASE_PRIMARY_DSN":         {},
	"DATABASE_READ_POLICY":         {},
	"DATABASE_REPLICA_DSNS":        {},
	"INSTALL_STATE_DIR":            {},
	"LOGGING_LEVEL":                {},
	"REDIS_ADDR":                   {},
	"REDIS_ADDRS":                  {},
	"REDIS_DB":                     {},
	"REDIS_DIAL_TIMEOUT":           {},
	"REDIS_ENABLED":                {},
	"REDIS_MASTER_NAME":            {},
	"REDIS_MODE":                   {},
	"REDIS_NAMESPACE":              {},
	"REDIS_PASSWORD":               {},
	"REDIS_PING_TIMEOUT":           {},
	"REDIS_READ_TIMEOUT":           {},
	"REDIS_USERNAME":               {},
	"REDIS_WRITE_TIMEOUT":          {},
	"SERVER_ADDR":                  {},
	"SERVER_IDLE_TIMEOUT":          {},
	"SERVER_READ_TIMEOUT":          {},
	"SERVER_SHUTDOWN_TIMEOUT":      {},
	"SERVER_WRITE_TIMEOUT":         {},
}

// EnvWriteReceipt is a credential-free summary of a published environment
// file. It is safe to persist in an installation transaction manifest.
type EnvWriteReceipt struct {
	Digest         string
	PreviousDigest string
	Replaced       bool
	targetPath     string
	backupPath     string
}

// AtomicEnvStore publishes the root environment file without exposing its
// values through return types or logs.
type AtomicEnvStore struct {
	path      string
	backupDir string
}

func NewAtomicEnvStore(path string, backupDir ...string) *AtomicEnvStore {
	store := &AtomicEnvStore{path: filepath.Clean(path)}
	if len(backupDir) > 0 {
		store.backupDir = filepath.Clean(backupDir[0])
	}
	return store
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

	previous, exists, err := readRegularFile(s.path, maxEnvironmentBytes)
	if err != nil {
		return EnvWriteReceipt{}, fmt.Errorf("inspect environment file: %w", err)
	}
	if exists && (s.backupDir == "" || s.backupDir == ".") {
		return EnvWriteReceipt{}, ErrEnvironmentExists
	}

	var backupPath, previousDigest string
	if exists {
		backupPath, previousDigest, err = createPrivateBackup(s.backupDir, previous)
		if err != nil {
			return EnvWriteReceipt{}, err
		}
	}
	if err := contextError(ctx); err != nil {
		return EnvWriteReceipt{}, err
	}
	if err := publishPrivateFile(s.path, contents); err != nil {
		return EnvWriteReceipt{}, err
	}
	digest := sha256.Sum256(contents)
	return EnvWriteReceipt{
		Digest:         hex.EncodeToString(digest[:]),
		PreviousDigest: previousDigest,
		Replaced:       exists,
		targetPath:     s.path,
		backupPath:     backupPath,
	}, nil
}

// Rollback restores the pre-installation file, or removes a file created by
// this transaction. A digest mismatch prevents overwriting later user edits.
func (s *AtomicEnvStore) Rollback(ctx context.Context, receipt EnvWriteReceipt) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || receipt.targetPath != s.path || receipt.Digest == "" {
		return errors.New("environment rollback receipt is invalid")
	}

	lockPath := s.path + ".install.lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return ErrEnvironmentBusy
	}
	if err != nil {
		return fmt.Errorf("acquire environment rollback lock: %w", err)
	}
	if err := lock.Close(); err != nil {
		_ = os.Remove(lockPath)
		return fmt.Errorf("close environment rollback lock: %w", err)
	}
	defer os.Remove(lockPath)

	current, exists, err := readRegularFile(s.path, maxEnvironmentBytes)
	if err != nil || !exists || digestBytes(current) != receipt.Digest {
		return ErrEnvironmentChanged
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if !receipt.Replaced {
		if err := os.Remove(s.path); err != nil {
			return fmt.Errorf("remove installed environment file: %w", err)
		}
		return syncDirectory(filepath.Dir(s.path))
	}

	backup, exists, err := readRegularFile(receipt.backupPath, maxEnvironmentBytes)
	if err != nil || !exists || digestBytes(backup) != receipt.PreviousDigest {
		return errors.New("environment backup is unavailable")
	}
	if err := publishPrivateFile(s.path, backup); err != nil {
		return fmt.Errorf("restore environment file: %w", err)
	}
	if err := os.Remove(receipt.backupPath); err != nil {
		return fmt.Errorf("remove environment backup: %w", err)
	}
	return syncDirectory(filepath.Dir(receipt.backupPath))
}

func createPrivateBackup(dir string, contents []byte) (string, string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("create environment backup directory: %w", err)
	}
	file, err := os.CreateTemp(dir, ".env.previous-*")
	if err != nil {
		return "", "", fmt.Errorf("create environment backup: %w", err)
	}
	path := file.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(path)
		}
	}()
	if err := writeAndSync(file, contents); err != nil {
		return "", "", fmt.Errorf("write environment backup: %w", err)
	}
	if err := syncDirectory(dir); err != nil {
		return "", "", fmt.Errorf("sync environment backup directory: %w", err)
	}
	remove = false
	return path, digestBytes(contents), nil
}

func publishPrivateFile(path string, contents []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary environment file: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := writeAndSync(temp, contents); err != nil {
		return fmt.Errorf("write temporary environment file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish environment file: %w", err)
	}
	removeTemp = false
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("sync environment directory: %w", err)
	}
	return nil
}

func writeAndSync(file *os.File, contents []byte) error {
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	writer := bufio.NewWriter(file)
	if _, err := writer.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func readRegularFile(path string, limit int64) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, errors.New("path is not a regular file")
	}
	if info.Size() > limit {
		return nil, false, errors.New("file exceeds the supported size")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, false, errors.New("file changed while it was opened")
	}
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(contents)) > limit {
		return nil, false, errors.New("file exceeds the supported size")
	}
	return contents, true, nil
}

func digestBytes(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
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
		if _, allowed := allowedInstallerEnvironmentKeys[key]; !allowed {
			return nil, errors.New("environment key is not approved for installation")
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
	for index := 0; index < len(key); index++ {
		character := key[index]
		if character == '_' || (character >= '0' && character <= '9') || (character >= 'A' && character <= 'Z') {
			continue
		}
		return false
	}
	return true
}
