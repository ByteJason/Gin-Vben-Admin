// Package settings provides versioned, auditable runtime settings. It is
// storage/provider agnostic so B6 can run with memory or a database adapter;
// no exporter or external collector is created here.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	platformi18n "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/i18n"
)

const (
	maskedValue        = "[REDACTED]"
	maxRequestIDLength = 128
)

var (
	ErrInvalidSetting   = errors.New("invalid setting")
	ErrVersionConflict  = errors.New("setting version conflict")
	ErrSettingNotFound  = errors.New("setting not found")
	ErrPermissionDenied = errors.New("setting permission denied")
	// ErrSettingLocked indicates that a deployment-owned source (environment,
	// dotenv or YAML) is authoritative.  A UI may display that value, but a
	// background save must never create a database override that would be
	// ignored at runtime.
	ErrSettingLocked = errors.New("setting is locked by deployment configuration")
	// ErrModuleNotFound and ErrModuleRevisionConflict are module-level aliases
	// retained separately from per-field errors so transports can expose a
	// stable conflict code without parsing error strings.
	ErrModuleNotFound         = errors.New("settings module not found")
	ErrModuleRevisionConflict = errors.New("settings module revision conflict")
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
	// CategoryMail is retained as a source-compatible value for older callers.
	// Mail transport configuration is owned by application/mail and is never
	// returned by the active settings schema (see isRetiredSettingKey).
	CategoryMail          Category = "mail"
	CategoryFile          Category = "file"
	CategoryCaptcha       Category = "captcha"
	CategoryI18n          Category = "i18n"
	CategoryObservability Category = "observability"
	CategoryRuntime       Category = "runtime"
	CategoryOther         Category = "other"
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
	Key string `json:"key"`
	// DisplayName is the administrator-facing name. Key remains available for
	// advanced/technical details and API addressing, but is not the primary UI
	// label.
	DisplayName string `json:"displayName,omitempty"`
	// Label and ValueKind/AllowedValues/ScopePolicy are schema aliases used by
	// newer clients.  The original fields remain source-compatible with older
	// integrations; enrichDefinitions keeps both representations in sync.
	Label    string   `json:"label,omitempty"`
	Category Category `json:"category"`
	// Group identifies the atomic save boundary. Definitions in one group are
	// validated and applied together.
	Group         string    `json:"group,omitempty"`
	Kind          ValueKind `json:"kind"`
	ValueKind     ValueKind `json:"valueKind,omitempty"`
	Sensitive     bool      `json:"sensitive"`
	Default       string    `json:"default"`
	Allowed       []string  `json:"allowed,omitempty"`
	AllowedValues []string  `json:"allowedValues,omitempty"`
	Description   string    `json:"description,omitempty"`
	// Editable controls whether the settings center may persist an override.
	// It is intentionally distinct from source precedence: deployment-owned
	// values remain visible but are read-only.
	Editable bool `json:"editable"`
	// SourcePolicy documents the authorities accepted for this setting. The
	// resolver still enforces the global precedence order.
	SourcePolicy []Source  `json:"sourcePolicy,omitempty"`
	Scope        Scope     `json:"scope,omitempty"`
	ScopePolicy  []Scope   `json:"scopePolicy,omitempty"`
	ApplyMode    ApplyMode `json:"applyMode,omitempty"`
	Unit         string    `json:"unit,omitempty"`
	InputHint    string    `json:"inputHint,omitempty"`
	Placeholder  string    `json:"placeholder,omitempty"`
	// Deprecated marks compatibility definitions which are not exposed by the
	// active Definitions endpoint. It is used for old persisted rows only.
	Deprecated bool `json:"deprecated,omitempty"`
	// RestartRequired is kept as a wire-compatible alias for older clients. New
	// callers should inspect ApplyMode.
	RestartRequired bool   `json:"restartRequired,omitempty"`
	EnvKey          string `json:"envKey,omitempty"`
	YAMLPath        string `json:"yamlPath,omitempty"`
}

