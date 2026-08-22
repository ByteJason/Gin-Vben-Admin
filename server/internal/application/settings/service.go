// Package settings provides versioned, auditable runtime settings. It is
// storage/provider agnostic so B6 can run with memory or a database adapter;
// no exporter or external collector is created here.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const maskedValue = "[REDACTED]"

var (
	ErrInvalidSetting   = errors.New("invalid setting")
	ErrVersionConflict  = errors.New("setting version conflict")
	ErrSettingNotFound  = errors.New("setting not found")
	ErrPermissionDenied = errors.New("setting permission denied")
)

type ValueKind string

const (
	KindString ValueKind = "string"
	KindBool   ValueKind = "bool"
	KindNumber ValueKind = "number"
	KindJSON   ValueKind = "json"
	KindSecret ValueKind = "secret"
)

// Category identifies the bounded configuration areas exposed by the 0.10
// settings center. Keeping the values stable lets all three admin templates
// render the same schema without duplicating validation rules.
type Category string

const (
	CategoryBasic    Category = "basic"
	CategorySecurity Category = "security"
	CategoryMail     Category = "mail"
	CategoryFile     Category = "file"
	CategoryCaptcha  Category = "captcha"
	CategoryI18n     Category = "i18n"
	CategoryOther    Category = "other"
)

// Source is the effective configuration authority for a setting. The order is
// intentional and follows DEC-018: process environment, root .env, YAML,
// database, then the compiled definition default.
type Source string

const (
	SourceEnv      Source = "env"
	SourceDotEnv   Source = "dotenv"
	SourceYAML     Source = "yaml"
	SourceDatabase Source = "database"
	SourceDefault  Source = "default"
)

type Definition struct {
	Key             string    `json:"key"`
	Category        Category  `json:"category"`
	Kind            ValueKind `json:"kind"`
	Sensitive       bool      `json:"sensitive"`
	Default         string    `json:"default"`
	Allowed         []string  `json:"allowed,omitempty"`
	Description     string    `json:"description,omitempty"`
	RestartRequired bool      `json:"restartRequired"`
	EnvKey          string    `json:"envKey,omitempty"`
	YAMLPath        string    `json:"yamlPath,omitempty"`
}

