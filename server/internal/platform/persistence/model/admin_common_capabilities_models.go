package model

import "time"

// NotificationCaller identifies a trusted in-process caller and its scope.
type NotificationCaller struct {
	ID           string     `gorm:"column:id;size:64;primaryKey;comment:调用者标识"`
	CallerKey    string     `gorm:"column:caller_key;size:128;not null;uniqueIndex:uq_gvba_notify_callers_scope_key,priority:4;comment:稳定调用者键"`
	ScopeType    string     `gorm:"column:scope_type;size:16;not null;uniqueIndex:uq_gvba_notify_callers_scope_key,priority:3;index:idx_gvba_notify_callers_scope,priority:1;comment:作用域类型"`
	TenantID     *string    `gorm:"column:tenant_id;size:128;uniqueIndex:uq_gvba_notify_callers_scope_key,priority:1;index:idx_gvba_notify_callers_scope,priority:2;comment:租户标识"`
	OrgID        *string    `gorm:"column:org_id;size:128;uniqueIndex:uq_gvba_notify_callers_scope_key,priority:2;index:idx_gvba_notify_callers_scope,priority:3;comment:组织标识"`
	DisplayName  string     `gorm:"column:display_name;size:191;not null;comment:显示名称"`
	Module       string     `gorm:"column:module;size:128;not null;default:'';comment:所属模块"`
	Capabilities JSONValue  `gorm:"column:capabilities;not null;comment:能力集合"`
	Enabled      bool       `gorm:"column:enabled;not null;default:true;index:idx_gvba_notify_callers_enabled;comment:是否启用"`
	SystemOwned  bool       `gorm:"column:system_owned;not null;default:false;comment:是否系统所有"`
	CreatedBy    *string    `gorm:"column:created_by;size:128;comment:创建者标识"`
	CreatedAt    time.Time  `gorm:"column:created_at;precision:6;not null;comment:创建时间"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt    *time.Time `gorm:"column:deleted_at;precision:6;index:idx_gvba_notify_callers_scope,priority:4;comment:删除时间"`
}

func (NotificationCaller) TableName() string { return "gvba_notify_callers" }

type NotificationCallerAccount struct {
	ID            string     `gorm:"column:id;size:64;primaryKey;comment:绑定标识"`
	CallerID      string     `gorm:"column:caller_id;size:64;not null;uniqueIndex:uq_gvba_notify_caller_accounts,priority:1;index:idx_gvba_notify_caller_accounts_caller,priority:1;comment:调用者标识"`
	SMTPAccountID string     `gorm:"column:smtp_account_id;size:64;not null;uniqueIndex:uq_gvba_notify_caller_accounts,priority:2;index:idx_gvba_notify_caller_accounts_account;comment:SMTP账户标识"`
	Weight        int32      `gorm:"column:weight;not null;default:1;comment:路由权重"`
	Priority      int32      `gorm:"column:priority;not null;default:0;comment:路由优先级"`
	Strategy      string     `gorm:"column:strategy;size:32;not null;default:weighted_random;comment:路由策略"`
	IsDefault     bool       `gorm:"column:is_default;not null;default:false;index:idx_gvba_notify_caller_accounts_default,priority:1;comment:是否默认账户"`
	CreatedAt     time.Time  `gorm:"column:created_at;precision:6;not null;index:idx_gvba_notify_caller_accounts_caller,priority:2;comment:创建时间"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt     *time.Time `gorm:"column:deleted_at;precision:6;index:idx_gvba_notify_caller_accounts_caller,priority:3;comment:删除时间"`
}

func (NotificationCallerAccount) TableName() string { return "gvba_notify_caller_accounts" }

