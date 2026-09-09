package installplatform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
	platformi18n "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/i18n"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
)

var ErrEnvironmentInstallation = errors.New("installation environment publication failed")

type EnvironmentInstaller struct {
	store    *AtomicEnvStore
	stateDir string
	random   io.Reader
	mutex    sync.Mutex
	receipts map[string]EnvWriteReceipt
}

func NewEnvironmentInstaller(store *AtomicEnvStore, stateDir string, randomSource io.Reader) *EnvironmentInstaller {
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &EnvironmentInstaller{
		store: store, stateDir: strings.TrimSpace(stateDir), random: randomSource,
		receipts: make(map[string]EnvWriteReceipt),
	}
}

func (s *EnvironmentInstaller) Publish(ctx context.Context, request installer.ApplyRequest, plan installer.Plan) (installer.EnvironmentReceipt, error) {
	if s == nil || s.random == nil {
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}
	referenceSuffix, err := randomHex(s.random, 16)
	if err != nil {
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}
	return s.PublishWithReference(ctx, request, plan, "environment-"+referenceSuffix)
}

// PublishWithReference publishes a dotenv file tagged with the durable
// transaction reference that was journaled before this call. The tag and the
// deterministic backup name let a restarted process compensate a completed
// write even when no in-memory receipt was returned to the application layer.
func (s *EnvironmentInstaller) PublishWithReference(ctx context.Context, request installer.ApplyRequest, plan installer.Plan, reference string) (installer.EnvironmentReceipt, error) {
	if s == nil || s.store == nil || s.random == nil || s.stateDir == "" {
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}
	if !validPreparedEnvironmentReference(reference) {
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return installer.EnvironmentReceipt{}, err
	}
	if !validEnvironmentSelection(string(plan.SelectedUI), string(plan.Mode)) || request.Mode != string(plan.Mode) {
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}
	locale := strings.TrimSpace(request.Locale)
	if locale == "" {
		locale = platformi18n.LocaleZhCN
	}
	localeMode := strings.TrimSpace(request.LocaleMode)
	if localeMode == "" {
		localeMode = string(platformi18n.ModeSingle)
	}
	localeConfig := platformi18n.Config{Mode: platformi18n.Mode(localeMode), DefaultLocale: locale, SupportedLocales: []string{platformi18n.LocaleZhCN, platformi18n.LocaleEnUS}}
	if err := localeConfig.Validate(); err != nil {
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}
	database, err := databaseOptionsFromRequest(request.Database)
	if err != nil {
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}
	redis, err := redisOptionsFromRequest(request.Redis)
	if err != nil {
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}
	jwtSecret, err := randomHex(s.random, 32)
	if err != nil {
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}
	// Persist installation inputs, not a second copy of compiled defaults.
	// UI selection belongs to the local profile; run mode belongs to the
	// installation manifest. Neither has a runtime dotenv consumer.
	values := map[string]string{
		"AUTH_ENABLED":           "true",
		"AUTH_JWT_SECRET":        jwtSecret,
		"DATABASE_DRIVER":        database.Driver,
		"DATABASE_ENABLED":       "true",
		"DATABASE_MODE":          string(database.Mode),
		"DATABASE_READ_POLICY":   string(database.ReadPolicy),
		"INSTALL_STATE_DIR":      s.stateDir,
		"INSTALL_TRANSACTION_ID": reference,
		"REDIS_DB":               strconv.Itoa(redis.DB),
		"REDIS_ENABLED":          "true",
		"REDIS_MODE":             redis.Mode,
		"REDIS_NAMESPACE":        redis.Namespace,
		"REDIS_PASSWORD":         redis.Password,
		"REDIS_USERNAME":         redis.Username,
	}
	// Preserve explicit locale policy for existing API clients without pinning
	// the browser installer's default language into every generated .env.
	if request.Locale != "" || request.LocaleMode != "" {
		values["I18N_MODE"] = localeMode
		values["I18N_DEFAULT_LOCALE"] = locale
		values["I18N_SUPPORTED_LOCALES"] = strings.Join(localeConfig.SupportedLocales, ",")
	}
	if database.Mode == gormdb.ModeReadWrite {
		values["DATABASE_PRIMARY_DSN"] = database.PrimaryDSN
		values["DATABASE_REPLICA_DSNS"] = strings.Join(database.ReplicaDSNs, ",")
	} else {
		values["DATABASE_DSN"] = database.DSN
	}
	if redis.Mode == "single" {
		values["REDIS_ADDR"] = redis.Addr
	} else {
		values["REDIS_ADDRS"] = strings.Join(redis.Addrs, ",")
		if redis.MasterName != "" {
			values["REDIS_MASTER_NAME"] = redis.MasterName
		}
	}

	writeReceipt, err := s.store.WritePrepared(ctx, values, reference)
	if err != nil {
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}
	s.mutex.Lock()
	if _, collision := s.receipts[reference]; collision {
		s.mutex.Unlock()
		_ = s.store.Rollback(context.Background(), writeReceipt)
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}
	s.receipts[reference] = writeReceipt
	s.mutex.Unlock()
	backupName := ""
	if writeReceipt.backupPath != "" {
		backupName = filepath.Base(writeReceipt.backupPath)
	}
	return installer.EnvironmentReceipt{
		Digest: writeReceipt.Digest, Reference: reference,
		PreviousDigest: writeReceipt.PreviousDigest, BackupName: backupName, Replaced: writeReceipt.Replaced,
	}, nil
}