func DefaultDefinitions() map[string]Definition {
	return map[string]Definition{
		"site.name":                         {Key: "site.name", Category: CategoryBasic, Kind: KindString, Default: `"Gin-Vben-Admin"`, EnvKey: "SITE_NAME", YAMLPath: "site.name"},
		"basic.site_name":                   {Key: "basic.site_name", Category: CategoryBasic, Kind: KindString, Default: `"Gin-Vben-Admin"`, Description: "管理端显示名称", EnvKey: "SITE_NAME", YAMLPath: "site.name"},
		"security.jwt_secret":               {Key: "security.jwt_secret", Category: CategorySecurity, Kind: KindSecret, Sensitive: true, Default: `""`, Description: "签发访问令牌的运行时密钥", RestartRequired: true, EnvKey: "AUTH_JWT_SECRET", YAMLPath: "auth.jwt_secret"},
		"security.access_ttl":               {Key: "security.access_ttl", Category: CategorySecurity, Kind: KindString, Default: `"30m"`, RestartRequired: true, EnvKey: "AUTH_ACCESS_TTL", YAMLPath: "auth.access_ttl"},
		"security.refresh_ttl":              {Key: "security.refresh_ttl", Category: CategorySecurity, Kind: KindString, Default: `"168h"`, RestartRequired: true, EnvKey: "AUTH_REFRESH_TTL", YAMLPath: "auth.refresh_ttl"},
		"security.secure_cookie":            {Key: "security.secure_cookie", Category: CategorySecurity, Kind: KindBool, Default: `false`, RestartRequired: true, EnvKey: "AUTH_SECURE_COOKIE", YAMLPath: "auth.secure_cookie"},
		"mail.enabled":                      {Key: "mail.enabled", Category: CategoryMail, Kind: KindBool, Default: `false`, RestartRequired: true, EnvKey: "MAIL_ENABLED", YAMLPath: "mail.enabled"},
		"mail.host":                         {Key: "mail.host", Category: CategoryMail, Kind: KindString, Default: `""`, Description: "SMTP 主机", RestartRequired: true, EnvKey: "MAIL_HOST", YAMLPath: "mail.host"},
		"mail.port":                         {Key: "mail.port", Category: CategoryMail, Kind: KindNumber, Default: `1025`, RestartRequired: true, EnvKey: "MAIL_PORT", YAMLPath: "mail.port"},
		"mail.username":                     {Key: "mail.username", Category: CategoryMail, Kind: KindString, Default: `""`, RestartRequired: true, EnvKey: "MAIL_USERNAME", YAMLPath: "mail.username"},
		"mail.password":                     {Key: "mail.password", Category: CategoryMail, Kind: KindSecret, Sensitive: true, Default: `""`, RestartRequired: true, EnvKey: "MAIL_PASSWORD", YAMLPath: "mail.password"},
		"mail.from":                         {Key: "mail.from", Category: CategoryMail, Kind: KindString, Default: `""`, RestartRequired: true, EnvKey: "MAIL_FROM", YAMLPath: "mail.from"},
		"mail.start_tls":                    {Key: "mail.start_tls", Category: CategoryMail, Kind: KindBool, Default: `false`, RestartRequired: true, EnvKey: "MAIL_START_TLS", YAMLPath: "mail.start_tls"},
		"file.root":                         {Key: "file.root", Category: CategoryFile, Kind: KindString, Default: `"./storage"`, RestartRequired: true, EnvKey: "FILE_ROOT", YAMLPath: "file.root"},
		"file.max_size":                     {Key: "file.max_size", Category: CategoryFile, Kind: KindNumber, Default: `104857600`, Description: "单文件字节上限", EnvKey: "FILE_MAX_SIZE", YAMLPath: "file.max_size"},
		"file.quota":                        {Key: "file.quota", Category: CategoryFile, Kind: KindNumber, Default: `1073741824`, EnvKey: "FILE_QUOTA", YAMLPath: "file.quota"},
		"file.allowed_mimes":                {Key: "file.allowed_mimes", Category: CategoryFile, Kind: KindJSON, Default: `[]`, EnvKey: "FILE_ALLOWED_MIMES", YAMLPath: "file.allowed_mimes"},
		"captcha.enabled":                   {Key: "captcha.enabled", Category: CategoryCaptcha, Kind: KindBool, Default: `false`, Description: "是否启用图片验证码", RestartRequired: true, EnvKey: "AUTH_CAPTCHA_ENABLED", YAMLPath: "auth.captcha_enabled"},
		"captcha.risk_threshold":            {Key: "captcha.risk_threshold", Category: CategoryCaptcha, Kind: KindNumber, Default: `3`, RestartRequired: true, EnvKey: "AUTH_CAPTCHA_RISK_THRESHOLD", YAMLPath: "auth.captcha_risk_threshold"},
		"captcha.challenge_ttl":             {Key: "captcha.challenge_ttl", Category: CategoryCaptcha, Kind: KindString, Default: `"2m"`, RestartRequired: true, EnvKey: "AUTH_CAPTCHA_CHALLENGE_TTL", YAMLPath: "auth.captcha_challenge_ttl"},
		"captcha.key_prefix":                {Key: "captcha.key_prefix", Category: CategoryCaptcha, Kind: KindString, Default: `"auth-captcha"`, RestartRequired: true, EnvKey: "AUTH_CAPTCHA_KEY_PREFIX", YAMLPath: "auth.captcha_key_prefix"},
		"i18n.mode":                         {Key: "i18n.mode", Category: CategoryI18n, Kind: KindString, Default: `"single"`, Allowed: []string{"single", "multi"}, Description: "single 隐藏语言切换，multi 显示语言切换", RestartRequired: true, EnvKey: "I18N_MODE", YAMLPath: "i18n.mode"},
		"i18n.default_locale":               {Key: "i18n.default_locale", Category: CategoryI18n, Kind: KindString, Default: `"zh-CN"`, Allowed: []string{"zh-CN", "en-US"}, RestartRequired: true, EnvKey: "I18N_DEFAULT_LOCALE", YAMLPath: "i18n.default_locale"},
		"i18n.supported_locales":            {Key: "i18n.supported_locales", Category: CategoryI18n, Kind: KindJSON, Default: `["zh-CN","en-US"]`, RestartRequired: true, EnvKey: "I18N_SUPPORTED_LOCALES", YAMLPath: "i18n.supported_locales"},
		"locale.default":                    {Key: "locale.default", Category: CategoryI18n, Kind: KindString, Default: `"zh-CN"`, Allowed: []string{"zh-CN", "en-US"}, RestartRequired: true},
		"observability.metrics.enabled":     {Key: "observability.metrics.enabled", Category: CategoryOther, Kind: KindBool, Default: `false`},
		"observability.metrics.endpoint":    {Key: "observability.metrics.endpoint", Category: CategoryOther, Kind: KindString, Default: `""`},
		"observability.tracing.enabled":     {Key: "observability.tracing.enabled", Category: CategoryOther, Kind: KindBool, Default: `false`},
		"observability.tracing.endpoint":    {Key: "observability.tracing.endpoint", Category: CategoryOther, Kind: KindString, Default: `""`},
		"observability.tracing.protocol":    {Key: "observability.tracing.protocol", Category: CategoryOther, Kind: KindString, Default: `"http/protobuf"`, Allowed: []string{"grpc", "http/protobuf"}},
		"observability.tracing.tls_verify":  {Key: "observability.tracing.tls_verify", Category: CategoryOther, Kind: KindBool, Default: `true`},
		"observability.tracing.sample_rate": {Key: "observability.tracing.sample_rate", Category: CategoryOther, Kind: KindNumber, Default: `0`},
		"observability.otlp.api_key":        {Key: "observability.otlp.api_key", Category: CategoryOther, Kind: KindSecret, Sensitive: true, Default: `""`},
	}
}

