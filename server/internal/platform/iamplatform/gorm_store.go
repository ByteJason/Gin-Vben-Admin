// Package iamplatform contains persistence adapters for the IAM domain.
package iamplatform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	domain "example.com/gin-vben-admin/server/internal/domain/iam"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
)

var (
	ErrStoreUnavailable = errors.New("iam persistence store is unavailable")
	ErrInvalidNumericID = errors.New("iam id must be numeric")
)

// GORMStore maps IAM records to the versioned RBAC schema. Reads use the
// configured read endpoint; all mutations use the primary endpoint.
type GORMStore struct{ db *gormdb.Store }

func NewGORMStore(db *gormdb.Store) *GORMStore { return &GORMStore{db: db} }

func tenantID(ctx context.Context) (string, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return "", err
	}
	return scope.TenantID, nil
}

func scopedDomain(value, current string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return current, nil
	}
	if value != current {
		return "", tenant.ErrCrossTenant
	}
	return value, nil
}

func (s *GORMStore) FindUser(ctx context.Context, id string) (domain.User, error) {
	numericID, err := numericID(id)
	if err != nil {
		return domain.User{}, err
	}
	if s == nil || s.db == nil {
		return domain.User{}, ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return domain.User{}, err
	}
	var row userRow
	if err := s.read(ctx).Table("users").Where("tenant_id = ? AND id = ?", tenantID, numericID).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.User{}, domain.ErrResourceNotFound
		}
		return domain.User{}, ErrStoreUnavailable
	}
	roles, err := s.roleIDs(ctx, tenantID, numericID)
	if err != nil {
		return domain.User{}, err
	}
	return row.toDomain(roles), nil
}

func (s *GORMStore) SaveUser(ctx context.Context, user domain.User) error {
	numericID, err := numericID(user.ID)
	if err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return err
	}
	status := "disabled"
	if user.Active {
		status = "active"
	}
	var existing userRow
	if err := s.read(ctx).Table("users").Where("tenant_id = ? AND id = ?", tenantID, numericID).Take(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ErrResourceNotFound
		}
		return ErrStoreUnavailable
	}
	values, err := profileUpdateValues(user)
	if err != nil {
		return err
	}
	values["status"] = status
	result := s.write(ctx).Table("users").Where("tenant_id = ? AND id = ?", tenantID, numericID).Updates(values)
	if result.Error != nil {
		return ErrStoreUnavailable
	}
	if err := s.write(ctx).Exec("DELETE FROM user_roles WHERE tenant_id = ? AND user_id = ?", tenantID, numericID).Error; err != nil {
		return ErrStoreUnavailable
	}
	for _, roleID := range user.RoleIDs {
		if err := s.write(ctx).Table("user_roles").Create(map[string]any{"tenant_id": tenantID, "user_id": numericID, "role_id": roleID}).Error; err != nil {
			return ErrStoreUnavailable
		}
	}
	return nil
}

func (s *GORMStore) ListUsers(ctx context.Context) ([]domain.User, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	var rows []userRow
	if err := s.read(ctx).Table("users").Where("tenant_id = ?", tenantID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	out := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		roles, err := s.roleIDs(ctx, tenantID, row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, row.toDomain(roles))
	}
	return out, nil
}

func (s *GORMStore) FindRole(ctx context.Context, id string) (domain.Role, error) {
	if s == nil || s.db == nil {
		return domain.Role{}, ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return domain.Role{}, err
	}
	var row roleRow
	if err := s.read(ctx).Table("roles").Where("tenant_id = ? AND id = ?", tenantID, id).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Role{}, domain.ErrResourceNotFound
		}
		return domain.Role{}, ErrStoreUnavailable
	}
	return domain.Role{ID: row.ID, Name: row.Name, Active: row.Status == "active", DataScope: domain.Scope(row.DataScope)}, nil
}

