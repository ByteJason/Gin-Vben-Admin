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

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
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
	return s.findUser(ctx, id, false)
}

// FindUserForAuthorization reads the account and its relationships from the
// primary so a just-disabled user cannot be re-authorized by a lagging replica.
func (s *GORMStore) FindUserForAuthorization(ctx context.Context, id string) (domain.User, error) {
	return s.findUser(ctx, id, true)
}

func (s *GORMStore) findUser(ctx context.Context, id string, authorization bool) (domain.User, error) {
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
	database := s.read(ctx)
	if authorization {
		database = s.write(ctx)
	}
	if err := database.Table("users").Where("tenant_id = ? AND id = ?", tenantID, numericID).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.User{}, domain.ErrResourceNotFound
		}
		return domain.User{}, ErrStoreUnavailable
	}
	roles, err := s.roleIDsFrom(database, tenantID, numericID)
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

// CreateUser inserts a profile and its already-hashed credential on the
// primary endpoint. Tenant-local unique indexes remain the race authority;
// driver details are collapsed to the stable domain conflict sentinel.
func (s *GORMStore) CreateUser(ctx context.Context, user domain.User) (domain.User, error) {
	if strings.TrimSpace(user.PasswordHash) == "" {
		return domain.User{}, domain.ErrInvalidUser
	}
	if s == nil || s.db == nil {
		return domain.User{}, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.User{}, err
	}
	if user.TenantID != "" && user.TenantID != scope.TenantID && !scope.PlatformAdmin {
		return domain.User{}, tenant.ErrCrossTenant
	}
	if !scope.PlatformAdmin && scope.Organization != "" && user.OrgID != scope.Organization {
		return domain.User{}, tenant.ErrOrganizationDenied
	}
	user.TenantID = scope.TenantID
	user, err = user.NormalizeProfile()
	if err != nil {
		return domain.User{}, err
	}
	values, err := profileUpdateValues(user)
	if err != nil {
		return domain.User{}, mapUserProfileError(err)
	}
	values["tenant_id"] = scope.TenantID
	values["org_id"] = nullableString(user.OrgID)
	values["password_hash"] = user.PasswordHash
	values["status"] = statusValue(user.Active)
	result := s.write(ctx).Table("users").Create(values)
	if err := mapUserWriteError(result.Error); err != nil {
		return domain.User{}, err
	}
	var row userRow
	if err := s.write(ctx).Table("users").Where("tenant_id = ? AND username_normalized = ?", scope.TenantID, user.UsernameNormalized).Take(&row).Error; err != nil {
		return domain.User{}, ErrStoreUnavailable
	}
	return row.toDomain(nil), nil
}

// UpdateUser replaces only the profile/status fields selected by the
// application service. Passwords and role relations are intentionally outside
// this slice and remain untouched.
func (s *GORMStore) UpdateUser(ctx context.Context, user domain.User) (domain.User, error) {
	numericID, err := numericID(user.ID)
	if err != nil {
		return domain.User{}, err
	}
	if s == nil || s.db == nil {
		return domain.User{}, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.User{}, err
	}
	if user.TenantID != "" && user.TenantID != scope.TenantID && !scope.PlatformAdmin {
		return domain.User{}, tenant.ErrCrossTenant
	}
	if !scope.PlatformAdmin && scope.Organization != "" && user.OrgID != scope.Organization {
		return domain.User{}, tenant.ErrOrganizationDenied
	}
	user.TenantID = scope.TenantID
	user, err = user.NormalizeProfile()
	if err != nil {
		return domain.User{}, err
	}
	values, err := profileUpdateValues(user)
	if err != nil {
		return domain.User{}, mapUserProfileError(err)
	}
	values["org_id"] = nullableString(user.OrgID)
	values["status"] = statusValue(user.Active)
	result := s.write(ctx).Table("users").Where("tenant_id = ? AND id = ?", scope.TenantID, numericID).Updates(values)
	if err := mapUserWriteError(result.Error); err != nil {
		return domain.User{}, err
	}
	if result.RowsAffected == 0 {
		return domain.User{}, domain.ErrResourceNotFound
	}
	return s.FindUser(ctx, user.ID)
}

// SoftDeleteUser changes only the status column on the primary endpoint. The
// existing row is read first so an already-disabled account remains an
// idempotent success and credentials/relations are never rewritten.
func (s *GORMStore) SoftDeleteUser(ctx context.Context, id string) (domain.User, error) {
	numericID, err := numericID(id)
	if err != nil {
		return domain.User{}, err
	}
	if s == nil || s.db == nil {
		return domain.User{}, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.User{}, err
	}
	var row userRow
	if err := s.write(ctx).Table("users").Where("tenant_id = ? AND id = ?", scope.TenantID, numericID).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.User{}, domain.ErrResourceNotFound
		}
		return domain.User{}, ErrStoreUnavailable
	}
	if !scope.PlatformAdmin && scope.Organization != "" && stringValue(row.OrgID) != scope.Organization {
		return domain.User{}, tenant.ErrOrganizationDenied
	}
	if row.Status != "disabled" {
		result := s.write(ctx).Table("users").Where("tenant_id = ? AND id = ?", scope.TenantID, numericID).Updates(map[string]any{"status": "disabled"})
		if err := mapUserWriteError(result.Error); err != nil {
			return domain.User{}, err
		}
		if result.RowsAffected == 0 {
			// The row may have disappeared between the read and the update;
			// do not turn that race into a false successful deletion.
			return domain.User{}, domain.ErrResourceNotFound
		}
	}
	row.Status = "disabled"
	roles, err := s.roleIDs(ctx, scope.TenantID, numericID)
	if err != nil {
		return domain.User{}, err
	}
	return row.toDomain(roles), nil
}