type Actor struct {
	ID string
}

type UpdateInput struct {
	Key             string
	Value           json.RawMessage
	ExpectedVersion int64
}

type RollbackInput struct {
	Key             string
	Version         int64
	ExpectedVersion int64
}

type Setting struct {
	Key             string    `json:"key"`
	Category        Category  `json:"category"`
	Value           string    `json:"value"`
	Version         int64     `json:"version"`
	Sensitive       bool      `json:"sensitive"`
	Source          Source    `json:"source"`
	RestartRequired bool      `json:"restartRequired"`
	UpdatedBy       string    `json:"updatedBy"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type StoredSetting struct {
	Key       string
	RawValue  []byte
	Version   int64
	Sensitive bool
	Encrypted bool
	Source    Source
	UpdatedBy string
	UpdatedAt time.Time
}

// ResolvedValue is returned by a SourceResolver. Present=false means the
// resolver has no value and lets the service continue to the next authority.
type ResolvedValue struct {
	RawValue []byte
	Source   Source
	Present  bool
}

type SourceResolver interface {
	Resolve(context.Context, string) (ResolvedValue, error)
}

// MapSourceResolver is a deterministic resolver used by bootstrap adapters and
// tests. Values are selected in DEC-018 order, regardless of map iteration.
type MapSourceResolver struct {
	values map[string]map[Source][]byte
}

func NewMapSourceResolver(values map[string]map[Source][]byte) *MapSourceResolver {
	copyValues := make(map[string]map[Source][]byte, len(values))
	for key, sources := range values {
		copyValues[key] = make(map[Source][]byte, len(sources))
		for source, value := range sources {
			copyValues[key][source] = append([]byte(nil), value...)
		}
	}
	return &MapSourceResolver{values: copyValues}
}

func (r *MapSourceResolver) Resolve(_ context.Context, key string) (ResolvedValue, error) {
	if r == nil {
		return ResolvedValue{}, nil
	}
	sources := r.values[key]
	for _, source := range []Source{SourceEnv, SourceDotEnv, SourceYAML, SourceDatabase} {
		if value, ok := sources[source]; ok {
			return ResolvedValue{RawValue: append([]byte(nil), value...), Source: source, Present: true}, nil
		}
	}
	return ResolvedValue{}, nil
}

type Encryptor interface {
	Encrypt(context.Context, string, []byte) ([]byte, error)
	Decrypt(context.Context, string, []byte) ([]byte, error)
}

type ConnectionTestResult struct {
	Key       string    `json:"key"`
	Category  Category  `json:"category"`
	Status    string    `json:"status"`
	Source    Source    `json:"source"`
	RequestID string    `json:"requestId"`
	Message   string    `json:"message,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
}

