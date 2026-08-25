// Package iam provides the application seam for RBAC administration and checks.
package iam

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

var (
	ErrInvalidID                       = errors.New("id is required")
	ErrRepositoryMissing               = errors.New("iam repository capability is unavailable")
	ErrInvalidUserQuery                = domain.ErrInvalidUserQuery
	ErrInvalidUser                     = domain.ErrInvalidUser
	ErrInvalidUserBatch                = errors.New("invalid user status batch")
	ErrInvalidRoleAssignment           = errors.New("invalid role assignment")
	ErrInvalidRolePermissionAssignment = errors.New("invalid role permission assignment")
	ErrInvalidRoleDataScopeAssignment  = errors.New("invalid role data scope assignment")
	ErrInvalidMenu                     = domain.ErrInvalidMenu
	ErrMenuConflict                    = errors.New("menu conflicts with an existing resource")
	ErrMenuHasChildren                 = domain.ErrMenuHasChildren
	ErrUserConflict                    = domain.ErrUserConflict
	ErrPasswordHasherMissing           = errors.New("iam password hasher is unavailable")
)

// maxUserBatchStatusItems bounds the number of status mutations accepted by
// one request. Keeping the bound in the application seam makes the same rule
// apply to memory fixtures and durable adapters.
const maxUserBatchStatusItems = 100

// MaxUserBatchStatusItems is exported for clients that need to mirror the
// contract without depending on the private implementation constant.
const MaxUserBatchStatusItems = maxUserBatchStatusItems

const maxRoleAssignmentUsers = 100

// MaxRoleAssignmentUsers is exported so transport and clients share the
// bounded role-membership contract.
const MaxRoleAssignmentUsers = maxRoleAssignmentUsers

const maxRolePermissionBindings = 200

// MaxRolePermissionBindings bounds one atomic replacement of a role's API and
// button permission relations. The same limit is enforced by HTTP, memory,
// and durable adapters.
const MaxRolePermissionBindings = maxRolePermissionBindings

const maxRoleDataScopeBindings = domain.MaxRoleDataScopeBindings

// MaxRoleDataScopeBindings bounds one atomic replacement of a role's data
// scope resources. The same limit is enforced by HTTP, memory and durable
// adapters.
const MaxRoleDataScopeBindings = maxRoleDataScopeBindings

const maxRoleDataScopeIDs = domain.MaxRoleDataScopeIDs

// MaxRoleDataScopeIDs bounds custom IDs inside one resource binding.
const MaxRoleDataScopeIDs = maxRoleDataScopeIDs

// PasswordHasher is the narrow credential boundary used by the IAM write
// seam. Plaintext passwords are accepted only for the duration of the
// application call and repositories receive PasswordHash exclusively.
type PasswordHasher interface {
	Hash(string) (string, error)
}

type UserCreateInput struct {
	Username string
	Nickname string
	Avatar   string
	Email    string
	Phone    string
	OrgID    string
	Active   *bool
	Password string
}

type UserUpdateInput struct {
	Username *string
	Nickname *string
	Avatar   *string
	Email    *string
	Phone    *string
	OrgID    *string
	Active   *bool
}

// UserPasswordResetInput is deliberately credential-only. Profile, status,
// role, and login-event fields stay outside the administrator reset seam.
type UserPasswordResetInput struct {
	Password string
}

// RoleUsersInput is the replacement payload for one role's membership. An
// empty list intentionally clears the role for the current scope.
type RoleUsersInput struct {
	UserIDs []string
}

// RolePermissionsInput is the replacement payload for one role's permission
// relations. An empty list intentionally clears the role in the current scope.
type RolePermissionsInput struct {
	PermissionIDs []string
}

// RoleDataScopeBinding keeps the application API aligned with the domain
// binding while allowing transport and adapters to share one value type.
type RoleDataScopeBinding = domain.RoleDataScopeBinding

// RoleDataScopesInput atomically replaces all role data-scope bindings in the
// caller's tenant/organization scope. An empty list clears that scope.
type RoleDataScopesInput struct {
	Scopes []RoleDataScopeBinding
}

// UserStatusChangeInput is the transport-neutral input for one status change.
// It deliberately contains no profile, credential, or relationship fields.
type UserStatusChangeInput struct {
	ID     string
	Active bool
}

type UserBatchStatusInput struct {
	Items []UserStatusChangeInput
}

// MenuCreateInput is the bounded management payload. Pointer booleans let the
// transport distinguish omitted values from an explicit false.
type MenuCreateInput struct {
	ID, ParentID, Name, Path string
	Type                     domain.MenuType
	Component, Redirect      string
	Icon, Permission         string
	Sort                     int
	Visible, Active          *bool
	KeepAlive, External      *bool
	OrgID                    string
}

type MenuPatchInput struct {
	ParentID, Name, Path *string
	Type                 *domain.MenuType
	Component, Redirect  *string
	Icon, Permission     *string
	Sort                 *int
	Visible, Active      *bool
	KeepAlive, External  *bool
	OrgID                *string
}

type MenuReorderInput struct {
	Items []domain.MenuOrder
}

// UserStatusResult preserves input order and carries a typed per-item error.
// A successful result contains the updated user; HTTP mappers intentionally
// project only its public status fields.
type UserStatusResult struct {
	ID   string
	User domain.User
	Err  error
}

func validManagementPassword(password string) bool {
	length := len([]byte(password))
	return length >= 8 && length <= 128
}

// MemoryStore is a local adapter used by unit tests and the initial bootstrap.
// Its methods intentionally implement the same repository seams used by the
// persistent adapter, so handlers do not depend on map storage.
type MemoryStore struct {
	mu          sync.RWMutex
	users       map[string]domain.User
	nextUserID  uint64
	roles       map[string]domain.Role
	menus       map[string]domain.Menu
	permissions map[string]domain.Permission
	policies    []domain.Policy
	scopes      []domain.DataScope
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:       map[string]domain.User{},
		roles:       map[string]domain.Role{},
		menus:       map[string]domain.Menu{},
		permissions: map[string]domain.Permission{},
	}
}

func (s *MemoryStore) FindUser(ctx context.Context, id string) (domain.User, error) {
	if err := check(ctx); err != nil {
		return domain.User{}, err
	}
	if strings.TrimSpace(id) == "" {
		return domain.User{}, ErrInvalidID
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrResourceNotFound
	}
	return cloneUser(user), nil
}

func (s *MemoryStore) SaveUser(ctx context.Context, user domain.User) error {
	return s.save(ctx, user.ID, func() { s.users[user.ID] = cloneUser(user) })
}