// UpdateUserPassword changes only password_hash and password_changed_at on
// the primary endpoint. Organization predicates are applied for scoped
// administrators; profile, status, login metadata, and role relations are
// deliberately excluded from this write.
func (s *GORMStore) UpdateUserPassword(ctx context.Context, id, passwordHash string, changedAt time.Time) (domain.User, error) {
	numericID, err := numericID(id)
	if err != nil {
		return domain.User{}, err
	}
	if strings.TrimSpace(passwordHash) == "" || changedAt.IsZero() {
		return domain.User{}, domain.ErrInvalidUser
	}
	if s == nil || s.db == nil {
		return domain.User{}, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.User{}, err
	}
	query := s.write(ctx).Table("users").Where("tenant_id = ? AND id = ?", scope.TenantID, numericID)
	if !scope.PlatformAdmin && scope.Organization != "" {
		query = query.Where("org_id = ?", scope.Organization)
	}
	var row userRow
	queryResult := query.Take(&row)
	if queryResult.Error != nil {
		if errors.Is(queryResult.Error, gorm.ErrRecordNotFound) {
			return domain.User{}, domain.ErrResourceNotFound
		}
		return domain.User{}, ErrStoreUnavailable
	}
	changedAt = changedAt.UTC()
	updateQuery := s.write(ctx).Table("users").Where("tenant_id = ? AND id = ?", scope.TenantID, numericID)
	if !scope.PlatformAdmin && scope.Organization != "" {
		updateQuery = updateQuery.Where("org_id = ?", scope.Organization)
	}
	result := updateQuery.Updates(map[string]any{
		"password_hash":       passwordHash,
		"password_changed_at": changedAt,
	})
	if err := mapUserWriteError(result.Error); err != nil {
		return domain.User{}, err
	}
	if result.RowsAffected == 0 {
		return domain.User{}, domain.ErrResourceNotFound
	}
	row.PasswordHash = passwordHash
	row.PasswordChangedAt = &changedAt
	roles, err := s.roleIDs(ctx, scope.TenantID, numericID)
	if err != nil {
		return domain.User{}, err
	}
	return row.toDomain(roles), nil
}

// UpdateUserStatus is the single-item adapter used by the application
// fallback seam. The bulk implementation keeps the SQL behavior identical
// for one and many requested changes.
func (s *GORMStore) UpdateUserStatus(ctx context.Context, change domain.UserStatusChange) (domain.User, error) {
	updated, err := s.UpdateUserStatuses(ctx, []domain.UserStatusChange{change})
	if err != nil {
		return domain.User{}, err
	}
	id := strings.TrimSpace(change.ID)
	user, ok := updated[id]
	if !ok {
		return domain.User{}, domain.ErrResourceNotFound
	}
	return user, nil
}

// UpdateUserStatuses changes only users.status in one primary transaction.
// Tenant and organization predicates are applied to every row; absent or
// out-of-organization rows are omitted so callers receive a stable not_found
// result without an existence oracle. Credentials, profile columns, and role
// relations are never part of this mutation.
func (s *GORMStore) UpdateUserStatuses(ctx context.Context, changes []domain.UserStatusChange) (map[string]domain.User, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	normalized := make([]domain.UserStatusChange, len(changes))
	seen := make(map[string]struct{}, len(changes))
	for index, change := range changes {
		id := strings.TrimSpace(change.ID)
		if id == "" {
			return nil, domain.ErrInvalidUser
		}
		if _, exists := seen[id]; exists {
			return nil, domain.ErrInvalidUser
		}
		if _, err := numericID(id); err != nil {
			return nil, domain.ErrInvalidUser
		}
		seen[id] = struct{}{}
		normalized[index] = domain.UserStatusChange{ID: id, Active: change.Active}
	}
	if len(normalized) == 0 {
		return map[string]domain.User{}, nil
	}
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	updated := make(map[string]domain.User, len(normalized))
	err = s.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		for _, change := range normalized {
			numeric, parseErr := numericID(change.ID)
			if parseErr != nil {
				return domain.ErrInvalidUser
			}
			var row userRow
			query := tx.Table("users").Where("tenant_id = ? AND id = ?", scope.TenantID, numeric)
			queryResult := query.Take(&row)
			if queryResult.Error != nil {
				if errors.Is(queryResult.Error, gorm.ErrRecordNotFound) {
					continue
				}
				return ErrStoreUnavailable
			}
			if !scope.PlatformAdmin && scope.Organization != "" && stringValue(row.OrgID) != scope.Organization {
				continue
			}
			desired := statusValue(change.Active)
			if row.Status != desired {
				result := tx.Table("users").Where("tenant_id = ? AND id = ?", scope.TenantID, numeric).Updates(map[string]any{"status": desired})
				if result.Error != nil {
					return ErrStoreUnavailable
				}
				if result.RowsAffected == 0 {
					return domain.ErrResourceNotFound
				}
				row.Status = desired
			}
			updated[change.ID] = row.toDomain(nil)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrInvalidUser) || errors.Is(err, domain.ErrResourceNotFound) || errors.Is(err, tenant.ErrOrganizationDenied) || errors.Is(err, tenant.ErrCrossTenant) {
			return nil, err
		}
		return nil, ErrStoreUnavailable
	}
	// The status transaction intentionally avoids relation writes. Enriching
	// successful rows from the read-side relation table preserves the response
	// shape for callers without changing the mutation boundary.
	for id, user := range updated {
		numeric, parseErr := numericID(id)
		if parseErr != nil {
			continue
		}
		roles, roleErr := s.roleIDs(ctx, scope.TenantID, numeric)
		if roleErr != nil {
			return nil, roleErr
		}
		user.RoleIDs = roles
		updated[id] = user
	}
	return updated, nil
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

