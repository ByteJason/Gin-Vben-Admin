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
	"gorm.io/gorm/clause"
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
	database := s.read(ctx)
	if authorization {
		database = s.write(ctx)
	}
	row, err := gorm.G[userRow](database).Where("tenant_id = ? AND id = ?", tenantID, numericID).Take(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.User{}, domain.ErrResourceNotFound
		}
		return domain.User{}, ErrStoreUnavailable
	}
	// Build the relation query from a fresh resolver session. Reusing the
	// completed user query carries its WHERE/LIMIT statement into `user_roles`.
	// Select the same read/write route explicitly so authorization still pins
	// to the primary while ordinary profile reads retain replica routing.
	roleDatabase := s.read(ctx)
	if authorization {
		roleDatabase = s.write(ctx)
	}
	roles, err := s.roleIDsFrom(roleDatabase, tenantID, numericID)
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
	values, err := profileUpdateValues(user)
	if err != nil {
		return err
	}
	values["status"] = status
	// Profile and role replacement are one logical mutation. Keep both on the
	// primary transaction so a failed relation write cannot leave a partially
	// updated account (and no replica precheck can race a just-created user).
	err = s.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		rows, updateErr := gorm.G[userRow](tx).Where("tenant_id = ? AND id = ?", tenantID, numericID).Set(clause.Assignments(values)).Update(ctx)
		if updateErr != nil {
			return ErrStoreUnavailable
		}
		if rows == 0 {
			return domain.ErrResourceNotFound
		}
		if _, deleteErr := gorm.G[userRoleRow](tx).Where("tenant_id = ? AND user_id = ?", tenantID, numericID).Delete(ctx); deleteErr != nil {
			return ErrStoreUnavailable
		}
		roleRows := make([]userRoleRow, 0, len(user.RoleIDs))
		for _, roleID := range user.RoleIDs {
			roleRows = append(roleRows, userRoleRow{TenantID: tenantID, UserID: numericID, RoleID: roleID})
		}
		if len(roleRows) > 0 {
			if createErr := gorm.G[userRoleRow](tx).CreateInBatches(ctx, &roleRows, 100); createErr != nil {
				return ErrStoreUnavailable
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, domain.ErrResourceNotFound) {
			return err
		}
		return ErrStoreUnavailable
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
	row := userRowFromValues(values)
	if err := mapUserWriteError(gorm.G[userRow](s.write(ctx)).Create(ctx, &row)); err != nil {
		return domain.User{}, err
	}
	row, err = gorm.G[userRow](s.write(ctx)).Where("tenant_id = ? AND username_normalized = ?", scope.TenantID, user.UsernameNormalized).Take(ctx)
	if err != nil {
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
	rows, updateErr := gorm.G[userRow](s.write(ctx)).Where("tenant_id = ? AND id = ?", scope.TenantID, numericID).Set(clause.Assignments(values)).Update(ctx)
	if err := mapUserWriteError(updateErr); err != nil {
		return domain.User{}, err
	}
	if rows == 0 {
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
	row, err := gorm.G[userRow](s.write(ctx)).Where("tenant_id = ? AND id = ?", scope.TenantID, numericID).Take(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.User{}, domain.ErrResourceNotFound
		}
		return domain.User{}, ErrStoreUnavailable
	}
	if !scope.PlatformAdmin && scope.Organization != "" && stringValue(row.OrgID) != scope.Organization {
		return domain.User{}, tenant.ErrOrganizationDenied
	}
	if row.Status != "disabled" {
		rows, updateErr := gorm.G[userRow](s.write(ctx)).Where("tenant_id = ? AND id = ?", scope.TenantID, numericID).Set(clause.Assignments(map[string]any{"status": "disabled"})).Update(ctx)
		if err := mapUserWriteError(updateErr); err != nil {
			return domain.User{}, err
		}
		if rows == 0 {
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
	query := gorm.G[userRow](s.write(ctx)).Where("tenant_id = ? AND id = ?", scope.TenantID, numericID)
	if !scope.PlatformAdmin && scope.Organization != "" {
		query = query.Where("org_id = ?", scope.Organization)
	}
	row, queryErr := query.Take(ctx)
	if queryErr != nil {
		if errors.Is(queryErr, gorm.ErrRecordNotFound) {
			return domain.User{}, domain.ErrResourceNotFound
		}
		return domain.User{}, ErrStoreUnavailable
	}
	changedAt = changedAt.UTC()
	updateQuery := gorm.G[userRow](s.write(ctx)).Where("tenant_id = ? AND id = ?", scope.TenantID, numericID)
	if !scope.PlatformAdmin && scope.Organization != "" {
		updateQuery = updateQuery.Where("org_id = ?", scope.Organization)
	}
	rows, updateErr := updateQuery.Set(clause.Assignments(map[string]any{
		"password_hash":       passwordHash,
		"password_changed_at": changedAt,
	})).Update(ctx)
	if err := mapUserWriteError(updateErr); err != nil {
		return domain.User{}, err
	}
	if rows == 0 {
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
			row, queryErr := gorm.G[userRow](tx).Where("tenant_id = ? AND id = ?", scope.TenantID, numeric).Take(ctx)
			if queryErr != nil {
				if errors.Is(queryErr, gorm.ErrRecordNotFound) {
					continue
				}
				return ErrStoreUnavailable
			}
			if !scope.PlatformAdmin && scope.Organization != "" && stringValue(row.OrgID) != scope.Organization {
				continue
			}
			desired := statusValue(change.Active)
			if row.Status != desired {
				rows, updateErr := gorm.G[userRow](tx).Where("tenant_id = ? AND id = ?", scope.TenantID, numeric).Set(clause.Assignments(map[string]any{"status": desired})).Update(ctx)
				if updateErr != nil {
					return ErrStoreUnavailable
				}
				if rows == 0 {
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
	rows, err := gorm.G[userRow](s.read(ctx)).Where("tenant_id = ?", tenantID).Order("id ASC").Find(ctx)
	if err != nil {
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
	base := gorm.G[userRow](s.read(ctx)).Where("tenant_id = ?", tenantID)
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
	total, err := base.Count(ctx, "*")
	if err != nil {
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
	rows, err = base.Order(order).Order("id ASC").Limit(filter.PageSize).Offset(offset).Find(ctx)
	if err != nil {
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
	row, err := gorm.G[roleRow](s.read(ctx)).Where("tenant_id = ? AND id = ?", tenantID, id).Take(ctx)
	if err != nil {
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
	return s.upsertRole(ctx, roleRow{ID: role.ID, TenantID: tenantID, OrgID: nullableStringPtr(role.OrgID), Name: role.Name, Status: status, DataScope: string(role.DataScope)})
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
		roleRowValue, queryErr := gorm.G[roleRow](tx).Where("tenant_id = ? AND id = ?", scope.TenantID, roleID).Take(ctx)
		if queryErr != nil {
			if errors.Is(queryErr, gorm.ErrRecordNotFound) {
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
			userQuery := gorm.G[userRow](tx).Where("tenant_id = ? AND id IN ?", scope.TenantID, numericIDs)
			if !scope.PlatformAdmin && scope.Organization != "" {
				userQuery = userQuery.Where("org_id = ?", scope.Organization)
			}
			var findErr error
			users, findErr = userQuery.Find(ctx)
			if findErr != nil {
				return ErrStoreUnavailable
			}
			if len(users) != len(numericIDs) {
				return domain.ErrResourceNotFound
			}
		}

		deleteQuery := gorm.G[userRoleRow](tx).Where("tenant_id = ? AND role_id = ?", scope.TenantID, roleID)
		if !scope.PlatformAdmin && scope.Organization != "" {
			deleteQuery = deleteQuery.Where("org_id = ?", scope.Organization)
		}
		if _, deleteErr := deleteQuery.Delete(ctx); deleteErr != nil {
			return ErrStoreUnavailable
		}
		assignments := make([]userRoleRow, 0, len(users))
		for _, user := range users {
			assignments = append(assignments, userRoleRow{TenantID: scope.TenantID, UserID: user.ID, RoleID: roleID, OrgID: nullableStringPtr(stringValue(user.OrgID))})
		}
		if len(assignments) > 0 {
			if createErr := gorm.G[userRoleRow](tx).CreateInBatches(ctx, &assignments, 100); createErr != nil {
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
		roleRowValue, queryErr := gorm.G[roleRow](tx).Where("tenant_id = ? AND id = ?", scope.TenantID, roleID).Take(ctx)
		if queryErr != nil {
			if errors.Is(queryErr, gorm.ErrRecordNotFound) {
				return domain.ErrResourceNotFound
			}
			return ErrStoreUnavailable
		}
		if !scope.PlatformAdmin && scope.Organization != "" && stringValue(roleRowValue.OrgID) != "" && stringValue(roleRowValue.OrgID) != scope.Organization {
			return tenant.ErrOrganizationDenied
		}

		var permissions []permissionRow
		if len(normalized) > 0 {
			permissionQuery := gorm.G[permissionRow](tx).Where("tenant_id = ? AND id IN ?", scope.TenantID, normalized)
			var findErr error
			permissions, findErr = permissionQuery.Find(ctx)
			if findErr != nil {
				return ErrStoreUnavailable
			}
			if len(permissions) != len(normalized) {
				return domain.ErrResourceNotFound
			}
		}
		byID := make(map[string]permissionRow, len(permissions))
		for _, permission := range permissions {
			if !scope.PlatformAdmin && scope.Organization != "" && stringValue(permission.OrgID) != "" && stringValue(permission.OrgID) != scope.Organization {
				return tenant.ErrOrganizationDenied
			}
			byID[permission.ID] = permission
		}

		deleteQuery := gorm.G[iamPolicyWriteRow](tx).Where("tenant_id = ? AND role_id = ?", scope.TenantID, roleID)
		if scope.Organization == "" {
			deleteQuery = deleteQuery.Where("org_id IS NULL")
		} else {
			deleteQuery = deleteQuery.Where("org_id = ?", scope.Organization)
		}
		if _, deleteErr := deleteQuery.Delete(ctx); deleteErr != nil {
			return ErrStoreUnavailable
		}
		policyRows := make([]iamPolicyWriteRow, 0, len(normalized))
		for _, permissionID := range normalized {
			permission := byID[permissionID]
			policyRows = append(policyRows, iamPolicyWriteRow{TenantID: scope.TenantID, RoleID: stringPtr(roleID), Domain: scope.TenantID, Method: permission.Method, Path: permission.Path, Effect: string(domain.EffectAllow), OrgID: nullableStringPtr(scope.Organization)})
		}
		if len(policyRows) > 0 {
			if createErr := gorm.G[iamPolicyWriteRow](tx).CreateInBatches(ctx, &policyRows, 100); createErr != nil {
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
		roleRowValue, queryErr := gorm.G[roleRow](tx).Where("tenant_id = ? AND id = ?", scope.TenantID, roleID).Take(ctx)
		if queryErr != nil {
			if errors.Is(queryErr, gorm.ErrRecordNotFound) {
				return domain.ErrResourceNotFound
			}
			return ErrStoreUnavailable
		}
		if !scope.PlatformAdmin && scope.Organization != "" && stringValue(roleRowValue.OrgID) != "" && stringValue(roleRowValue.OrgID) != scope.Organization {
			return tenant.ErrOrganizationDenied
		}
		deleteQuery := gorm.G[iamDataScopeWriteRow](tx).Where("tenant_id = ? AND role_id = ?", scope.TenantID, roleID)
		if scope.Organization == "" {
			deleteQuery = deleteQuery.Where("org_id IS NULL")
		} else {
			deleteQuery = deleteQuery.Where("org_id = ?", scope.Organization)
		}
		if _, deleteErr := deleteQuery.Delete(ctx); deleteErr != nil {
			return ErrStoreUnavailable
		}
		scopeRows := make([]iamDataScopeWriteRow, 0, len(normalized))
		for _, binding := range normalized {
			if err := domain.ValidateDataScope(domain.DataScope{RoleID: roleID, Domain: scope.TenantID, Resource: binding.Resource, Scope: binding.Scope, IDs: binding.IDs}); err != nil {
				return err
			}
			ids, marshalErr := json.Marshal(binding.IDs)
			if marshalErr != nil {
				return marshalErr
			}
			scopeRows = append(scopeRows, iamDataScopeWriteRow{TenantID: scope.TenantID, RoleID: stringPtr(roleID), Domain: scope.TenantID, Resource: binding.Resource, Scope: string(binding.Scope), IDs: ids, OrgID: nullableStringPtr(scope.Organization)})
		}
		if len(scopeRows) > 0 {
			if createErr := gorm.G[iamDataScopeWriteRow](tx).CreateInBatches(ctx, &scopeRows, 100); createErr != nil {
				return ErrStoreUnavailable
			}
		}
		roleScope := domain.ScopeOwn
		if len(normalized) == 1 {
			roleScope = normalized[0].Scope
		} else if len(normalized) > 1 {
			roleScope = domain.ScopeCustom
		}
		if _, updateErr := gorm.G[roleRow](tx).Where("tenant_id = ? AND id = ?", scope.TenantID, roleID).Set(clause.Assignments(map[string]any{"data_scope": roleScope})).Update(ctx); updateErr != nil {
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
	rows, err := gorm.G[roleRow](s.read(ctx)).Scopes(scopedRoleScope(scope)).Order("id ASC").Find(ctx)
	if err != nil {
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

func scopedRoleScope(scope tenant.Context) func(*gorm.Statement) {
	return func(statement *gorm.Statement) {
		statement.AddClause(clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "tenant_id"}, Value: scope.TenantID},
		}})
		if !scope.PlatformAdmin && scope.Organization != "" {
			statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "(org_id = ? OR org_id IS NULL)", Vars: []any{scope.Organization}}}})
		}
	}
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
	return s.upsertMenu(ctx, menuWriteRow{ID: menu.ID, TenantID: scope.TenantID, OrgID: nullableStringPtr(menu.OrgID), ParentID: nullableStringPtr(menu.ParentID), Name: menu.Name, Path: menu.Path, MenuType: string(menu.Type), Component: nullableStringPtr(menu.Component), Redirect: nullableStringPtr(menu.Redirect), Icon: nullableStringPtr(menu.Icon), Permission: nullableStringPtr(menu.Permission), SortOrder: menu.Sort, Visible: menu.Visible, Status: statusValue(menu.Active), KeepAlive: menu.KeepAlive, External: menu.External})
}

func (s *GORMStore) ListMenus(ctx context.Context) ([]domain.Menu, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	query := gorm.G[menuRow](s.read(ctx)).Where("tenant_id = ?", scope.TenantID)
	if !scope.PlatformAdmin && scope.Organization != "" {
		query = query.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
	}
	rows, err := query.Order("sort_order ASC, id ASC").Find(ctx)
	if err != nil {
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
	query := gorm.G[menuRow](s.read(ctx)).Where("tenant_id = ? AND id = ?", scope.TenantID, id)
	if !scope.PlatformAdmin && scope.Organization != "" {
		query = query.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
	}
	row, err := query.Take(ctx)
	if err != nil {
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
		where := gorm.G[menuRow](tx).Where("tenant_id = ? AND id = ?", scope.TenantID, id)
		if !scope.PlatformAdmin && scope.Organization != "" {
			where = where.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
		}
		count, countErr := where.Count(ctx, "*")
		if countErr != nil {
			return ErrStoreUnavailable
		} else if count == 0 {
			return domain.ErrResourceNotFound
		}
		childQuery := gorm.G[menuRow](tx).Where("tenant_id = ? AND parent_id = ?", scope.TenantID, id)
		if !scope.PlatformAdmin && scope.Organization != "" {
			childQuery = childQuery.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
		}
		count, countErr = childQuery.Count(ctx, "*")
		if countErr != nil {
			return ErrStoreUnavailable
		} else if count > 0 {
			return domain.ErrMenuHasChildren
		}
		if _, deleteErr := where.Delete(ctx); deleteErr != nil {
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
			where := gorm.G[menuRow](tx).Where("tenant_id = ? AND id = ?", scope.TenantID, item.ID)
			if !scope.PlatformAdmin && scope.Organization != "" {
				where = where.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
			}
			count, countErr := where.Count(ctx, "*")
			if countErr != nil {
				return ErrStoreUnavailable
			} else if count == 0 {
				return domain.ErrResourceNotFound
			}
			if item.ParentID != "" {
				parent := gorm.G[menuRow](tx).Where("tenant_id = ? AND id = ?", scope.TenantID, item.ParentID)
				if !scope.PlatformAdmin && scope.Organization != "" {
					parent = parent.Where("(org_id = ? OR org_id IS NULL)", scope.Organization)
				}
				count, countErr = parent.Count(ctx, "*")
				if countErr != nil {
					return ErrStoreUnavailable
				} else if count == 0 {
					return domain.ErrResourceNotFound
				}
			}
			if _, updateErr := where.Set(clause.Assignments(map[string]any{"parent_id": nullableString(item.ParentID), "sort_order": item.Sort})).Update(ctx); updateErr != nil {
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
	return s.upsertPermission(ctx, permissionRow{ID: permission.ID, TenantID: scope.TenantID, OrgID: nullableStringPtr(organization), Name: permission.Name, Method: permission.Method, Path: permission.Path, Status: statusValue(permission.Active)})
}

func (s *GORMStore) ListPermissions(ctx context.Context) ([]domain.Permission, error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	query := gorm.G[permissionRow](s.write(ctx)).Where("tenant_id = ?", scope.TenantID)
	query = query.Scopes(organizationReadScope("org_id", scope))
	rows, err := query.Order("id ASC").Find(ctx)
	if err != nil {
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
	row := iamPolicyWriteRow{TenantID: scope.TenantID, OrgID: nullableStringPtr(organization), UserID: numericPointer(userID), RoleID: nullableStringPtr(policy.RoleID), Domain: policy.Domain, Method: method, Path: path, Effect: string(effect)}
	if err := gorm.G[iamPolicyWriteRow](s.write(ctx)).Create(ctx, &row); err != nil {
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
	query := gorm.G[policyRow](s.write(ctx)).Where("tenant_id = ?", scope.TenantID)
	query = query.Scopes(organizationReadScope("org_id", scope))
	rows, err := query.Order("id ASC").Find(ctx)
	if err != nil {
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
	row := iamDataScopeWriteRow{TenantID: tenantID, UserID: numericPointer(userID), RoleID: nullableStringPtr(scope.RoleID), Domain: scope.Domain, Resource: scope.Resource, Scope: string(scope.Scope), IDs: ids, OrgID: nullableStringPtr(scope.OrgID)}
	if err := gorm.G[iamDataScopeWriteRow](s.write(ctx)).Create(ctx, &row); err != nil {
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
	query := gorm.G[scopeRow](s.write(ctx)).Where("tenant_id = ?", scope.TenantID).Scopes(organizationReadScope("org_id", scope))
	rows, err := query.Order("id ASC").Find(ctx)
	if err != nil {
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
	rows, err := gorm.G[roleIDRow](database).Select("role_id").Where("tenant_id = ? AND user_id = ?", tenantID, userID).Order("role_id ASC").Find(dbContext(database))
	if err != nil {
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
	rows, err := activeRoleIDsGeneric(s.write(ctx), ctx, scope, numericUserID)
	if err != nil {
		return nil, ErrStoreUnavailable
	}
	roles := make([]string, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, row.RoleID)
	}
	return roles, nil
}

func activeRoleIDsGeneric(database *gorm.DB, ctx context.Context, scope tenant.Context, userID uint64) ([]activeRoleIDRow, error) {
	if database == nil {
		return nil, ErrStoreUnavailable
	}
	// GORM's generic JoinTarget is association-oriented and cannot describe
	// these scalar-ID relations without adding ORM associations to the schema
	// models. Build the fixed, identifier-only FROM/JOIN clause as a GORM
	// clause option instead; the terminal query and scan remain gorm.G[T].
	from := clause.From{
		Tables: []clause.Table{{Name: "user_roles AS ur", Raw: true}},
		Joins: []clause.Join{{
			Table: clause.Table{Name: "roles AS r", Raw: true},
			ON: clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "r.tenant_id = ur.tenant_id AND r.id = ur.role_id"},
			}},
		}},
	}
	query := gorm.G[activeRoleIDRow](database, from).Select("ur.role_id").Where("ur.tenant_id = ? AND ur.user_id = ? AND r.status = ?", scope.TenantID, userID, "active")
	if !scope.PlatformAdmin {
		if scope.Organization == "" {
			query = query.Where("r.org_id IS NULL AND ur.org_id IS NULL")
		} else {
			query = query.Where("(r.org_id IS NULL OR r.org_id = ?) AND (ur.org_id IS NULL OR ur.org_id = ?)", scope.Organization, scope.Organization)
		}
	}
	rows, err := query.Order("ur.role_id ASC").Find(ctx)
	if err != nil {
		return nil, ErrStoreUnavailable
	}
	return rows, nil
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
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	from := clause.From{
		Tables: []clause.Table{{Name: "iam_policies AS ip", Raw: true}},
		Joins: []clause.Join{{
			Table: clause.Table{Name: "permissions AS p", Raw: true},
			ON: clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "p.tenant_id = ip.tenant_id AND p.method = ip.method AND p.path = ip.path"},
			}},
		}},
	}
	query := gorm.G[permissionIDRow](s.read(ctx), from).Select("p.id").Where("ip.tenant_id = ? AND ip.role_id = ? AND ip.effect = ? AND p.status = ?", tenantID, roleID, domain.EffectAllow, "active")
	if scope.Organization == "" {
		query = query.Where("ip.org_id IS NULL AND p.org_id IS NULL")
	} else {
		query = query.Where("(ip.org_id = ? OR ip.org_id IS NULL) AND (p.org_id = ? OR p.org_id IS NULL)", scope.Organization, scope.Organization)
	}
	rows, err := query.Order("p.id ASC").Find(ctx)
	if err != nil {
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
	column, ok := organizationColumn(column)
	if !ok {
		// Organization scoping is an internal identifier; fail closed rather
		// than interpolating an arbitrary column into a SQL predicate.
		return query.Where("1 = 0")
	}
	if scope.Organization == "" {
		return query.Where(column + " IS NULL")
	}
	return query.Where("("+column+" = ? OR "+column+" IS NULL)", scope.Organization)
}

func organizationReadScope(column string, scope tenant.Context) func(*gorm.Statement) {
	return func(statement *gorm.Statement) {
		column, ok := organizationColumn(column)
		if !ok {
			statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "1 = 0"}}})
			return
		}
		if scope.Organization == "" {
			statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: column + " IS NULL"}}})
			return
		}
		statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "(" + column + " = ? OR " + column + " IS NULL)", Vars: []any{scope.Organization}}}})
	}
}

// organizationColumn returns the only organization column accepted by the
// scope helpers. Keep this whitelist closed because the value is embedded in
// SQL expressions by both the compatibility and generic query paths.
func organizationColumn(column string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(column)) {
	case "org_id":
		return "org_id", true
	default:
		return "", false
	}
}

func (s *GORMStore) write(ctx context.Context) *gorm.DB {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Write(ctx)
}

func dbContext(db *gorm.DB) context.Context {
	if db != nil && db.Statement != nil && db.Statement.Context != nil {
		return db.Statement.Context
	}
	return context.Background()
}

func (s *GORMStore) upsertRole(ctx context.Context, row roleRow) error {
	return upsertRecord(ctx, s.write(ctx), map[string]any{"tenant_id": row.TenantID, "id": row.ID}, map[string]any{
		"tenant_id": row.TenantID, "id": row.ID, "name": row.Name, "status": row.Status,
		"data_scope": row.DataScope, "org_id": row.OrgID,
	}, &row)
}

func (s *GORMStore) upsertMenu(ctx context.Context, row menuWriteRow) error {
	return upsertRecord(ctx, s.write(ctx), map[string]any{"tenant_id": row.TenantID, "id": row.ID}, map[string]any{
		"tenant_id": row.TenantID, "org_id": row.OrgID, "id": row.ID, "parent_id": row.ParentID,
		"name": row.Name, "path": row.Path, "menu_type": row.MenuType, "component": row.Component,
		"redirect": row.Redirect, "icon": row.Icon, "permission": row.Permission, "sort_order": row.SortOrder,
		"visible": row.Visible, "status": row.Status, "keep_alive": row.KeepAlive, "external": row.External,
	}, &row)
}

func (s *GORMStore) upsertPermission(ctx context.Context, row permissionRow) error {
	return upsertRecord(ctx, s.write(ctx), map[string]any{"tenant_id": row.TenantID, "id": row.ID}, map[string]any{
		"tenant_id": row.TenantID, "org_id": row.OrgID, "id": row.ID, "name": row.Name,
		"method": row.Method, "path": row.Path, "status": row.Status,
	}, &row)
}

// upsertRecord keeps the explicit update-then-create semantics of the legacy
// adapters while making the terminal operations typed. The caller supplies a
// concrete persistence/projection value, so no map-backed Create reaches the
// database.
func upsertRecord[T any](ctx context.Context, db *gorm.DB, key, values map[string]any, record *T) error {
	if db == nil {
		return ErrStoreUnavailable
	}
	query := gorm.G[T](db).Where(key)
	rows, err := query.Set(clause.Assignments(values)).Update(ctx)
	if err != nil {
		return ErrStoreUnavailable
	}
	if rows > 0 {
		return nil
	}
	count, err := query.Count(ctx, "*")
	if err != nil {
		return ErrStoreUnavailable
	}
	if count > 0 {
		return nil
	}
	if err := gorm.G[T](db).Create(ctx, record); err != nil {
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

func nullableStringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func numericPointer(value any) *uint64 {
	switch typed := value.(type) {
	case uint64:
		return &typed
	case *uint64:
		return typed
	default:
		return nil
	}
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

func (userRow) TableName() string { return "users" }

type userRoleRow struct {
	UserID   uint64  `gorm:"column:user_id"`
	RoleID   string  `gorm:"column:role_id"`
	TenantID string  `gorm:"column:tenant_id"`
	OrgID    *string `gorm:"column:org_id"`
}

func (userRoleRow) TableName() string { return "user_roles" }

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

func userRowFromValues(values map[string]any) userRow {
	row := userRow{
		TenantID:     mapString(values["tenant_id"]),
		Username:     mapString(values["username"]),
		PasswordHash: mapString(values["password_hash"]),
		Status:       mapString(values["status"]),
	}
	row.UsernameNormalized = mapStringPtr(values["username_normalized"])
	row.Email = mapStringPtr(values["email"])
	row.EmailNormalized = mapStringPtr(values["email_normalized"])
	row.Nickname = mapStringPtr(values["nickname"])
	row.Avatar = mapStringPtr(values["avatar"])
	row.Phone = mapStringPtr(values["phone"])
	row.OrgID = mapStringPtr(values["org_id"])
	return row
}

func mapString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func mapStringPtr(value any) *string {
	if text, ok := value.(string); ok {
		return &text
	}
	return nil
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

func (roleRow) TableName() string { return "roles" }

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

func (menuRow) TableName() string { return "menus" }

type menuWriteRow struct {
	ID         string  `gorm:"column:id;primaryKey"`
	TenantID   string  `gorm:"column:tenant_id"`
	OrgID      *string `gorm:"column:org_id"`
	ParentID   *string `gorm:"column:parent_id"`
	Name       string  `gorm:"column:name"`
	Path       string  `gorm:"column:path"`
	MenuType   string  `gorm:"column:menu_type"`
	Component  *string `gorm:"column:component"`
	Redirect   *string `gorm:"column:redirect"`
	Icon       *string `gorm:"column:icon"`
	Permission *string `gorm:"column:permission"`
	SortOrder  int     `gorm:"column:sort_order"`
	Visible    bool    `gorm:"column:visible"`
	Status     string  `gorm:"column:status"`
	KeepAlive  bool    `gorm:"column:keep_alive"`
	External   bool    `gorm:"column:external"`
}

func (menuWriteRow) TableName() string { return "menus" }

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

func (permissionRow) TableName() string { return "permissions" }

type iamPolicyWriteRow struct {
	ID       uint64  `gorm:"column:id;primaryKey"`
	TenantID string  `gorm:"column:tenant_id"`
	OrgID    *string `gorm:"column:org_id"`
	UserID   *uint64 `gorm:"column:user_id"`
	RoleID   *string `gorm:"column:role_id"`
	Domain   string  `gorm:"column:domain"`
	Method   string  `gorm:"column:method"`
	Path     string  `gorm:"column:path"`
	Effect   string  `gorm:"column:effect"`
}

func (iamPolicyWriteRow) TableName() string { return "iam_policies" }

type iamDataScopeWriteRow struct {
	ID       uint64  `gorm:"column:id;primaryKey"`
	TenantID string  `gorm:"column:tenant_id"`
	OrgID    *string `gorm:"column:org_id"`
	UserID   *uint64 `gorm:"column:user_id"`
	RoleID   *string `gorm:"column:role_id"`
	Domain   string  `gorm:"column:domain"`
	Resource string  `gorm:"column:resource"`
	Scope    string  `gorm:"column:scope"`
	IDs      []byte  `gorm:"column:ids"`
}

func (iamDataScopeWriteRow) TableName() string { return "iam_data_scopes" }

type roleIDRow struct {
	RoleID string `gorm:"column:role_id"`
}

func (roleIDRow) TableName() string { return "user_roles" }

type permissionIDRow struct {
	ID string `gorm:"column:id"`
}

func (permissionIDRow) TableName() string { return "permissions" }

type policyRow struct {
	UserID sql.NullInt64  `gorm:"column:user_id"`
	RoleID sql.NullString `gorm:"column:role_id"`
	OrgID  sql.NullString `gorm:"column:org_id"`
	Domain string
	Method string
	Path   string
	Effect string
}

func (policyRow) TableName() string { return "iam_policies" }

type scopeRow struct {
	TenantID string         `gorm:"column:tenant_id"`
	UserID   sql.NullInt64  `gorm:"column:user_id"`
	RoleID   sql.NullString `gorm:"column:role_id"`
	OrgID    sql.NullString `gorm:"column:org_id"`
	Domain   string
	Resource string
	Scope    string
	IDs      []byte
}

func (scopeRow) TableName() string { return "iam_data_scopes" }

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
