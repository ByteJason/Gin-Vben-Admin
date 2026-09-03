// Package model contains the persistence-side GORM models.
//
// Files are grouped by capability (shared, identity, admin, audit and
// infrastructure) while remaining one Go package so migrations and repositories
// share exactly one set of column definitions. Relationship IDs intentionally
// remain scalar fields; ORM read relations live outside the migration registry.
package model

import "time"

type SMTPAccount struct {
	ID                 string       `gorm:"column:id;size:64;primaryKey;comment:账户标识"`
	TenantID           string       `gorm:"column:tenant_id;size:128;not null;uniqueIndex:uq_smtp_accounts_tenant_name,priority:1;uniqueIndex:uq_smtp_accounts_tenant_endpoint,priority:1;index:idx_smtp_accounts_scope,priority:1;index:idx_smtp_accounts_enabled,priority:1;comment:租户标识"`
	OrgID              *string      `gorm:"column:org_id;size:128;index:idx_smtp_accounts_scope,priority:2;comment:组织标识"`
	ScopeType          string       `gorm:"column:scope_type;size:16;not null;default:tenant;index:idx_smtp_accounts_scope,priority:3;comment:作用域类型"`
	AccountName        string       `gorm:"column:account_name;size:128;not null;uniqueIndex:uq_smtp_accounts_tenant_name,priority:2;comment:账户名称"`
	Enabled            bool         `gorm:"column:enabled;not null;default:true;index:idx_smtp_accounts_enabled,priority:2;comment:是否启用"`
	Host               string       `gorm:"column:host;size:255;not null;uniqueIndex:uq_smtp_accounts_tenant_endpoint,priority:2;comment:SMTP主机"`
	Port               int32        `gorm:"column:port;not null;uniqueIndex:uq_smtp_accounts_tenant_endpoint,priority:3;comment:SMTP端口"`
	Username           string       `gorm:"column:username;size:255;not null;default:'';uniqueIndex:uq_smtp_accounts_tenant_endpoint,priority:4;comment:用户名"`
	PasswordCiphertext *BinaryValue `gorm:"column:password_ciphertext;comment:加密密码"`
	Weight             int32        `gorm:"column:weight;not null;default:1;comment:发送权重"`
	FromEmail          string       `gorm:"column:from_email;size:320;not null;comment:发件地址"`
	FromName           string       `gorm:"column:from_name;size:255;not null;default:'';comment:发件名称"`
	ImplicitTLS        bool         `gorm:"column:implicit_tls;not null;default:false;comment:隐式TLS"`
	CreatedAt          time.Time    `gorm:"column:created_at;precision:6;not null;comment:创建时间"`
	UpdatedAt          time.Time    `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt          *time.Time   `gorm:"column:deleted_at;precision:6;index:idx_smtp_accounts_scope,priority:4;index:idx_smtp_accounts_enabled,priority:3;comment:删除时间"`
}

func (SMTPAccount) TableName() string { return "smtp_accounts" }

type EmailMessage struct {
	ID                   string      `gorm:"column:id;size:64;primaryKey;comment:邮件标识"`
	TenantID             string      `gorm:"column:tenant_id;size:128;not null;index:idx_email_messages_scope,priority:1;index:idx_email_messages_status,priority:1;index:idx_email_messages_idempotency,priority:1;comment:租户标识"`
	OrgID                *string     `gorm:"column:org_id;size:128;index:idx_email_messages_scope,priority:2;comment:组织标识"`
	ScopeType            string      `gorm:"column:scope_type;size:16;not null;default:tenant;index:idx_email_messages_scope,priority:3;comment:作用域类型"`
	SMTPAccountID        *string     `gorm:"column:smtp_account_id;size:64;comment:SMTP账户标识"`
	SenderID             *string     `gorm:"column:sender_id;size:128;comment:发送者标识"`
	CallerKey            *string     `gorm:"column:caller_key;size:128;index:idx_email_messages_caller;comment:调用者键"`
	TemplateKey          *string     `gorm:"column:template_key;size:191;index:idx_email_messages_template;comment:模板键"`
	TemplateGeneration   *uint64     `gorm:"column:template_generation;comment:模板发布代次"`
	PolicyGeneration     *string     `gorm:"column:policy_generation;size:64;comment:策略发布代次"`
	Locale               *string     `gorm:"column:locale;size:32;comment:实际语言区域"`
	IsTest               bool        `gorm:"column:is_test;not null;default:false;index:idx_email_messages_test;comment:是否测试消息"`
	ChallengeID          *string     `gorm:"column:challenge_id;size:64;index:idx_email_messages_challenge;comment:验证码挑战标识"`
	RelayStatus          string      `gorm:"column:relay_status;size:16;not null;default:pending;index:idx_email_messages_relay;comment:中继状态"`
	Subject              string      `gorm:"column:subject;size:998;not null;comment:邮件主题"`
	BodyCiphertext       BinaryValue `gorm:"column:body_ciphertext;not null;comment:加密正文"`
	BodyDigest           string      `gorm:"column:body_digest;type:char(64);not null;comment:正文摘要"`
	Status               string      `gorm:"column:status;size:16;not null;index:idx_email_messages_status,priority:2;comment:邮件状态"`
	AttemptCount         int32       `gorm:"column:attempt_count;not null;default:0;comment:投递次数"`
	ProviderMessageID    *string     `gorm:"column:provider_message_id;size:255;comment:服务商消息标识"`
	LastErrorCode        *string     `gorm:"column:last_error_code;size:64;comment:最近错误码"`
	SentAt               *time.Time  `gorm:"column:sent_at;precision:6;comment:发送时间"`
	IdempotencyKey       *string     `gorm:"column:idempotency_key;size:128;index:idx_email_messages_idempotency,priority:2;comment:幂等键"`
	IdempotencyScopeHash *string     `gorm:"column:idempotency_scope_hash;type:char(64);uniqueIndex:uq_email_messages_idempotency_scope,priority:1;comment:作用域幂等摘要"`
	CreatedAt            time.Time   `gorm:"column:created_at;precision:6;not null;index:idx_email_messages_scope,priority:4;index:idx_email_messages_status,priority:3;comment:创建时间"`
	UpdatedAt            time.Time   `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt            *time.Time  `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (EmailMessage) TableName() string { return "email_messages" }

type EmailRecipient struct {
	ID        string     `gorm:"column:id;size:64;primaryKey;comment:收件人记录标识"`
	MessageID string     `gorm:"column:message_id;size:64;not null;index:idx_email_recipients_message,priority:1;comment:邮件标识"`
	Kind      string     `gorm:"column:kind;size:16;not null;default:to;comment:收件人类型"`
	Address   string     `gorm:"column:address;size:320;not null;index:idx_email_recipients_address;comment:收件地址"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;index:idx_email_recipients_message,priority:2;comment:删除时间"`
}

func (EmailRecipient) TableName() string { return "email_recipients" }

type EmailDeliveryAttempt struct {
	ID        string     `gorm:"column:id;size:64;primaryKey;comment:投递记录标识"`
	MessageID string     `gorm:"column:message_id;size:64;not null;index:idx_email_attempts_message,priority:1;comment:邮件标识"`
	AccountID string     `gorm:"column:account_id;size:64;not null;index:idx_email_attempts_account,priority:1;comment:账户标识"`
	AttemptNo int32      `gorm:"column:attempt_no;not null;index:idx_email_attempts_message,priority:2;comment:尝试序号"`
	Stage     string     `gorm:"column:stage;size:32;not null;default:'';comment:投递阶段"`
	Code      string     `gorm:"column:code;size:64;not null;default:'';comment:结果码"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;index:idx_email_attempts_account,priority:2;comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (EmailDeliveryAttempt) TableName() string { return "email_delivery_attempts" }