// ListUsersPage keeps user collection filtering/counting in the read endpoint
// while preserving the tenant predicate for every query. The sort column is
// selected from the normalized domain allowlist and never interpolates raw
// request input.
func (s *GORMStore) ListUsersPage(ctx context.Context, filter domain.UserListQuery) (domain.UserPage, error) {
	filter, err := filter.Normalize()
	if err != nil {
		return domain.UserPage{}, err
	}
	if s == nil || s.db == nil {
		return domain.UserPage{}, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.UserPage{}, err
	}
	if !scope.PlatformAdmin && scope.Organization != "" {
		if filter.OrgID != "" && filter.OrgID != scope.Organization {
			return domain.UserPage{}, tenant.ErrOrganizationDenied
		}
		filter.OrgID = scope.Organization
	}
	tenantID := scope.TenantID
	base := s.read(ctx).Table("users").Where("tenant_id = ?", tenantID)
	if filter.Keyword != "" {
		pattern := "%" + strings.ToLower(filter.Keyword) + "%"
		base = base.Where("LOWER(username) LIKE ? OR LOWER(COALESCE(nickname, '')) LIKE ? OR LOWER(COALESCE(email, '')) LIKE ?", pattern, pattern, pattern)
	}
	switch filter.Status {
	case "active":
		base = base.Where("status = ?", "active")
	case "disabled":
		base = base.Where("status <> ?", "active")
	}
	if filter.RoleID != "" {
		base = base.Where("EXISTS (SELECT 1 FROM user_roles ur WHERE ur.tenant_id = users.tenant_id AND ur.user_id = users.id AND ur.role_id = ?)", filter.RoleID)
	}
	if filter.OrgID != "" {
		base = base.Where("org_id = ?", filter.OrgID)
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return domain.UserPage{}, ErrStoreUnavailable
	}
	sortKey, descending := filter.SortKey()
	sortColumns := map[string]string{
		"id": "id", "username": "username", "displayName": "COALESCE(nickname, username)",
		"email": "email", "lastLoginAt": "last_login_at", "orgId": "org_id",
	}
	sortColumn := sortColumns[sortKey]
	order := sortColumn + " ASC"
	if descending {
		order = sortColumn + " DESC"
	}
	var rows []userRow
	offset := (filter.Page - 1) * filter.PageSize
	if err := base.Order(order).Order("id ASC").Limit(filter.PageSize).Offset(offset).Find(&rows).Error; err != nil {
		return domain.UserPage{}, ErrStoreUnavailable
	}
	items := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		roles, roleErr := s.roleIDs(ctx, tenantID, row.ID)
		if roleErr != nil {
			return domain.UserPage{}, roleErr
		}
		items = append(items, row.toDomain(roles))
	}
	return domain.UserPage{Items: items, Total: int(total), Page: filter.Page, PageSize: filter.PageSize}, nil
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
	permissionIDs, err := s.rolePermissionIDs(ctx, tenantID, id)
	if err != nil {
		return domain.Role{}, err
	}
	role := row.toDomain(nil)
	role.PermissionIDs = permissionIDs
	return role, nil
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
	return s.upsert(ctx, "roles", map[string]any{"tenant_id": tenantID, "id": role.ID}, map[string]any{"tenant_id": tenantID, "id": role.ID, "name": role.Name, "status": status, "data_scope": role.DataScope, "org_id": nullableString(role.OrgID)})
}

