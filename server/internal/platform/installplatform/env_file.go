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
	"AUTH_CAPTCHA_ENABLED":         {},
	"AUTH_CAPTCHA_RISK_THRESHOLD":  {},
	"AUTH_CAPTCHA_RISK_WINDOW":     {},
	"AUTH_CAPTCHA_CHALLENGE_TTL":   {},
	"AUTH_CAPTCHA_KEY_PREFIX":      {},
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
	"INSTALL_TRANSACTION_ID":       {},
	"I18N_DEFAULT_LOCALE":          {},
	"I18N_MODE":                    {},
	"I18N_SUPPORTED_LOCALES":       {},
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
	path         string
	backupDir    string
	processGuard string
}

func NewAtomicEnvStore(path string, backupDir ...string) *AtomicEnvStore {
	store := &AtomicEnvStore{
		path:         filepath.Clean(path),
		processGuard: filepath.Join(filepath.Dir(filepath.Clean(path)), "process.guard"),
	}
	if len(backupDir) > 0 {
		store.backupDir = filepath.Clean(backupDir[0])
		store.processGuard = filepath.Join(filepath.Dir(store.backupDir), "process.guard")
	}
	return store
}

// Write creates a private dotenv file using an fsync + atomic rename sequence.
// An existing file is preserved until the separate backup/rollback workflow is
// explicitly used.
func (s *AtomicEnvStore) Write(ctx context.Context, values map[string]string) (EnvWriteReceipt, error) {
	return s.write(ctx, values, "")
}

// WritePrepared publishes values with a deterministic, transaction-owned
// backup. This closes the crash window between the filesystem rename and the
// application persisting its returned receipt.
func (s *AtomicEnvStore) WritePrepared(ctx context.Context, values map[string]string, reference string) (EnvWriteReceipt, error) {
	if !validPreparedEnvironmentReference(reference) {
		return EnvWriteReceipt{}, errors.New("environment transaction reference is invalid")
	}
	if values["INSTALL_TRANSACTION_ID"] != reference {
		return EnvWriteReceipt{}, errors.New("environment transaction tag does not match its prepared reference")
	}
	return s.write(ctx, values, preparedEnvironmentBackupName(reference))
}

