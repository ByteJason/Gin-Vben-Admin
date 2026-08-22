package installplatform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"

	installer "example.com/gin-vben-admin/server/internal/application/installer"
	installstate "example.com/gin-vben-admin/server/internal/domain/installstate"
	platformi18n "example.com/gin-vben-admin/server/internal/platform/i18n"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
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

func (s *EnvironmentInstaller) Publish(ctx context.Context, request installer.ApplyRequest, _ installer.AssetReceipt) (installer.EnvironmentReceipt, error) {
	if s == nil || s.store == nil || s.random == nil || s.stateDir == "" {
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return installer.EnvironmentReceipt{}, err
	}
	if !validEnvironmentSelection(request.SelectedUI, request.Mode) {
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
	reference, err := randomHex(s.random, 16)
	if err != nil {
		return installer.EnvironmentReceipt{}, ErrEnvironmentInstallation
	}

	values := map[string]string{
		"APP_UI_ACTIVE":                request.SelectedUI,
		"APP_UI_MODE":                  request.Mode,
		"I18N_MODE":                    localeMode,
		"I18N_DEFAULT_LOCALE":          locale,
		"I18N_SUPPORTED_LOCALES":       strings.Join(localeConfig.SupportedLocales, ","),
		"AUTH_ACCESS_TTL":              "30m",
		"AUTH_AUDIENCE":                "admin",
		"AUTH_BCRYPT_COST":             "12",
		"AUTH_ENABLED":                 "true",
		"AUTH_ISSUER":                  "gin-vben-admin",
		"AUTH_JWT_SECRET":              jwtSecret,
		"AUTH_LOCKOUT_DURATION":        "15m",
		"AUTH_LOCKOUT_THRESHOLD":       "5",
		"AUTH_CAPTCHA_ENABLED":         "false",
		"AUTH_CAPTCHA_RISK_THRESHOLD":  "3",
		"AUTH_CAPTCHA_RISK_WINDOW":     "15m",
		"AUTH_CAPTCHA_CHALLENGE_TTL":   "2m",
		"AUTH_CAPTCHA_KEY_PREFIX":      "auth-captcha",
		"AUTH_RATE_LIMIT_MAX_ATTEMPTS": "10",
		"AUTH_RATE_LIMIT_WINDOW":       "1m",
		"AUTH_REFRESH_COOKIE_NAME":     "refresh_token",
		"AUTH_REFRESH_TTL":             "168h",
		"AUTH_REGISTRATION_ENABLED":    "false",
		"AUTH_SECURE_COOKIE":           "false",
		"DATABASE_CONN_MAX_IDLE_TIME":  "15m",
		"DATABASE_CONN_MAX_LIFETIME":   "1h",
		"DATABASE_DRIVER":              database.Driver,
		"DATABASE_ENABLED":             "true",
		"DATABASE_MAX_IDLE_CONNS":      "5",
		"DATABASE_MAX_OPEN_CONNS":      "10",
		"DATABASE_MODE":                string(database.Mode),
		"DATABASE_PING_TIMEOUT":        "5s",
		"DATABASE_READ_POLICY":         string(database.ReadPolicy),
		"INSTALL_STATE_DIR":            s.stateDir,
		"REDIS_DB":                     strconv.Itoa(redis.DB),
		"REDIS_DIAL_TIMEOUT":           "5s",
		"REDIS_ENABLED":                "true",
		"REDIS_MODE":                   redis.Mode,
		"REDIS_NAMESPACE":              redis.Namespace,
		"REDIS_PASSWORD":               redis.Password,
		"REDIS_PING_TIMEOUT":           "3s",
		"REDIS_READ_TIMEOUT":           "3s",
		"REDIS_USERNAME":               redis.Username,
		"REDIS_WRITE_TIMEOUT":          "3s",
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

	writeReceipt, err := s.store.Write(ctx, values)
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
	return installer.EnvironmentReceipt{Digest: writeReceipt.Digest, Reference: reference}, nil
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
	if !ok || writeReceipt.Digest != receipt.Digest {
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

func randomHex(source io.Reader, bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := io.ReadFull(source, buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