func DefaultDefinitions() map[string]Definition {
	definitions := map[string]Definition{
		"site.name":                         {Key: "site.name", Category: CategoryBasic, Kind: KindString, Default: `"Gin-Vben-Admin"`, EnvKey: "SITE_NAME", YAMLPath: "site.name"},
		"basic.site_name":                   {Key: "basic.site_name", Category: CategoryBasic, Kind: KindString, Default: `"Gin-Vben-Admin"`, Description: "管理端显示名称", EnvKey: "SITE_NAME", YAMLPath: "site.name"},
		"branding":                          {Key: "branding", Category: CategoryBasic, Kind: KindJSON, Default: `{}`, Description: "品牌媒体资源引用（例如 logoResourceId）"},
		"security.jwt_secret":               {Key: "security.jwt_secret", Category: CategorySecurity, Kind: KindSecret, Sensitive: true, Default: `""`, Description: "签发访问令牌的运行时密钥", RestartRequired: true, EnvKey: "AUTH_JWT_SECRET", YAMLPath: "auth.jwt_secret"},
		"security.access_ttl":               {Key: "security.access_ttl", Category: CategorySecurity, Kind: KindString, Default: `"30m"`, RestartRequired: true, EnvKey: "AUTH_ACCESS_TTL", YAMLPath: "auth.access_ttl"},
		"security.refresh_ttl":              {Key: "security.refresh_ttl", Category: CategorySecurity, Kind: KindString, Default: `"168h"`, RestartRequired: true, EnvKey: "AUTH_REFRESH_TTL", YAMLPath: "auth.refresh_ttl"},
		"security.secure_cookie":            {Key: "security.secure_cookie", Category: CategorySecurity, Kind: KindBool, Default: `false`, RestartRequired: true, EnvKey: "AUTH_SECURE_COOKIE", YAMLPath: "auth.secure_cookie"},
		"file.provider":                     {Key: "file.provider", Category: CategoryFile, Kind: KindString, Default: `"local"`, Allowed: []string{"local", "s3", "oss", "cos"}, Description: "对象存储 provider；local 为默认本地存储", RestartRequired: true, EnvKey: "FILE_PROVIDER", YAMLPath: "file.provider"},
		"file.max_size":                     {Key: "file.max_size", Category: CategoryFile, Kind: KindNumber, Default: `104857600`, Description: "单文件字节上限", EnvKey: "FILE_MAX_SIZE", YAMLPath: "file.max_size"},
		"file.quota":                        {Key: "file.quota", Category: CategoryFile, Kind: KindNumber, Default: `1073741824`, EnvKey: "FILE_QUOTA", YAMLPath: "file.quota"},
		"file.allowed_mimes":                {Key: "file.allowed_mimes", Category: CategoryFile, Kind: KindJSON, Default: `[]`, EnvKey: "FILE_ALLOWED_MIMES", YAMLPath: "file.allowed_mimes"},
		"file.s3.endpoint":                  {Key: "file.s3.endpoint", Category: CategoryFile, Kind: KindString, Default: `""`, Description: "S3 兼容 endpoint", RestartRequired: true, EnvKey: "FILE_S3_ENDPOINT", YAMLPath: "file.s3.endpoint"},
		"file.s3.bucket":                    {Key: "file.s3.bucket", Category: CategoryFile, Kind: KindString, Default: `""`, Description: "S3 bucket", RestartRequired: true, EnvKey: "FILE_S3_BUCKET", YAMLPath: "file.s3.bucket"},
		"file.s3.region":                    {Key: "file.s3.region", Category: CategoryFile, Kind: KindString, Default: `""`, Description: "S3 region", RestartRequired: true, EnvKey: "FILE_S3_REGION", YAMLPath: "file.s3.region"},
		"file.s3.access_key":                {Key: "file.s3.access_key", Category: CategoryFile, Kind: KindString, Default: `""`, Description: "S3 access key", RestartRequired: true, EnvKey: "FILE_S3_ACCESS_KEY", YAMLPath: "file.s3.access_key"},
		"file.s3.secret_key":                {Key: "file.s3.secret_key", Category: CategoryFile, Kind: KindSecret, Sensitive: true, Default: `""`, Description: "S3 secret key", RestartRequired: true, EnvKey: "FILE_S3_SECRET_KEY", YAMLPath: "file.s3.secret_key"},
		"file.s3.path_style":                {Key: "file.s3.path_style", Category: CategoryFile, Kind: KindBool, Default: `false`, Description: "使用 path-style S3 URL", RestartRequired: true, EnvKey: "FILE_S3_PATH_STYLE", YAMLPath: "file.s3.path_style"},
		"captcha.enabled":                   {Key: "captcha.enabled", Category: CategoryCaptcha, Kind: KindBool, Default: `false`, Description: "是否启用图片验证码", RestartRequired: true, EnvKey: "AUTH_CAPTCHA_ENABLED", YAMLPath: "auth.captcha_enabled"},
		"captcha.risk_threshold":            {Key: "captcha.risk_threshold", Category: CategoryCaptcha, Kind: KindNumber, Default: `3`, RestartRequired: true, EnvKey: "AUTH_CAPTCHA_RISK_THRESHOLD", YAMLPath: "auth.captcha_risk_threshold"},
		"captcha.challenge_ttl":             {Key: "captcha.challenge_ttl", Category: CategoryCaptcha, Kind: KindString, Default: `"2m"`, RestartRequired: true, EnvKey: "AUTH_CAPTCHA_CHALLENGE_TTL", YAMLPath: "auth.captcha_challenge_ttl"},
		"captcha.key_prefix":                {Key: "captcha.key_prefix", Category: CategoryCaptcha, Kind: KindString, Default: `"auth-captcha"`, RestartRequired: true, EnvKey: "AUTH_CAPTCHA_KEY_PREFIX", YAMLPath: "auth.captcha_key_prefix"},
		"i18n.mode":                         {Key: "i18n.mode", Category: CategoryI18n, Kind: KindString, Default: `"single"`, Allowed: []string{"single", "multi"}, Description: "single 隐藏语言切换，multi 显示语言切换", RestartRequired: true, EnvKey: "I18N_MODE", YAMLPath: "i18n.mode"},
		"i18n.default_locale":               {Key: "i18n.default_locale", Category: CategoryI18n, Kind: KindString, Default: `"zh-CN"`, Allowed: []string{"zh-CN", "en-US"}, RestartRequired: true, EnvKey: "I18N_DEFAULT_LOCALE", YAMLPath: "i18n.default_locale"},
		"i18n.supported_locales":            {Key: "i18n.supported_locales", Category: CategoryI18n, Kind: KindJSON, Default: `["zh-CN","en-US"]`, RestartRequired: true, EnvKey: "I18N_SUPPORTED_LOCALES", YAMLPath: "i18n.supported_locales"},
		"locale.default":                    {Key: "locale.default", Category: CategoryI18n, Kind: KindString, Default: `"zh-CN"`, Allowed: []string{"zh-CN", "en-US"}, RestartRequired: true},
		"observability.metrics.enabled":     {Key: "observability.metrics.enabled", Category: CategoryObservability, Kind: KindBool, Default: `false`},
		"observability.metrics.endpoint":    {Key: "observability.metrics.endpoint", Category: CategoryObservability, Kind: KindString, Default: `""`},
		"observability.tracing.enabled":     {Key: "observability.tracing.enabled", Category: CategoryObservability, Kind: KindBool, Default: `false`},
		"observability.tracing.endpoint":    {Key: "observability.tracing.endpoint", Category: CategoryObservability, Kind: KindString, Default: `""`},
		"observability.tracing.protocol":    {Key: "observability.tracing.protocol", Category: CategoryObservability, Kind: KindString, Default: `"http/protobuf"`, Allowed: []string{"grpc", "http/protobuf"}},
		"observability.tracing.tls_verify":  {Key: "observability.tracing.tls_verify", Category: CategoryObservability, Kind: KindBool, Default: `true`},
		"observability.tracing.sample_rate": {Key: "observability.tracing.sample_rate", Category: CategoryObservability, Kind: KindNumber, Default: `0`},
		"observability.otlp.api_key":        {Key: "observability.otlp.api_key", Category: CategoryObservability, Kind: KindSecret, Sensitive: true, Default: `""`},
		// Runtime is a read-only diagnostic module. Values are supplied by the
		// composition root through RuntimeModuleProvider when live probes are
		// available; these safe defaults keep the endpoint useful during startup
		// and in dependency-free fixtures. Runtime keys are never persisted.
		"runtime.version":         {Key: "runtime.version", Category: CategoryRuntime, Kind: KindString, Default: `"unknown"`, Description: "当前服务版本", ApplyMode: ApplyDeployment, Scope: ScopeSystem, SourcePolicy: []Source{SourceEnv, SourceDotEnv, SourceYAML, SourceDefault}},
		"runtime.database.status": {Key: "runtime.database.status", Category: CategoryRuntime, Kind: KindString, Default: `"unknown"`, Allowed: []string{"ok", "degraded", "unavailable", "not_configured", "unknown"}, Description: "数据库连接状态", ApplyMode: ApplyDeployment, Scope: ScopeSystem, SourcePolicy: []Source{SourceEnv, SourceDotEnv, SourceYAML, SourceDefault}},
		"runtime.database.source": {Key: "runtime.database.source", Category: CategoryRuntime, Kind: KindString, Default: `"default"`, Allowed: []string{"env", "dotenv", "yaml", "default", "database"}, Description: "数据库连接配置来源", ApplyMode: ApplyDeployment, Scope: ScopeSystem, SourcePolicy: []Source{SourceEnv, SourceDotEnv, SourceYAML, SourceDefault}},
		"runtime.redis.status":    {Key: "runtime.redis.status", Category: CategoryRuntime, Kind: KindString, Default: `"unknown"`, Allowed: []string{"ok", "degraded", "unavailable", "not_configured", "unknown"}, Description: "Redis 连接状态", ApplyMode: ApplyDeployment, Scope: ScopeSystem, SourcePolicy: []Source{SourceEnv, SourceDotEnv, SourceYAML, SourceDefault}},
		"runtime.redis.source":    {Key: "runtime.redis.source", Category: CategoryRuntime, Kind: KindString, Default: `"default"`, Allowed: []string{"env", "dotenv", "yaml", "default", "database"}, Description: "Redis 连接配置来源", ApplyMode: ApplyDeployment, Scope: ScopeSystem, SourcePolicy: []Source{SourceEnv, SourceDotEnv, SourceYAML, SourceDefault}},
		"runtime.http.address":    {Key: "runtime.http.address", Category: CategoryRuntime, Kind: KindString, Default: `""`, Description: "HTTP 监听地址", ApplyMode: ApplyDeployment, Scope: ScopeSystem, SourcePolicy: []Source{SourceEnv, SourceDotEnv, SourceYAML, SourceDefault}},
		"runtime.node":            {Key: "runtime.node", Category: CategoryRuntime, Kind: KindString, Default: `"unknown"`, Description: "当前运行节点", ApplyMode: ApplyDeployment, Scope: ScopeSystem, SourcePolicy: []Source{SourceEnv, SourceDotEnv, SourceYAML, SourceDefault}},
		"runtime.config.revision": {Key: "runtime.config.revision", Category: CategoryRuntime, Kind: KindNumber, Default: `0`, Description: "当前动态配置修订号", ApplyMode: ApplyDeployment, Scope: ScopeSystem, SourcePolicy: []Source{SourceDatabase, SourceDefault}},
		"runtime.pending_restart": {Key: "runtime.pending_restart", Category: CategoryRuntime, Kind: KindBool, Default: `false`, Description: "是否有配置等待重启", ApplyMode: ApplyDeployment, Scope: ScopeSystem, SourcePolicy: []Source{SourceDatabase, SourceDefault}},
	}
	return enrichDefinitions(definitions)
}

