// Package model contains the persistence-side GORM models.
//
// Files are grouped by capability (shared, identity, admin, audit and
// infrastructure) while remaining one Go package so migrations and repositories
// share exactly one set of column definitions. Relationship IDs intentionally
// remain scalar fields; ORM read relations live outside the migration registry.
package model

import "time"

type User struct {
	ID                 uint64     `gorm:"column:id;primaryKey;autoIncrement;comment:用户标识"`
	TenantID           string     `gorm:"column:tenant_id;size:64;not null;default:default;index:idx_users_tenant_org,priority:1;index:idx_users_tenant_status_created,priority:1;uniqueIndex:uq_users_tenant_username,priority:1;uniqueIndex:uq_users_tenant_username_normalized,priority:1;uniqueIndex:uq_users_tenant_email_normalized,priority:1;comment:租户标识"`
	OrgID              *string    `gorm:"column:org_id;size:64;index:idx_users_tenant_org,priority:2;comment:组织标识"`
	Username           string     `gorm:"column:username;size:191;not null;uniqueIndex:uq_users_tenant_username,priority:2;comment:登录名"`
	UsernameNormalized string     `gorm:"column:username_normalized;size:191;not null;uniqueIndex:uq_users_tenant_username_normalized,priority:2;comment:规范化登录名"`
	PasswordHash       string     `gorm:"column:password_hash;size:255;not null;comment:密码摘要"`
	Email              *string    `gorm:"column:email;size:254;comment:邮箱"`
	EmailNormalized    *string    `gorm:"column:email_normalized;size:254;uniqueIndex:uq_users_tenant_email_normalized,priority:2;comment:规范化邮箱"`
	Nickname           string     `gorm:"column:nickname;size:191;not null;default:'';comment:昵称"`
	Avatar             string     `gorm:"column:avatar;size:512;not null;default:'';comment:头像地址"`
	Phone              *string    `gorm:"column:phone;size:32;comment:手机号"`
	Status             string     `gorm:"column:status;size:32;not null;default:active;index:idx_users_tenant_status_created,priority:2;check:chk_users_status,status IN ('active','disabled','locked');comment:用户状态"`
	FailedAttempts     uint32     `gorm:"column:failed_attempts;not null;default:0;comment:失败次数"`
	LockedUntil        *time.Time `gorm:"column:locked_until;precision:6;comment:锁定截止时间"`
	MustChangePassword bool       `gorm:"column:must_change_password;not null;default:false;comment:是否必须改密"`
	LastLoginIP        string     `gorm:"column:last_login_ip;size:64;not null;default:'';comment:最近登录地址"`
	LastLoginAt        *time.Time `gorm:"column:last_login_at;precision:6;comment:最近登录时间"`
	PasswordChangedAt  *time.Time `gorm:"column:password_changed_at;precision:6;comment:密码更新时间"`
	CreatedAt          time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);index:idx_users_tenant_status_created,priority:3;comment:创建时间"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	DeletedAt          *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (User) TableName() string { return "users" }

type AuthSession struct {
	ID               string     `gorm:"column:id;size:64;primaryKey;comment:会话标识"`
	UserID           uint64     `gorm:"column:user_id;not null;index:idx_auth_sessions_user_status,priority:1;index:idx_auth_sessions_user_created,priority:1;comment:用户标识"`
	TenantID         string     `gorm:"column:tenant_id;size:64;not null;default:default;index:idx_auth_sessions_tenant,priority:1;comment:租户标识"`
	OrgID            *string    `gorm:"column:org_id;size:64;index:idx_auth_sessions_tenant,priority:2;comment:组织标识"`
	RefreshTokenHash string     `gorm:"column:refresh_token_hash;type:char(64);not null;uniqueIndex:uq_auth_sessions_refresh_hash;comment:刷新令牌摘要"`
	FamilyID         string     `gorm:"column:family_id;size:64;not null;comment:令牌族标识"`
	Status           string     `gorm:"column:status;size:32;not null;default:active;index:idx_auth_sessions_user_status,priority:2;comment:会话状态"`
	ExpiresAt        time.Time  `gorm:"column:expires_at;precision:6;not null;comment:过期时间"`
	LastSeenAt       time.Time  `gorm:"column:last_seen_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:最近访问时间"`
	RevokedAt        *time.Time `gorm:"column:revoked_at;precision:6;comment:撤销时间"`
	DeviceID         string     `gorm:"column:device_id;size:128;not null;default:'';comment:设备标识"`
	DeviceName       string     `gorm:"column:device_name;size:191;not null;default:'';comment:设备名称"`
	IPAddress        string     `gorm:"column:ip_address;size:64;not null;default:'';comment:登录地址"`
	UserAgent        string     `gorm:"column:user_agent;size:512;not null;default:'';comment:客户端标识"`
	CreatedAt        time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);index:idx_auth_sessions_user_created,priority:2;comment:创建时间"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	DeletedAt        *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (AuthSession) TableName() string { return "auth_sessions" }