// AssignRoleUsers replaces one role's user_roles rows in a primary transaction.
// Every user and relationship predicate carries the tenant boundary; an
// organization-scoped caller only replaces members in that organization.
func (s *GORMStore) AssignRoleUsers(ctx context.Context, roleID string, userIDs []string) (domain.Role, error) {
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return domain.Role{}, domain.ErrInvalidUser
	}
	if len(userIDs) > 100 {
		return domain.Role{}, domain.ErrInvalidUser
	}
	normalized := make([]string, len(userIDs))
	seen := make(map[string]struct{}, len(userIDs))
	numericIDs := make([]uint64, len(userIDs))
	for index, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			return domain.Role{}, domain.ErrInvalidUser
		}
		if _, exists := seen[userID]; exists {
			return domain.Role{}, domain.ErrInvalidUser
		}
		numeric, err := numericID(userID)
		if err != nil {
			return domain.Role{}, domain.ErrInvalidUser
		}
		seen[userID] = struct{}{}
		normalized[index] = userID
		numericIDs[index] = numeric
	}
	if s == nil || s.db == nil {
		return domain.Role{}, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Role{}, err
	}
	var updated domain.Role
	err = s.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var roleRowValue roleRow
		roleQuery := tx.Table("roles").Where("tenant_id = ? AND id = ?", scope.TenantID, roleID)
		queryResult := roleQuery.Take(&roleRowValue)
		if queryResult.Error != nil {
			if errors.Is(queryResult.Error, gorm.ErrRecordNotFound) {
				return domain.ErrResourceNotFound
			}
			return ErrStoreUnavailable
		}
		if !scope.PlatformAdmin && scope.Organization != "" && stringValue(roleRowValue.OrgID) != "" && stringValue(roleRowValue.OrgID) != scope.Organization {
			return tenant.ErrOrganizationDenied
		}
		updated = roleRowValue.toDomain(nil)

		var users []userRow
		if len(numericIDs) > 0 {
			userQuery := tx.Table("users").Where("tenant_id = ? AND id IN ?", scope.TenantID, numericIDs)
			if !scope.PlatformAdmin && scope.Organization != "" {
				userQuery = userQuery.Where("org_id = ?", scope.Organization)
			}
			if result := userQuery.Find(&users); result.Error != nil {
				return ErrStoreUnavailable
			}
			if len(users) != len(numericIDs) {
				return domain.ErrResourceNotFound
			}
		}

		deleteSQL := "DELETE FROM user_roles WHERE tenant_id = ? AND role_id = ?"
		deleteArgs := []any{scope.TenantID, roleID}
		if !scope.PlatformAdmin && scope.Organization != "" {
			deleteSQL += " AND org_id = ?"
			deleteArgs = append(deleteArgs, scope.Organization)
		}
		if result := tx.Exec(deleteSQL, deleteArgs...); result.Error != nil {
			return ErrStoreUnavailable
		}
		for _, user := range users {
			if result := tx.Table("user_roles").Create(map[string]any{
				"tenant_id": scope.TenantID,
				"user_id":   user.ID,
				"role_id":   roleID,
				"org_id":    nullableString(stringValue(user.OrgID)),
			}); result.Error != nil {
				return ErrStoreUnavailable
			}
		}
		updated.UserIDs = append([]string(nil), normalized...)
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) || errors.Is(err, domain.ErrInvalidUser) || errors.Is(err, tenant.ErrOrganizationDenied) || errors.Is(err, tenant.ErrCrossTenant) {
			return domain.Role{}, err
		}
		return domain.Role{}, ErrStoreUnavailable
	}
	return updated, nil
}

// AssignRolePermissions replaces role policy rows in one primary transaction.
// iam_policies remains the canonical relation store; permissions supply the
// method/path projection used by the authorizer.
func (s *GORMStore) AssignRolePermissions(ctx context.Context, roleID string, permissionIDs []string) (domain.Role, error) {
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return domain.Role{}, domain.ErrInvalidPolicy
	}
	if len(permissionIDs) > 200 {
		return domain.Role{}, domain.ErrInvalidPolicy
	}
	normalized := make([]string, len(permissionIDs))
	seen := make(map[string]struct{}, len(permissionIDs))
	for index, permissionID := range permissionIDs {
		permissionID = strings.TrimSpace(permissionID)
		if permissionID == "" || len(permissionID) > 128 {
			return domain.Role{}, domain.ErrInvalidPolicy
		}
		if _, exists := seen[permissionID]; exists {
			return domain.Role{}, domain.ErrInvalidPolicy
		}
		seen[permissionID] = struct{}{}
		normalized[index] = permissionID
	}
	if s == nil || s.db == nil {
		return domain.Role{}, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Role{}, err
	}
	var updated domain.Role
	err = s.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var roleRowValue roleRow
		roleQuery := tx.Table("roles").Where("tenant_id = ? AND id = ?", scope.TenantID, roleID)
		if result := roleQuery.Take(&roleRowValue); result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return domain.ErrResourceNotFound
			}
			return ErrStoreUnavailable
		}
		if !scope.PlatformAdmin && scope.Organization != "" && stringValue(roleRowValue.OrgID) != "" && stringValue(roleRowValue.OrgID) != scope.Organization {
			return tenant.ErrOrganizationDenied
		}

		type permissionRelationRow struct {
			ID     string
			Method string
			Path   string
			OrgID  *string `gorm:"column:org_id"`
		}
		var permissions []permissionRelationRow
		if len(normalized) > 0 {
			permissionQuery := tx.Table("permissions").Select("id, method, path, org_id").Where("tenant_id = ? AND id IN ?", scope.TenantID, normalized)
			if result := permissionQuery.Find(&permissions); result.Error != nil {
				return ErrStoreUnavailable
			}
			if len(permissions) != len(normalized) {
				return domain.ErrResourceNotFound
			}
		}
		byID := make(map[string]permissionRelationRow, len(permissions))
		for _, permission := range permissions {
			if !scope.PlatformAdmin && scope.Organization != "" && stringValue(permission.OrgID) != "" && stringValue(permission.OrgID) != scope.Organization {
				return tenant.ErrOrganizationDenied
			}
			byID[permission.ID] = permission
		}

		deleteSQL := "DELETE FROM iam_policies WHERE tenant_id = ? AND role_id = ?"
		deleteArgs := []any{scope.TenantID, roleID}
		if scope.Organization == "" {
			deleteSQL += " AND org_id IS NULL"
		} else {
			deleteSQL += " AND org_id = ?"
			deleteArgs = append(deleteArgs, scope.Organization)
		}
		if result := tx.Exec(deleteSQL, deleteArgs...); result.Error != nil {
			return ErrStoreUnavailable
		}
		for _, permissionID := range normalized {
			permission := byID[permissionID]
			values := map[string]any{
				"tenant_id": scope.TenantID, "role_id": roleID, "domain": scope.TenantID,
				"method": permission.Method, "path": permission.Path, "effect": domain.EffectAllow,
				"org_id": nullableString(scope.Organization),
			}
			if result := tx.Table("iam_policies").Create(values); result.Error != nil {
				return ErrStoreUnavailable
			}
		}
		updated = roleRowValue.toDomain(nil)
		updated.PermissionIDs = append([]string(nil), normalized...)
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) || errors.Is(err, domain.ErrInvalidPolicy) || errors.Is(err, tenant.ErrOrganizationDenied) || errors.Is(err, tenant.ErrCrossTenant) {
			return domain.Role{}, err
		}
		return domain.Role{}, ErrStoreUnavailable
	}
	return updated, nil
}