func (s *AtomicEnvStore) write(ctx context.Context, values map[string]string, preparedBackupName string) (EnvWriteReceipt, error) {
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
	release, err := acquireProcessLeaseWithGuard(lockPath, s.processGuard)
	if errors.Is(err, errProcessLeaseBusy) {
		return EnvWriteReceipt{}, ErrEnvironmentBusy
	}
	if err != nil {
		return EnvWriteReceipt{}, fmt.Errorf("acquire environment file lock: %w", err)
	}
	defer release()

	previous, exists, err := readRegularFile(s.path, maxEnvironmentBytes)
	if err != nil {
		return EnvWriteReceipt{}, fmt.Errorf("inspect environment file: %w", err)
	}
	if exists && (s.backupDir == "" || s.backupDir == ".") {
		return EnvWriteReceipt{}, ErrEnvironmentExists
	}

	var backupPath, previousDigest string
	if exists {
		if preparedBackupName == "" {
			backupPath, previousDigest, err = createPrivateBackup(s.backupDir, previous)
		} else {
			backupPath, previousDigest, err = createPrivateNamedBackup(s.backupDir, preparedBackupName, previous)
		}
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

// RecoverPrepared compensates all filesystem states reachable from
// WritePrepared. It never overwrites unrelated current contents and never
// follows a symlink.
func (s *AtomicEnvStore) RecoverPrepared(ctx context.Context, reference string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.path == "" || s.path == "." || !validPreparedEnvironmentReference(reference) {
		return errors.New("environment prepared recovery is invalid")
	}

	lockPath := s.path + ".install.lock"
	release, err := acquireProcessLeaseWithGuard(lockPath, s.processGuard)
	if errors.Is(err, errProcessLeaseBusy) {
		return ErrEnvironmentBusy
	}
	if err != nil {
		return fmt.Errorf("acquire environment recovery lock: %w", err)
	}
	defer release()

	current, currentExists, err := readRegularFile(s.path, maxEnvironmentBytes)
	if err != nil {
		return fmt.Errorf("inspect prepared environment file: %w", err)
	}
	backupPath := ""
	var backup []byte
	backupExists := false
	if s.backupDir != "" && s.backupDir != "." {
		backupPath = filepath.Join(s.backupDir, preparedEnvironmentBackupName(reference))
		backup, backupExists, err = readRegularFile(backupPath, maxEnvironmentBytes)
		if err != nil {
			return fmt.Errorf("inspect prepared environment backup: %w", err)
		}
	}

	ownedCurrent := currentExists && environmentContainsTransaction(current, reference)
	switch {
	case backupExists && (!currentExists || ownedCurrent):
		if err := publishPrivateFile(s.path, backup); err != nil {
			return fmt.Errorf("restore prepared environment backup: %w", err)
		}
		if err := removeAndSync(backupPath); err != nil {
			return fmt.Errorf("remove prepared environment backup: %w", err)
		}
	case backupExists && currentExists && digestBytes(current) == digestBytes(backup):
		// The process stopped after preparing the backup but before publishing.
		if err := removeAndSync(backupPath); err != nil {
			return fmt.Errorf("remove unused prepared environment backup: %w", err)
		}
	case backupExists:
		return ErrEnvironmentChanged
	case ownedCurrent:
		if err := removeAndSync(s.path); err != nil {
			return fmt.Errorf("remove prepared environment file: %w", err)
		}
	}

	if err := cleanupPreparedEnvironmentTemps(s.path, reference); err != nil {
		return err
	}
	if err := cleanupPreparedEnvironmentBackupTemps(s.backupDir, reference); err != nil {
		return err
	}
	return nil
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
	release, err := acquireProcessLeaseWithGuard(lockPath, s.processGuard)
	if errors.Is(err, errProcessLeaseBusy) {
		return ErrEnvironmentBusy
	}
	if err != nil {
		return fmt.Errorf("acquire environment rollback lock: %w", err)
	}
	defer release()

	current, exists, err := readRegularFile(s.path, maxEnvironmentBytes)
	if err != nil {
		return ErrEnvironmentChanged
	}
	if !exists && !receipt.Replaced {
		return nil
	}
	if receipt.Replaced && (!exists || (exists && digestBytes(current) == receipt.PreviousDigest)) {
		backup, backupExists, backupErr := readRegularFile(receipt.backupPath, maxEnvironmentBytes)
		if backupErr != nil {
			return errors.New("environment backup is unavailable")
		}
		if !backupExists {
			if exists {
				// The previous contents are already restored and the transaction
				// backup was already consumed by an earlier compensation attempt.
				return nil
			}
			return errors.New("environment backup is unavailable")
		}
		if digestBytes(backup) != receipt.PreviousDigest {
			return errors.New("environment backup is unavailable")
		}
		if !exists {
			if err := publishPrivateFile(s.path, backup); err != nil {
				return fmt.Errorf("restore environment file: %w", err)
			}
		}
		if err := removeAndSync(receipt.backupPath); err != nil {
			return fmt.Errorf("remove environment backup: %w", err)
		}
		return nil
	}
	if !exists || digestBytes(current) != receipt.Digest {
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

// Finalize removes only the deterministic pre-install backup owned by a
// committed transaction. The installed dotenv file is deliberately left
// untouched. A missing backup is an idempotent success for restart recovery.
func (s *AtomicEnvStore) Finalize(ctx context.Context, receipt EnvWriteReceipt) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || receipt.targetPath != s.path || receipt.Digest == "" {
		return errors.New("environment finalize receipt is invalid")
	}
	if !receipt.Replaced {
		if receipt.PreviousDigest != "" || receipt.backupPath != "" {
			return errors.New("environment finalize receipt is invalid")
		}
		return nil
	}
	if s.backupDir == "" || s.backupDir == "." || receipt.PreviousDigest == "" ||
		filepath.Dir(receipt.backupPath) != s.backupDir ||
		filepath.Base(receipt.backupPath) != preparedEnvironmentBackupName(strings.TrimPrefix(filepath.Base(receipt.backupPath), ".env.previous-")) {
		return errors.New("environment finalize receipt is invalid")
	}

	lockPath := s.path + ".install.lock"
	release, err := acquireProcessLeaseWithGuard(lockPath, s.processGuard)
	if errors.Is(err, errProcessLeaseBusy) {
		return ErrEnvironmentBusy
	}
	if err != nil {
		return fmt.Errorf("acquire environment finalize lock: %w", err)
	}
	defer release()

	backup, exists, err := readRegularFile(receipt.backupPath, maxEnvironmentBytes)
	if err != nil {
		return errors.New("environment backup is unavailable")
	}
	if !exists {
		return nil
	}
	if digestBytes(backup) != receipt.PreviousDigest {
		return errors.New("environment backup is unavailable")
	}
	if err := removeAndSync(receipt.backupPath); err != nil {
		return fmt.Errorf("remove committed environment backup: %w", err)
	}
	return nil
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

func preparedEnvironmentBackupName(reference string) string {
	return ".env.previous-" + reference
}

func createPrivateNamedBackup(dir, name string, contents []byte) (string, string, error) {
	if dir == "" || dir == "." || filepath.Base(name) != name || !strings.HasPrefix(name, ".env.previous-") {
		return "", "", errors.New("prepared environment backup path is invalid")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("create environment backup directory: %w", err)
	}
	path := filepath.Join(dir, name)
	file, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return "", "", fmt.Errorf("create temporary prepared environment backup: %w", err)
	}
	temporaryPath := file.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := writeAndSync(file, contents); err != nil {
		return "", "", fmt.Errorf("write prepared environment backup: %w", err)
	}
	if err := os.Link(temporaryPath, path); errors.Is(err, os.ErrExist) {
		return "", "", ErrEnvironmentBusy
	} else if err != nil {
		return "", "", fmt.Errorf("publish prepared environment backup: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return "", "", fmt.Errorf("remove temporary prepared environment backup: %w", err)
	}
	if err := syncDirectory(dir); err != nil {
		return "", "", fmt.Errorf("sync prepared environment backup directory: %w", err)
	}
	remove = false
	return path, digestBytes(contents), nil
}

func environmentContainsTransaction(contents []byte, reference string) bool {
	want := "INSTALL_TRANSACTION_ID=" + strconv.Quote(reference)
	scanner := bufio.NewScanner(strings.NewReader(string(contents)))
	for scanner.Scan() {
		if scanner.Text() == want {
			return true
		}
	}
	return false
}

func cleanupPreparedEnvironmentTemps(targetPath, reference string) error {
	dir := filepath.Dir(targetPath)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect prepared environment temporary files: %w", err)
	}
	prefix := filepath.Base(targetPath) + ".tmp-"
	removed := false
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		contents, exists, readErr := readRegularFile(path, maxEnvironmentBytes)
		if readErr != nil || !exists || !environmentContainsTransaction(contents, reference) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove prepared environment temporary file: %w", err)
		}
		removed = true
	}
	if removed {
		return syncDirectory(dir)
	}
	return nil
}

func cleanupPreparedEnvironmentBackupTemps(backupDir, reference string) error {
	if backupDir == "" || backupDir == "." {
		return nil
	}
	entries, err := os.ReadDir(backupDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect prepared environment backup temporary files: %w", err)
	}
	prefix := preparedEnvironmentBackupName(reference) + ".tmp-"
	removed := false
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		path := filepath.Join(backupDir, entry.Name())
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove prepared environment backup temporary file: %w", err)
		}
		removed = true
	}
	if removed {
		return syncDirectory(backupDir)
	}
	return nil
}

func removeAndSync(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
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