// RecoverPrepared compensates a prepared publication after process restart.
// It only touches a dotenv file or deterministic backup that proves ownership
// through the exact transaction reference.
func (s *EnvironmentInstaller) RecoverPrepared(ctx context.Context, reference string) error {
	if s == nil || s.store == nil || !validPreparedEnvironmentReference(reference) {
		return ErrEnvironmentInstallation
	}
	if err := s.store.RecoverPrepared(ctx, reference); err != nil {
		return ErrEnvironmentInstallation
	}
	s.mutex.Lock()
	delete(s.receipts, reference)
	s.mutex.Unlock()
	return nil
}

func validPreparedEnvironmentReference(reference string) bool {
	reference = strings.TrimSpace(reference)
	if reference == "" || len(reference) > 96 {
		return false
	}
	for _, character := range reference {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func validEnvironmentSelection(ui, mode string) bool {
	switch installstate.UI(ui) {
	case installstate.UIAntd, installstate.UIEle, installstate.UINaive:
	default:
		return false
	}
	switch installstate.Mode(mode) {
	case installstate.ModeEmbedded, installstate.ModeStandalone, installstate.ModeAPIOnly, installstate.ModeDev:
		return true
	default:
		return false
	}
}

func (s *EnvironmentInstaller) Rollback(ctx context.Context, receipt installer.EnvironmentReceipt) error {
	if s == nil || s.store == nil || receipt.Reference == "" || receipt.Digest == "" {
		return ErrEnvironmentInstallation
	}
	s.mutex.Lock()
	writeReceipt, ok := s.receipts[receipt.Reference]
	s.mutex.Unlock()
	if !ok {
		writeReceipt = EnvWriteReceipt{
			Digest: receipt.Digest, PreviousDigest: receipt.PreviousDigest, Replaced: receipt.Replaced,
			targetPath: s.store.path,
		}
		if receipt.Replaced {
			if receipt.BackupName != preparedEnvironmentBackupName(receipt.Reference) || filepath.Base(receipt.BackupName) != receipt.BackupName || s.store.backupDir == "" || s.store.backupDir == "." {
				return ErrEnvironmentInstallation
			}
			writeReceipt.backupPath = filepath.Join(s.store.backupDir, receipt.BackupName)
		}
	}
	if writeReceipt.Digest != receipt.Digest || writeReceipt.PreviousDigest != receipt.PreviousDigest || writeReceipt.Replaced != receipt.Replaced {
		return ErrEnvironmentInstallation
	}
	if err := s.store.Rollback(ctx, writeReceipt); err != nil {
		return ErrEnvironmentInstallation
	}
	s.mutex.Lock()
	delete(s.receipts, receipt.Reference)
	s.mutex.Unlock()
	return nil
}

// Finalize commits an environment publication by deleting only its exact
// transaction-owned pre-install backup. It is safe to call again after a
// restart when the backup was already removed but the journal still exists.
func (s *EnvironmentInstaller) Finalize(ctx context.Context, receipt installer.EnvironmentReceipt) error {
	if s == nil || s.store == nil || !validPreparedEnvironmentReference(receipt.Reference) || receipt.Digest == "" {
		return ErrEnvironmentInstallation
	}
	s.mutex.Lock()
	writeReceipt, ok := s.receipts[receipt.Reference]
	s.mutex.Unlock()
	if !ok {
		writeReceipt = EnvWriteReceipt{
			Digest: receipt.Digest, PreviousDigest: receipt.PreviousDigest, Replaced: receipt.Replaced,
			targetPath: s.store.path,
		}
		if receipt.Replaced {
			if receipt.BackupName != preparedEnvironmentBackupName(receipt.Reference) || filepath.Base(receipt.BackupName) != receipt.BackupName || s.store.backupDir == "" || s.store.backupDir == "." {
				return ErrEnvironmentInstallation
			}
			writeReceipt.backupPath = filepath.Join(s.store.backupDir, receipt.BackupName)
		} else if receipt.PreviousDigest != "" || receipt.BackupName != "" {
			return ErrEnvironmentInstallation
		}
	}
	if writeReceipt.Digest != receipt.Digest || writeReceipt.PreviousDigest != receipt.PreviousDigest || writeReceipt.Replaced != receipt.Replaced {
		return ErrEnvironmentInstallation
	}
	if err := s.store.Finalize(ctx, writeReceipt); err != nil {
		return ErrEnvironmentInstallation
	}
	s.mutex.Lock()
	delete(s.receipts, receipt.Reference)
	s.mutex.Unlock()
	return nil
}

func randomHex(source io.Reader, bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := io.ReadFull(source, buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