type NotificationTemplate struct {
	ID             string     `gorm:"column:id;size:64;primaryKey;comment:模板标识"`
	TemplateKey    string     `gorm:"column:template_key;size:191;not null;uniqueIndex:uq_gvba_notify_templates_scope_key,priority:4;comment:模板键"`
	ScopeType      string     `gorm:"column:scope_type;size:16;not null;uniqueIndex:uq_gvba_notify_templates_scope_key,priority:3;index:idx_gvba_notify_templates_scope,priority:1;comment:作用域类型"`
	TenantID       *string    `gorm:"column:tenant_id;size:128;uniqueIndex:uq_gvba_notify_templates_scope_key,priority:1;index:idx_gvba_notify_templates_scope,priority:2;comment:租户标识"`
	OrgID          *string    `gorm:"column:org_id;size:128;uniqueIndex:uq_gvba_notify_templates_scope_key,priority:2;index:idx_gvba_notify_templates_scope,priority:3;comment:组织标识"`
	VariableSchema JSONValue  `gorm:"column:variable_schema;not null;comment:变量约束模式"`
	Status         string     `gorm:"column:status;size:16;not null;default:draft;index:idx_gvba_notify_templates_status;comment:模板状态"`
	Generation     uint64     `gorm:"column:generation;not null;default:1;comment:当前发布代次"`
	SystemOwned    bool       `gorm:"column:system_owned;not null;default:false;comment:是否系统所有"`
	CreatedBy      *string    `gorm:"column:created_by;size:128;comment:创建者标识"`
	UpdatedBy      *string    `gorm:"column:updated_by;size:128;comment:更新者标识"`
	CreatedAt      time.Time  `gorm:"column:created_at;precision:6;not null;comment:创建时间"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt      *time.Time `gorm:"column:deleted_at;precision:6;index:idx_gvba_notify_templates_scope,priority:4;comment:删除时间"`
}

func (NotificationTemplate) TableName() string { return "gvba_notify_templates" }

type NotificationTemplateLocale struct {
	ID         string     `gorm:"column:id;size:64;primaryKey;comment:模板语言标识"`
	TemplateID string     `gorm:"column:template_id;size:64;not null;uniqueIndex:uq_gvba_notify_template_locales,priority:1;index:idx_gvba_notify_template_locales_template,priority:1;comment:模板标识"`
	Locale     string     `gorm:"column:locale;size:32;not null;uniqueIndex:uq_gvba_notify_template_locales,priority:2;comment:语言区域"`
	Subject    string     `gorm:"column:subject;size:998;not null;comment:邮件主题模板"`
	Body       string     `gorm:"column:body;type:text;not null;comment:邮件正文模板"`
	Status     string     `gorm:"column:status;size:16;not null;default:draft;comment:语言版本状态"`
	CreatedAt  time.Time  `gorm:"column:created_at;precision:6;not null;comment:创建时间"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt  *time.Time `gorm:"column:deleted_at;precision:6;index:idx_gvba_notify_template_locales_template,priority:2;comment:删除时间"`
}

func (NotificationTemplateLocale) TableName() string { return "gvba_notify_template_locales" }

type NotificationTemplateVersion struct {
	ID             string     `gorm:"column:id;size:64;primaryKey;comment:模板快照标识"`
	TemplateID     string     `gorm:"column:template_id;size:64;not null;index:idx_gvba_notify_template_versions_template,priority:1;comment:模板标识"`
	Generation     uint64     `gorm:"column:generation;not null;index:idx_gvba_notify_template_versions_template,priority:2;comment:发布代次"`
	Locale         string     `gorm:"column:locale;size:32;not null;index:idx_gvba_notify_template_versions_locale;comment:语言区域"`
	Subject        string     `gorm:"column:subject;size:998;not null;comment:邮件主题快照"`
	Body           string     `gorm:"column:body;type:text;not null;comment:邮件正文快照"`
	VariableSchema JSONValue  `gorm:"column:variable_schema;not null;comment:变量约束快照"`
	PublishedBy    *string    `gorm:"column:published_by;size:128;comment:发布者标识"`
	CreatedAt      time.Time  `gorm:"column:created_at;precision:6;not null;comment:创建时间"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt      *time.Time `gorm:"column:deleted_at;precision:6;index:idx_gvba_notify_template_versions_template,priority:3;comment:删除时间"`
}

func (NotificationTemplateVersion) TableName() string { return "gvba_notify_template_versions" }

type VerificationPolicy struct {
	ID         string  `gorm:"column:id;size:64;primaryKey;comment:验证码策略标识"`
	PolicyKey  string  `gorm:"column:policy_key;size:191;not null;uniqueIndex:uq_gvba_verify_policies_scope_key,priority:4;comment:策略键"`
	Purpose    string  `gorm:"column:purpose;size:64;not null;index:idx_gvba_verify_policies_purpose;comment:验证码用途"`
	CallerID   *string `gorm:"column:caller_id;size:64;index:idx_gvba_verify_policies_caller;comment:调用者标识"`
	ScopeType  string  `gorm:"column:scope_type;size:16;not null;uniqueIndex:uq_gvba_verify_policies_scope_key,priority:3;index:idx_gvba_verify_policies_scope,priority:1;comment:作用域类型"`
	TenantID   *string `gorm:"column:tenant_id;size:128;uniqueIndex:uq_gvba_verify_policies_scope_key,priority:1;index:idx_gvba_verify_policies_scope,priority:2;comment:租户标识"`
	OrgID      *string `gorm:"column:org_id;size:128;uniqueIndex:uq_gvba_verify_policies_scope_key,priority:2;index:idx_gvba_verify_policies_scope,priority:3;comment:组织标识"`
	CodeLength int32   `gorm:"column:code_length;not null;default:6;comment:验证码长度"`
	// Keep the database width aligned with the runtime's validated custom
	// charset limit (128 printable bytes).
	Charset           string     `gorm:"column:charset;size:128;not null;default:numeric;comment:验证码字符集"`
	TTLSec            int32      `gorm:"column:ttl_sec;not null;default:600;comment:有效期秒数"`
	MaxFailures       int32      `gorm:"column:max_failures;not null;default:5;comment:最大失败次数"`
	ResendIntervalSec int32      `gorm:"column:resend_interval_sec;not null;default:60;comment:重发间隔秒数"`
	HourlyLimit       int32      `gorm:"column:hourly_limit;not null;default:5;comment:每小时次数限制"`
	Enabled           bool       `gorm:"column:enabled;not null;default:true;comment:是否启用"`
	CreatedAt         time.Time  `gorm:"column:created_at;precision:6;not null;comment:创建时间"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt         *time.Time `gorm:"column:deleted_at;precision:6;index:idx_gvba_verify_policies_scope,priority:4;comment:删除时间"`
}