func (s *GORMStore) SaveRole(ctx context.Context, role domain.Role) error {
	if s == nil || s.db == nil {
		return ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return err
	}
	status := "disabled"
	if role.Active {
		status = "active"
	}
	return s.upsert(ctx, "roles", map[string]any{"tenant_id": tenantID, "id": role.ID}, map[string]any{"tenant_id": tenantID, "id": role.ID, "name": role.Name, "status": status, "data_scope": role.DataScope})
}

func (s *GORMStore) ListRoles(ctx context.Context) ([]domain.Role, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	var rows []roleRow
	if err := s.read(ctx).Table("roles").Where("tenant_id = ?", tenantID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	out := make([]domain.Role, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Role{ID: row.ID, Name: row.Name, Active: row.Status == "active", DataScope: domain.Scope(row.DataScope)})
	}
	return out, nil
}

func (s *GORMStore) SaveMenu(ctx context.Context, menu domain.Menu) error {
	if s == nil || s.db == nil {
		return ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return err
	}
	return s.upsert(ctx, "menus", map[string]any{"tenant_id": tenantID, "id": menu.ID}, map[string]any{"tenant_id": tenantID, "id": menu.ID, "parent_id": nullableString(menu.ParentID), "name": menu.Name, "path": menu.Path, "visible": menu.Visible, "status": statusValue(menu.Active)})
}

func (s *GORMStore) ListMenus(ctx context.Context) ([]domain.Menu, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	var rows []menuRow
	if err := s.read(ctx).Table("menus").Where("tenant_id = ?", tenantID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	out := make([]domain.Menu, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Menu{ID: row.ID, ParentID: row.ParentID.String, Name: row.Name, Path: row.Path, Visible: row.Visible, Active: row.Status == "active"})
	}
	return out, nil
}

func (s *GORMStore) SavePermission(ctx context.Context, permission domain.Permission) error {
	if s == nil || s.db == nil {
		return ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return err
	}
	return s.upsert(ctx, "permissions", map[string]any{"tenant_id": tenantID, "id": permission.ID}, map[string]any{"tenant_id": tenantID, "id": permission.ID, "name": permission.Name, "method": permission.Method, "path": permission.Path, "status": statusValue(permission.Active)})
}

func (s *GORMStore) ListPermissions(ctx context.Context) ([]domain.Permission, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	var rows []permissionRow
	if err := s.read(ctx).Table("permissions").Where("tenant_id = ?", tenantID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	out := make([]domain.Permission, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Permission{ID: row.ID, Name: row.Name, Method: row.Method, Path: row.Path, Active: row.Status == "active"})
	}
	return out, nil
}

func (s *GORMStore) SavePolicy(ctx context.Context, policy domain.Policy) error {
	if s == nil || s.db == nil {
		return ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return err
	}
	policy.Domain, err = scopedDomain(policy.Domain, tenantID)
	if err != nil {
		return err
	}
	if err := domain.ValidatePolicy(policy); err != nil {
		return err
	}
	var userID any
	if strings.TrimSpace(policy.Subject) != "" {
		id, err := numericID(policy.Subject)
		if err != nil {
			return err
		}
		userID = id
	}
	method := policy.Method
	if strings.TrimSpace(method) == "" {
		method = policy.Action
	}
	path := policy.Path
	if strings.TrimSpace(path) == "" {
		path = policy.Object
	}
	effect := policy.Effect
	if effect == "" {
		effect = domain.EffectDeny
	}
	result := s.write(ctx).Table("iam_policies").Create(map[string]any{
		"tenant_id": tenantID, "user_id": userID, "role_id": nullableString(policy.RoleID), "domain": policy.Domain,
		"method": method, "path": path, "effect": effect,
	})
	if result.Error != nil {
		return ErrStoreUnavailable
	}
	return nil
}