// AssignRoleDataScopes replaces one role's data-scope rows in a primary
// transaction. The tenant/org predicate is carried by both delete and
// insert paths so a scoped administrator cannot alter another organization.
func (s *GORMStore) AssignRoleDataScopes(ctx context.Context, roleID string, bindings []domain.RoleDataScopeBinding) (domain.Role, error) {
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return domain.Role{}, domain.ErrInvalidDataScope
	}
	normalized, err := domain.NormalizeRoleDataScopeBindings(bindings)
	if err != nil {
		return domain.Role{}, err
	}
	if s == nil || s.db == nil {
		return domain.Role{}, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Role{}, err
	}
	var updated domain.Role
	err = s.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var roleRowValue roleRow
		roleQuery := tx.Table("roles").Where("tenant_id = ? AND id = ?", scope.TenantID, roleID)
		if result := roleQuery.Take(&roleRowValue); result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return domain.ErrResourceNotFound
			}
			return ErrStoreUnavailable
		}
		if !scope.PlatformAdmin && scope.Organization != "" && stringValue(roleRowValue.OrgID) != "" && stringValue(roleRowValue.OrgID) != scope.Organization {
			return tenant.ErrOrganizationDenied
		}
		deleteSQL := "DELETE FROM iam_data_scopes WHERE tenant_id = ? AND role_id = ?"
		deleteArgs := []any{scope.TenantID, roleID}
		if scope.Organization == "" {
			deleteSQL += " AND org_id IS NULL"
		} else {
			deleteSQL += " AND org_id = ?"
			deleteArgs = append(deleteArgs, scope.Organization)
		}
		if result := tx.Exec(deleteSQL, deleteArgs...); result.Error != nil {
			return ErrStoreUnavailable
		}
		for _, binding := range normalized {
			if err := domain.ValidateDataScope(domain.DataScope{RoleID: roleID, Domain: scope.TenantID, Resource: binding.Resource, Scope: binding.Scope, IDs: binding.IDs}); err != nil {
				return err
			}
			ids, marshalErr := json.Marshal(binding.IDs)
			if marshalErr != nil {
				return marshalErr
			}
			values := map[string]any{
				"tenant_id": scope.TenantID, "role_id": roleID, "domain": scope.TenantID,
				"resource": binding.Resource, "scope": binding.Scope, "ids": ids,
				"org_id": nullableString(scope.Organization),
			}
			if result := tx.Table("iam_data_scopes").Create(values); result.Error != nil {
				return ErrStoreUnavailable
			}
		}
		roleScope := domain.ScopeOwn
		if len(normalized) == 1 {
			roleScope = normalized[0].Scope
		} else if len(normalized) > 1 {
			roleScope = domain.ScopeCustom
		}
		if result := tx.Table("roles").Where("tenant_id = ? AND id = ?", scope.TenantID, roleID).Updates(map[string]any{"data_scope": roleScope}); result.Error != nil {
			return ErrStoreUnavailable
		}
		updated = roleRowValue.toDomain(nil)
		updated.DataScope = roleScope
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) || errors.Is(err, domain.ErrInvalidDataScope) || errors.Is(err, tenant.ErrOrganizationDenied) || errors.Is(err, tenant.ErrCrossTenant) {
			return domain.Role{}, err
		}
		return domain.Role{}, ErrStoreUnavailable
	}
	return updated, nil
}

func (s *GORMStore) ListRoles(ctx context.Context) ([]domain.Role, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	var rows []roleRow
	if err := scopedRoleListQuery(s.read(ctx).Table("roles"), scope).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	out := make([]domain.Role, 0, len(rows))
	for _, row := range rows {
		role := row.toDomain(nil)
		permissionIDs, permissionErr := s.rolePermissionIDs(ctx, scope.TenantID, row.ID)
		if permissionErr != nil {
			return nil, permissionErr
		}
		role.PermissionIDs = permissionIDs
		out = append(out, role)
	}
	return out, nil
}

func scopedRoleListQuery(query *gorm.DB, scope tenant.Context) *gorm.DB {
	query = query.Where("tenant_id = ?", scope.TenantID)
	if !scope.PlatformAdmin && scope.Organization != "" {
		query = query.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
	}
	return query
}