// legacyMailDefinitions is isolated from DefaultDefinitions so the active
// schema and compiled defaults contain no mail transport settings. NewService
// attaches this compatibility-only map for old embedded callers that still
// invoke the retired key methods; every public Definitions/Modules path filters
// these entries and the production router does not mount their write actions.
func legacyMailDefinitions() map[string]Definition {
	return enrichDefinitions(map[string]Definition{
		"mail.enabled":   {Key: "mail.enabled", Category: CategoryMail, Kind: KindBool, Default: `false`, RestartRequired: true, EnvKey: "MAIL_ENABLED", YAMLPath: "mail.enabled", Deprecated: true},
		"mail.host":      {Key: "mail.host", Category: CategoryMail, Kind: KindString, Default: `""`, Description: "SMTP 主机", RestartRequired: true, EnvKey: "MAIL_HOST", YAMLPath: "mail.host", Deprecated: true},
		"mail.port":      {Key: "mail.port", Category: CategoryMail, Kind: KindNumber, Default: `1025`, RestartRequired: true, EnvKey: "MAIL_PORT", YAMLPath: "mail.port", Deprecated: true},
		"mail.username":  {Key: "mail.username", Category: CategoryMail, Kind: KindString, Default: `""`, RestartRequired: true, EnvKey: "MAIL_USERNAME", YAMLPath: "mail.username", Deprecated: true},
		"mail.password":  {Key: "mail.password", Category: CategoryMail, Kind: KindSecret, Sensitive: true, Default: `""`, RestartRequired: true, EnvKey: "MAIL_PASSWORD", YAMLPath: "mail.password", Deprecated: true},
		"mail.from":      {Key: "mail.from", Category: CategoryMail, Kind: KindString, Default: `""`, RestartRequired: true, EnvKey: "MAIL_FROM", YAMLPath: "mail.from", Deprecated: true},
		"mail.start_tls": {Key: "mail.start_tls", Category: CategoryMail, Kind: KindBool, Default: `false`, RestartRequired: true, EnvKey: "MAIL_START_TLS", YAMLPath: "mail.start_tls", Deprecated: true},
	})
}

// enrichDefinitions fills the policy metadata introduced by the module based
// settings contract. Keeping this in one place lets old persisted definitions
// (and callers that provide a hand-written map) continue to work while every
// active definition has a complete display/source/apply description.
func enrichDefinitions(definitions map[string]Definition) map[string]Definition {
	for key, definition := range definitions {
		// Capture whether this was the pre-module definition shape before
		// defaults below populate canonical policy fields.
		legacyShape := definition.DisplayName == "" && definition.Label == "" && definition.ApplyMode == "" && definition.Scope == "" && len(definition.ScopePolicy) == 0 && len(definition.SourcePolicy) == 0 && !definition.Deprecated
		if definition.Key == "" {
			definition.Key = key
		}
		if definition.Group == "" {
			if strings.HasPrefix(definition.Key, "observability.") {
				definition.Group = "observability"
			} else {
				definition.Group = definition.Category.String()
			}
		}
		if definition.DisplayName == "" {
			definition.DisplayName = displayNameForKey(key)
		}
		if definition.Label == "" {
			definition.Label = definition.DisplayName
		}
		if definition.DisplayName == "" {
			definition.DisplayName = definition.Label
		}
		if definition.Kind == "" {
			definition.Kind = definition.ValueKind
		}
		if definition.ValueKind == "" {
			definition.ValueKind = definition.Kind
		}
		if len(definition.Allowed) == 0 && len(definition.AllowedValues) > 0 {
			definition.Allowed = append([]string(nil), definition.AllowedValues...)
		}
		if len(definition.AllowedValues) == 0 && len(definition.Allowed) > 0 {
			definition.AllowedValues = append([]string(nil), definition.Allowed...)
		}
		if definition.Scope == "" && len(definition.ScopePolicy) > 0 {
			definition.Scope = definition.ScopePolicy[0]
		}
		if definition.Scope == "" {
			definition.Scope = ScopeTenant
		}
		if len(definition.ScopePolicy) == 0 {
			definition.ScopePolicy = []Scope{definition.Scope}
		}
		if definition.ApplyMode == "" {
			definition.ApplyMode = applyModeForDefinition(definition)
		}
		// Keep the legacy boolean as a derived wire alias rather than a second
		// source of truth.
		definition.RestartRequired = definition.ApplyMode == ApplyRestart || definition.ApplyMode == ApplyDeployment || definition.ApplyMode == ApplyMigration
		if !definition.Editable {
			// Definitions created before the module contract omitted Editable and
			// all policy metadata. Infer editability only for that legacy shape;
			// once a caller supplies canonical metadata, an explicit false remains
			// read-only even when its apply mode is immediate.
			if legacyShape {
				definition.Editable = !isInfrastructureSetting(key) && definition.ApplyMode != ApplyDeployment
			}
		}
		if len(definition.SourcePolicy) == 0 {
			definition.SourcePolicy = []Source{SourceEnv, SourceDotEnv, SourceYAML, SourceDatabase, SourceDefault}
		}
		if isRetiredSettingKey(key) {
			definition.Deprecated = true
			definition.Editable = false
		}
		definitions[key] = definition
	}
	return definitions
}

func displayNameForKey(key string) string {
	if name, ok := map[string]string{
		"site.name": "站点名称", "basic.site_name": "站点名称", "branding": "品牌标识",
		"security.jwt_secret": "访问令牌密钥", "security.access_ttl": "访问令牌有效期",
		"security.refresh_ttl": "刷新令牌有效期", "security.secure_cookie": "安全 Cookie",
		"file.provider": "存储驱动", "file.max_size": "单文件大小上限", "file.quota": "存储配额",
		"file.allowed_mimes": "允许的文件类型", "file.s3.endpoint": "对象存储地址",
		"file.s3.bucket": "对象存储存储桶", "file.s3.region": "对象存储区域",
		"file.s3.access_key": "对象存储访问密钥", "file.s3.secret_key": "对象存储密钥",
		"file.s3.path_style": "对象存储路径样式", "captcha.enabled": "启用验证码",
		"captcha.risk_threshold": "验证码风险阈值", "captcha.challenge_ttl": "验证码有效期",
		"captcha.key_prefix": "验证码键前缀", "i18n.mode": "语言模式",
		"i18n.default_locale": "默认语言", "i18n.supported_locales": "支持的语言",
		"locale.default": "默认语言", "observability.metrics.enabled": "启用指标",
		"observability.metrics.endpoint": "指标地址", "observability.tracing.enabled": "启用链路追踪",
		"observability.tracing.endpoint": "链路追踪地址", "observability.tracing.protocol": "链路追踪协议",
		"observability.tracing.tls_verify": "验证追踪 TLS", "observability.tracing.sample_rate": "追踪采样率",
		"observability.otlp.api_key": "OTLP 访问密钥",
		"runtime.version":            "服务版本", "runtime.database.status": "数据库状态",
		"runtime.database.source": "数据库配置来源", "runtime.redis.status": "Redis 状态",
		"runtime.redis.source": "Redis 配置来源", "runtime.http.address": "HTTP 监听地址",
		"runtime.node": "运行节点", "runtime.config.revision": "配置修订号",
		"runtime.pending_restart": "待重启状态",
	}[key]; ok {
		return name
	}
	return key
}

func applyModeForDefinition(definition Definition) ApplyMode {
	// Explicit policy replaces the old blanket RestartRequired flag. Only
	// startup dependencies remain restart/deployment scoped; ordinary security,
	// language, quota and sampling controls are safe to apply immediately.
	switch definition.Key {
	case "security.jwt_secret":
		return ApplyDeployment
	case "file.provider":
		return ApplyMigration
	case "file.s3.endpoint", "file.s3.bucket", "file.s3.region", "file.s3.access_key", "file.s3.secret_key", "file.s3.path_style":
		return ApplyComponentReload
	case "observability.metrics.enabled", "observability.metrics.endpoint", "observability.tracing.enabled", "observability.tracing.endpoint", "observability.tracing.protocol", "observability.tracing.tls_verify", "observability.tracing.sample_rate", "observability.otlp.api_key":
		return ApplyComponentReload
	}
	return ApplyImmediate
}

type Actor struct {
	ID string
}