func (s *GORMStore) ListPolicies(ctx context.Context) ([]domain.Policy, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	var rows []policyRow
	if err := s.read(ctx).Table("iam_policies").Where("tenant_id = ?", tenantID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	out := make([]domain.Policy, 0, len(rows))
	for _, row := range rows {
		policy := domain.Policy{RoleID: row.RoleID.String, Domain: row.Domain, Method: row.Method, Path: row.Path, Effect: domain.Effect(row.Effect)}
		if row.UserID.Valid {
			policy.Subject = strconv.FormatInt(row.UserID.Int64, 10)
		}
		out = append(out, policy)
	}
	return out, nil
}

func (s *GORMStore) SaveDataScope(ctx context.Context, scope domain.DataScope) error {
	if s == nil || s.db == nil {
		return ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return err
	}
	scope.Domain, err = scopedDomain(scope.Domain, tenantID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(scope.Resource) == "" || scope.Scope == "" || (strings.TrimSpace(scope.Subject) == "" && strings.TrimSpace(scope.RoleID) == "") {
		return domain.ErrDataScopeNotFound
	}
	ids, err := json.Marshal(scope.IDs)
	if err != nil {
		return err
	}
	var userID any
	if strings.TrimSpace(scope.Subject) != "" {
		userID, err = numericID(scope.Subject)
		if err != nil {
			return err
		}
	}
	result := s.write(ctx).Table("iam_data_scopes").Create(map[string]any{
		"tenant_id": tenantID, "user_id": userID, "role_id": nullableString(scope.RoleID), "domain": scope.Domain,
		"resource": scope.Resource, "scope": scope.Scope, "ids": ids,
	})
	if result.Error != nil {
		return ErrStoreUnavailable
	}
	return nil
}

func (s *GORMStore) ListDataScopes(ctx context.Context) ([]domain.DataScope, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	tenantID, err := tenantID(ctx)
	if err != nil {
		return nil, err
	}
	var rows []scopeRow
	if err := s.read(ctx).Table("iam_data_scopes").Where("tenant_id = ?", tenantID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	out := make([]domain.DataScope, 0, len(rows))
	for _, row := range rows {
		scope := domain.DataScope{RoleID: row.RoleID.String, Domain: row.Domain, Resource: row.Resource, Scope: domain.Scope(row.Scope)}
		if row.UserID.Valid {
			scope.Subject = strconv.FormatInt(row.UserID.Int64, 10)
		}
		if len(row.IDs) > 0 {
			if err := json.Unmarshal(row.IDs, &scope.IDs); err != nil {
				return nil, ErrStoreUnavailable
			}
		}
		out = append(out, scope)
	}
	return out, nil
}

func (s *GORMStore) roleIDs(ctx context.Context, tenantID string, userID uint64) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	var rows []struct{ RoleID string }
	if err := s.read(ctx).Table("user_roles").Select("role_id").Where("tenant_id = ? AND user_id = ?", tenantID, userID).Order("role_id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	roles := make([]string, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, row.RoleID)
	}
	return roles, nil
}

func (s *GORMStore) read(ctx context.Context) *gorm.DB {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Read(ctx)
}

func (s *GORMStore) write(ctx context.Context) *gorm.DB {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Write(ctx)
}

func (s *GORMStore) upsert(ctx context.Context, table string, key, values map[string]any) error {
	db := s.write(ctx)
	if db == nil {
		return ErrStoreUnavailable
	}
	result := db.Table(table).Where(key).Updates(values)
	if result.Error != nil {
		return ErrStoreUnavailable
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var count int64
	if err := db.Table(table).Where(key).Count(&count).Error; err != nil {
		return ErrStoreUnavailable
	}
	if count > 0 {
		return nil
	}
	if err := db.Table(table).Create(values).Error; err != nil {
		return ErrStoreUnavailable
	}
	return nil
}

func numericID(value string) (uint64, error) {
	id, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || id == 0 {
		return 0, ErrInvalidNumericID
	}
	return id, nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func statusValue(active bool) string {
	if active {
		return "active"
	}
	return "disabled"
}

type userRow struct {
	ID                 uint64     `gorm:"column:id"`
	TenantID           string     `gorm:"column:tenant_id"`
	OrgID              *string    `gorm:"column:org_id"`
	Username           string     `gorm:"column:username"`
	UsernameNormalized *string    `gorm:"column:username_normalized"`
	Email              *string    `gorm:"column:email"`
	EmailNormalized    *string    `gorm:"column:email_normalized"`
	Nickname           *string    `gorm:"column:nickname"`
	Avatar             *string    `gorm:"column:avatar"`
	Phone              *string    `gorm:"column:phone"`
	Status             string     `gorm:"column:status"`
	LastLoginIP        *string    `gorm:"column:last_login_ip"`
	LastLoginAt        *time.Time `gorm:"column:last_login_at"`
	PasswordChangedAt  *time.Time `gorm:"column:password_changed_at"`
}

func (row userRow) toDomain(roleIDs []string) domain.User {
	nickname := stringValue(row.Nickname)
	displayName := nickname
	if displayName == "" {
		displayName = row.Username
	}
	return domain.User{
		ID:                 strconv.FormatUint(row.ID, 10),
		Username:           row.Username,
		DisplayName:        displayName,
		UsernameNormalized: stringValue(row.UsernameNormalized),
		Email:              stringValue(row.Email),
		EmailNormalized:    stringValue(row.EmailNormalized),
		Nickname:           nickname,
		Avatar:             stringValue(row.Avatar),
		Phone:              stringValue(row.Phone),
		LastLoginIP:        stringValue(row.LastLoginIP),
		LastLoginAt:        timeValue(row.LastLoginAt),
		PasswordChangedAt:  timeValue(row.PasswordChangedAt),
		TenantID:           row.TenantID,
		OrgID:              stringValue(row.OrgID),
		Active:             row.Status == "active",
		RoleIDs:            append([]string(nil), roleIDs...),
	}
}

func profileUpdateValues(user domain.User) (map[string]any, error) {
	username := strings.TrimSpace(user.Username)
	normalizedUsername, identifierType, err := authdomain.NormalizeIdentifier(username)
	if err != nil || identifierType != authdomain.IdentifierUsername {
		return nil, authdomain.ErrInvalidIdentifier
	}
	phone, err := authdomain.NormalizePhone(user.Phone)
	if err != nil {
		return nil, err
	}
	values := map[string]any{
		"username":            username,
		"username_normalized": normalizedUsername,
		"nickname":            nullableString(firstNonEmpty(user.Nickname, user.DisplayName)),
		"avatar":              nullableString(user.Avatar),
		"phone":               nullableString(phone),
	}
	if email := strings.TrimSpace(user.Email); email != "" {
		normalizedEmail, kind, normalizeErr := authdomain.NormalizeIdentifier(email)
		if normalizeErr != nil || kind != authdomain.IdentifierEmail {
			return nil, authdomain.ErrInvalidIdentifier
		}
		values["email"] = email
		values["email_normalized"] = normalizedEmail
	} else {
		values["email"] = nil
		values["email_normalized"] = nil
	}
	return values, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stringPtr(value string) *string { return &value }

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

type roleRow struct {
	ID        string
	Name      string
	Status    string
	DataScope string `gorm:"column:data_scope"`
}
type menuRow struct {
	ID       string
	ParentID sql.NullString `gorm:"column:parent_id"`
	Name     string
	Path     string
	Visible  bool
	Status   string
}
type permissionRow struct {
	ID     string
	Name   string
	Method string
	Path   string
	Status string
}
type policyRow struct {
	UserID sql.NullInt64  `gorm:"column:user_id"`
	RoleID sql.NullString `gorm:"column:role_id"`
	Domain string
	Method string
	Path   string
	Effect string
}
type scopeRow struct {
	UserID   sql.NullInt64  `gorm:"column:user_id"`
	RoleID   sql.NullString `gorm:"column:role_id"`
	Domain   string
	Resource string
	Scope    string
	IDs      []byte
}

var (
	_ interface {
		FindUser(context.Context, string) (domain.User, error)
		SaveUser(context.Context, domain.User) error
		ListUsers(context.Context) ([]domain.User, error)
	} = (*GORMStore)(nil)
)