func (s *GORMStore) SaveMenu(ctx context.Context, menu domain.Menu) error {
	if s == nil || s.db == nil {
		return ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	menu, err = menu.NormalizeMenu()
	if err != nil {
		return err
	}
	if menu.TenantID != "" && menu.TenantID != scope.TenantID && !scope.PlatformAdmin {
		return tenant.ErrCrossTenant
	}
	if menu.OrgID != "" && !scope.PlatformAdmin && scope.Organization != "" && menu.OrgID != scope.Organization {
		return tenant.ErrOrganizationDenied
	}
	if menu.OrgID == "" {
		menu.OrgID = scope.Organization
	}
	return s.upsert(ctx, "menus", map[string]any{"tenant_id": scope.TenantID, "id": menu.ID}, map[string]any{
		"tenant_id": scope.TenantID, "org_id": nullableString(menu.OrgID), "id": menu.ID,
		"parent_id": nullableString(menu.ParentID), "name": menu.Name, "path": menu.Path,
		"menu_type": string(menu.Type), "component": nullableString(menu.Component), "redirect": nullableString(menu.Redirect),
		"icon": nullableString(menu.Icon), "permission": nullableString(menu.Permission), "sort_order": menu.Sort,
		"visible": menu.Visible, "status": statusValue(menu.Active), "keep_alive": menu.KeepAlive, "external": menu.External,
	})
}

func (s *GORMStore) ListMenus(ctx context.Context) ([]domain.Menu, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	var rows []menuRow
	query := s.read(ctx).Table("menus").Where("tenant_id = ?", scope.TenantID)
	if !scope.PlatformAdmin && scope.Organization != "" {
		query = query.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
	}
	if err := query.Order("sort_order ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	out := make([]domain.Menu, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toDomain())
	}
	return out, nil
}

func (s *GORMStore) FindMenu(ctx context.Context, id string) (domain.Menu, error) {
	if s == nil || s.db == nil {
		return domain.Menu{}, ErrStoreUnavailable
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.Menu{}, domain.ErrInvalidMenu
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Menu{}, err
	}
	query := s.read(ctx).Table("menus").Where("tenant_id = ? AND id = ?", scope.TenantID, id)
	if !scope.PlatformAdmin && scope.Organization != "" {
		query = query.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
	}
	var row menuRow
	if err := query.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Menu{}, domain.ErrResourceNotFound
		}
		return domain.Menu{}, ErrStoreUnavailable
	}
	return row.toDomain(), nil
}

func (s *GORMStore) DeleteMenu(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return domain.ErrInvalidMenu
	}
	return s.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		where := tx.Table("menus").Where("tenant_id = ? AND id = ?", scope.TenantID, id)
		if !scope.PlatformAdmin && scope.Organization != "" {
			where = where.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
		}
		var count int64
		if result := where.Count(&count); result.Error != nil {
			return ErrStoreUnavailable
		} else if count == 0 {
			return domain.ErrResourceNotFound
		}
		childQuery := tx.Table("menus").Where("tenant_id = ? AND parent_id = ?", scope.TenantID, id)
		if !scope.PlatformAdmin && scope.Organization != "" {
			childQuery = childQuery.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
		}
		if result := childQuery.Count(&count); result.Error != nil {
			return ErrStoreUnavailable
		} else if count > 0 {
			return domain.ErrMenuHasChildren
		}
		if result := where.Delete(&menuRow{}); result.Error != nil {
			return ErrStoreUnavailable
		}
		return nil
	})
}

func (s *GORMStore) ReorderMenus(ctx context.Context, items []domain.MenuOrder) error {
	if s == nil || s.db == nil {
		return ErrStoreUnavailable
	}
	if len(items) == 0 || len(items) > 500 {
		return domain.ErrInvalidMenu
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(items))
	err = s.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		for _, item := range items {
			item.ID = strings.TrimSpace(item.ID)
			item.ParentID = strings.TrimSpace(item.ParentID)
			if item.ID == "" || item.Sort < -1000000 || item.Sort > 1000000 {
				return domain.ErrInvalidMenu
			}
			if _, exists := seen[item.ID]; exists {
				return domain.ErrInvalidMenu
			}
			seen[item.ID] = struct{}{}
			where := tx.Table("menus").Where("tenant_id = ? AND id = ?", scope.TenantID, item.ID)
			if !scope.PlatformAdmin && scope.Organization != "" {
				where = where.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
			}
			var count int64
			if result := where.Count(&count); result.Error != nil {
				return ErrStoreUnavailable
			} else if count == 0 {
				return domain.ErrResourceNotFound
			}
			if item.ParentID != "" {
				parent := tx.Table("menus").Where("tenant_id = ? AND id = ?", scope.TenantID, item.ParentID)
				if !scope.PlatformAdmin && scope.Organization != "" {
					parent = parent.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
				}
				if result := parent.Count(&count); result.Error != nil {
					return ErrStoreUnavailable
				} else if count == 0 {
					return domain.ErrResourceNotFound
				}
			}
			if result := where.Updates(map[string]any{"parent_id": nullableString(item.ParentID), "sort_order": item.Sort}); result.Error != nil {
				return ErrStoreUnavailable
			}
		}
		return nil
	})
	return err
}