// CreateUser implements the write-side repository seam used by the bounded
// management slice. It keeps uniqueness tenant-local, matching the durable
// database constraints, while retaining the legacy SaveUser fixture API.
func (s *MemoryStore) CreateUser(ctx context.Context, user domain.User) (domain.User, error) {
	if err := check(ctx); err != nil {
		return domain.User{}, err
	}
	if strings.TrimSpace(user.Username) == "" || strings.TrimSpace(user.PasswordHash) == "" {
		return domain.User{}, domain.ErrInvalidUser
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if user.ID == "" {
		s.nextUserID++
		user.ID = "memory-user-" + strconv.FormatUint(s.nextUserID, 10)
	}
	if _, exists := s.users[user.ID]; exists {
		return domain.User{}, domain.ErrUserConflict
	}
	if hasUserIdentifierConflict(s.users, user, "") {
		return domain.User{}, domain.ErrUserConflict
	}
	s.users[user.ID] = cloneUser(user)
	return cloneUser(user), nil
}

func (s *MemoryStore) UpdateUser(ctx context.Context, user domain.User) (domain.User, error) {
	if err := check(ctx); err != nil {
		return domain.User{}, err
	}
	if strings.TrimSpace(user.ID) == "" {
		return domain.User{}, ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.users[user.ID]; !exists {
		return domain.User{}, domain.ErrResourceNotFound
	}
	if hasUserIdentifierConflict(s.users, user, user.ID) {
		return domain.User{}, domain.ErrUserConflict
	}
	s.users[user.ID] = cloneUser(user)
	return cloneUser(user), nil
}

// UpdateUserPassword changes only the credential columns in the memory
// adapter. The application seam supplies an already-hashed value and the
// change timestamp, so plaintext never enters a repository.
func (s *MemoryStore) UpdateUserPassword(ctx context.Context, id, passwordHash string, changedAt time.Time) (domain.User, error) {
	if err := check(ctx); err != nil {
		return domain.User{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.User{}, ErrInvalidID
	}
	if strings.TrimSpace(passwordHash) == "" || changedAt.IsZero() {
		return domain.User{}, domain.ErrInvalidUser
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrResourceNotFound
	}
	user.PasswordHash = passwordHash
	user.PasswordChangedAt = changedAt.UTC()
	s.users[id] = cloneUser(user)
	return cloneUser(user), nil
}

// SoftDeleteUser disables a user in place and preserves credentials, profile
// fields, and role relations. Repeating the operation is intentionally
// idempotent for the management API.
func (s *MemoryStore) SoftDeleteUser(ctx context.Context, id string) (domain.User, error) {
	if err := check(ctx); err != nil {
		return domain.User{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.User{}, ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrResourceNotFound
	}
	user.Active = false
	s.users[id] = cloneUser(user)
	return cloneUser(user), nil
}

// UpdateUserStatus changes only the active flag and retains every other user
// field. It is the single-item fallback for repositories without a bulk port.
func (s *MemoryStore) UpdateUserStatus(ctx context.Context, change domain.UserStatusChange) (domain.User, error) {
	if err := check(ctx); err != nil {
		return domain.User{}, err
	}
	id := strings.TrimSpace(change.ID)
	if id == "" {
		return domain.User{}, ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrResourceNotFound
	}
	user.Active = change.Active
	s.users[id] = cloneUser(user)
	return cloneUser(user), nil
}

// UpdateUserStatuses applies a validated set of status changes under one
// memory lock. Missing rows are omitted from the returned map so the
// application seam can report not_found without revealing cross-scope rows.
func (s *MemoryStore) UpdateUserStatuses(ctx context.Context, changes []domain.UserStatusChange) (map[string]domain.User, error) {
	if err := check(ctx); err != nil {
		return nil, err
	}
	normalized := make([]domain.UserStatusChange, len(changes))
	seen := make(map[string]struct{}, len(changes))
	for i, change := range changes {
		change.ID = strings.TrimSpace(change.ID)
		if change.ID == "" {
			return nil, ErrInvalidID
		}
		if _, exists := seen[change.ID]; exists {
			return nil, ErrInvalidUser
		}
		seen[change.ID] = struct{}{}
		normalized[i] = change
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	updated := make(map[string]domain.User, len(normalized))
	for _, change := range normalized {
		user, ok := s.users[change.ID]
		if !ok {
			continue
		}
		user.Active = change.Active
		s.users[change.ID] = cloneUser(user)
		updated[change.ID] = cloneUser(user)
	}
	return updated, nil
}

func hasUserIdentifierConflict(users map[string]domain.User, candidate domain.User, excludeID string) bool {
	for id, existing := range users {
		if id == excludeID || (existing.TenantID != "" && existing.TenantID != candidate.TenantID) {
			continue
		}
		candidateUsername := normalizedOrLower(candidate.UsernameNormalized, candidate.Username)
		existingUsername := normalizedOrLower(existing.UsernameNormalized, existing.Username)
		candidateEmail := normalizedOrLower(candidate.EmailNormalized, candidate.Email)
		existingEmail := normalizedOrLower(existing.EmailNormalized, existing.Email)
		if candidateUsername != "" && candidateUsername == existingUsername {
			return true
		}
		if candidateEmail != "" && candidateEmail == existingEmail {
			return true
		}
	}
	return false
}

func normalizedOrLower(normalized, raw string) string {
	if strings.TrimSpace(normalized) != "" {
		return strings.ToLower(strings.TrimSpace(normalized))
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

func (s *MemoryStore) ListUsers(ctx context.Context) ([]domain.User, error) {
	if err := check(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.User, 0, len(s.users))
	for _, user := range s.users {
		out = append(out, cloneUser(user))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *MemoryStore) FindRole(ctx context.Context, id string) (domain.Role, error) {
	if err := check(ctx); err != nil {
		return domain.Role{}, err
	}
	if strings.TrimSpace(id) == "" {
		return domain.Role{}, ErrInvalidID
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	role, ok := s.roles[id]
	if !ok {
		return domain.Role{}, domain.ErrResourceNotFound
	}
	return cloneRole(role), nil
}

func (s *MemoryStore) SaveRole(ctx context.Context, role domain.Role) error {
	return s.save(ctx, role.ID, func() { s.roles[role.ID] = cloneRole(role) })
}

// AssignRoleUsers replaces one role's membership within the validated tenant
// and organization scope. It updates only relationship fields and keeps
// unrelated roles on each user intact.
func (s *MemoryStore) AssignRoleUsers(ctx context.Context, roleID string, userIDs []string) (domain.Role, error) {
	if err := check(ctx); err != nil {
		return domain.Role{}, err
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return domain.Role{}, domain.ErrInvalidUser
	}
	normalized, err := normalizeRoleUserIDs(userIDs)
	if err != nil {
		return domain.Role{}, err
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Role{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.roles[roleID]
	if !ok {
		return domain.Role{}, domain.ErrResourceNotFound
	}
	if err := checkRoleScope(scope, role); err != nil {
		return domain.Role{}, err
	}
	// Validate every requested target before changing any relationship so a
	// malformed or out-of-scope list is atomic in the memory adapter too.
	for _, userID := range normalized {
		user, exists := s.users[userID]
		if !exists {
			return domain.Role{}, domain.ErrResourceNotFound
		}
		if err := checkUserScope(scope, user); err != nil {
			return domain.Role{}, err
		}
	}
	for id, user := range s.users {
		if err := checkUserScope(scope, user); err != nil {
			continue
		}
		user.RoleIDs = withoutString(user.RoleIDs, roleID)
		s.users[id] = cloneUser(user)
	}
	for _, userID := range normalized {
		user := s.users[userID]
		if !containsString(user.RoleIDs, roleID) {
			user.RoleIDs = append(user.RoleIDs, roleID)
		}
		s.users[userID] = cloneUser(user)
	}
	members := make(map[string]struct{}, len(role.UserIDs)+len(normalized))
	for _, memberID := range role.UserIDs {
		memberID = strings.TrimSpace(memberID)
		if memberID == "" {
			continue
		}
		if user, exists := s.users[memberID]; exists && checkUserScope(scope, user) == nil {
			continue
		}
		members[memberID] = struct{}{}
	}
	for _, userID := range normalized {
		members[userID] = struct{}{}
	}
	role.UserIDs = make([]string, 0, len(members))
	for memberID := range members {
		role.UserIDs = append(role.UserIDs, memberID)
	}
	sort.Strings(role.UserIDs)
	s.roles[roleID] = cloneRole(role)
	return cloneRole(role), nil
}

// AssignRolePermissions atomically replaces only role policy rows. Direct-user
// policies and policies belonging to other roles remain untouched.
func (s *MemoryStore) AssignRolePermissions(ctx context.Context, roleID string, permissionIDs []string) (domain.Role, error) {
	if err := check(ctx); err != nil {
		return domain.Role{}, err
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return domain.Role{}, ErrInvalidID
	}
	normalized, err := normalizeRolePermissionIDs(permissionIDs)
	if err != nil {
		return domain.Role{}, err
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Role{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.roles[roleID]
	if !ok {
		return domain.Role{}, domain.ErrResourceNotFound
	}
	if err := checkRoleScope(scope, role); err != nil {
		return domain.Role{}, err
	}
	for _, permissionID := range normalized {
		if _, exists := s.permissions[permissionID]; !exists {
			return domain.Role{}, domain.ErrResourceNotFound
		}
	}
	retained := make([]domain.Policy, 0, len(s.policies)+len(normalized))
	for _, policy := range s.policies {
		if strings.TrimSpace(policy.RoleID) == roleID {
			continue
		}
		retained = append(retained, policy)
	}
	s.policies = retained
	for _, permissionID := range normalized {
		permission := s.permissions[permissionID]
		s.policies = append(s.policies, domain.Policy{
			RoleID: roleID, PermissionID: permissionID, Domain: scope.TenantID,
			Method: permission.Method, Path: permission.Path,
			Action: permission.Method, Object: permission.Path, Effect: domain.EffectAllow,
		})
	}
	role.PermissionIDs = append([]string(nil), normalized...)
	s.roles[roleID] = cloneRole(role)
	return cloneRole(role), nil
}

// AssignRoleDataScopes atomically replaces only one role's data-scope rows in
// the caller's tenant/organization scope. User rows and other organization
// rows remain untouched.
func (s *MemoryStore) AssignRoleDataScopes(ctx context.Context, roleID string, bindings []RoleDataScopeBinding) (domain.Role, error) {
	if err := check(ctx); err != nil {
		return domain.Role{}, err
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return domain.Role{}, ErrInvalidID
	}
	normalized, err := normalizeRoleDataScopes(bindings)
	if err != nil {
		return domain.Role{}, err
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Role{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.roles[roleID]
	if !ok {
		return domain.Role{}, domain.ErrResourceNotFound
	}
	if err := checkRoleScope(scope, role); err != nil {
		return domain.Role{}, err
	}
	retained := make([]domain.DataScope, 0, len(s.scopes)+len(normalized))
	for _, existing := range s.scopes {
		if existing.RoleID == roleID && existing.Domain == scope.TenantID && existing.OrgID == scope.Organization {
			continue
		}
		retained = append(retained, cloneDataScope(existing))
	}
	for _, binding := range normalized {
		retained = append(retained, domain.DataScope{
			RoleID: roleID, Domain: scope.TenantID, OrgID: scope.Organization,
			Resource: binding.Resource, Scope: binding.Scope, IDs: append([]string(nil), binding.IDs...),
		})
	}
	s.scopes = retained
	role.DataScope = roleDataScopeSummary(normalized)
	s.roles[roleID] = cloneRole(role)
	return cloneRole(role), nil
}

func (s *MemoryStore) ListRoles(ctx context.Context) ([]domain.Role, error) {
	if err := check(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	requestScope, scoped := tenant.FromContext(ctx)
	out := make([]domain.Role, 0, len(s.roles))
	for _, role := range s.roles {
		if scoped && role.TenantID != "" && role.TenantID != requestScope.TenantID {
			continue
		}
		if scoped && !requestScope.PlatformAdmin && requestScope.Organization != "" && role.OrgID != "" && role.OrgID != requestScope.Organization {
			continue
		}
		out = append(out, cloneRole(role))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *MemoryStore) SaveMenu(ctx context.Context, menu domain.Menu) error {
	if err := check(ctx); err != nil {
		return err
	}
	normalized, err := menu.NormalizeMenu()
	if err != nil {
		return err
	}
	if scope, scoped := tenant.FromContext(ctx); scoped {
		if normalized.TenantID != "" && normalized.TenantID != scope.TenantID && !scope.PlatformAdmin {
			return tenant.ErrCrossTenant
		}
		if normalized.OrgID != "" && !scope.PlatformAdmin && scope.Organization != "" && normalized.OrgID != scope.Organization {
			return tenant.ErrOrganizationDenied
		}
		normalized.TenantID = scope.TenantID
		if normalized.OrgID == "" {
			normalized.OrgID = scope.Organization
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// The memory adapter keeps IDs tenant-local while retaining the legacy
	// unscoped key shape used by older fixtures.
	key := normalized.ID
	if normalized.TenantID != "" {
		key = normalized.TenantID + "\x00" + normalized.ID
	}
	s.menus[key] = normalized
	return nil
}

func (s *MemoryStore) ListMenus(ctx context.Context) ([]domain.Menu, error) {
	if err := check(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	requestScope, scoped := tenant.FromContext(ctx)
	out := make([]domain.Menu, 0, len(s.menus))
	for _, menu := range s.menus {
		if scoped && menu.TenantID != "" && menu.TenantID != requestScope.TenantID {
			continue
		}
		if scoped && !requestScope.PlatformAdmin && requestScope.Organization != "" && menu.OrgID != "" && menu.OrgID != requestScope.Organization {
			continue
		}
		out = append(out, cloneMenu(menu))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *MemoryStore) FindMenu(ctx context.Context, id string) (domain.Menu, error) {
	if err := check(ctx); err != nil {
		return domain.Menu{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.Menu{}, ErrInvalidID
	}
	requestScope, scoped := tenant.FromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, menu := range s.menus {
		if menu.ID != id {
			continue
		}
		if scoped && menu.TenantID != "" && menu.TenantID != requestScope.TenantID {
			continue
		}
		if scoped && !requestScope.PlatformAdmin && requestScope.Organization != "" && menu.OrgID != "" && menu.OrgID != requestScope.Organization {
			continue
		}
		return cloneMenu(menu), nil
	}
	return domain.Menu{}, domain.ErrResourceNotFound
}

func (s *MemoryStore) DeleteMenu(ctx context.Context, id string) error {
	menu, err := s.FindMenu(ctx, id)
	if err != nil {
		return err
	}
	menus, err := s.ListMenus(ctx)
	if err != nil {
		return err
	}
	for _, candidate := range menus {
		if candidate.ParentID == menu.ID {
			return ErrMenuHasChildren
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, candidate := range s.menus {
		if candidate.ID == menu.ID && candidate.TenantID == menu.TenantID {
			delete(s.menus, key)
		}
	}
	return nil
}

func (s *MemoryStore) ReorderMenus(ctx context.Context, items []domain.MenuOrder) error {
	if err := check(ctx); err != nil {
		return err
	}
	if len(items) == 0 || len(items) > 500 {
		return ErrInvalidMenu
	}
	requestScope, scoped := tenant.FromContext(ctx)
	seen := make(map[string]struct{}, len(items))
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" || item.Sort < -1000000 || item.Sort > 1000000 {
			return ErrInvalidMenu
		}
		if _, exists := seen[item.ID]; exists {
			return ErrInvalidMenu
		}
		seen[item.ID] = struct{}{}
		for key, menu := range s.menus {
			if menu.ID != item.ID || (scoped && menu.TenantID != "" && menu.TenantID != requestScope.TenantID) {
				continue
			}
			if !requestScope.PlatformAdmin && scoped && requestScope.Organization != "" && menu.OrgID != "" && menu.OrgID != requestScope.Organization {
				continue
			}
			menu.ParentID = strings.TrimSpace(item.ParentID)
			menu.Sort = item.Sort
			s.menus[key] = menu
			goto nextItem
		}
		return domain.ErrResourceNotFound
	nextItem:
	}
	return nil
}

func (s *MemoryStore) SavePermission(ctx context.Context, permission domain.Permission) error {
	if err := check(ctx); err != nil {
		return err
	}
	key := permission.ID
	if scope, scoped := tenant.FromContext(ctx); scoped {
		if permission.TenantID != "" && permission.TenantID != scope.TenantID && !scope.PlatformAdmin {
			return tenant.ErrCrossTenant
		}
		if permission.OrgID != "" && scope.Organization != "" && permission.OrgID != scope.Organization && !scope.PlatformAdmin {
			return tenant.ErrOrganizationDenied
		}
		permission.TenantID = scope.TenantID
		if permission.OrgID == "" {
			permission.OrgID = scope.Organization
		}
		key = scope.TenantID + "\x00" + permission.ID
	}
	return s.save(ctx, permission.ID, func() { s.permissions[key] = permission })
}

func (s *MemoryStore) ListPermissions(ctx context.Context) ([]domain.Permission, error) {
	if err := check(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	requestScope, scoped := tenant.FromContext(ctx)
	out := make([]domain.Permission, 0, len(s.permissions))
	for _, permission := range s.permissions {
		if scoped && permission.TenantID != "" && permission.TenantID != requestScope.TenantID {
			continue
		}
		if scoped && requestScope.Organization == "" && permission.OrgID != "" {
			continue
		}
		if scoped && requestScope.Organization != "" && permission.OrgID != "" && permission.OrgID != requestScope.Organization {
			continue
		}
		out = append(out, permission)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *MemoryStore) SavePolicy(ctx context.Context, policy domain.Policy) error {
	if err := check(ctx); err != nil {
		return err
	}
	if err := domain.ValidatePolicy(policy); err != nil {
		return err
	}
	if policy.Effect == "" {
		policy.Effect = domain.EffectDeny
	}
	if scope, scoped := tenant.FromContext(ctx); scoped {
		if policy.Domain != "" && policy.Domain != scope.TenantID && !scope.PlatformAdmin {
			return tenant.ErrCrossTenant
		}
		if policy.OrgID != "" && scope.Organization != "" && policy.OrgID != scope.Organization && !scope.PlatformAdmin {
			return tenant.ErrOrganizationDenied
		}
		policy.Domain = scope.TenantID
		if policy.OrgID == "" {
			policy.OrgID = scope.Organization
		}
	}
	s.mu.Lock()
	s.policies = append(s.policies, policy)
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) AddPolicy(ctx context.Context, policy domain.Policy) error {
	return s.SavePolicy(ctx, policy)
}

func (s *MemoryStore) ListPolicies(ctx context.Context) ([]domain.Policy, error) {
	if err := check(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	requestScope, scoped := tenant.FromContext(ctx)
	out := make([]domain.Policy, 0, len(s.policies))
	for _, policy := range s.policies {
		if scoped && policy.Domain != "" && policy.Domain != requestScope.TenantID {
			continue
		}
		if scoped && requestScope.Organization == "" && policy.OrgID != "" {
			continue
		}
		if scoped && requestScope.Organization != "" && policy.OrgID != "" && policy.OrgID != requestScope.Organization {
			continue
		}
		out = append(out, policy)
	}
	return out, nil
}

func (s *MemoryStore) SaveDataScope(ctx context.Context, scope domain.DataScope) error {
	if err := check(ctx); err != nil {
		return err
	}
	if err := domain.ValidateDataScope(scope); err != nil {
		return err
	}
	scope = cloneDataScope(scope)
	s.mu.Lock()
	s.scopes = append(s.scopes, scope)
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) AddDataScope(ctx context.Context, scope domain.DataScope) error {
	return s.SaveDataScope(ctx, scope)
}

func (s *MemoryStore) ListDataScopes(ctx context.Context) ([]domain.DataScope, error) {
	if err := check(ctx); err != nil {
		return nil, err
	}
	requestScope, scoped := tenant.FromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.DataScope, 0, len(s.scopes))
	for _, scope := range s.scopes {
		if scoped && scope.Domain != requestScope.TenantID {
			continue
		}
		if scoped && requestScope.Organization == "" && scope.OrgID != "" {
			continue
		}
		if scoped && requestScope.Organization != "" && scope.OrgID != "" && scope.OrgID != requestScope.Organization {
			continue
		}
		out = append(out, cloneDataScope(scope))
	}
	return out, nil
}

func (s *MemoryStore) save(ctx context.Context, id string, fn func()) error {
	if err := check(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
	return nil
}

func check(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func cloneUser(user domain.User) domain.User {
	user.RoleIDs = append([]string(nil), user.RoleIDs...)
	return user
}

func cloneRole(role domain.Role) domain.Role {
	role.UserIDs = append([]string(nil), role.UserIDs...)
	role.PermissionIDs = append([]string(nil), role.PermissionIDs...)
	return role
}

func cloneDataScope(scope domain.DataScope) domain.DataScope {
	scope.IDs = append([]string(nil), scope.IDs...)
	return scope
}

func cloneMenu(menu domain.Menu) domain.Menu {
	return menu
}

func withoutString(values []string, target string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func normalizeRoleUserIDs(userIDs []string) ([]string, error) {
	if len(userIDs) > maxRoleAssignmentUsers {
		return nil, ErrInvalidRoleAssignment
	}
	normalized := make([]string, len(userIDs))
	seen := make(map[string]struct{}, len(userIDs))
	for index, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			return nil, ErrInvalidUser
		}
		if _, exists := seen[userID]; exists {
			return nil, ErrInvalidUser
		}
		seen[userID] = struct{}{}
		normalized[index] = userID
	}
	return normalized, nil
}

func normalizeRolePermissionIDs(permissionIDs []string) ([]string, error) {
	if len(permissionIDs) > maxRolePermissionBindings {
		return nil, ErrInvalidRolePermissionAssignment
	}
	normalized := make([]string, len(permissionIDs))
	seen := make(map[string]struct{}, len(permissionIDs))
	for index, permissionID := range permissionIDs {
		permissionID = strings.TrimSpace(permissionID)
		if permissionID == "" || len(permissionID) > 128 {
			return nil, ErrInvalidRolePermissionAssignment
		}
		if _, exists := seen[permissionID]; exists {
			return nil, ErrInvalidRolePermissionAssignment
		}
		seen[permissionID] = struct{}{}
		normalized[index] = permissionID
	}
	return normalized, nil
}

func normalizeRoleDataScopes(bindings []RoleDataScopeBinding) ([]RoleDataScopeBinding, error) {
	normalized, err := domain.NormalizeRoleDataScopeBindings(bindings)
	if err != nil {
		return nil, ErrInvalidRoleDataScopeAssignment
	}
	return normalized, nil
}

func roleDataScopeSummary(bindings []RoleDataScopeBinding) domain.Scope {
	if len(bindings) == 0 {
		return domain.ScopeOwn
	}
	if len(bindings) == 1 {
		return bindings[0].Scope
	}
	return domain.ScopeCustom
}

type Service struct {
	Users          domain.UserRepository
	Roles          domain.RoleRepository
	Menus          domain.MenuRepository
	Permissions    domain.PermissionRepository
	Policies       domain.PolicyStore
	DataScopes     domain.DataScopeStore
	Components     ComponentRegistry
	Authorizer     domain.Authorizer
	Scopes         domain.DataScopeResolver
	passwordHasher PasswordHasher
	cache          DecisionCache
}

type userLister interface {
	ListUsers(context.Context) ([]domain.User, error)
}
type authorizationUserFinder interface {
	FindUserForAuthorization(context.Context, string) (domain.User, error)
}
type roleLister interface {
	ListRoles(context.Context) ([]domain.Role, error)
}
type activeUserRoleLister interface {
	ListActiveRoleIDsForUser(context.Context, string) ([]string, error)
}
type menuLister interface {
	ListMenus(context.Context) ([]domain.Menu, error)
}
type permissionLister interface {
	ListPermissions(context.Context) ([]domain.Permission, error)
}
type userSaver interface {
	SaveUser(context.Context, domain.User) error
}
type userCreator interface {
	CreateUser(context.Context, domain.User) (domain.User, error)
}
type userUpdater interface {
	UpdateUser(context.Context, domain.User) (domain.User, error)
}
type userSoftDeleter interface {
	SoftDeleteUser(context.Context, string) (domain.User, error)
}
type userStatusUpdater interface {
	UpdateUserStatus(context.Context, domain.UserStatusChange) (domain.User, error)
}
type userBatchStatusUpdater interface {
	UpdateUserStatuses(context.Context, []domain.UserStatusChange) (map[string]domain.User, error)
}
type roleUserAssigner interface {
	AssignRoleUsers(context.Context, string, []string) (domain.Role, error)
}
type rolePermissionAssigner interface {
	AssignRolePermissions(context.Context, string, []string) (domain.Role, error)
}
type roleDataScopeAssigner interface {
	AssignRoleDataScopes(context.Context, string, []RoleDataScopeBinding) (domain.Role, error)
}
type userPasswordUpdater interface {
	UpdateUserPassword(context.Context, string, string, time.Time) (domain.User, error)
}
type roleSaver interface {
	SaveRole(context.Context, domain.Role) error
}
type menuSaver interface {
	SaveMenu(context.Context, domain.Menu) error
}
type menuFinder interface {
	FindMenu(context.Context, string) (domain.Menu, error)
}
type menuWriter interface {
	menuSaver
	menuFinder
	DeleteMenu(context.Context, string) error
	ReorderMenus(context.Context, []domain.MenuOrder) error
}
type permissionSaver interface {
	SavePermission(context.Context, domain.Permission) error
}

func NewService(store *MemoryStore) *Service {
	if store == nil {
		store = NewMemoryStore()
	}
	return NewServiceWithRepositories(store, store, store, store, store, store)
}

// NewServiceWithRepositories composes the authorization service from ports so
// the HTTP layer can use either the in-memory test adapter or the GORM adapter.
func NewServiceWithRepositories(users domain.UserRepository, roles domain.RoleRepository, menus domain.MenuRepository, permissions domain.PermissionRepository, policies domain.PolicyStore, scopes domain.DataScopeStore) *Service {
	return &Service{
		Users:       users,
		Roles:       roles,
		Menus:       menus,
		Permissions: permissions,
		Policies:    policies,
		DataScopes:  scopes,
		Components:  NewStaticComponentRegistry(),
		Authorizer:  domain.NewAuthorizer(policies),
		Scopes:      domain.NewMemoryDataScopeResolver(scopes),
	}
}

// ListComponents returns the immutable component allowlist used by menu
// editors and backend route conversion.
func (s *Service) ListComponents(ctx context.Context) ([]Component, error) {
	if s == nil || s.Components == nil {
		return nil, ErrRepositoryMissing
	}
	return s.Components.List(ctx)
}

// ValidateComponent rejects arbitrary module paths before a menu reaches a
// persistence adapter.
func (s *Service) ValidateComponent(value string) error {
	if s == nil || s.Components == nil {
		return ErrRepositoryMissing
	}
	if registry, ok := s.Components.(interface{ Validate(string) error }); ok {
		return registry.Validate(value)
	}
	if _, ok := s.Components.Resolve(value); !ok {
		return ErrComponentNotRegistered
	}
	return nil
}

// SetPermissionCache wraps the current authorizer with a versioned decision
// cache. It is intentionally explicit so deployments can keep caching off.
func (s *Service) SetPermissionCache(cache DecisionCache, ttl time.Duration) {
	if s == nil {
		return
	}
	s.cache = cache
	s.Authorizer = NewCachedAuthorizer(s.Authorizer, cache, ttl)
}

// SetPasswordHasher wires the same bcrypt implementation used by auth. It is
// intentionally explicit so a service without a credential dependency stays
// read-only rather than inventing a weak fallback hash.
func (s *Service) SetPasswordHasher(hasher PasswordHasher) {
	if s != nil {
		s.passwordHasher = hasher
	}
}

func (s *Service) invalidate(ctx context.Context) error {
	return InvalidatePermissionCache(ctx, s.cache)
}

func (s *Service) Authorize(ctx context.Context, subject domain.Subject, request domain.Request) (bool, error) {
	if s == nil || s.Authorizer == nil {
		return false, domain.ErrAccessDenied
	}
	subject = subjectWithTenant(ctx, subject)
	return s.Authorizer.Authorize(ctx, subject, request)
}

// ResolveSubject builds the only authorization subject that HTTP adapters
// should trust for an authenticated user. Management reads deliberately keep
// every assigned role, including disabled ones, while this boundary intersects
// assignments with active roles inside the effective tenant/organization.
// Direct-user policies remain effective because the user ID is preserved even
// when no role survives the intersection.
func (s *Service) ResolveSubject(ctx context.Context, user domain.User) (domain.Subject, error) {
	if s == nil || s.Roles == nil {
		return domain.Subject{}, ErrRepositoryMissing
	}
	if !user.Active || strings.TrimSpace(user.ID) == "" {
		return domain.Subject{}, domain.ErrAccessDenied
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Subject{}, err
	}
	effectiveScope, err := scope.BindPrincipal(user.TenantID, user.OrgID)
	if err != nil {
		return domain.Subject{}, err
	}
	effectiveContext := tenant.WithContext(ctx, effectiveScope)

	var roleIDs []string
	if lister, ok := s.Roles.(activeUserRoleLister); ok {
		roleIDs, err = lister.ListActiveRoleIDsForUser(effectiveContext, user.ID)
	} else {
		var roles []domain.Role
		roles, err = s.listEffectiveRoles(effectiveContext, user.RoleIDs)
		roleIDs = make([]string, 0, len(roles))
		for _, role := range roles {
			roleIDs = append(roleIDs, role.ID)
		}
	}
	if err != nil {
		return domain.Subject{}, err
	}
	roleIDs = normalizedRoleIDs(roleIDs)
	return domain.Subject{UserID: user.ID, RoleIDs: roleIDs, Domain: effectiveScope.TenantID}, nil
}

func (s *Service) listEffectiveRoles(ctx context.Context, assignedRoleIDs []string) ([]domain.Role, error) {
	lister, ok := s.Roles.(roleLister)
	if !ok {
		return nil, ErrRepositoryMissing
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	assigned := make(map[string]struct{}, len(assignedRoleIDs))
	for _, roleID := range assignedRoleIDs {
		if roleID = strings.TrimSpace(roleID); roleID != "" {
			assigned[roleID] = struct{}{}
		}
	}
	roles, err := lister.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	effective := make([]domain.Role, 0, len(roles))
	for _, role := range roles {
		if _, ok := assigned[role.ID]; !ok || !role.Active {
			continue
		}
		if role.TenantID != "" && role.TenantID != scope.TenantID {
			continue
		}
		if !scope.PlatformAdmin {
			if scope.Organization == "" && role.OrgID != "" {
				continue
			}
			if scope.Organization != "" && role.OrgID != "" && role.OrgID != scope.Organization {
				continue
			}
		}
		effective = append(effective, role)
	}
	sort.Slice(effective, func(i, j int) bool { return effective[i].ID < effective[j].ID })
	return effective, nil
}

func normalizedRoleIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// ListAccessCodes resolves the active permission IDs currently granted to a
// subject. Permissions and policies are each loaded once, then the canonical
// domain evaluator applies the same wildcard, deny-wins, and tenant-domain
// semantics as Authorize without triggering a cache or repository side effect
// for every permission in the catalog.
func (s *Service) ListAccessCodes(ctx context.Context, subject domain.Subject) ([]string, error) {
	if s == nil || s.Permissions == nil || s.Policies == nil {
		return nil, ErrRepositoryMissing
	}
	subject = subjectWithTenant(ctx, subject)
	permissions, err := s.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}
	permissions = mergeProductionPermissions(permissions)
	eligible := make([]domain.Permission, 0, len(permissions))
	seen := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		code := strings.TrimSpace(permission.ID)
		if !permission.Active || code == "" {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		permission.ID = code
		eligible = append(eligible, permission)
	}
	if len(eligible) == 0 {
		return []string{}, nil
	}
	policies, err := s.Policies.ListPolicies(ctx)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(eligible))
	for _, permission := range eligible {
		allowed, authErr := domain.EvaluatePolicies(policies, subject, domain.Request{
			Domain: subject.Domain,
			Method: permission.Method,
			Path:   permissionEvaluationPath(permission.Path),
		})
		if authErr != nil {
			if errors.Is(authErr, domain.ErrAccessDenied) {
				continue
			}
			return nil, authErr
		}
		if allowed {
			codes = append(codes, permission.ID)
		}
	}
	sort.Strings(codes)
	return codes, nil
}

// mergeProductionPermissions backfills only catalog IDs that an older tenant
// has never persisted. A persisted row with the same ID remains authoritative,
// including an explicit disabled row, while unrelated legacy permissions no
// longer suppress new production menu eligibility.
func mergeProductionPermissions(persisted []domain.Permission) []domain.Permission {
	merged := append([]domain.Permission(nil), persisted...)
	known := make(map[string]struct{}, len(persisted))
	for _, permission := range persisted {
		if id := strings.TrimSpace(permission.ID); id != "" {
			known[id] = struct{}{}
		}
	}
	for _, permission := range ProductionPermissionCatalog() {
		id := strings.TrimSpace(permission.ID)
		if id == "" {
			continue
		}
		if _, exists := known[id]; exists {
			continue
		}
		known[id] = struct{}{}
		merged = append(merged, permission)
	}
	return merged
}

// permissionEvaluationPath turns a permission pattern into one canonical
// request witness before policy evaluation. Evaluating pattern against pattern
// lets a parameter policy such as /settings/:key accidentally match the wider
// literal /settings/* permission. A collection permission uses its root as the
// witness; parameter segments use a concrete sentinel. Global and genuinely
// broader wildcard policies still match through the regular domain evaluator.
func permissionEvaluationPath(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "*" {
		return "/__permission_scope__"
	}
	if strings.HasSuffix(pattern, "/*") {
		root := strings.TrimSuffix(pattern, "/*")
		if root == "" {
			return "/"
		}
		return root
	}
	segments := strings.Split(pattern, "/")
	for index, segment := range segments {
		if segment == "*" || strings.HasPrefix(segment, ":") {
			segments[index] = "__permission_scope__"
		}
	}
	return strings.Join(segments, "/")
}

func (s *Service) ResolveDataScope(ctx context.Context, subject domain.Subject, resource string) (domain.DataScope, error) {
	if s == nil || s.Scopes == nil {
		return domain.DataScope{}, domain.ErrDataScopeNotFound
	}
	subject = subjectWithTenant(ctx, subject)
	return s.Scopes.Resolve(ctx, subject, resource)
}

func subjectWithTenant(ctx context.Context, subject domain.Subject) domain.Subject {
	if strings.TrimSpace(subject.Domain) != "" {
		return subject
	}
	if scope, err := tenant.RequireContext(ctx); err == nil {
		subject.Domain = scope.TenantID
	}
	return subject
}

func (s *Service) ListUsers(ctx context.Context) ([]domain.User, error) {
	if s == nil {
		return nil, ErrRepositoryMissing
	}
	repo, ok := s.Users.(userLister)
	if !ok {
		return nil, ErrRepositoryMissing
	}
	return repo.ListUsers(ctx)
}

// ListUsersPage is the bounded read-side seam used by the first user
// management slice. Persistent adapters may push filtering/counting into SQL;
// legacy repositories fall back to the same deterministic in-memory rules.
func (s *Service) ListUsersPage(ctx context.Context, query domain.UserListQuery) (domain.UserPage, error) {
	if s == nil {
		return domain.UserPage{}, ErrRepositoryMissing
	}
	normalized, err := query.Normalize()
	if err != nil {
		return domain.UserPage{}, ErrInvalidUserQuery
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return domain.UserPage{}, err
		}
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.UserPage{}, err
	}
	if !scope.PlatformAdmin && scope.Organization != "" {
		if normalized.OrgID != "" && normalized.OrgID != scope.Organization {
			return domain.UserPage{}, tenant.ErrOrganizationDenied
		}
		normalized.OrgID = scope.Organization
	}
	if repo, ok := s.Users.(domain.UserPageRepository); ok {
		return repo.ListUsersPage(ctx, normalized)
	}
	users, err := s.ListUsers(ctx)
	if err != nil {
		return domain.UserPage{}, err
	}
	filtered := filterUsers(users, normalized, scope)
	return paginateUsers(filtered, normalized), nil
}

func (s *Service) GetUser(ctx context.Context, id string) (domain.User, error) {
	return s.getUser(ctx, id, false)
}

// GetAuthorizationUser pins security-sensitive identity reads to an adapter's
// primary/read-your-write seam when available. Management reads may continue
// using replicas, but account disable and role revocation must take effect
// before another request is authorized.
func (s *Service) GetAuthorizationUser(ctx context.Context, id string) (domain.User, error) {
	return s.getUser(ctx, id, true)
}

func (s *Service) getUser(ctx context.Context, id string, authorization bool) (domain.User, error) {
	if s == nil || s.Users == nil {
		return domain.User{}, ErrRepositoryMissing
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.User{}, ErrInvalidID
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.User{}, err
	}
	var user domain.User
	if finder, ok := s.Users.(authorizationUserFinder); authorization && ok {
		user, err = finder.FindUserForAuthorization(ctx, id)
	} else {
		user, err = s.Users.FindUser(ctx, id)
	}
	if err != nil {
		return domain.User{}, err
	}
	if user.TenantID != "" && user.TenantID != scope.TenantID {
		return domain.User{}, tenant.ErrCrossTenant
	}
	if authorization {
		if _, err := scope.BindPrincipal(user.TenantID, user.OrgID); err != nil {
			return domain.User{}, err
		}
	} else if err := checkUserScope(scope, user); err != nil {
		return domain.User{}, err
	}
	if user.TenantID == "" {
		// Legacy in-memory fixtures predate mandatory tenant fields. Resolve
		// their response to the validated request scope without weakening the
		// persistent adapter predicate.
		user.TenantID = scope.TenantID
	}
	return user, nil
}

func (s *Service) CreateUser(ctx context.Context, input UserCreateInput) (domain.User, error) {
	if s == nil || s.Users == nil {
		return domain.User{}, ErrRepositoryMissing
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.User{}, err
	}
	if !validManagementPassword(input.Password) || s.passwordHasher == nil {
		if s.passwordHasher == nil && validManagementPassword(input.Password) {
			return domain.User{}, ErrPasswordHasherMissing
		}
		return domain.User{}, ErrInvalidUser
	}
	orgID := strings.TrimSpace(input.OrgID)
	if !scope.PlatformAdmin && scope.Organization != "" {
		if orgID != "" && orgID != scope.Organization {
			return domain.User{}, tenant.ErrOrganizationDenied
		}
		orgID = scope.Organization
	}
	active := true
	if input.Active != nil {
		active = *input.Active
	}
	user := domain.User{
		Username: input.Username, Nickname: input.Nickname, Avatar: input.Avatar,
		Email: input.Email, Phone: input.Phone, TenantID: scope.TenantID,
		OrgID: orgID, Active: active,
	}
	user, err = user.NormalizeProfile()
	if err != nil {
		return domain.User{}, ErrInvalidUser
	}
	hash, err := s.passwordHasher.Hash(input.Password)
	if err != nil || strings.TrimSpace(hash) == "" {
		return domain.User{}, ErrPasswordHasherMissing
	}
	user.PasswordHash = hash
	created, err := s.createUser(ctx, user)
	if err != nil {
		return domain.User{}, err
	}
	if created.TenantID == "" {
		created.TenantID = scope.TenantID
	}
	if err := s.invalidate(ctx); err != nil {
		return domain.User{}, err
	}
	return created, nil
}

func (s *Service) createUser(ctx context.Context, user domain.User) (domain.User, error) {
	if repo, ok := s.Users.(userCreator); ok {
		return repo.CreateUser(ctx, user)
	}
	if repo, ok := s.Users.(userSaver); ok {
		if err := repo.SaveUser(ctx, user); err != nil {
			return domain.User{}, err
		}
		return user, nil
	}
	return domain.User{}, ErrRepositoryMissing
}

func (s *Service) UpdateUser(ctx context.Context, id string, input UserUpdateInput) (domain.User, error) {
	if s == nil || s.Users == nil {
		return domain.User{}, ErrRepositoryMissing
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.User{}, ErrInvalidID
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.User{}, err
	}
	existing, err := s.Users.FindUser(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	if err := checkUserScope(scope, existing); err != nil {
		return domain.User{}, err
	}
	updated := existing
	if input.Username != nil {
		updated.Username = *input.Username
	}
	if input.Nickname != nil {
		updated.Nickname = *input.Nickname
	}
	if input.Avatar != nil {
		updated.Avatar = *input.Avatar
	}
	if input.Email != nil {
		updated.Email = *input.Email
	}
	if input.Phone != nil {
		updated.Phone = *input.Phone
	}
	if input.OrgID != nil {
		updated.OrgID = strings.TrimSpace(*input.OrgID)
	}
	if input.Active != nil {
		updated.Active = *input.Active
	}
	if updated.TenantID == "" {
		updated.TenantID = scope.TenantID
	}
	if !scope.PlatformAdmin && scope.Organization != "" && updated.OrgID != scope.Organization {
		return domain.User{}, tenant.ErrOrganizationDenied
	}
	updated, err = updated.NormalizeProfile()
	if err != nil {
		return domain.User{}, ErrInvalidUser
	}
	// PasswordHash is copied from the existing record and never accepted from
	// the PATCH transport input.
	if repo, ok := s.Users.(userUpdater); ok {
		updated, err = repo.UpdateUser(ctx, updated)
	} else if repo, ok := s.Users.(userSaver); ok {
		err = repo.SaveUser(ctx, updated)
	} else {
		return domain.User{}, ErrRepositoryMissing
	}
	if err != nil {
		return domain.User{}, err
	}
	if err := s.invalidate(ctx); err != nil {
		return domain.User{}, err
	}
	return updated, nil
}

// ResetUserPassword performs the bounded administrator credential reset. It
// validates the tenant/organization scope before hashing, sends only the
// encoded hash to a dedicated repository port, and leaves all other user
// state untouched.
func (s *Service) ResetUserPassword(ctx context.Context, id string, input UserPasswordResetInput) (domain.User, error) {
	if s == nil || s.Users == nil {
		return domain.User{}, ErrRepositoryMissing
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.User{}, ErrInvalidID
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return domain.User{}, err
		}
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.User{}, err
	}
	existing, err := s.Users.FindUser(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	if err := checkUserScope(scope, existing); err != nil {
		return domain.User{}, err
	}
	if !validManagementPassword(input.Password) {
		return domain.User{}, ErrInvalidUser
	}
	if s.passwordHasher == nil {
		return domain.User{}, ErrPasswordHasherMissing
	}
	hash, err := s.passwordHasher.Hash(input.Password)
	if err != nil || strings.TrimSpace(hash) == "" {
		return domain.User{}, ErrPasswordHasherMissing
	}
	updater, ok := s.Users.(userPasswordUpdater)
	if !ok {
		return domain.User{}, ErrRepositoryMissing
	}
	changedAt := time.Now().UTC()
	updated, err := updater.UpdateUserPassword(ctx, id, hash, changedAt)
	if err != nil {
		return domain.User{}, err
	}
	if updated.ID == "" {
		// A repository may return only an acknowledgement. Keep the response
		// deterministic without weakening the credential-only write boundary.
		updated = existing
	}
	updated.PasswordHash = hash
	updated.PasswordChangedAt = changedAt
	if updated.TenantID == "" {
		updated.TenantID = scope.TenantID
	}
	return updated, nil
}

// DeleteUser implements the bounded default soft-delete seam. The repository
// must retain the row and relationships while changing only its active state;
// legacy repositories fall back to the existing profile update/save ports.
func (s *Service) DeleteUser(ctx context.Context, id string) (domain.User, error) {
	if s == nil || s.Users == nil {
		return domain.User{}, ErrRepositoryMissing
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.User{}, ErrInvalidID
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.User{}, err
	}
	existing, err := s.Users.FindUser(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	if err := checkUserScope(scope, existing); err != nil {
		return domain.User{}, err
	}
	existing.Active = false
	var deleted domain.User
	if repo, ok := s.Users.(userSoftDeleter); ok {
		deleted, err = repo.SoftDeleteUser(ctx, id)
	} else if repo, ok := s.Users.(userUpdater); ok {
		deleted, err = repo.UpdateUser(ctx, existing)
	} else if repo, ok := s.Users.(userSaver); ok {
		err = repo.SaveUser(ctx, existing)
		deleted = existing
	} else {
		return domain.User{}, ErrRepositoryMissing
	}
	if err != nil {
		return domain.User{}, err
	}
	if deleted.ID == "" {
		deleted = existing
	}
	if deleted.TenantID == "" {
		deleted.TenantID = scope.TenantID
	}
	deleted.Active = false
	if err := s.invalidate(ctx); err != nil {
		return domain.User{}, err
	}
	return deleted, nil
}

// BatchUpdateUserStatus applies bounded, tenant-scoped active-state changes.
// Validation and scope checks happen before any repository mutation; each
// item then receives an independent result so one missing or duplicate ID does
// not hide successful changes for other IDs.
func (s *Service) BatchUpdateUserStatus(ctx context.Context, input UserBatchStatusInput) ([]UserStatusResult, error) {
	if s == nil || s.Users == nil {
		return nil, ErrRepositoryMissing
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if len(input.Items) == 0 || len(input.Items) > maxUserBatchStatusItems {
		return nil, ErrInvalidUserBatch
	}

	results := make([]UserStatusResult, len(input.Items))
	changes := make([]domain.UserStatusChange, 0, len(input.Items))
	changeIndexes := make(map[string]int, len(input.Items))
	seen := make(map[string]struct{}, len(input.Items))
	for index, item := range input.Items {
		id := strings.TrimSpace(item.ID)
		results[index].ID = id
		if id == "" {
			results[index].Err = ErrInvalidUser
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			results[index].Err = ErrInvalidUser
			continue
		}
		seen[id] = struct{}{}
		user, findErr := s.Users.FindUser(ctx, id)
		if findErr != nil {
			switch {
			case errors.Is(findErr, domain.ErrResourceNotFound):
				results[index].Err = domain.ErrResourceNotFound
			case errors.Is(findErr, ErrInvalidID), errors.Is(findErr, domain.ErrInvalidUser):
				results[index].Err = ErrInvalidUser
			default:
				// Repository failures are retained on the item. This keeps the
				// response shape stable while allowing the handler to expose the
				// generic error code without leaking row existence.
				results[index].Err = findErr
			}
			continue
		}
		if scopeErr := checkUserScope(scope, user); scopeErr != nil {
			// A caller must not learn whether an ID belongs to another tenant
			// or organization. Both scope failures use the not_found result.
			if errors.Is(scopeErr, tenant.ErrCrossTenant) || errors.Is(scopeErr, tenant.ErrOrganizationDenied) {
				results[index].Err = domain.ErrResourceNotFound
			} else {
				results[index].Err = scopeErr
			}
			continue
		}
		changes = append(changes, domain.UserStatusChange{ID: id, Active: item.Active})
		changeIndexes[id] = index
	}

	if len(changes) == 0 {
		return results, nil
	}

	updated := make(map[string]domain.User, len(changes))
	if repo, ok := s.Users.(userBatchStatusUpdater); ok {
		var bulkErr error
		updated, bulkErr = repo.UpdateUserStatuses(ctx, changes)
		if bulkErr != nil {
			for _, index := range changeIndexes {
				results[index].Err = bulkErr
			}
			return results, nil
		}
	} else {
		for _, change := range changes {
			var user domain.User
			if repo, ok := s.Users.(userStatusUpdater); ok {
				user, err = repo.UpdateUserStatus(ctx, change)
			} else {
				// Preserve compatibility with older repositories that expose only
				// the profile update/save ports.
				existing, findErr := s.Users.FindUser(ctx, change.ID)
				if findErr != nil {
					err = findErr
				} else {
					existing.Active = change.Active
					if updater, updaterOK := s.Users.(userUpdater); updaterOK {
						user, err = updater.UpdateUser(ctx, existing)
					} else if saver, saverOK := s.Users.(userSaver); saverOK {
						err = saver.SaveUser(ctx, existing)
						user = existing
					} else {
						err = ErrRepositoryMissing
					}
				}
			}
			if err != nil {
				results[changeIndexes[change.ID]].Err = batchItemError(err)
				continue
			}
			if user.ID == "" {
				user.ID = change.ID
			}
			user.Active = change.Active
			updated[change.ID] = user
		}
	}

	mutated := false
	for id, index := range changeIndexes {
		user, ok := updated[id]
		if !ok {
			if results[index].Err == nil {
				results[index].Err = domain.ErrResourceNotFound
			}
			continue
		}
		user.Active = input.Items[index].Active
		results[index].User = user
		results[index].Err = nil
		mutated = true
	}
	if mutated {
		if invalidateErr := s.invalidate(ctx); invalidateErr != nil {
			return nil, invalidateErr
		}
	}
	return results, nil
}

func batchItemError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domain.ErrResourceNotFound) || errors.Is(err, tenant.ErrCrossTenant) || errors.Is(err, tenant.ErrOrganizationDenied) {
		return domain.ErrResourceNotFound
	}
	if errors.Is(err, ErrInvalidID) || errors.Is(err, domain.ErrInvalidUser) {
		return ErrInvalidUser
	}
	return err
}

func checkUserScope(scope tenant.Context, user domain.User) error {
	if user.TenantID != "" && user.TenantID != scope.TenantID {
		return tenant.ErrCrossTenant
	}
	if scope.PlatformAdmin {
		return nil
	}
	if scope.Organization != "" && user.OrgID != scope.Organization {
		return tenant.ErrOrganizationDenied
	}
	return nil
}

func checkRoleScope(scope tenant.Context, role domain.Role) error {
	if role.TenantID != "" && role.TenantID != scope.TenantID {
		return tenant.ErrCrossTenant
	}
	if scope.PlatformAdmin {
		return nil
	}
	if scope.Organization != "" && role.OrgID != "" && role.OrgID != scope.Organization {
		return tenant.ErrOrganizationDenied
	}
	return nil
}

func filterUsers(users []domain.User, query domain.UserListQuery, scope tenant.Context) []domain.User {
	keyword := strings.ToLower(query.Keyword)
	filtered := make([]domain.User, 0, len(users))
	for _, user := range users {
		if user.TenantID != "" && user.TenantID != scope.TenantID {
			continue
		}
		if query.OrgID != "" && user.OrgID != query.OrgID {
			continue
		}
		if query.Status == "active" && !user.Active || query.Status == "disabled" && user.Active {
			continue
		}
		if query.RoleID != "" && !containsString(user.RoleIDs, query.RoleID) {
			continue
		}
		if keyword != "" {
			values := []string{user.Username, user.DisplayName, user.Nickname, user.Email}
			matched := false
			for _, value := range values {
				if strings.Contains(strings.ToLower(value), keyword) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		filtered = append(filtered, cloneUser(user))
	}
	sortUsers(filtered, query.Sort)
	return filtered
}

func paginateUsers(users []domain.User, query domain.UserListQuery) domain.UserPage {
	page := domain.UserPage{Total: len(users), Page: query.Page, PageSize: query.PageSize, Items: []domain.User{}}
	offset := (query.Page - 1) * query.PageSize
	if offset >= len(users) {
		return page
	}
	end := offset + query.PageSize
	if end > len(users) {
		end = len(users)
	}
	page.Items = append(page.Items, users[offset:end]...)
	return page
}

func sortUsers(users []domain.User, sortValue string) {
	desc := strings.HasPrefix(sortValue, "-")
	key := strings.TrimPrefix(sortValue, "-")
	compare := func(left, right domain.User) int {
		var l, r string
		switch key {
		case "username":
			l, r = strings.ToLower(left.Username), strings.ToLower(right.Username)
		case "displayName":
			l, r = strings.ToLower(left.DisplayName), strings.ToLower(right.DisplayName)
		case "email":
			l, r = strings.ToLower(left.Email), strings.ToLower(right.Email)
		case "lastLoginAt":
			l, r = left.LastLoginAt.UTC().Format(time.RFC3339Nano), right.LastLoginAt.UTC().Format(time.RFC3339Nano)
		case "orgId":
			l, r = left.OrgID, right.OrgID
		default:
			l, r = left.ID, right.ID
		}
		if l < r {
			return -1
		}
		if l > r {
			return 1
		}
		if left.ID < right.ID {
			return -1
		}
		if left.ID > right.ID {
			return 1
		}
		return 0
	}
	sort.SliceStable(users, func(i, j int) bool {
		result := compare(users[i], users[j])
		if desc {
			return result > 0
		}
		return result < 0
	})
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Service) SaveUser(ctx context.Context, user domain.User) error {
	if s == nil {
		return ErrRepositoryMissing
	}
	repo, ok := s.Users.(userSaver)
	if !ok {
		return ErrRepositoryMissing
	}
	if err := repo.SaveUser(ctx, user); err != nil {
		return err
	}
	return s.invalidate(ctx)
}

func (s *Service) ListRoles(ctx context.Context) ([]domain.Role, error) {
	if s == nil {
		return nil, ErrRepositoryMissing
	}
	repo, ok := s.Roles.(roleLister)
	if !ok {
		return nil, ErrRepositoryMissing
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	roles, err := repo.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]domain.Role, 0, len(roles))
	for _, role := range roles {
		if role.TenantID != "" && role.TenantID != scope.TenantID {
			continue
		}
		if !scope.PlatformAdmin && scope.Organization != "" && role.OrgID != "" && role.OrgID != scope.Organization {
			continue
		}
		filtered = append(filtered, role)
	}
	return filtered, nil
}

func (s *Service) SaveRole(ctx context.Context, role domain.Role) error {
	if s == nil {
		return ErrRepositoryMissing
	}
	repo, ok := s.Roles.(roleSaver)
	if !ok {
		return ErrRepositoryMissing
	}
	if err := repo.SaveRole(ctx, role); err != nil {
		return err
	}
	return s.invalidate(ctx)
}

// ReplaceRoleUsers validates every target in the caller's tenant/org scope,
// then delegates one bounded relationship replacement to the durable adapter.
// No profile, credential, status, or unrelated role field is accepted here.
func (s *Service) ReplaceRoleUsers(ctx context.Context, roleID string, input RoleUsersInput) (domain.Role, error) {
	if s == nil || s.Roles == nil || s.Users == nil {
		return domain.Role{}, ErrRepositoryMissing
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return domain.Role{}, ErrInvalidID
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return domain.Role{}, err
		}
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Role{}, err
	}
	normalized, err := normalizeRoleUserIDs(input.UserIDs)
	if err != nil {
		return domain.Role{}, err
	}
	role, err := s.Roles.FindRole(ctx, roleID)
	if err != nil {
		return domain.Role{}, err
	}
	if err := checkRoleScope(scope, role); err != nil {
		return domain.Role{}, err
	}
	for _, userID := range normalized {
		user, findErr := s.Users.FindUser(ctx, userID)
		if findErr != nil {
			return domain.Role{}, findErr
		}
		if scopeErr := checkUserScope(scope, user); scopeErr != nil {
			return domain.Role{}, scopeErr
		}
	}
	var updated domain.Role
	if repo, ok := s.Roles.(roleUserAssigner); ok {
		updated, err = repo.AssignRoleUsers(ctx, roleID, normalized)
	} else if repo, ok := s.Users.(roleUserAssigner); ok {
		updated, err = repo.AssignRoleUsers(ctx, roleID, normalized)
	} else {
		return domain.Role{}, ErrRepositoryMissing
	}
	if err != nil {
		return domain.Role{}, err
	}
	if updated.ID == "" {
		updated = role
	}
	updated.UserIDs = append([]string(nil), normalized...)
	if err := s.invalidate(ctx); err != nil {
		return domain.Role{}, err
	}
	return updated, nil
}

// ReplaceRolePermissions validates permission IDs inside the caller's
// tenant/org scope, then delegates one atomic relation replacement. The
// policy store remains the single source of truth for role authorization.
func (s *Service) ReplaceRolePermissions(ctx context.Context, roleID string, input RolePermissionsInput) (domain.Role, error) {
	if s == nil || s.Roles == nil || s.Permissions == nil {
		return domain.Role{}, ErrRepositoryMissing
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return domain.Role{}, ErrInvalidID
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return domain.Role{}, err
		}
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Role{}, err
	}
	normalized, err := normalizeRolePermissionIDs(input.PermissionIDs)
	if err != nil {
		return domain.Role{}, err
	}
	role, err := s.Roles.FindRole(ctx, roleID)
	if err != nil {
		return domain.Role{}, err
	}
	if err := checkRoleScope(scope, role); err != nil {
		return domain.Role{}, err
	}
	permissions, err := s.ListPermissions(ctx)
	if err != nil {
		return domain.Role{}, err
	}
	known := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		if id := strings.TrimSpace(permission.ID); id != "" {
			known[id] = struct{}{}
		}
	}
	for _, permissionID := range normalized {
		if _, exists := known[permissionID]; !exists {
			return domain.Role{}, domain.ErrResourceNotFound
		}
	}
	var updated domain.Role
	if repo, ok := s.Roles.(rolePermissionAssigner); ok {
		updated, err = repo.AssignRolePermissions(ctx, roleID, normalized)
	} else if repo, ok := s.Policies.(rolePermissionAssigner); ok {
		updated, err = repo.AssignRolePermissions(ctx, roleID, normalized)
	} else {
		return domain.Role{}, ErrRepositoryMissing
	}
	if err != nil {
		return domain.Role{}, err
	}
	if updated.ID == "" {
		updated = role
	}
	updated.PermissionIDs = append([]string(nil), normalized...)
	if err := s.invalidate(ctx); err != nil {
		return domain.Role{}, err
	}
	return updated, nil
}

// ReplaceRoleDataScopes validates every resource binding inside the caller's
// tenant/org scope, then delegates one atomic replacement. Empty input clears
// only the current organization slice and never touches user bindings.
func (s *Service) ReplaceRoleDataScopes(ctx context.Context, roleID string, input RoleDataScopesInput) (domain.Role, error) {
	if s == nil || s.Roles == nil {
		return domain.Role{}, ErrRepositoryMissing
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return domain.Role{}, ErrInvalidID
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return domain.Role{}, err
		}
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Role{}, err
	}
	normalized, err := normalizeRoleDataScopes(input.Scopes)
	if err != nil {
		return domain.Role{}, err
	}
	role, err := s.Roles.FindRole(ctx, roleID)
	if err != nil {
		return domain.Role{}, err
	}
	if err := checkRoleScope(scope, role); err != nil {
		return domain.Role{}, err
	}
	var updated domain.Role
	if repo, ok := s.Roles.(roleDataScopeAssigner); ok {
		updated, err = repo.AssignRoleDataScopes(ctx, roleID, normalized)
	} else if repo, ok := s.DataScopes.(roleDataScopeAssigner); ok {
		updated, err = repo.AssignRoleDataScopes(ctx, roleID, normalized)
	} else {
		return domain.Role{}, ErrRepositoryMissing
	}
	if err != nil {
		return domain.Role{}, err
	}
	if updated.ID == "" {
		updated = role
	}
	updated.DataScope = roleDataScopeSummary(normalized)
	if err := s.invalidate(ctx); err != nil {
		return domain.Role{}, err
	}
	return updated, nil
}

func (s *Service) ListMenus(ctx context.Context) ([]domain.Menu, error) {
	if s == nil {
		return nil, ErrRepositoryMissing
	}
	repo, ok := s.Menus.(menuLister)
	if !ok {
		return nil, ErrRepositoryMissing
	}
	return repo.ListMenus(ctx)
}

// ListMenuRoutes projects the current tenant's visible menu records into the
// route tree consumed by the three UI templates.
func (s *Service) ListMenuRoutes(ctx context.Context) ([]MenuRoute, error) {
	menus, err := s.ListMenus(ctx)
	if err != nil {
		return nil, err
	}
	return s.buildValidatedMenuRoutes(menus)
}

// ListMenuRoutesForSubject resolves the subject's effective permission codes
// and filters permission-bound route nodes on the server. Public nodes retain
// their existing visibility semantics, and a directory remains available when
// it is needed to contain at least one authorized visible descendant.
func (s *Service) ListMenuRoutesForSubject(ctx context.Context, subject domain.Subject) ([]MenuRoute, error) {
	codes, err := s.ListAccessCodes(ctx, subject)
	if err != nil {
		return nil, err
	}
	menus, err := s.ListMenus(ctx)
	if err != nil {
		return nil, err
	}
	if len(menus) == 0 {
		menus = ProductionMenuCatalog()
	}
	return s.buildValidatedMenuRoutes(filterMenusByAccessCodes(menus, codes))
}

func (s *Service) buildValidatedMenuRoutes(menus []domain.Menu) ([]MenuRoute, error) {
	for _, menu := range menus {
		if strings.TrimSpace(menu.Component) == "" {
			continue
		}
		if err := s.ValidateComponent(menu.Component); err != nil {
			return nil, err
		}
	}
	return BuildMenuRoutes(menus)
}

func filterMenusByAccessCodes(menus []domain.Menu, codes []string) []domain.Menu {
	allowedCodes := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		if code = strings.TrimSpace(code); code != "" {
			allowedCodes[code] = struct{}{}
		}
	}
	byID := make(map[string]domain.Menu, len(menus))
	children := make(map[string][]string, len(menus))
	for _, menu := range menus {
		byID[menu.ID] = menu
		if menu.ParentID != "" {
			children[menu.ParentID] = append(children[menu.ParentID], menu.ID)
		}
	}

	structuralMemo := make(map[string]bool, len(menus))
	structuralVisiting := make(map[string]bool, len(menus))
	var structurallyVisible func(string) bool
	structurallyVisible = func(id string) bool {
		if visible, resolved := structuralMemo[id]; resolved {
			return visible
		}
		menu, exists := byID[id]
		if !exists || !menu.Active || !menu.Visible || menu.Type == domain.MenuTypeButton || structuralVisiting[id] {
			structuralMemo[id] = false
			return false
		}
		structuralVisiting[id] = true
		visible := true
		if menu.ParentID != "" {
			if _, parentExists := byID[menu.ParentID]; parentExists {
				visible = structurallyVisible(menu.ParentID)
			}
		}
		delete(structuralVisiting, id)
		structuralMemo[id] = visible
		return visible
	}

	permissionMemo := make(map[string]bool, len(menus))
	permissionVisiting := make(map[string]bool, len(menus))
	var permitted func(string) bool
	permitted = func(id string) bool {
		if keep, resolved := permissionMemo[id]; resolved {
			return keep
		}
		menu, exists := byID[id]
		if !exists || !structurallyVisible(id) || permissionVisiting[id] {
			permissionMemo[id] = false
			return false
		}
		permissionVisiting[id] = true
		permission := strings.TrimSpace(menu.Permission)
		_, explicitlyAllowed := allowedCodes[permission]
		keep := explicitlyAllowed || (permission == "" && menu.Type != domain.MenuTypeDirectory)
		if menu.Type == domain.MenuTypeDirectory && !keep {
			for _, childID := range children[id] {
				if permitted(childID) {
					keep = true
					break
				}
			}
		}
		delete(permissionVisiting, id)
		permissionMemo[id] = keep
		return keep
	}

	filtered := make([]domain.Menu, 0, len(menus))
	for _, menu := range menus {
		if permitted(menu.ID) {
			filtered = append(filtered, menu)
		}
	}
	return filtered
}

func (s *Service) GetMenu(ctx context.Context, id string) (domain.Menu, error) {
	if s == nil || s.Menus == nil {
		return domain.Menu{}, ErrRepositoryMissing
	}
	repo, ok := s.Menus.(menuFinder)
	if !ok {
		return domain.Menu{}, ErrRepositoryMissing
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.Menu{}, ErrInvalidID
	}
	return repo.FindMenu(ctx, id)
}

func (s *Service) CreateMenu(ctx context.Context, input MenuCreateInput) (domain.Menu, error) {
	if s == nil || s.Menus == nil {
		return domain.Menu{}, ErrRepositoryMissing
	}
	writer, ok := s.Menus.(menuWriter)
	if !ok {
		return domain.Menu{}, ErrRepositoryMissing
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Menu{}, err
	}
	menu := domain.Menu{
		ID: strings.TrimSpace(input.ID), ParentID: strings.TrimSpace(input.ParentID), Name: strings.TrimSpace(input.Name), Path: strings.TrimSpace(input.Path),
		Type: input.Type, Component: input.Component, Redirect: input.Redirect, Icon: input.Icon, Permission: input.Permission, Sort: input.Sort,
		TenantID: scope.TenantID, OrgID: strings.TrimSpace(input.OrgID),
	}
	if input.Visible == nil {
		menu.Visible = true
	} else {
		menu.Visible = *input.Visible
	}
	if input.Active == nil {
		menu.Active = true
	} else {
		menu.Active = *input.Active
	}
	if input.KeepAlive != nil {
		menu.KeepAlive = *input.KeepAlive
	}
	if input.External != nil {
		menu.External = *input.External
	}
	if !scope.PlatformAdmin && scope.Organization != "" {
		if menu.OrgID != "" && menu.OrgID != scope.Organization {
			return domain.Menu{}, tenant.ErrOrganizationDenied
		}
		menu.OrgID = scope.Organization
	}
	menu, err = menu.NormalizeMenu()
	if err != nil {
		return domain.Menu{}, ErrInvalidMenu
	}
	if menu.Component != "" {
		if err := s.ValidateComponent(menu.Component); err != nil {
			return domain.Menu{}, err
		}
	}
	if _, err := writer.FindMenu(ctx, menu.ID); err == nil {
		return domain.Menu{}, ErrMenuConflict
	} else if !errors.Is(err, domain.ErrResourceNotFound) {
		return domain.Menu{}, err
	}
	if err := s.validateMenuParent(ctx, menu); err != nil {
		return domain.Menu{}, err
	}
	if err := writer.SaveMenu(ctx, menu); err != nil {
		return domain.Menu{}, err
	}
	if err := s.invalidate(ctx); err != nil {
		return domain.Menu{}, err
	}
	return menu, nil
}

func (s *Service) UpdateMenu(ctx context.Context, id string, input MenuPatchInput) (domain.Menu, error) {
	if s == nil || s.Menus == nil {
		return domain.Menu{}, ErrRepositoryMissing
	}
	writer, ok := s.Menus.(menuWriter)
	if !ok {
		return domain.Menu{}, ErrRepositoryMissing
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.Menu{}, ErrInvalidID
	}
	current, err := writer.FindMenu(ctx, id)
	if err != nil {
		return domain.Menu{}, err
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Menu{}, err
	}
	if !scope.PlatformAdmin && scope.Organization != "" && input.OrgID != nil && strings.TrimSpace(*input.OrgID) != scope.Organization {
		return domain.Menu{}, tenant.ErrOrganizationDenied
	}
	if input.ParentID != nil {
		current.ParentID = *input.ParentID
	}
	if input.Name != nil {
		current.Name = *input.Name
	}
	if input.Path != nil {
		current.Path = *input.Path
	}
	if input.Type != nil {
		current.Type = *input.Type
	}
	if input.Component != nil {
		current.Component = *input.Component
	}
	if input.Redirect != nil {
		current.Redirect = *input.Redirect
	}
	if input.Icon != nil {
		current.Icon = *input.Icon
	}
	if input.Permission != nil {
		current.Permission = *input.Permission
	}
	if input.Sort != nil {
		current.Sort = *input.Sort
	}
	if input.Visible != nil {
		current.Visible = *input.Visible
	}
	if input.Active != nil {
		current.Active = *input.Active
	}
	if input.KeepAlive != nil {
		current.KeepAlive = *input.KeepAlive
	}
	if input.External != nil {
		current.External = *input.External
	}
	if input.OrgID != nil {
		current.OrgID = strings.TrimSpace(*input.OrgID)
	}
	if current.Component != "" {
		if err := s.ValidateComponent(current.Component); err != nil {
			return domain.Menu{}, err
		}
	}
	current, err = current.NormalizeMenu()
	if err != nil {
		return domain.Menu{}, ErrInvalidMenu
	}
	if err := s.validateMenuParent(ctx, current); err != nil {
		return domain.Menu{}, err
	}
	if err := writer.SaveMenu(ctx, current); err != nil {
		return domain.Menu{}, err
	}
	if err := s.invalidate(ctx); err != nil {
		return domain.Menu{}, err
	}
	return current, nil
}

func (s *Service) DeleteMenu(ctx context.Context, id string) error {
	if s == nil || s.Menus == nil {
		return ErrRepositoryMissing
	}
	writer, ok := s.Menus.(menuWriter)
	if !ok {
		return ErrRepositoryMissing
	}
	if strings.TrimSpace(id) == "" {
		return ErrInvalidID
	}
	if _, err := writer.FindMenu(ctx, id); err != nil {
		return err
	}
	if err := writer.DeleteMenu(ctx, strings.TrimSpace(id)); err != nil {
		return err
	}
	return s.invalidate(ctx)
}

func (s *Service) ReorderMenus(ctx context.Context, input MenuReorderInput) error {
	if s == nil || s.Menus == nil {
		return ErrRepositoryMissing
	}
	writer, ok := s.Menus.(menuWriter)
	if !ok {
		return ErrRepositoryMissing
	}
	if len(input.Items) == 0 || len(input.Items) > 500 {
		return ErrInvalidMenu
	}
	seen := make(map[string]struct{}, len(input.Items))
	for _, item := range input.Items {
		item.ID = strings.TrimSpace(item.ID)
		item.ParentID = strings.TrimSpace(item.ParentID)
		if item.ID == "" || item.Sort < -1000000 || item.Sort > 1000000 {
			return ErrInvalidMenu
		}
		if _, exists := seen[item.ID]; exists {
			return ErrInvalidMenu
		}
		seen[item.ID] = struct{}{}
		if _, err := writer.FindMenu(ctx, item.ID); err != nil {
			return err
		}
	}
	// Validate the complete proposed tree before handing the batch to a
	// repository. This catches parent cycles and button parents atomically even
	// when the adapter only exposes a primitive update port.
	menus, err := s.ListMenus(ctx)
	if err != nil {
		return err
	}
	byID := make(map[string]domain.Menu, len(menus))
	for _, menu := range menus {
		byID[menu.ID] = menu
	}
	for _, item := range input.Items {
		menu, ok := byID[item.ID]
		if !ok {
			return domain.ErrResourceNotFound
		}
		menu.ParentID = strings.TrimSpace(item.ParentID)
		menu.Sort = item.Sort
		byID[item.ID] = menu
	}
	for _, menu := range byID {
		if menu.ParentID == "" {
			continue
		}
		parent, ok := byID[menu.ParentID]
		if !ok || parent.Type == domain.MenuTypeButton {
			return ErrInvalidMenu
		}
	}
	proposed := make([]domain.Menu, 0, len(byID))
	for _, menu := range byID {
		proposed = append(proposed, menu)
	}
	if _, err := BuildMenuRoutes(proposed); err != nil {
		return ErrInvalidMenu
	}
	if err := writer.ReorderMenus(ctx, input.Items); err != nil {
		return err
	}
	return s.invalidate(ctx)
}

func (s *Service) validateMenuParent(ctx context.Context, menu domain.Menu) error {
	if menu.ParentID == "" {
		return nil
	}
	if menu.ParentID == menu.ID {
		return ErrInvalidMenu
	}
	repo, ok := s.Menus.(menuFinder)
	if !ok {
		return ErrRepositoryMissing
	}
	parent, err := repo.FindMenu(ctx, menu.ParentID)
	if err != nil {
		return err
	}
	if parent.Type == domain.MenuTypeButton {
		return ErrInvalidMenu
	}
	// Walk the existing chain so updating a node cannot introduce a cycle.
	seen := map[string]struct{}{menu.ID: {}}
	for current := parent; current.ParentID != ""; {
		if _, exists := seen[current.ID]; exists {
			return ErrInvalidMenu
		}
		seen[current.ID] = struct{}{}
		next, findErr := repo.FindMenu(ctx, current.ParentID)
		if errors.Is(findErr, domain.ErrResourceNotFound) {
			break
		}
		if findErr != nil {
			return findErr
		}
		current = next
	}
	return nil
}

func (s *Service) SaveMenu(ctx context.Context, menu domain.Menu) error {
	if s == nil {
		return ErrRepositoryMissing
	}
	repo, ok := s.Menus.(menuSaver)
	if !ok {
		return ErrRepositoryMissing
	}
	if err := repo.SaveMenu(ctx, menu); err != nil {
		return err
	}
	return s.invalidate(ctx)
}

func (s *Service) ListPermissions(ctx context.Context) ([]domain.Permission, error) {
	if s == nil {
		return nil, ErrRepositoryMissing
	}
	repo, ok := s.Permissions.(permissionLister)
	if !ok {
		return nil, ErrRepositoryMissing
	}
	return repo.ListPermissions(ctx)
}

func (s *Service) SavePermission(ctx context.Context, permission domain.Permission) error {
	if s == nil {
		return ErrRepositoryMissing
	}
	repo, ok := s.Permissions.(permissionSaver)
	if !ok {
		return ErrRepositoryMissing
	}
	if err := repo.SavePermission(ctx, permission); err != nil {
		return err
	}
	return s.invalidate(ctx)
}

func (s *Service) ListPolicies(ctx context.Context) ([]domain.Policy, error) {
	if s == nil || s.Policies == nil {
		return nil, ErrRepositoryMissing
	}
	return s.Policies.ListPolicies(ctx)
}

func (s *Service) SavePolicy(ctx context.Context, policy domain.Policy) error {
	if s == nil {
		return ErrRepositoryMissing
	}
	repo, ok := s.Policies.(interface {
		SavePolicy(context.Context, domain.Policy) error
	})
	if !ok {
		return ErrRepositoryMissing
	}
	if err := repo.SavePolicy(ctx, policy); err != nil {
		return err
	}
	return s.invalidate(ctx)
}

func (s *Service) ListDataScopes(ctx context.Context) ([]domain.DataScope, error) {
	if s == nil || s.DataScopes == nil {
		return nil, ErrRepositoryMissing
	}
	return s.DataScopes.ListDataScopes(ctx)
}

func (s *Service) SaveDataScope(ctx context.Context, scope domain.DataScope) error {
	if s == nil {
		return ErrRepositoryMissing
	}
	repo, ok := s.DataScopes.(interface {
		SaveDataScope(context.Context, domain.DataScope) error
	})
	if !ok {
		return ErrRepositoryMissing
	}
	if err := repo.SaveDataScope(ctx, scope); err != nil {
		return err
	}
	return s.invalidate(ctx)
}