type UpdateInput struct {
	Key             string
	Value           json.RawMessage
	ExpectedVersion int64
	RequestID       string `json:"requestId,omitempty"`
}

type RollbackInput struct {
	Key             string
	Version         int64
	ExpectedVersion int64
	RequestID       string `json:"requestId,omitempty"`
}

type Setting struct {
	Key             string    `json:"key"`
	DisplayName     string    `json:"displayName,omitempty"`
	Category        Category  `json:"category"`
	Group           string    `json:"group,omitempty"`
	Value           string    `json:"value"`
	Version         int64     `json:"version"`
	Sensitive       bool      `json:"sensitive"`
	Source          Source    `json:"source"`
	Editable        bool      `json:"editable"`
	Scope           Scope     `json:"scope,omitempty"`
	ApplyMode       ApplyMode `json:"applyMode,omitempty"`
	RestartRequired bool      `json:"restartRequired,omitempty"`
	UpdatedBy       string    `json:"updatedBy,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt,omitempty"`
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
	// TenantID identifies the persistence scope used by the in-memory adapter.
	// Database adapters derive this value from the request context and do not
	// need to expose it on the API model. Keeping it here prevents two tenants
	// that happen to use the same organization ID from sharing a fixture row.
	TenantID string
	// Organization identifies the persisted override scope. An empty value is
	// the tenant-wide fallback. It is intentionally internal metadata (no JSON
	// tag) so API payloads continue to expose the policy from Definition rather
	// than storage identifiers.
	Organization string
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

// ProcessEnvironmentResolver exposes explicitly supplied process environment
// values as a high-precedence, read-only source. It is intentionally separate
// from the config loader so the settings service can reject a database
// override that the running process would ignore. Root dotenv/YAML adapters
// can be layered through MapSourceResolver when their source metadata is
// available.
type ProcessEnvironmentResolver struct {
	definitions map[string]Definition
}

func NewProcessEnvironmentResolver(definitions map[string]Definition) *ProcessEnvironmentResolver {
	copyDefinitions := make(map[string]Definition, len(definitions))
	for key, definition := range definitions {
		copyDefinitions[key] = definition
	}
	return &ProcessEnvironmentResolver{definitions: copyDefinitions}
}

func (r *ProcessEnvironmentResolver) Resolve(_ context.Context, key string) (ResolvedValue, error) {
	if r == nil {
		return ResolvedValue{}, nil
	}
	definition, ok := r.definitions[key]
	if !ok {
		return ResolvedValue{}, nil
	}
	environment := environmentKeyForDefinition(key, definition)
	if environment == "" {
		return ResolvedValue{}, nil
	}
	value, present := os.LookupEnv(environment)
	if !present {
		return ResolvedValue{}, nil
	}
	raw, err := encodeEnvironmentValue(definition, value)
	if err != nil {
		return ResolvedValue{}, err
	}
	return ResolvedValue{RawValue: raw, Source: SourceEnv, Present: true}, nil
}

func environmentKeyForDefinition(key string, definition Definition) string {
	if strings.TrimSpace(definition.EnvKey) != "" {
		return strings.TrimSpace(definition.EnvKey)
	}
	// Config uses a few historical aliases for observability fields; keep the
	// mapping explicit so endpoint/protocol settings resolve the same value as
	// the bootstrap config rather than guessing from the dotted key.
	if source, ok := map[string]string{
		"observability.metrics.enabled":     "OBSERVABILITY_METRICS_ENABLED",
		"observability.metrics.endpoint":    "OBSERVABILITY_METRICS_ENDPOINT",
		"observability.tracing.enabled":     "OBSERVABILITY_TRACING_ENABLED",
		"observability.tracing.endpoint":    "OBSERVABILITY_OTLP_ENDPOINT",
		"observability.tracing.protocol":    "OBSERVABILITY_OTLP_PROTOCOL",
		"observability.tracing.tls_verify":  "OBSERVABILITY_TLS_VERIFY",
		"observability.tracing.sample_rate": "OBSERVABILITY_SAMPLE_RATE",
		"observability.otlp.api_key":        "OBSERVABILITY_OTLP_API_KEY",
	}[key]; ok {
		return source
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	return strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(key))
}

func encodeEnvironmentValue(definition Definition, value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	switch definition.Kind {
	case KindString, KindSecret:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, ErrInvalidSetting
		}
		return encoded, nil
	case KindBool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("%w: %s expects boolean", ErrInvalidSetting, definition.Key)
		}
		return []byte(strconv.FormatBool(parsed)), nil
	case KindNumber:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return nil, fmt.Errorf("%w: %s expects number", ErrInvalidSetting, definition.Key)
		}
		return []byte(value), nil
	case KindJSON:
		if json.Valid([]byte(value)) {
			return []byte(value), nil
		}
		// List-valued environment bindings use comma-separated notation in the
		// config loader. Convert that notation only for JSON array definitions.
		parts := strings.Split(value, ",")
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			if part = strings.TrimSpace(part); part != "" {
				items = append(items, part)
			}
		}
		encoded, err := json.Marshal(items)
		if err != nil {
			return nil, ErrInvalidSetting
		}
		return encoded, nil
	default:
		return nil, ErrInvalidSetting
	}
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
	ActorID     string
	Action      string
	Key         string
	Module      string
	Keys        []string
	Version     int64
	Revision    int64
	SaveResult  string
	ApplyResult string
	RequestID   string
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
	repo            Repository
	audit           AuditSink
	cache           CacheInvalidator
	runtime         *RuntimeSnapshotStore
	authorizer      Authorizer
	definitions     map[string]Definition
	sources         SourceResolver
	encryptor       Encryptor
	connection      ConnectionTester
	runtimeProvider RuntimeModuleProvider
	// moduleMu serializes legacy repositories that do not yet expose a native
	// module transaction. Native AtomicModuleRepository adapters still perform
	// their own compare-and-swap inside the database transaction.
	moduleMu       sync.Mutex
	moduleStateMu  sync.RWMutex
	moduleStates   map[string]ModuleStatus
	moduleOutcomes map[string]moduleOutcome
	applier        RuntimeApplier
}

func NewService(repo Repository, audit AuditSink, cache CacheInvalidator, definitions map[string]Definition) *Service {
	if definitions == nil {
		definitions = DefaultDefinitions()
	} else {
		// Clone caller-owned metadata so a UI/schema mutation cannot race a
		// running request or alter the service's policy table.
		copyDefinitions := make(map[string]Definition, len(definitions))
		for key, definition := range definitions {
			definition.Allowed = append([]string(nil), definition.Allowed...)
			definition.AllowedValues = append([]string(nil), definition.AllowedValues...)
			definition.SourcePolicy = append([]Source(nil), definition.SourcePolicy...)
			definition.ScopePolicy = append([]Scope(nil), definition.ScopePolicy...)
			copyDefinitions[key] = definition
		}
		definitions = enrichDefinitions(copyDefinitions)
	}
	// Keep retired key-method consumers operational during a rolling upgrade
	// without putting mail transport definitions back into the active schema or
	// the public default-definition map. The independent mail module remains the
	// sole owner of new mail configuration writes.
	for key, definition := range legacyMailDefinitions() {
		if _, exists := definitions[key]; !exists {
			definitions[key] = definition
		}
	}
	return &Service{repo: repo, audit: audit, cache: cache, definitions: definitions, moduleStates: map[string]ModuleStatus{}, moduleOutcomes: map[string]moduleOutcome{}}
}

func (s *Service) SetAuthorizer(authorizer Authorizer) { s.authorizer = authorizer }

func (s *Service) SetSourceResolver(resolver SourceResolver) { s.sources = resolver }

func (s *Service) SetEncryptor(encryptor Encryptor) { s.encryptor = encryptor }

func (s *Service) SetConnectionTester(tester ConnectionTester) { s.connection = tester }

// SetRuntimeApplier installs the component rebuild seam used by module saves.
// The applier is called only after persistence commits; an error leaves the
// previous runtime snapshot untouched and is surfaced as saved_apply_failed.
func (s *Service) SetRuntimeApplier(applier RuntimeApplier) {
	if s != nil {
		s.applier = applier
	}
}