type ConnectionTester interface {
	Test(context.Context, Definition, []byte) error
}

type Repository interface {
	Current(context.Context, string) (StoredSetting, error)
	Append(context.Context, StoredSetting) (StoredSetting, error)
	History(context.Context, string) ([]StoredSetting, error)
}

type AuditEvent struct {
	ActorID string
	Action  string
	Key     string
	Version int64
}

type AuditSink interface {
	Record(context.Context, AuditEvent) error
}
type CacheInvalidator interface {
	Invalidate(context.Context, string) error
}
type Authorizer interface {
	Authorize(context.Context, Actor, string, string) error
}

type Service struct {
	repo        Repository
	audit       AuditSink
	cache       CacheInvalidator
	authorizer  Authorizer
	definitions map[string]Definition
	sources     SourceResolver
	encryptor   Encryptor
	connection  ConnectionTester
}

func NewService(repo Repository, audit AuditSink, cache CacheInvalidator, definitions map[string]Definition) *Service {
	if definitions == nil {
		definitions = DefaultDefinitions()
	}
	return &Service{repo: repo, audit: audit, cache: cache, definitions: definitions}
}

func (s *Service) SetAuthorizer(authorizer Authorizer) { s.authorizer = authorizer }

func (s *Service) SetSourceResolver(resolver SourceResolver) { s.sources = resolver }

func (s *Service) SetEncryptor(encryptor Encryptor) { s.encryptor = encryptor }

func (s *Service) SetConnectionTester(tester ConnectionTester) { s.connection = tester }

// Definitions returns a stable, read-only schema view for administration UIs.
// Secret defaults are never exposed by this method; callers only receive the
// type and policy metadata needed to render a form.
func (s *Service) Definitions(ctx context.Context, actor Actor) ([]Definition, error) {
	if err := s.authorize(ctx, actor, "*", "read"); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrSettingNotFound
	}
	keys := make([]string, 0, len(s.definitions))
	for key := range s.definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]Definition, 0, len(keys))
	for _, key := range keys {
		definition := s.definitions[key]
		if definition.Sensitive {
			definition.Default = maskedValue
		}
		definition.Allowed = append([]string(nil), definition.Allowed...)
		items = append(items, definition)
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, actor Actor, key string) (Setting, error) {
	if err := s.authorize(ctx, actor, key, "read"); err != nil {
		return Setting{}, err
	}
	definition, ok := s.definitions[key]
	if !ok {
		return Setting{}, ErrSettingNotFound
	}
	record, err := s.resolve(ctx, key, definition)
	if err != nil {
		return Setting{}, err
	}
	return s.present(record, definition), nil
}

