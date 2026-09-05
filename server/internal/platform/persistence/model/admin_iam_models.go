// Package model contains the persistence-side GORM models.
//
// Files are grouped by capability (shared, identity, admin, audit and
// infrastructure) while remaining one Go package so migrations and repositories
// share exactly one set of column definitions. Relationship IDs intentionally
// remain scalar fields; ORM read relations live outside the migration registry.
package model

import "time"

type Role struct {
	ID        string     `gorm:"column:id;size:64;primaryKey;comment:角色标识"`
	TenantID  string     `gorm:"column:tenant_id;size:64;not null;default:default;index:idx_gvba_iam_roles_tenant_org,priority:1;comment:租户标识"`
	OrgID     *string    `gorm:"column:org_id;size:64;index:idx_gvba_iam_roles_tenant_org,priority:2;comment:组织标识"`
	Name      string     `gorm:"column:name;size:191;not null;uniqueIndex:uq_gvba_iam_roles_name;comment:角色名称"`
	Status    string     `gorm:"column:status;size:32;not null;default:active;check:chk_gvba_iam_roles_status,status IN ('active','disabled');comment:角色状态"`
	DataScope string     `gorm:"column:data_scope;size:32;not null;default:own;check:chk_gvba_iam_roles_scope,data_scope IN ('all','own','org','custom');comment:数据范围"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (Role) TableName() string { return "gvba_iam_roles" }

type UserRole struct {
	UserID    uint64     `gorm:"column:user_id;primaryKey;autoIncrement:false;comment:用户标识"`
	RoleID    string     `gorm:"column:role_id;size:64;primaryKey;comment:角色标识"`
	TenantID  string     `gorm:"column:tenant_id;size:64;not null;default:default;index:idx_gvba_iam_user_roles_tenant;comment:租户标识"`
	OrgID     *string    `gorm:"column:org_id;size:64;comment:组织标识"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (UserRole) TableName() string { return "gvba_iam_user_roles" }

type Menu struct {
	ID         string     `gorm:"column:id;size:64;primaryKey;comment:菜单标识"`
	TenantID   string     `gorm:"column:tenant_id;size:64;not null;default:default;uniqueIndex:uq_gvba_iam_menus_tenant_path,priority:1;index:idx_gvba_iam_menus_tenant_org,priority:1;index:idx_gvba_iam_menus_tenant_parent_order,priority:1;comment:租户标识"`
	OrgID      *string    `gorm:"column:org_id;size:64;index:idx_gvba_iam_menus_tenant_org,priority:2;index:idx_gvba_iam_menus_tenant_parent_order,priority:2;comment:组织标识"`
	ParentID   *string    `gorm:"column:parent_id;size:64;index:idx_gvba_iam_menus_parent_order,priority:1;index:idx_gvba_iam_menus_tenant_parent_order,priority:3;comment:父菜单标识"`
	Name       string     `gorm:"column:name;size:191;not null;comment:菜单名称"`
	Path       string     `gorm:"column:path;size:255;not null;uniqueIndex:uq_gvba_iam_menus_tenant_path,priority:2;comment:路由路径"`
	MenuType   string     `gorm:"column:menu_type;size:32;not null;default:directory;check:chk_gvba_iam_menus_type,menu_type IN ('directory','menu','button');comment:菜单类型"`
	Component  *string    `gorm:"column:component;size:255;comment:组件路径"`
	Redirect   *string    `gorm:"column:redirect;size:255;comment:重定向地址"`
	Icon       *string    `gorm:"column:icon;size:191;comment:图标"`
	Permission *string    `gorm:"column:permission;size:191;comment:权限标识"`
	Visible    bool       `gorm:"column:visible;not null;default:true;comment:是否可见"`
	KeepAlive  bool       `gorm:"column:keep_alive;not null;default:false;comment:是否缓存"`
	External   bool       `gorm:"column:external;not null;default:false;comment:是否外链"`
	Status     string     `gorm:"column:status;size:32;not null;default:active;check:chk_gvba_iam_menus_status,status IN ('active','disabled');comment:菜单状态"`
	SortOrder  int32      `gorm:"column:sort_order;not null;default:0;index:idx_gvba_iam_menus_parent_order,priority:2;index:idx_gvba_iam_menus_tenant_parent_order,priority:4;comment:排序值"`
	CreatedAt  time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:创建时间"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	DeletedAt  *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (Menu) TableName() string { return "gvba_iam_menus" }

type Permission struct {
	ID        string     `gorm:"column:id;size:64;primaryKey;comment:权限标识"`
	TenantID  string     `gorm:"column:tenant_id;size:64;not null;default:default;index:idx_gvba_iam_permissions_tenant_org,priority:1;comment:租户标识"`
	OrgID     *string    `gorm:"column:org_id;size:64;index:idx_gvba_iam_permissions_tenant_org,priority:2;comment:组织标识"`
	Name      string     `gorm:"column:name;size:191;not null;comment:权限名称"`
	Method    string     `gorm:"column:method;size:16;not null;uniqueIndex:uq_gvba_iam_permissions_method_path,priority:1;comment:请求方法"`
	Path      string     `gorm:"column:path;size:255;not null;uniqueIndex:uq_gvba_iam_permissions_method_path,priority:2;comment:请求路径"`
	Status    string     `gorm:"column:status;size:32;not null;default:active;check:chk_gvba_iam_permissions_status,status IN ('active','disabled');comment:权限状态"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (Permission) TableName() string { return "gvba_iam_permissions" }

type IAMPolicy struct {
	ID        uint64     `gorm:"column:id;primaryKey;autoIncrement;comment:策略标识"`
	TenantID  string     `gorm:"column:tenant_id;size:64;not null;default:default;index:idx_gvba_iam_policies_tenant,priority:1;comment:租户标识"`
	OrgID     *string    `gorm:"column:org_id;size:64;index:idx_gvba_iam_policies_tenant,priority:2;comment:组织标识"`
	UserID    *uint64    `gorm:"column:user_id;index:idx_gvba_iam_policies_user_match,priority:1;check:chk_gvba_iam_policies_subject,user_id IS NOT NULL OR role_id IS NOT NULL;comment:用户标识"`
	RoleID    *string    `gorm:"column:role_id;size:64;index:idx_gvba_iam_policies_role_match,priority:1;comment:角色标识"`
	Domain    string     `gorm:"column:domain;size:191;not null;default:'';index:idx_gvba_iam_policies_user_match,priority:2;index:idx_gvba_iam_policies_role_match,priority:2;comment:策略域"`
	Method    string     `gorm:"column:method;size:16;not null;index:idx_gvba_iam_policies_user_match,priority:3;index:idx_gvba_iam_policies_role_match,priority:3;comment:请求方法"`
	Path      string     `gorm:"column:path;size:255;not null;index:idx_gvba_iam_policies_user_match,priority:4;index:idx_gvba_iam_policies_role_match,priority:4;comment:请求路径"`
	Effect    string     `gorm:"column:effect;size:16;not null;default:deny;check:chk_gvba_iam_policies_effect,effect IN ('allow','deny');comment:策略效果"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (IAMPolicy) TableName() string { return "gvba_iam_policies" }

type IAMDataScope struct {
	ID        uint64     `gorm:"column:id;primaryKey;autoIncrement;comment:数据范围标识"`
	TenantID  string     `gorm:"column:tenant_id;size:64;not null;default:default;index:idx_gvba_iam_data_scopes_tenant,priority:1;comment:租户标识"`
	OrgID     *string    `gorm:"column:org_id;size:64;index:idx_gvba_iam_data_scopes_tenant,priority:2;comment:组织标识"`
	UserID    *uint64    `gorm:"column:user_id;index:idx_gvba_iam_data_scopes_user,priority:1;check:chk_gvba_iam_data_scopes_subject,user_id IS NOT NULL OR role_id IS NOT NULL;comment:用户标识"`
	RoleID    *string    `gorm:"column:role_id;size:64;index:idx_gvba_iam_data_scopes_role,priority:1;comment:角色标识"`
	Domain    string     `gorm:"column:domain;size:191;not null;default:'';index:idx_gvba_iam_data_scopes_user,priority:2;index:idx_gvba_iam_data_scopes_role,priority:2;comment:作用域域"`
	Resource  string     `gorm:"column:resource;size:191;not null;index:idx_gvba_iam_data_scopes_user,priority:3;index:idx_gvba_iam_data_scopes_role,priority:3;comment:资源名称"`
	Scope     string     `gorm:"column:scope;size:32;not null;check:chk_gvba_iam_data_scopes_scope,scope IN ('all','own','org','custom');comment:数据范围"`
	IDs       JSONValue  `gorm:"column:ids;not null;comment:资源标识集合"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (IAMDataScope) TableName() string { return "gvba_iam_data_scopes" }