func (s *GORMStore) SavePermission(ctx context.Context, permission domain.Permission) error {
	if s == nil || s.db == nil {
		return ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	if permission.TenantID != "" && permission.TenantID != scope.TenantID && !scope.PlatformAdmin {
		return tenant.ErrCrossTenant
	}
	if permission.OrgID != "" && scope.Organization != "" && permission.OrgID != scope.Organization && !scope.PlatformAdmin {
		return tenant.ErrOrganizationDenied
	}
	organization := permission.OrgID
	if organization == "" {
		organization = scope.Organization
	}
	return s.upsert(ctx, "permissions", map[string]any{"tenant_id": scope.TenantID, "id": permission.ID}, map[string]any{
		"tenant_id": scope.TenantID, "org_id": nullableString(organization), "id": permission.ID,
		"name": permission.Name, "method": permission.Method, "path": permission.Path, "status": statusValue(permission.Active),
	})
}

func (s *GORMStore) ListPermissions(ctx context.Context) ([]domain.Permission, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	var rows []permissionRow
	query := s.write(ctx).Table("permissions").Where("tenant_id = ?", scope.TenantID)
	query = organizationReadQuery(query, "org_id", scope)
	if err := query.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	out := make([]domain.Permission, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Permission{
			ID: row.ID, Name: row.Name, Method: row.Method, Path: row.Path,
			TenantID: row.TenantID, OrgID: stringValue(row.OrgID), Active: row.Status == "active",
		})
	}
	return out, nil
}