// History returns redacted versions in storage order so an operator can
// inspect a diff source before choosing an explicit rollback target.
func (s *Service) History(ctx context.Context, actor Actor, key string) ([]Setting, error) {
	if err := s.authorize(ctx, actor, key, "read"); err != nil {
		return nil, err
	}
	definition, ok := s.definitions[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	if s.repo == nil {
		return nil, errors.New("settings repository unavailable")
	}
	records, err := s.repo.History(ctx, key)
	if err != nil {
		return nil, err
	}
	items := make([]Setting, 0, len(records))
	for _, record := range records {
		if record.Source == "" {
			record.Source = SourceDatabase
		}
		items = append(items, s.present(record, definition))
	}
	return items, nil
}

func (s *Service) Update(ctx context.Context, actor Actor, input UpdateInput) (Setting, error) {
	if err := s.authorize(ctx, actor, input.Key, "write"); err != nil {
		return Setting{}, err
	}
	definition, ok := s.definitions[input.Key]
	if !ok {
		return Setting{}, ErrSettingNotFound
	}
	if err := validateValue(definition, input.Value); err != nil {
		return Setting{}, err
	}
	current, err := s.repo.Current(ctx, input.Key)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return Setting{}, err
	}
	if errors.Is(err, ErrSettingNotFound) {
		current.Version = 0
	}
	if current.Version != input.ExpectedVersion {
		return Setting{}, ErrVersionConflict
	}
	payload, encrypted, err := s.preparePayload(ctx, input.Key, definition, input.Value)
	if err != nil {
		return Setting{}, err
	}
	record, err := s.repo.Append(ctx, StoredSetting{Key: input.Key, RawValue: payload, Sensitive: definition.Sensitive, Encrypted: encrypted, Source: SourceDatabase, UpdatedBy: actor.ID, UpdatedAt: time.Now().UTC()})
	if err != nil {
		return Setting{}, err
	}
	if err := s.invalidateAndAudit(ctx, actor, input.Key, record.Version, "update"); err != nil {
		return Setting{}, err
	}
	return s.present(record, definition), nil
}

func (s *Service) Rollback(ctx context.Context, actor Actor, input RollbackInput) (Setting, error) {
	if err := s.authorize(ctx, actor, input.Key, "rollback"); err != nil {
		return Setting{}, err
	}
	definition, ok := s.definitions[input.Key]
	if !ok {
		return Setting{}, ErrSettingNotFound
	}
	current, err := s.repo.Current(ctx, input.Key)
	if err != nil {
		return Setting{}, err
	}
	if current.Version != input.ExpectedVersion {
		return Setting{}, ErrVersionConflict
	}
	history, err := s.repo.History(ctx, input.Key)
	if err != nil {
		return Setting{}, err
	}
	var source *StoredSetting
	for index := range history {
		if history[index].Version == input.Version {
			source = &history[index]
			break
		}
	}
	if source == nil {
		return Setting{}, ErrSettingNotFound
	}
	payload, encrypted, err := s.prepareStoredPayload(ctx, input.Key, definition, *source)
	if err != nil {
		return Setting{}, err
	}
	record, err := s.repo.Append(ctx, StoredSetting{Key: input.Key, RawValue: payload, Sensitive: definition.Sensitive, Encrypted: encrypted, Source: SourceDatabase, UpdatedBy: actor.ID, UpdatedAt: time.Now().UTC()})
	if err != nil {
		return Setting{}, err
	}
	if err := s.invalidateAndAudit(ctx, actor, input.Key, record.Version, "rollback"); err != nil {
		return Setting{}, err
	}
	return s.present(record, definition), nil
}