// SetRuntimeSnapshotStore attaches the hot-reload publication seam used by
// SMTP callers, templates, verification policies and mutable media settings.
// Startup-only topology (database, Redis and object-store roots) remains
// outside this store.
func (s *Service) SetRuntimeSnapshotStore(store *RuntimeSnapshotStore) {
	if s != nil {
		s.runtime = store
	}
}

// SetRuntimeModuleProvider attaches the read-only runtime environment
// collector. Providers are called only by GetModule("runtime") and are never
// consulted for mutable settings or save/reset operations.
func (s *Service) SetRuntimeModuleProvider(provider RuntimeModuleProvider) {
	if s != nil {
		s.runtimeProvider = provider
	}
}

// RuntimeSnapshot returns the process-local immutable view. A nil service
// yields an empty snapshot so read paths can remain branch-free.
func (s *Service) RuntimeSnapshot() RuntimeSnapshot {
	if s == nil || s.runtime == nil {
		return RuntimeSnapshot{Values: map[string]json.RawMessage{}}
	}
	return s.runtime.Snapshot()
}

// RuntimeSnapshotForContext returns the immutable snapshot for the exact
// tenant/organization scope carried by ctx. It is the safe read API for
// request-scoped business services; unlike RuntimeSnapshot, it never falls
// back to the legacy process-global slot.
func (s *Service) RuntimeSnapshotForContext(ctx context.Context) RuntimeSnapshot {
	if s == nil || s.runtime == nil {
		return RuntimeSnapshot{Values: map[string]json.RawMessage{}, Sources: map[string]Source{}}
	}
	return s.runtime.SnapshotFor(runtimeSnapshotScope(ctx))
}

// RuntimeValue is the fast path for business services that consume mutable
// settings. It never performs I/O; callers can fall back to their compiled
// default when the key has not been loaded yet.
func (s *Service) RuntimeValue(key string) (json.RawMessage, bool) {
	if s == nil || s.runtime == nil {
		return nil, false
	}
	return s.runtime.Value(key)
}

// RuntimeValueForContext reads a value from the exact tenant/organization
// snapshot without performing I/O. A missing scoped snapshot is a cache miss,
// not permission to use another tenant's value.
func (s *Service) RuntimeValueForContext(ctx context.Context, key string) (json.RawMessage, bool) {
	if s == nil || s.runtime == nil {
		return nil, false
	}
	return s.runtime.ValueFor(runtimeSnapshotScope(ctx), key)
}

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
		if isRetiredSettingKey(key) || definition.Deprecated {
			continue
		}
		if isInfrastructureSetting(key) {
			continue
		}
		if definition.Sensitive {
			definition.Default = maskedValue
		}
		definition.Allowed = append([]string(nil), definition.Allowed...)
		definition.AllowedValues = append([]string(nil), definition.AllowedValues...)
		definition.SourcePolicy = append([]Source(nil), definition.SourcePolicy...)
		definition.ScopePolicy = append([]Scope(nil), definition.ScopePolicy...)
		items = append(items, definition)
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, actor Actor, key string) (Setting, error) {
	if isInfrastructureSetting(key) {
		return Setting{}, ErrSettingNotFound
	}
	if err := s.authorize(ctx, actor, key, "read"); err != nil {
		return Setting{}, err
	}
	definition, ok := s.definitions[key]
	if !ok {
		return Setting{}, ErrSettingNotFound
	}
	// Once a process snapshot has been published, non-sensitive reads stay
	// entirely in-process. This is the hot path used by business requests; the
	// repository is consulted only for metadata/source when a key has not been
	// loaded yet (or for a sensitive value that is intentionally absent from the
	// snapshot).
	if s.runtime != nil && !definition.Sensitive {
		if raw, present := s.runtime.ValueFor(runtimeSnapshotScope(ctx), key); present {
			source := s.runtime.Source(key)
			if scope := runtimeSnapshotScope(ctx); scope != "" {
				source = s.runtime.SourceFor(scope, key)
			}
			// A snapshot may have been created by an older process before the
			// definition's source policy was tightened. Do not expose that stale
			// value as effective; resolve it again from the allowed authorities.
			if source != "" && !sourceAllowed(definition, source) {
				// Fall through to the resolver/repository path below.
				source = ""
			} else {
				if source == "" {
					// Snapshots written by older callers did not carry source
					// metadata. Resolve the authoritative value instead of attaching
					// a guessed source to potentially stale bytes.
					resolved, resolveErr := s.resolve(ctx, key, definition)
					if resolveErr != nil {
						return Setting{}, resolveErr
					}
					raw = resolved.RawValue
					source = resolved.Source
				}
				if source == "" {
					source = SourceDefault
				}
				return s.present(StoredSetting{Key: key, RawValue: raw, Source: source}, definition), nil
			}
		}
	}
	record, err := s.resolve(ctx, key, definition)
	if err != nil {
		return Setting{}, err
	}
	// Hydrate only the exact scope that was requested. This keeps the common
	// request path local after the first read while preventing a tenant miss
	// from being satisfied by the legacy global snapshot.
	if s.runtime != nil && !definition.Sensitive {
		if raw, payloadErr := s.readPayload(ctx, key, record); payloadErr == nil {
			_, _ = s.runtime.UpdateWithSourceFor(runtimeSnapshotScope(ctx), ctx, key, raw, record.Source)
		}
	}
	return s.present(record, definition), nil
}