func (s *GORMStore) SavePolicy(ctx context.Context, policy domain.Policy) error {
	if s == nil || s.db == nil {
		return ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	policy.Domain, err = scopedDomain(policy.Domain, scope.TenantID)
	if err != nil {
		return err
	}
	if policy.OrgID != "" && scope.Organization != "" && policy.OrgID != scope.Organization && !scope.PlatformAdmin {
		return tenant.ErrOrganizationDenied
	}
	organization := policy.OrgID
	if organization == "" {
		organization = scope.Organization
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
		"tenant_id": scope.TenantID, "org_id": nullableString(organization), "user_id": userID,
		"role_id": nullableString(policy.RoleID), "domain": policy.Domain, "method": method, "path": path, "effect": effect,
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
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	var rows []policyRow
	query := s.write(ctx).Table("iam_policies").Where("tenant_id = ?", scope.TenantID)
	query = organizationReadQuery(query, "org_id", scope)
	if err := query.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	out := make([]domain.Policy, 0, len(rows))
	for _, row := range rows {
		policy := domain.Policy{RoleID: row.RoleID.String, Domain: row.Domain, OrgID: row.OrgID.String, Method: row.Method, Path: row.Path, Effect: domain.Effect(row.Effect)}
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
	if err := domain.ValidateDataScope(scope); err != nil {
		return err
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
		"resource": scope.Resource, "scope": scope.Scope, "ids": ids, "org_id": nullableString(scope.OrgID),
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
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	var rows []scopeRow
	query := s.authorizationDataScopesQuery(ctx, scope)
	if err := query.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	out := make([]domain.DataScope, 0, len(rows))
	for _, row := range rows {
		scope := domain.DataScope{RoleID: row.RoleID.String, Domain: row.Domain, OrgID: row.OrgID.String, Resource: row.Resource, Scope: domain.Scope(row.Scope)}
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

// authorizationDataScopesQuery is pinned to the primary because the result is
// an authorization decision. A replica may lag immediately after a role scope
// is narrowed, which must never preserve the former wider access window.
func (s *GORMStore) authorizationDataScopesQuery(ctx context.Context, scope tenant.Context) *gorm.DB {
	query := s.write(ctx).Table("iam_data_scopes").Where("tenant_id = ?", scope.TenantID)
	return organizationReadQuery(query, "org_id", scope)
}

func (s *GORMStore) roleIDs(ctx context.Context, tenantID string, userID uint64) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	return s.roleIDsFrom(s.read(ctx), tenantID, userID)
}

func (s *GORMStore) roleIDsFrom(database *gorm.DB, tenantID string, userID uint64) ([]string, error) {
	if database == nil {
		return nil, ErrStoreUnavailable
	}
	var rows []struct{ RoleID string }
	if err := database.Table("user_roles").Select("role_id").Where("tenant_id = ? AND user_id = ?", tenantID, userID).Order("role_id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	roles := make([]string, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, row.RoleID)
	}
	return roles, nil
}

// ListActiveRoleIDsForUser is the authorization-only role resolver. The
// management repository intentionally exposes disabled assignments, whereas
// authenticated subjects must include only active, in-scope role relations.
func (s *GORMStore) ListActiveRoleIDsForUser(ctx context.Context, userID string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	numericUserID, err := numericID(userID)
	if err != nil {
		return nil, err
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	var rows []activeRoleIDRow
	query := activeRoleIDsQuery(s.write(ctx), scope, numericUserID)
	if err := query.Order("ur.role_id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	roles := make([]string, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, row.RoleID)
	}
	return roles, nil
}

type activeRoleIDRow struct {
	RoleID string `gorm:"column:role_id"`
}

func activeRoleIDsQuery(database *gorm.DB, scope tenant.Context, userID uint64) *gorm.DB {
	query := database.Table("user_roles AS ur").
		Select("ur.role_id").
		Joins("JOIN roles AS r ON r.tenant_id = ur.tenant_id AND r.id = ur.role_id").
		Where("ur.tenant_id = ? AND ur.user_id = ? AND r.status = ?", scope.TenantID, userID, "active")
	if !scope.PlatformAdmin {
		if scope.Organization == "" {
			query = query.Where("r.org_id IS NULL AND ur.org_id IS NULL")
		} else {
			query = query.Where("(r.org_id IS NULL OR r.org_id = ?) AND (ur.org_id IS NULL OR ur.org_id = ?)", scope.Organization, scope.Organization)
		}
	}
	return query
}

func (s *GORMStore) rolePermissionIDs(ctx context.Context, tenantID, roleID string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	query := s.read(ctx).Table("iam_policies AS ip").
		Select("p.id").
		Joins("JOIN permissions AS p ON p.tenant_id = ip.tenant_id AND p.method = ip.method AND p.path = ip.path").
		Where("ip.tenant_id = ? AND ip.role_id = ? AND ip.effect = ? AND p.status = ?", tenantID, roleID, domain.EffectAllow, "active")
	if scope, err := tenant.RequireContext(ctx); err == nil {
		if scope.Organization == "" {
			query = query.Where("ip.org_id IS NULL AND p.org_id IS NULL")
		} else {
			query = query.Where("(ip.org_id = ? OR ip.org_id IS NULL) AND (p.org_id = ? OR p.org_id IS NULL)", scope.Organization, scope.Organization)
		}
	}
	var rows []struct{ ID string }
	if err := query.Order("p.id ASC").Find(&rows).Error; err != nil {
		return nil, ErrStoreUnavailable
	}
	permissionIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		permissionIDs = append(permissionIDs, row.ID)
	}
	return permissionIDs, nil
}

func (s *GORMStore) read(ctx context.Context) *gorm.DB {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Read(ctx)
}

func organizationReadQuery(query *gorm.DB, column string, scope tenant.Context) *gorm.DB {
	if scope.Organization == "" {
		return query.Where(column + " IS NULL")
	}
	return query.Where("("+column+" = ? OR "+column+" IS NULL)", scope.Organization)
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
	PasswordHash       string     `gorm:"column:password_hash"`
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
		PasswordHash:       row.PasswordHash,
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
		"org_id":              nullableString(user.OrgID),
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

func mapUserProfileError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, authdomain.ErrInvalidIdentifier) || errors.Is(err, authdomain.ErrInvalidPhone) {
		return domain.ErrInvalidUser
	}
	return err
}

func mapUserWriteError(err error) error {
	if err == nil {
		return nil
	}
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return domain.ErrUserConflict
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.ErrUserConflict
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return ErrStoreUnavailable
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
	TenantID  string  `gorm:"column:tenant_id"`
	OrgID     *string `gorm:"column:org_id"`
	Name      string
	Status    string
	DataScope string `gorm:"column:data_scope"`
}

func (row roleRow) toDomain(userIDs []string) domain.Role {
	return domain.Role{
		ID: row.ID, Name: row.Name, TenantID: row.TenantID, OrgID: stringValue(row.OrgID),
		Active: row.Status == "active", DataScope: domain.Scope(row.DataScope), UserIDs: append([]string(nil), userIDs...),
	}
}

type menuRow struct {
	ID         string
	TenantID   string         `gorm:"column:tenant_id"`
	OrgID      sql.NullString `gorm:"column:org_id"`
	ParentID   sql.NullString `gorm:"column:parent_id"`
	Name       string
	Path       string
	MenuType   sql.NullString `gorm:"column:menu_type"`
	Component  sql.NullString `gorm:"column:component"`
	Redirect   sql.NullString `gorm:"column:redirect"`
	Icon       sql.NullString `gorm:"column:icon"`
	Permission sql.NullString `gorm:"column:permission"`
	SortOrder  int            `gorm:"column:sort_order"`
	Visible    bool
	Status     string
	KeepAlive  bool `gorm:"column:keep_alive"`
	External   bool
}

func (row menuRow) toDomain() domain.Menu {
	menuType := domain.MenuType(row.MenuType.String)
	if menuType == "" {
		menuType = domain.MenuTypeDirectory
	}
	return domain.Menu{
		ID: row.ID, TenantID: row.TenantID, OrgID: row.OrgID.String, ParentID: row.ParentID.String,
		Name: row.Name, Path: row.Path, Type: menuType, Component: row.Component.String,
		Redirect: row.Redirect.String, Icon: row.Icon.String, Permission: row.Permission.String,
		Sort: row.SortOrder, Visible: row.Visible, Active: row.Status == "active",
		KeepAlive: row.KeepAlive, External: row.External,
	}
}

type permissionRow struct {
	ID       string
	TenantID string  `gorm:"column:tenant_id"`
	OrgID    *string `gorm:"column:org_id"`
	Name     string
	Method   string
	Path     string
	Status   string
}
type policyRow struct {
	UserID sql.NullInt64  `gorm:"column:user_id"`
	RoleID sql.NullString `gorm:"column:role_id"`
	OrgID  sql.NullString `gorm:"column:org_id"`
	Domain string
	Method string
	Path   string
	Effect string
}
type scopeRow struct {
	UserID   sql.NullInt64  `gorm:"column:user_id"`
	RoleID   sql.NullString `gorm:"column:role_id"`
	OrgID    sql.NullString `gorm:"column:org_id"`
	Domain   string
	Resource string
	Scope    string
	IDs      []byte
}

var (
	_ interface {
		FindUser(context.Context, string) (domain.User, error)
		SaveUser(context.Context, domain.User) error
		CreateUser(context.Context, domain.User) (domain.User, error)
		UpdateUser(context.Context, domain.User) (domain.User, error)
		SoftDeleteUser(context.Context, string) (domain.User, error)
		ListUsers(context.Context) ([]domain.User, error)
		ListUsersPage(context.Context, domain.UserListQuery) (domain.UserPage, error)
	} = (*GORMStore)(nil)
)