// TestConnection validates the effective value for a category without
// persisting it. Providers can replace the bounded local validator through
// SetConnectionTester when a real Mailpit/database seam is available.
func (s *Service) TestConnection(ctx context.Context, actor Actor, key, requestID string, candidate json.RawMessage) (ConnectionTestResult, error) {
	if err := s.authorize(ctx, actor, key, "test"); err != nil {
		return ConnectionTestResult{}, err
	}
	definition, ok := s.definitions[key]
	if !ok {
		return ConnectionTestResult{}, ErrSettingNotFound
	}
	value := []byte(candidate)
	if len(value) == 0 {
		record, err := s.resolve(ctx, key, definition)
		if err != nil {
			return ConnectionTestResult{}, err
		}
		value, err = s.readPayload(ctx, key, record)
		if err != nil {
			return ConnectionTestResult{}, err
		}
	}
	if err := validateValue(definition, value); err != nil {
		return ConnectionTestResult{}, err
	}
	if s.connection != nil {
		if err := s.connection.Test(ctx, definition, value); err != nil {
			return ConnectionTestResult{}, err
		}
	} else if err := defaultConnectionValidation(definition, value); err != nil {
		return ConnectionTestResult{}, err
	}
	return ConnectionTestResult{Key: key, Category: definition.Category, Status: "ok", Source: s.effectiveSource(ctx, key), RequestID: requestID, CheckedAt: time.Now().UTC()}, nil
}

func (s *Service) resolve(ctx context.Context, key string, definition Definition) (StoredSetting, error) {
	if s.sources != nil {
		resolved, err := s.sources.Resolve(ctx, key)
		if err != nil {
			return StoredSetting{}, err
		}
		if resolved.Present {
			if err := validateValue(definition, resolved.RawValue); err != nil {
				return StoredSetting{}, err
			}
			return StoredSetting{Key: key, RawValue: append([]byte(nil), resolved.RawValue...), Sensitive: definition.Sensitive, Source: resolved.Source}, nil
		}
	}
	if s.repo != nil {
		record, err := s.repo.Current(ctx, key)
		if err == nil {
			if record.Source == "" {
				record.Source = SourceDatabase
			}
			return record, nil
		}
		if !errors.Is(err, ErrSettingNotFound) {
			return StoredSetting{}, err
		}
	}
	return StoredSetting{Key: key, RawValue: []byte(definition.Default), Sensitive: definition.Sensitive, Source: SourceDefault}, nil
}

func (s *Service) effectiveSource(ctx context.Context, key string) Source {
	definition, ok := s.definitions[key]
	if !ok {
		return SourceDefault
	}
	record, err := s.resolve(ctx, key, definition)
	if err != nil || record.Source == "" {
		return SourceDefault
	}
	return record.Source
}

func (s *Service) preparePayload(ctx context.Context, key string, definition Definition, value []byte) ([]byte, bool, error) {
	if !definition.Sensitive || s.encryptor == nil {
		return append([]byte(nil), value...), false, nil
	}
	encrypted, err := s.encryptor.Encrypt(ctx, key, value)
	if err != nil {
		return nil, false, err
	}
	return encrypted, true, nil
}

func (s *Service) prepareStoredPayload(ctx context.Context, key string, definition Definition, source StoredSetting) ([]byte, bool, error) {
	if !definition.Sensitive || s.encryptor == nil {
		return append([]byte(nil), source.RawValue...), source.Encrypted, nil
	}
	if source.Encrypted {
		return append([]byte(nil), source.RawValue...), true, nil
	}
	return s.preparePayload(ctx, key, definition, source.RawValue)
}

func (s *Service) readPayload(ctx context.Context, key string, record StoredSetting) ([]byte, error) {
	if !record.Encrypted || s.encryptor == nil {
		return append([]byte(nil), record.RawValue...), nil
	}
	return s.encryptor.Decrypt(ctx, key, record.RawValue)
}

func (s *Service) authorize(ctx context.Context, actor Actor, key, action string) error {
	if strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(key) == "" || strings.TrimSpace(action) == "" {
		return ErrPermissionDenied
	}
	if s.authorizer != nil {
		return s.authorizer.Authorize(ctx, actor, key, action)
	}
	return nil
}