// History returns redacted versions in storage order so an operator can
// inspect a diff source before choosing an explicit rollback target.
func (s *Service) History(ctx context.Context, actor Actor, key string) ([]Setting, error) {
	if isInfrastructureSetting(key) {
		return nil, ErrSettingNotFound
	}
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
	if isInfrastructureSetting(input.Key) {
		return Setting{}, ErrPermissionDenied
	}
	if s == nil || s.repo == nil {
		return Setting{}, errors.New("settings repository unavailable")
	}
	if input.ExpectedVersion < 0 {
		return Setting{}, fmt.Errorf("%w: expectedVersion must be non-negative", ErrInvalidSetting)
	}
	if len(input.RequestID) > maxRequestIDLength {
		return Setting{}, fmt.Errorf("%w: requestId is too long", ErrInvalidSetting)
	}
	if err := s.authorize(ctx, actor, input.Key, "write"); err != nil {
		return Setting{}, err
	}
	definition, ok := s.definitions[input.Key]
	if !ok {
		return Setting{}, ErrSettingNotFound
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
	if sensitiveInputIsNoop(definition, input.Value) {
		// Blank/masked credentials intentionally do not become database rows and
		// do not advance the revision. Resolve again so a deployment-owned value
		// is reflected accurately instead of returning a stale database override.
		effective, resolveErr := s.resolve(ctx, input.Key, definition)
		if resolveErr != nil {
			return Setting{}, resolveErr
		}
		return s.present(effective, definition), nil
	}
	// The compatibility key endpoint still routes through this method, so it
	// must not create a database override that the configured source policy
	// excludes or that a higher-precedence deployment source will hide.
	if err := s.ensureDatabaseWritable(ctx, input.Key, definition); err != nil {
		return Setting{}, err
	}
	if err := validateValue(definition, input.Value); err != nil {
		return Setting{}, err
	}
	payload, encrypted, err := s.preparePayload(ctx, input.Key, definition, input.Value)
	if err != nil {
		return Setting{}, err
	}
	record, err := s.repo.Append(ctx, StoredSetting{Key: input.Key, RawValue: payload, Sensitive: definition.Sensitive, Encrypted: encrypted, Source: SourceDatabase, UpdatedBy: actor.ID, UpdatedAt: time.Now().UTC()})
	if err != nil {
		return Setting{}, err
	}
	if err := s.invalidateAndAuditWithEvent(ctx, actor, AuditEvent{ActorID: actor.ID, Action: "update", Key: input.Key, Module: moduleForDefinition(definition), Keys: []string{input.Key}, Version: record.Version, Revision: record.Version, SaveResult: "saved", ApplyResult: string(StatusSavedAndApplied), RequestID: input.RequestID}); err != nil {
		return Setting{}, err
	}
	if err := s.publishRuntime(ctx, input.Key, definition, input.Value); err != nil {
		return Setting{}, err
	}
	return s.present(record, definition), nil
}

func (s *Service) Rollback(ctx context.Context, actor Actor, input RollbackInput) (Setting, error) {
	if isInfrastructureSetting(input.Key) {
		return Setting{}, ErrPermissionDenied
	}
	if err := s.authorize(ctx, actor, input.Key, "rollback"); err != nil {
		return Setting{}, err
	}
	if len(input.RequestID) > maxRequestIDLength {
		return Setting{}, fmt.Errorf("%w: requestId is too long", ErrInvalidSetting)
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
	if err := s.invalidateAndAuditWithEvent(ctx, actor, AuditEvent{ActorID: actor.ID, Action: "rollback", Key: input.Key, Module: moduleForDefinition(definition), Keys: []string{input.Key}, Version: record.Version, Revision: record.Version, SaveResult: "saved", ApplyResult: string(StatusSavedAndApplied), RequestID: input.RequestID}); err != nil {
		return Setting{}, err
	}
	if err := s.publishRuntime(ctx, input.Key, definition, source.RawValue); err != nil {
		return Setting{}, err
	}
	return s.present(record, definition), nil
}

// publishRuntime mirrors a committed, non-secret final state into the
// process-local hot-reload snapshot. Sensitive values stay encrypted in the
// repository and are intentionally never copied into an in-memory snapshot or
// subscriber callback. The caller validates the candidate before persistence;
// RuntimeSnapshotStore performs a second JSON validation at publication time.
func (s *Service) publishRuntime(ctx context.Context, key string, definition Definition, raw []byte) error {
	if s == nil || s.runtime == nil || definition.Sensitive {
		return nil
	}
	if _, err := s.runtime.UpdateWithSourceFor(runtimeSnapshotScope(ctx), ctx, key, raw, SourceDatabase); err != nil {
		return err
	}
	return nil
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
			resolved.Source = normalizeSource(resolved.Source)
			if !sourceAllowed(definition, resolved.Source) {
				// A resolver can expose a broad source map while a particular
				// definition intentionally accepts only a subset (for example a
				// diagnostic value that may come from deployment metadata but not
				// the database). Ignore disallowed authorities and continue down
				// the precedence chain instead of treating them as effective.
				resolved = ResolvedValue{}
			} else {
				if err := validateValue(definition, resolved.RawValue); err != nil {
					return StoredSetting{}, err
				}
				return StoredSetting{Key: key, RawValue: append([]byte(nil), resolved.RawValue...), Sensitive: definition.Sensitive, Source: resolved.Source}, nil
			}
		}
	}
	if s.repo != nil {
		record, err := s.repo.Current(ctx, key)
		if err == nil {
			if record.Source == "" {
				record.Source = SourceDatabase
			}
			record.Source = normalizeSource(record.Source)
			if sourceAllowed(definition, record.Source) {
				return record, nil
			}
			// A stale row from a source no longer accepted by the definition is
			// ignored. The effective value falls through to the compiled default;
			// writes are separately rejected by ensureDatabaseWritable.
			err = ErrSettingNotFound
		}
		if !errors.Is(err, ErrSettingNotFound) {
			return StoredSetting{}, err
		}
	}
	return StoredSetting{Key: key, RawValue: []byte(definition.Default), Sensitive: definition.Sensitive, Source: SourceDefault}, nil
}

// normalizeSource keeps source comparisons stable when adapters deserialize
// values from a database or external configuration file with incidental case
// or whitespace differences.
func normalizeSource(source Source) Source {
	return Source(strings.ToLower(strings.TrimSpace(string(source))))
}

// sourceAllowed enforces Definition.SourcePolicy at the service boundary. An
// empty policy is treated as the legacy "all authorities" policy so callers
// that provide pre-module definitions remain source-compatible.
func sourceAllowed(definition Definition, source Source) bool {
	source = normalizeSource(source)
	if source == "" || len(definition.SourcePolicy) == 0 {
		return true
	}
	for _, candidate := range definition.SourcePolicy {
		if normalizeSource(candidate) == source {
			return true
		}
	}
	return false
}

// ensureDatabaseWritable checks both the static policy and the currently
// resolved higher-precedence source. It is intentionally shared by the legacy
// key endpoint and module saves so no transport can persist an ineffective
// override.
func (s *Service) ensureDatabaseWritable(ctx context.Context, key string, definition Definition) error {
	if !sourceAllowed(definition, SourceDatabase) {
		return fmt.Errorf("%w: %s", ErrSettingLocked, key)
	}
	if s == nil || s.sources == nil {
		return nil
	}
	resolved, err := s.sources.Resolve(ctx, key)
	if err != nil {
		return err
	}
	if !resolved.Present {
		return nil
	}
	resolved.Source = normalizeSource(resolved.Source)
	if !sourceAllowed(definition, resolved.Source) {
		return nil
	}
	if resolved.Source != SourceDatabase && resolved.Source != SourceDefault {
		return fmt.Errorf("%w: %s", ErrSettingLocked, key)
	}
	return nil
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
	return s.invalidateAndAuditWithEvent(ctx, actor, AuditEvent{ActorID: actor.ID, Action: action, Key: key, Module: moduleForKeyFallback(key), Keys: []string{key}, Version: version, Revision: version, SaveResult: "saved", ApplyResult: string(StatusSavedAndApplied)})
}