func (VerificationPolicy) TableName() string { return "gvba_verify_policies" }

type VerificationChallenge struct {
	ID                  string       `gorm:"column:id;size:64;primaryKey;comment:验证码挑战标识"`
	TenantID            *string      `gorm:"column:tenant_id;size:128;index:idx_gvba_verify_challenges_scope,priority:1;comment:租户标识"`
	OrgID               *string      `gorm:"column:org_id;size:128;index:idx_gvba_verify_challenges_scope,priority:2;comment:组织标识"`
	ScopeType           string       `gorm:"column:scope_type;size:16;not null;index:idx_gvba_verify_challenges_scope,priority:3;comment:作用域类型"`
	CallerID            string       `gorm:"column:caller_id;size:64;not null;index:idx_gvba_verify_challenges_caller_purpose,priority:1;comment:调用者标识"`
	Purpose             string       `gorm:"column:purpose;size:64;not null;index:idx_gvba_verify_challenges_caller_purpose,priority:2;comment:验证码用途"`
	RecipientDigest     string       `gorm:"column:recipient_digest;type:char(64);not null;index:idx_gvba_verify_challenges_recipient;comment:收件人摘要"`
	RecipientCiphertext *BinaryValue `gorm:"column:recipient_ciphertext;comment:收件人密文"`
	CodeDigest          string       `gorm:"column:code_digest;type:char(128);not null;comment:验证码摘要"`
	Status              string       `gorm:"column:status;size:20;not null;index:idx_gvba_verify_challenges_status;comment:挑战状态"`
	FailedAttempts      int32        `gorm:"column:failed_attempts;not null;default:0;comment:失败次数"`
	ExpiresAt           time.Time    `gorm:"column:expires_at;precision:6;not null;index:idx_gvba_verify_challenges_expires;comment:过期时间"`
	ResendAt            *time.Time   `gorm:"column:resend_at;precision:6;comment:可重发时间"`
	MessageID           *string      `gorm:"column:message_id;size:64;index:idx_gvba_verify_challenges_message;comment:邮件消息标识"`
	TemplateKey         *string      `gorm:"column:template_key;size:191;comment:模板键"`
	TemplateGeneration  *uint64      `gorm:"column:template_generation;comment:模板发布代次"`
	Locale              string       `gorm:"column:locale;size:32;not null;default:zh-CN;comment:实际语言区域"`
	CreatedAt           time.Time    `gorm:"column:created_at;precision:6;not null;index:idx_gvba_verify_challenges_created;comment:创建时间"`
	UpdatedAt           time.Time    `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt           *time.Time   `gorm:"column:deleted_at;precision:6;index:idx_gvba_verify_challenges_scope,priority:4;comment:删除时间"`
}

func (VerificationChallenge) TableName() string { return "gvba_verify_challenges" }

type MediaCategory struct {
	ID        string  `gorm:"column:id;size:64;primaryKey;comment:媒体分类标识"`
	ScopeType string  `gorm:"column:scope_type;size:16;not null;uniqueIndex:uq_gvba_storage_media_categories_scope_parent_name,priority:3;index:idx_gvba_storage_media_categories_scope,priority:1;comment:作用域类型"`
	TenantID  *string `gorm:"column:tenant_id;size:128;uniqueIndex:uq_gvba_storage_media_categories_scope_parent_name,priority:1;index:idx_gvba_storage_media_categories_scope,priority:2;comment:租户标识"`
	OrgID     *string `gorm:"column:org_id;size:128;uniqueIndex:uq_gvba_storage_media_categories_scope_parent_name,priority:2;index:idx_gvba_storage_media_categories_scope,priority:3;comment:组织标识"`
	ParentID  *string `gorm:"column:parent_id;size:64;uniqueIndex:uq_gvba_storage_media_categories_scope_parent_name,priority:4;index:idx_gvba_storage_media_categories_parent;comment:父分类标识"`
	// Path intentionally has no full-value MySQL index: 1024 utf8mb4
	// characters exceed InnoDB's 3072-byte key limit. Scope/parent indexes
	// cover tree navigation; prefix/hash indexing can be added per dialect if
	// path search becomes a measured workload.
	Path        string     `gorm:"column:path;size:1024;not null;comment:分类路径"`
	Depth       int32      `gorm:"column:depth;not null;default:0;comment:分类深度"`
	Name        string     `gorm:"column:name;size:191;not null;uniqueIndex:uq_gvba_storage_media_categories_scope_parent_name,priority:5;comment:分类名称"`
	SortOrder   int32      `gorm:"column:sort_order;not null;default:0;comment:排序值"`
	Enabled     bool       `gorm:"column:enabled;not null;default:true;comment:是否启用"`
	SystemOwned bool       `gorm:"column:system_owned;not null;default:false;comment:是否系统所有"`
	CreatedAt   time.Time  `gorm:"column:created_at;precision:6;not null;comment:创建时间"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt   *time.Time `gorm:"column:deleted_at;precision:6;index:idx_gvba_storage_media_categories_scope,priority:4;comment:删除时间"`
}

