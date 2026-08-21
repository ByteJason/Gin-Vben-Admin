// Package iam defines framework-neutral identity and authorization contracts.
package iam

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrAccessDenied      = errors.New("access denied")
	ErrInvalidSubject    = errors.New("subject is required")
	ErrInvalidPolicy     = errors.New("policy is invalid")
	ErrDataScopeNotFound = errors.New("data scope not found")
	ErrResourceNotFound  = errors.New("resource not found")
)

type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

type Scope string

const (
	ScopeAll    Scope = "all"
	ScopeOwn    Scope = "own"
	ScopeOrg    Scope = "org"
	ScopeCustom Scope = "custom"
)

type User struct {
	ID, Username, DisplayName string
	UsernameNormalized        string
	Email, EmailNormalized    string
	Nickname, Avatar, Phone   string
	LastLoginIP               string
	LastLoginAt               time.Time
	PasswordChangedAt         time.Time
	TenantID, OrgID           string
	Active                    bool
	RoleIDs                   []string
}

type Role struct {
	ID, Name  string
	Active    bool
	UserIDs   []string
	DataScope Scope
}

type Menu struct {
	ID, ParentID, Name, Path string
	Visible, Active          bool
}

type Permission struct {
	ID, Name, Method, Path string
	Active                 bool
}

// Policy follows the Casbin subject/domain/object/action model. Method and
// Path are the HTTP-friendly aliases for Action and Object; either pair may
// be used by adapters, but a request must resolve to one effective pair.
type Policy struct {
	Subject, RoleID, PermissionID, Domain string
	Method, Path, Action, Object          string
	Effect                                Effect
}

type DataScope struct {
	Subject, RoleID, Domain, Resource string
	Scope                             Scope
	IDs                               []string
}

type Subject struct {
	UserID    string
	RoleIDs   []string
	Domain    string
	Superuser bool
}

type Request struct {
	Domain         string
	Method, Path   string
	Action, Object string
}

type UserRepository interface {
	FindUser(context.Context, string) (User, error)
	SaveUser(context.Context, User) error
}
type RoleRepository interface {
	FindRole(context.Context, string) (Role, error)
	SaveRole(context.Context, Role) error
}
type MenuRepository interface {
	ListMenus(context.Context) ([]Menu, error)
}
type PermissionRepository interface {
	ListPermissions(context.Context) ([]Permission, error)
}
type PolicyStore interface {
	ListPolicies(context.Context) ([]Policy, error)
}
type Authorizer interface {
	Authorize(context.Context, Subject, Request) (bool, error)
}
type DataScopeStore interface {
	ListDataScopes(context.Context) ([]DataScope, error)
}
type DataScopeResolver interface {
	Resolve(context.Context, Subject, string) (DataScope, error)
}

// MemoryPolicyStore is a deterministic, concurrency-safe policy seam used by
// unit tests and local bootstrap. Production adapters can implement the same
// PolicyStore/DataScopeStore interfaces without changing authorization rules.
type MemoryPolicyStore struct {
	mu       sync.RWMutex
	policies []Policy
	scopes   []DataScope
}

func NewMemoryPolicyStore() *MemoryPolicyStore { return &MemoryPolicyStore{} }

// Add preserves the original fixture API. Invalid values are ignored; typed
// AddPolicy/AddDataScope should be used by application code when errors matter.
func (m *MemoryPolicyStore) Add(value interface{}) {
	switch p := value.(type) {
	case Policy:
		_ = m.AddPolicy(p)
	case DataScope:
		_ = m.AddDataScope(p)
	}
}

func (m *MemoryPolicyStore) AddPolicy(p Policy) error {
	if err := ValidatePolicy(p); err != nil {
		return err
	}
	if p.Effect == "" {
		p.Effect = EffectDeny
	}
	m.mu.Lock()
	m.policies = append(m.policies, p)
	m.mu.Unlock()
	return nil
}

func (m *MemoryPolicyStore) AddDataScope(s DataScope) error {
	if strings.TrimSpace(s.Resource) == "" || s.Scope == "" || (strings.TrimSpace(s.Subject) == "" && strings.TrimSpace(s.RoleID) == "") {
		return ErrDataScopeNotFound
	}
	s.IDs = append([]string(nil), s.IDs...)
	m.mu.Lock()
	m.scopes = append(m.scopes, s)
	m.mu.Unlock()
	return nil
}

func ValidatePolicy(p Policy) error {
	if strings.TrimSpace(p.Subject) == "" && strings.TrimSpace(p.RoleID) == "" {
		return ErrInvalidPolicy
	}
	if effectiveAction(p) == "" || effectiveObject(p) == "" {
		return ErrInvalidPolicy
	}
	if p.Effect != "" && p.Effect != EffectAllow && p.Effect != EffectDeny {
		return ErrInvalidPolicy
	}
	return nil
}

func (m *MemoryPolicyStore) ListPolicies(ctx context.Context) ([]Policy, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Policy(nil), m.policies...), nil
}

func (m *MemoryPolicyStore) ListDataScopes(ctx context.Context) ([]DataScope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]DataScope, 0, len(m.scopes))
	for _, scope := range m.scopes {
		out = append(out, cloneScope(scope))
	}
	return out, nil
}