func (s *Service) invalidateAndAudit(ctx context.Context, actor Actor, key string, version int64, action string) error {
	if s.cache != nil {
		if err := s.cache.Invalidate(ctx, key); err != nil {
			return err
		}
	}
	if s.audit != nil {
		if err := s.audit.Record(ctx, AuditEvent{ActorID: actor.ID, Action: action, Key: key, Version: version}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) present(record StoredSetting, definition Definition) Setting {
	value := string(record.RawValue)
	if definition.Sensitive {
		value = maskedValue
	}
	source := record.Source
	if source == "" {
		source = SourceDatabase
	}
	return Setting{Key: record.Key, Category: definition.Category, Value: value, Version: record.Version, Sensitive: definition.Sensitive, Source: source, RestartRequired: definition.RestartRequired, UpdatedBy: record.UpdatedBy, UpdatedAt: record.UpdatedAt}
}

func defaultConnectionValidation(definition Definition, raw []byte) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ErrInvalidSetting
	}
	switch definition.Category {
	case CategoryMail:
		if definition.Key == "mail.enabled" {
			return nil
		}
		if definition.Key == "mail.port" {
			port, ok := value.(float64)
			if !ok || port < 1 || port > 65535 {
				return ErrInvalidSetting
			}
		}
	case CategoryFile:
		if definition.Key == "file.max_size" || definition.Key == "file.quota" {
			if number, ok := value.(float64); !ok || number <= 0 {
				return ErrInvalidSetting
			}
		}
	case CategoryI18n:
		if definition.Key == "i18n.mode" || definition.Key == "i18n.default_locale" {
			return validateValue(definition, raw)
		}
	}
	return nil
}

func validateValue(definition Definition, raw json.RawMessage) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return ErrInvalidSetting
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ErrInvalidSetting
	}
	switch definition.Kind {
	case KindString, KindSecret:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%w: %s expects string", ErrInvalidSetting, definition.Key)
		}
	case KindBool:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%w: %s expects boolean", ErrInvalidSetting, definition.Key)
		}
	case KindNumber:
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("%w: %s expects number", ErrInvalidSetting, definition.Key)
		}
	case KindJSON:
	default:
		return ErrInvalidSetting
	}
	if len(definition.Allowed) > 0 {
		valueString, ok := value.(string)
		if !ok {
			return ErrInvalidSetting
		}
		for _, allowed := range definition.Allowed {
			if valueString == allowed {
				return nil
			}
		}
		return fmt.Errorf("%w: %s value is not allowed", ErrInvalidSetting, definition.Key)
	}
	return nil
}

// MemoryRepository is a deterministic adapter for tests and local bootstrap.
type MemoryRepository struct {
	mu     sync.RWMutex
	values map[string][]StoredSetting
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{values: map[string][]StoredSetting{}}
}
func (r *MemoryRepository) Current(_ context.Context, key string) (StoredSetting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := r.values[key]
	if len(values) == 0 {
		return StoredSetting{}, ErrSettingNotFound
	}
	return cloneStored(values[len(values)-1]), nil
}
func (r *MemoryRepository) Append(_ context.Context, value StoredSetting) (StoredSetting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value.Version = int64(len(r.values[value.Key]) + 1)
	r.values[value.Key] = append(r.values[value.Key], cloneStored(value))
	return cloneStored(value), nil
}
func (r *MemoryRepository) History(_ context.Context, key string) ([]StoredSetting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := r.values[key]
	if len(values) == 0 {
		return nil, ErrSettingNotFound
	}
	out := make([]StoredSetting, len(values))
	for i := range values {
		out[i] = cloneStored(values[i])
	}
	return out, nil
}
func cloneStored(value StoredSetting) StoredSetting {
	value.RawValue = append([]byte(nil), value.RawValue...)
	return value
}