func (s *Service) invalidateAndAuditWithEvent(ctx context.Context, actor Actor, event AuditEvent) error {
	if s.cache != nil {
		key := event.Key
		if key == "" && len(event.Keys) > 0 {
			key = event.Keys[0]
		}
		if err := s.cache.Invalidate(ctx, key); err != nil {
			return err
		}
	}
	if s.audit != nil {
		event.ActorID = actor.ID
		if err := s.audit.Record(ctx, event); err != nil {
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
	applyMode := definition.ApplyMode
	if applyMode == "" {
		applyMode = applyModeForDefinition(definition)
	}
	return Setting{Key: record.Key, DisplayName: definition.DisplayName, Category: definition.Category, Group: definition.Group, Value: value, Version: record.Version, Sensitive: definition.Sensitive, Source: source, Editable: definition.Editable && source != SourceEnv && source != SourceDotEnv && source != SourceYAML, Scope: definition.Scope, ApplyMode: applyMode, RestartRequired: definition.RestartRequired, UpdatedBy: record.UpdatedBy, UpdatedAt: record.UpdatedAt}
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
		if definition.Key == "i18n.supported_locales" {
			var values []string
			if err := json.Unmarshal(raw, &values); err != nil {
				return ErrInvalidSetting
			}
			if _, err := platformi18n.ValidateLocaleList(values); err != nil {
				return fmt.Errorf("%w: %v", ErrInvalidSetting, err)
			}
		}
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

func isInfrastructureSetting(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "file.root", "storage", "file.storage", "storage.path", "storage.root", "storage.dir", "file.path", "file.storage_path", "file.storage.root":
		return true
	}
	if (strings.HasPrefix(key, "storage.") || strings.HasPrefix(key, "file.storage.")) && (strings.HasSuffix(key, ".path") || strings.HasSuffix(key, ".root") || strings.HasSuffix(key, ".dir")) {
		return true
	}
	return false
}

// MemoryRepository is a deterministic adapter for tests and local bootstrap.
type MemoryRepository struct {
	mu             sync.RWMutex
	values         map[string][]StoredSetting
	moduleRevision map[string]int64
	moduleKeys     map[string]string
}

func memoryOrganization(ctx context.Context) string {
	_, organization := memoryScope(ctx)
	return organization
}

func memoryScope(ctx context.Context) (string, string) {
	if scope, err := tenant.RequireContext(ctx); err == nil {
		return strings.TrimSpace(scope.TenantID), strings.TrimSpace(scope.Organization)
	}
	return "", ""
}

func memoryTenant(ctx context.Context) string {
	tenantID, _ := memoryScope(ctx)
	return tenantID
}

func memoryRevisionKey(tenantID, module, organization string) string {
	tenantID = strings.TrimSpace(tenantID)
	module = moduleName(module)
	organization = strings.TrimSpace(organization)
	return tenantID + "\x00" + module + "\x00" + organization
}

func memoryRecordTenantMatches(record StoredSetting, tenantID string) bool {
	recordTenant := strings.TrimSpace(record.TenantID)
	tenantID = strings.TrimSpace(tenantID)
	if recordTenant == tenantID {
		return true
	}
	// Rows written by pre-scope in-memory fixtures had no tenant marker. Treat
	// them as belonging to the default tenant only, preserving bootstrap/test
	// compatibility without allowing an arbitrary tenant to inherit them.
	return tenantID == "default" && recordTenant == ""
}

func memoryLatest(records []StoredSetting, tenantID, organization string) (StoredSetting, bool) {
	tenantID = strings.TrimSpace(tenantID)
	organization = strings.TrimSpace(organization)
	var global, scoped *StoredSetting
	for index := range records {
		record := records[index]
		if !memoryRecordTenantMatches(record, tenantID) {
			continue
		}
		recordOrg := strings.TrimSpace(record.Organization)
		if organization != "" && recordOrg == organization {
			if scoped == nil || record.Version >= scoped.Version {
				copy := cloneStored(record)
				scoped = &copy
			}
			continue
		}
		if recordOrg == "" && (global == nil || record.Version >= global.Version) {
			copy := cloneStored(record)
			global = &copy
		}
	}
	if scoped != nil {
		return *scoped, true
	}
	if global != nil {
		return *global, true
	}
	return StoredSetting{}, false
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{values: map[string][]StoredSetting{}, moduleRevision: map[string]int64{}, moduleKeys: map[string]string{}}
}
func (r *MemoryRepository) Current(ctx context.Context, key string) (StoredSetting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := r.values[key]
	if len(values) == 0 {
		return StoredSetting{}, ErrSettingNotFound
	}
	tenantID, organization := memoryScope(ctx)
	if value, ok := memoryLatest(values, tenantID, organization); ok {
		return cloneStored(value), nil
	}
	return StoredSetting{}, ErrSettingNotFound
}
func (r *MemoryRepository) Append(ctx context.Context, value StoredSetting) (StoredSetting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.values == nil {
		r.values = map[string][]StoredSetting{}
	}
	if r.moduleRevision == nil {
		r.moduleRevision = map[string]int64{}
	}
	if r.moduleKeys == nil {
		r.moduleKeys = map[string]string{}
	}
	tenantID, organization := memoryScope(ctx)
	value.TenantID = tenantID
	value.Organization = organization
	var latestVersion int64
	for _, previous := range r.values[value.Key] {
		if !memoryRecordTenantMatches(previous, tenantID) {
			continue
		}
		if previous.Version > latestVersion {
			latestVersion = previous.Version
		}
	}
	value.Version = latestVersion + 1
	r.values[value.Key] = append(r.values[value.Key], cloneStored(value))
	module := moduleForKeyFallback(value.Key)
	if mapped := r.moduleKeys[value.Key]; mapped != "" {
		module = mapped
	}
	revisionKey := memoryRevisionKey(value.TenantID, module, value.Organization)
	if next := value.Version; next > r.moduleRevision[revisionKey] {
		r.moduleRevision[revisionKey] = next
	}
	return cloneStored(value), nil
}
func (r *MemoryRepository) History(ctx context.Context, key string) ([]StoredSetting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := r.values[key]
	if len(values) == 0 {
		return nil, ErrSettingNotFound
	}
	tenantID, organization := memoryScope(ctx)
	hasScoped := false
	if organization != "" {
		for _, value := range values {
			if !memoryRecordTenantMatches(value, tenantID) {
				continue
			}
			if strings.TrimSpace(value.Organization) == organization {
				hasScoped = true
				break
			}
		}
	}
	out := make([]StoredSetting, 0, len(values))
	for _, value := range values {
		if !memoryRecordTenantMatches(value, tenantID) {
			continue
		}
		valueOrg := strings.TrimSpace(value.Organization)
		if (organization != "" && hasScoped && valueOrg != organization) || (organization != "" && !hasScoped && valueOrg != "") || (organization == "" && valueOrg != "") {
			continue
		}
		out = append(out, cloneStored(value))
	}
	if len(out) == 0 {
		return nil, ErrSettingNotFound
	}
	return out, nil
}
func cloneStored(value StoredSetting) StoredSetting {
	value.RawValue = append([]byte(nil), value.RawValue...)
	return value
}

// CurrentModule returns the latest value for each key in a module and its
// opaque revision. It intentionally does not expose historical rows.
func (r *MemoryRepository) CurrentModule(ctx context.Context, module string) (StoredModule, error) {
	if r == nil {
		return StoredModule{}, ErrSettingNotFound
	}
	module = moduleName(module)
	tenantID, organization := memoryScope(ctx)
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := make(map[string]StoredSetting)
	var updated time.Time
	var revision int64
	for key, records := range r.values {
		mapped := r.moduleKeys[key]
		if mapped == "" {
			mapped = moduleForKeyFallback(key)
		}
		if mapped != module || len(records) == 0 {
			continue
		}
		record, ok := memoryLatest(records, tenantID, organization)
		if !ok {
			continue
		}
		if record.Version > revision {
			revision = record.Version
		}
		values[key] = cloneStored(record)
		if record.UpdatedAt.After(updated) {
			updated = record.UpdatedAt
		}
	}
	if len(values) == 0 {
		revision = r.moduleRevision[memoryRevisionKey(tenantID, module, organization)]
		return StoredModule{Module: module, Values: values, Revision: revision}, ErrSettingNotFound
	}
	if storedRevision := r.moduleRevision[memoryRevisionKey(tenantID, module, organization)]; storedRevision > revision {
		revision = storedRevision
	}
	return StoredModule{Module: module, Values: values, Revision: revision, UpdatedAt: updated}, nil
}

// SaveModule appends all changed keys while holding one repository lock. A
// validation failure or revision conflict leaves every key untouched.
func (r *MemoryRepository) SaveModule(ctx context.Context, module string, values map[string]StoredSetting, expectedRevision int64) (StoredModule, error) {
	if r == nil {
		return StoredModule{}, errors.New("settings repository unavailable")
	}
	module = moduleName(module)
	tenantID, organization := memoryScope(ctx)
	if module == "" || len(values) == 0 {
		return StoredModule{}, ErrInvalidSetting
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.values == nil {
		r.values = map[string][]StoredSetting{}
	}
	if r.moduleRevision == nil {
		r.moduleRevision = map[string]int64{}
	}
	if r.moduleKeys == nil {
		r.moduleKeys = map[string]string{}
	}
	revisionKey := memoryRevisionKey(tenantID, module, organization)
	current := r.moduleRevision[revisionKey]
	if current == 0 {
		for key, records := range r.values {
			mapped := r.moduleKeys[key]
			if mapped == "" {
				mapped = moduleForKeyFallback(key)
			}
			if mapped != module || len(records) == 0 {
				continue
			}
			if record, ok := memoryLatest(records, tenantID, organization); ok && record.Version > current {
				current = record.Version
			}
		}
	}
	if current != expectedRevision {
		return StoredModule{Module: module, Revision: current}, errors.Join(ErrVersionConflict, ErrModuleRevisionConflict)
	}
	// Preflight every row before mutating the map, preserving all-or-nothing
	// semantics even when callers pass malformed data.
	for key, value := range values {
		if key == "" || len(value.RawValue) == 0 || !json.Valid(value.RawValue) {
			return StoredModule{}, ErrInvalidSetting
		}
	}
	// Keep the logical revision aligned with the physical per-key versions even
	// when another organization advanced a sibling scope between reads. This
	// mirrors the database adapter's tenant-wide physical allocator and avoids
	// returning a revision that the next CurrentModule call would immediately
	// reinterpret as stale.
	physicalCurrent := current
	for key, records := range r.values {
		mapped := r.moduleKeys[key]
		if mapped == "" {
			mapped = moduleForKeyFallback(key)
		}
		if mapped != module {
			continue
		}
		for _, previous := range records {
			if !memoryRecordTenantMatches(previous, tenantID) {
				continue
			}
			if previous.Version > physicalCurrent {
				physicalCurrent = previous.Version
			}
		}
	}
	result := StoredModule{Module: module, Values: make(map[string]StoredSetting, len(values)), Revision: physicalCurrent + 1, UpdatedAt: time.Now().UTC()}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := cloneStored(values[key])
		value.TenantID = tenantID
		value.Organization = organization
		value.Version = result.Revision
		if value.UpdatedAt.IsZero() {
			value.UpdatedAt = result.UpdatedAt
		}
		r.values[key] = append(r.values[key], cloneStored(value))
		r.moduleKeys[key] = module
		result.Values[key] = cloneStored(value)
	}
	r.moduleRevision[revisionKey] = result.Revision
	return result, nil
}

// ListCurrent is used to reconstruct a process snapshot after cache loss.
func (r *MemoryRepository) ListCurrent(ctx context.Context) ([]StoredSetting, error) {
	if r == nil {
		return nil, ErrSettingNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	tenantID, organization := memoryScope(ctx)
	keys := make([]string, 0, len(r.values))
	for key, records := range r.values {
		if _, ok := memoryLatest(records, tenantID, organization); ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	result := make([]StoredSetting, 0, len(keys))
	for _, key := range keys {
		if record, ok := memoryLatest(r.values[key], tenantID, organization); ok {
			result = append(result, cloneStored(record))
		}
	}
	if len(result) == 0 {
		return nil, ErrSettingNotFound
	}
	return result, nil
}

// Delete removes a database override. Historical rows are discarded in this
// in-memory adapter because reset semantics expose only the current state.
func (r *MemoryRepository) Delete(ctx context.Context, key string) error {
	if r == nil {
		return ErrSettingNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	records, ok := r.values[key]
	if !ok {
		return ErrSettingNotFound
	}
	tenantID, organization := memoryScope(ctx)
	filtered := records[:0]
	found := false
	for _, record := range records {
		if memoryRecordTenantMatches(record, tenantID) && strings.TrimSpace(record.Organization) == organization {
			found = true
			continue
		}
		filtered = append(filtered, record)
	}
	if !found {
		return ErrSettingNotFound
	}
	if len(filtered) == 0 {
		delete(r.values, key)
		delete(r.moduleKeys, key)
	} else {
		r.values[key] = filtered
	}
	// Keep the aggregate revision monotonic. A direct per-key delete has no
	// transaction boundary, so the service's native reset path is preferred;
	// retaining the counter here prevents a later save from reusing an already
	// observed revision when a legacy caller invokes Delete directly.
	return nil
}

// ResetModule atomically removes all database overrides in a module and
// advances its aggregate revision. It is used by the service's restore-default
// operation so no transient partial module is observable.
func (r *MemoryRepository) ResetModule(ctx context.Context, module string, expectedRevision int64) (StoredModule, error) {
	if r == nil {
		return StoredModule{}, errors.New("settings repository unavailable")
	}
	module = moduleName(module)
	tenantID, organization := memoryScope(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.moduleRevision == nil {
		r.moduleRevision = map[string]int64{}
	}
	if r.moduleKeys == nil {
		r.moduleKeys = map[string]string{}
	}
	revisionKey := memoryRevisionKey(tenantID, module, organization)
	current := r.moduleRevision[revisionKey]
	if current != expectedRevision {
		return StoredModule{Module: module, Revision: current}, errors.Join(ErrVersionConflict, ErrModuleRevisionConflict)
	}
	removed := false
	for key, records := range r.values {
		mapped := r.moduleKeys[key]
		if mapped == "" {
			mapped = moduleForKeyFallback(key)
		}
		if mapped != module || len(records) == 0 {
			continue
		}
		filtered := records[:0]
		for _, record := range records {
			if memoryRecordTenantMatches(record, tenantID) && strings.TrimSpace(record.Organization) == organization {
				removed = true
				continue
			}
			filtered = append(filtered, record)
		}
		if len(filtered) == 0 {
			delete(r.values, key)
			delete(r.moduleKeys, key)
		} else {
			r.values[key] = filtered
		}
	}
	if !removed {
		return StoredModule{Module: module, Values: map[string]StoredSetting{}, Revision: current}, nil
	}
	revision := current + 1
	r.moduleRevision[revisionKey] = revision
	return StoredModule{Module: module, Values: map[string]StoredSetting{}, Revision: revision, UpdatedAt: time.Now().UTC()}, nil
}

// ClearSensitiveKeys atomically removes a selected set of database overrides
// for the current scope. The in-memory adapter drops obsolete rows entirely;
// its aggregate revision counter provides the same compare-and-swap and
// monotonic revision semantics as the persistent adapter's tombstones.
func (r *MemoryRepository) ClearSensitiveKeys(ctx context.Context, module string, keys []string, expectedRevision int64) (StoredModule, error) {
	if r == nil {
		return StoredModule{}, errors.New("settings repository unavailable")
	}
	module = moduleName(module)
	tenantID, organization := memoryScope(ctx)
	if module == "" || len(keys) == 0 {
		return StoredModule{}, ErrInvalidSetting
	}
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			wanted[key] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return StoredModule{}, ErrInvalidSetting
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.moduleRevision == nil {
		r.moduleRevision = map[string]int64{}
	}
	if r.moduleKeys == nil {
		r.moduleKeys = map[string]string{}
	}
	revisionKey := memoryRevisionKey(tenantID, module, organization)
	current := r.moduleRevision[revisionKey]
	if current == 0 {
		for key, records := range r.values {
			mapped := r.moduleKeys[key]
			if mapped == "" {
				mapped = moduleForKeyFallback(key)
			}
			if mapped != module {
				continue
			}
			for _, record := range records {
				if memoryRecordTenantMatches(record, tenantID) && strings.TrimSpace(record.Organization) == organization && record.Version > current {
					current = record.Version
				}
			}
		}
	}
	if current != expectedRevision {
		return StoredModule{Module: module, Revision: current}, errors.Join(ErrVersionConflict, ErrModuleRevisionConflict)
	}
	changed := false
	for key := range wanted {
		records, ok := r.values[key]
		if !ok {
			continue
		}
		mapped := r.moduleKeys[key]
		if mapped == "" {
			mapped = moduleForKeyFallback(key)
		}
		if mapped != module {
			continue
		}
		filtered := records[:0]
		for _, record := range records {
			if memoryRecordTenantMatches(record, tenantID) && strings.TrimSpace(record.Organization) == organization && (record.Source == "" || record.Source == SourceDatabase) {
				changed = true
				continue
			}
			filtered = append(filtered, record)
		}
		if len(filtered) == 0 {
			delete(r.values, key)
			delete(r.moduleKeys, key)
		} else {
			r.values[key] = filtered
		}
	}
	if !changed {
		return StoredModule{Module: module, Values: map[string]StoredSetting{}, Revision: current}, nil
	}
	physicalCurrent := current
	for key, records := range r.values {
		mapped := r.moduleKeys[key]
		if mapped == "" {
			mapped = moduleForKeyFallback(key)
		}
		if mapped != module {
			continue
		}
		for _, record := range records {
			if !memoryRecordTenantMatches(record, tenantID) {
				continue
			}
			if record.Version > physicalCurrent {
				physicalCurrent = record.Version
			}
		}
	}
	revision := physicalCurrent + 1
	r.moduleRevision[revisionKey] = revision
	return StoredModule{Module: module, Values: map[string]StoredSetting{}, Revision: revision, UpdatedAt: time.Now().UTC()}, nil
}

func moduleForKeyFallback(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if strings.HasPrefix(key, "observability.") {
		return "observability"
	}
	if index := strings.IndexByte(key, '.'); index > 0 {
		return key[:index]
	}
	if key == "branding" {
		return "basic"
	}
	return "other"
}
