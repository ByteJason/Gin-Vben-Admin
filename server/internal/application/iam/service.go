// Package iam provides the application seam for RBAC administration and checks.
package iam

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	domain "example.com/gin-vben-admin/server/internal/domain/iam"
)

var (
	ErrInvalidID         = errors.New("id is required")
	ErrRepositoryMissing = errors.New("iam repository capability is unavailable")
)

// MemoryStore is a local adapter used by unit tests and the initial bootstrap.
// Its methods intentionally implement the same repository seams used by the
// persistent adapter, so handlers do not depend on map storage.
type MemoryStore struct {
	mu          sync.RWMutex
	users       map[string]domain.User
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

func (s *MemoryStore) ListRoles(ctx context.Context) ([]domain.Role, error) {
	if err := check(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Role, 0, len(s.roles))
	for _, role := range s.roles {
		out = append(out, cloneRole(role))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *MemoryStore) SaveMenu(ctx context.Context, menu domain.Menu) error {
	return s.save(ctx, menu.ID, func() { s.menus[menu.ID] = menu })
}

func (s *MemoryStore) ListMenus(ctx context.Context) ([]domain.Menu, error) {
	if err := check(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Menu, 0, len(s.menus))
	for _, menu := range s.menus {
		out = append(out, menu)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *MemoryStore) SavePermission(ctx context.Context, permission domain.Permission) error {
	return s.save(ctx, permission.ID, func() { s.permissions[permission.ID] = permission })
}

func (s *MemoryStore) ListPermissions(ctx context.Context) ([]domain.Permission, error) {
	if err := check(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Permission, 0, len(s.permissions))
	for _, permission := range s.permissions {
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
	return append([]domain.Policy(nil), s.policies...), nil
}

func (s *MemoryStore) SaveDataScope(ctx context.Context, scope domain.DataScope) error {
	if err := check(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(scope.Resource) == "" || scope.Scope == "" || (strings.TrimSpace(scope.Subject) == "" && strings.TrimSpace(scope.RoleID) == "") {
		return domain.ErrDataScopeNotFound
	}
	scope.IDs = append([]string(nil), scope.IDs...)
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.DataScope, 0, len(s.scopes))
	for _, scope := range s.scopes {
		scope.IDs = append([]string(nil), scope.IDs...)
		out = append(out, scope)
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
	return role
}

type Service struct {
	Users       domain.UserRepository
	Roles       domain.RoleRepository
	Menus       domain.MenuRepository
	Permissions domain.PermissionRepository
	Policies    domain.PolicyStore
	DataScopes  domain.DataScopeStore
	Authorizer  domain.Authorizer
	Scopes      domain.DataScopeResolver
	cache       DecisionCache
}

type userLister interface {
	ListUsers(context.Context) ([]domain.User, error)
}
type roleLister interface {
	ListRoles(context.Context) ([]domain.Role, error)
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
type roleSaver interface {
	SaveRole(context.Context, domain.Role) error
}
type menuSaver interface {
	SaveMenu(context.Context, domain.Menu) error
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
		Authorizer:  domain.NewAuthorizer(policies),
		Scopes:      domain.NewMemoryDataScopeResolver(scopes),
	}
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

func (s *Service) invalidate(ctx context.Context) error {
	return InvalidatePermissionCache(ctx, s.cache)
}

func (s *Service) Authorize(ctx context.Context, subject domain.Subject, request domain.Request) (bool, error) {
	if s == nil || s.Authorizer == nil {
		return false, domain.ErrAccessDenied
	}
	return s.Authorizer.Authorize(ctx, subject, request)
}

func (s *Service) ResolveDataScope(ctx context.Context, subject domain.Subject, resource string) (domain.DataScope, error) {
	if s == nil || s.Scopes == nil {
		return domain.DataScope{}, domain.ErrDataScopeNotFound
	}
	return s.Scopes.Resolve(ctx, subject, resource)
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
	return repo.ListRoles(ctx)
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