func (MediaCategory) TableName() string { return "gvba_storage_media_categories" }

type MediaUsage struct {
	ID         string     `gorm:"column:id;size:64;primaryKey;comment:媒体引用标识"`
	ScopeType  string     `gorm:"column:scope_type;size:16;not null;index:idx_gvba_storage_media_usages_scope,priority:1;comment:作用域类型"`
	TenantID   *string    `gorm:"column:tenant_id;size:128;index:idx_gvba_storage_media_usages_scope,priority:2;comment:租户标识"`
	OrgID      *string    `gorm:"column:org_id;size:128;index:idx_gvba_storage_media_usages_scope,priority:3;comment:组织标识"`
	ResourceID string     `gorm:"column:resource_id;size:64;not null;uniqueIndex:uq_gvba_storage_media_usages_target,priority:1;index:idx_gvba_storage_media_usages_resource,priority:1;comment:资源标识"`
	CallerKey  string     `gorm:"column:caller_key;size:128;not null;uniqueIndex:uq_gvba_storage_media_usages_target,priority:2;comment:调用者键"`
	Module     string     `gorm:"column:module;size:128;not null;uniqueIndex:uq_gvba_storage_media_usages_target,priority:3;comment:业务模块"`
	EntityType string     `gorm:"column:entity_type;size:128;not null;uniqueIndex:uq_gvba_storage_media_usages_target,priority:4;comment:实体类型"`
	EntityID   string     `gorm:"column:entity_id;size:128;not null;uniqueIndex:uq_gvba_storage_media_usages_target,priority:5;comment:实体标识"`
	Field      string     `gorm:"column:field;size:128;not null;uniqueIndex:uq_gvba_storage_media_usages_target,priority:6;comment:引用字段"`
	CreatedAt  time.Time  `gorm:"column:created_at;precision:6;not null;index:idx_gvba_storage_media_usages_resource,priority:2;comment:创建时间"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt  *time.Time `gorm:"column:deleted_at;precision:6;index:idx_gvba_storage_media_usages_scope,priority:4;comment:删除时间"`
}

func (MediaUsage) TableName() string { return "gvba_storage_media_usages" }