type memoryAuthorizer struct{ store PolicyStore }

func NewAuthorizer(store PolicyStore) Authorizer { return &memoryAuthorizer{store: store} }

func (a *memoryAuthorizer) Authorize(ctx context.Context, subject Subject, request Request) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if subject.Superuser {
		return true, nil
	}
	if strings.TrimSpace(subject.UserID) == "" {
		return false, ErrInvalidSubject
	}
	if a == nil || a.store == nil {
		return false, ErrAccessDenied
	}
	policies, err := a.store.ListPolicies(ctx)
	if err != nil {
		return false, err
	}
	allowed := false
	for _, policy := range policies {
		if !policyMatches(policy, subject, request) {
			continue
		}
		if policy.Effect == EffectDeny || policy.Effect == "" {
			return false, ErrAccessDenied
		}
		if policy.Effect == EffectAllow {
			allowed = true
		}
	}
	if !allowed {
		return false, ErrAccessDenied
	}
	return true, nil
}

func policyMatches(policy Policy, subject Subject, request Request) bool {
	if policy.Subject != "" && policy.Subject != subject.UserID {
		return false
	}
	if policy.RoleID != "" && !contains(subject.RoleIDs, policy.RoleID) {
		return false
	}
	if policy.Subject == "" && policy.RoleID == "" {
		return false
	}
	if policy.Domain != "" {
		if subject.Domain != "" && policy.Domain != subject.Domain {
			return false
		}
		if request.Domain != "" && policy.Domain != request.Domain {
			return false
		}
	}
	if subject.Domain != "" && request.Domain != "" && subject.Domain != request.Domain {
		return false
	}
	return methodMatches(effectiveAction(policy), effectiveAction(request)) && pathMatches(effectiveObject(policy), effectiveObject(request))
}

func effectiveAction(v interface{}) string {
	switch x := v.(type) {
	case Policy:
		if strings.TrimSpace(x.Action) != "" {
			return strings.ToUpper(strings.TrimSpace(x.Action))
		}
		return strings.ToUpper(strings.TrimSpace(x.Method))
	case Request:
		if strings.TrimSpace(x.Action) != "" {
			return strings.ToUpper(strings.TrimSpace(x.Action))
		}
		return strings.ToUpper(strings.TrimSpace(x.Method))
	default:
		return ""
	}
}

func effectiveObject(v interface{}) string {
	switch x := v.(type) {
	case Policy:
		if strings.TrimSpace(x.Object) != "" {
			return strings.TrimSpace(x.Object)
		}
		return strings.TrimSpace(x.Path)
	case Request:
		if strings.TrimSpace(x.Object) != "" {
			return strings.TrimSpace(x.Object)
		}
		return strings.TrimSpace(x.Path)
	default:
		return ""
	}
}

func methodMatches(policy, request string) bool { return policy == "*" || policy == request }

func pathMatches(policy, request string) bool {
	if policy == "*" || policy == request {
		return true
	}
	if strings.HasSuffix(policy, "/*") {
		prefix := strings.TrimSuffix(policy, "*")
		return strings.HasPrefix(request, prefix)
	}
	pp, rp := strings.Split(strings.Trim(policy, "/"), "/"), strings.Split(strings.Trim(request, "/"), "/")
	if len(pp) != len(rp) {
		return false
	}
	for i := range pp {
		if strings.HasPrefix(pp[i], ":") || pp[i] == "*" {
			continue
		}
		if pp[i] != rp[i] {
			return false
		}
	}
	return len(pp) > 0
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func NewMemoryDataScopeResolver(store DataScopeStore) DataScopeResolver {
	return &genericScopeResolver{store: store}
}

type genericScopeResolver struct{ store DataScopeStore }

func (r *genericScopeResolver) Resolve(ctx context.Context, subject Subject, resource string) (DataScope, error) {
	if err := ctx.Err(); err != nil {
		return DataScope{}, err
	}
	if subject.Superuser {
		return DataScope{Subject: subject.UserID, Domain: subject.Domain, Resource: resource, Scope: ScopeAll}, nil
	}
	if r == nil || r.store == nil {
		return DataScope{}, ErrDataScopeNotFound
	}
	scopes, err := r.store.ListDataScopes(ctx)
	if err != nil {
		return DataScope{}, err
	}
	// A direct user rule is more specific than a role rule. Role order is
	// supplied by the subject so adapters can make precedence deterministic.
	for _, scope := range scopes {
		if scope.Subject == subject.UserID && scope.Resource == resource && scopeDomainMatches(scope, subject) {
			return cloneScope(scope), nil
		}
	}
	for _, roleID := range subject.RoleIDs {
		for _, scope := range scopes {
			if scope.RoleID == roleID && scope.Resource == resource && scopeDomainMatches(scope, subject) {
				return cloneScope(scope), nil
			}
		}
	}
	return DataScope{}, ErrDataScopeNotFound
}

func scopeDomainMatches(scope DataScope, subject Subject) bool {
	return scope.Domain == "" || scope.Domain == subject.Domain
}

func cloneScope(scope DataScope) DataScope {
	scope.IDs = append([]string(nil), scope.IDs...)
	return scope
}
