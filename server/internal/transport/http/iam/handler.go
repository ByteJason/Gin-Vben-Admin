// Package iamhttp exposes the protected management RBAC API.
package iamhttp

import (
	"errors"
	"net/http"
	"strings"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	iamapp "example.com/gin-vben-admin/server/internal/application/iam"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	domain "example.com/gin-vben-admin/server/internal/domain/iam"
	authhttp "example.com/gin-vben-admin/server/internal/transport/http/auth"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const (
	basePath          = "/api/admin/v1/iam"
	codeBadRequest    = 10000
	codeForbidden     = 30000
	codeUnavailable   = 40001
	codeInternalError = 50000
)

type Handler struct {
	service *iamapp.Service
	auth    appauth.AuthService
}

func NewHandler(service *iamapp.Service, auth appauth.AuthService) *Handler {
	return &Handler{service: service, auth: auth}
}

// RegisterRoutes installs protected CRUD/read seams. The server remains
// default-deny: a missing service or auth dependency exposes only 503 stubs.
func RegisterRoutes(r gin.IRouter, handler *Handler) {
	group := r.Group(basePath)
	if handler == nil || handler.service == nil || handler.auth == nil {
		registerDisabled(group)
		return
	}
	group.Use(authhttp.Middleware(handler.auth))
	group.GET("/me", handler.currentUser)
	group.GET("/users", handler.listUsers)
	group.POST("/users", handler.createUser)
	group.GET("/roles", handler.listRoles)
	group.POST("/roles", handler.createRole)
	group.GET("/menus", handler.listMenus)
	group.GET("/permissions", handler.listPermissions)
	group.GET("/policies", handler.listPolicies)
	group.POST("/policies", handler.createPolicy)
	group.GET("/data-scopes", handler.listDataScopes)
	group.POST("/data-scopes", handler.createDataScope)
	// The UI contract uses this compact menu path; it is the same guarded
	// collection as /iam/menus and deliberately has no separate policy source.
	r.Group("/api/admin/v1").Use(authhttp.Middleware(handler.auth)).GET("/menu/all", handler.listMenus)
}

func registerDisabled(group *gin.RouterGroup) {
	for _, path := range []string{"/me", "/users", "/roles", "/menus", "/permissions", "/policies", "/data-scopes"} {
		group.GET(path, disabled)
		group.POST(path, disabled)
	}
}

func disabled(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, codeUnavailable, "dependency unavailable")
}

func (h *Handler) guard(c *gin.Context) bool {
	_, allowed := h.authorizedUser(c)
	return allowed
}

func (h *Handler) authorizedUser(c *gin.Context) (domain.User, bool) {
	if h == nil || h.service == nil || h.service.Users == nil {
		response.Error(c, http.StatusServiceUnavailable, codeUnavailable, "dependency unavailable")
		return domain.User{}, false
	}
	value, exists := c.Get("auth_claims")
	claims, ok := value.(authdomain.Claims)
	if !exists || !ok || strings.TrimSpace(claims.Subject) == "" {
		response.Error(c, http.StatusUnauthorized, 20000, "unauthenticated")
		return domain.User{}, false
	}
	user, err := h.service.Users.FindUser(c.Request.Context(), claims.Subject)
	if err != nil {
		if errors.Is(err, iamapp.ErrRepositoryMissing) {
			response.Error(c, http.StatusServiceUnavailable, codeUnavailable, "dependency unavailable")
		} else {
			response.Error(c, http.StatusForbidden, codeForbidden, "forbidden")
		}
		return domain.User{}, false
	}
	if !user.Active {
		response.Error(c, http.StatusForbidden, codeForbidden, "forbidden")
		return domain.User{}, false
	}
	allowed, err := h.service.Authorize(c.Request.Context(), domain.Subject{UserID: user.ID, RoleIDs: user.RoleIDs}, domain.Request{
		Domain: c.GetHeader("X-Tenant-ID"),
		Method: c.Request.Method,
		Path:   c.FullPath(),
	})
	if err != nil || !allowed {
		if errors.Is(err, domain.ErrAccessDenied) || err == nil {
			response.Error(c, http.StatusForbidden, codeForbidden, "forbidden")
		} else {
			response.Error(c, http.StatusInternalServerError, codeInternalError, "internal error")
		}
		return domain.User{}, false
	}
	return user, true
}

type userRequest struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Active      *bool    `json:"active"`
	RoleIDs     []string `json:"roleIds"`
}

type userResponse struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Active      bool     `json:"active"`
	RoleIDs     []string `json:"roleIds"`
}

type currentUserResponse struct {
	UserID   string   `json:"userId"`
	Username string   `json:"username"`
	RealName string   `json:"realName"`
	Avatar   string   `json:"avatar"`
	Roles    []string `json:"roles"`
	HomePath string   `json:"homePath"`
	Desc     string   `json:"desc"`
}

func (h *Handler) currentUser(c *gin.Context) {
	user, allowed := h.authorizedUser(c)
	if !allowed {
		return
	}
	response.OK(c, currentUserResponse{
		UserID: user.ID, Username: user.Username, RealName: user.DisplayName,
		Roles: append([]string(nil), user.RoleIDs...), HomePath: "/analytics",
	})
}

func (h *Handler) listUsers(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	users, err := h.service.ListUsers(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	out := make([]userResponse, 0, len(users))
	for _, user := range users {
		out = append(out, userResponse{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Active: user.Active, RoleIDs: append([]string(nil), user.RoleIDs...)})
	}
	response.OK(c, out)
}

func (h *Handler) createUser(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	var req userRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Username) == "" {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	if err := h.service.SaveUser(c.Request.Context(), domain.User{ID: strings.TrimSpace(req.ID), Username: strings.TrimSpace(req.Username), DisplayName: req.DisplayName, Active: active, RoleIDs: req.RoleIDs}); err != nil {
		writeServiceError(c, err)
		return
	}
	response.OK(c, userResponse{ID: req.ID, Username: req.Username, DisplayName: req.DisplayName, Active: active, RoleIDs: req.RoleIDs})
}

type roleRequest struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Active    *bool        `json:"active"`
	DataScope domain.Scope `json:"dataScope"`
}

type roleResponse struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Active    bool         `json:"active"`
	DataScope domain.Scope `json:"dataScope"`
	UserIDs   []string     `json:"userIds"`
}

func (h *Handler) listRoles(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	roles, err := h.service.ListRoles(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	out := make([]roleResponse, 0, len(roles))
	for _, role := range roles {
		out = append(out, roleResponse{ID: role.ID, Name: role.Name, Active: role.Active, DataScope: role.DataScope, UserIDs: append([]string(nil), role.UserIDs...)})
	}
	response.OK(c, out)
}

func (h *Handler) createRole(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	var req roleRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Name) == "" {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	if req.DataScope == "" {
		req.DataScope = domain.ScopeOwn
	}
	role := domain.Role{ID: strings.TrimSpace(req.ID), Name: strings.TrimSpace(req.Name), Active: active, DataScope: req.DataScope}
	if err := h.service.SaveRole(c.Request.Context(), role); err != nil {
		writeServiceError(c, err)
		return
	}
	response.OK(c, roleResponse{ID: role.ID, Name: role.Name, Active: role.Active, DataScope: role.DataScope})
}

type menuResponse struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Visible  bool   `json:"visible"`
	Active   bool   `json:"active"`
}

func (h *Handler) listMenus(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	menus, err := h.service.ListMenus(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	out := make([]menuResponse, 0, len(menus))
	for _, menu := range menus {
		out = append(out, menuResponse{ID: menu.ID, ParentID: menu.ParentID, Name: menu.Name, Path: menu.Path, Visible: menu.Visible, Active: menu.Active})
	}
	response.OK(c, out)
}

type permissionResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Active bool   `json:"active"`
}

func (h *Handler) listPermissions(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	permissions, err := h.service.ListPermissions(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	out := make([]permissionResponse, 0, len(permissions))
	for _, permission := range permissions {
		out = append(out, permissionResponse{ID: permission.ID, Name: permission.Name, Method: permission.Method, Path: permission.Path, Active: permission.Active})
	}
	response.OK(c, out)
}

type policyRequest struct {
	Subject string        `json:"subject"`
	RoleID  string        `json:"roleId"`
	Domain  string        `json:"domain"`
	Method  string        `json:"method"`
	Path    string        `json:"path"`
	Effect  domain.Effect `json:"effect"`
}

type policyResponse struct {
	Subject string        `json:"subject,omitempty"`
	RoleID  string        `json:"roleId,omitempty"`
	Domain  string        `json:"domain,omitempty"`
	Method  string        `json:"method"`
	Path    string        `json:"path"`
	Effect  domain.Effect `json:"effect"`
}

func (h *Handler) listPolicies(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	policies, err := h.service.ListPolicies(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	out := make([]policyResponse, 0, len(policies))
	for _, policy := range policies {
		out = append(out, policyResponse{Subject: policy.Subject, RoleID: policy.RoleID, Domain: policy.Domain, Method: policy.Method, Path: policy.Path, Effect: policy.Effect})
	}
	response.OK(c, out)
}

func (h *Handler) createPolicy(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	var req policyRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Method) == "" || strings.TrimSpace(req.Path) == "" || (strings.TrimSpace(req.Subject) == "" && strings.TrimSpace(req.RoleID) == "") {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	if req.Effect == "" {
		req.Effect = domain.EffectDeny
	}
	if req.Effect != domain.EffectAllow && req.Effect != domain.EffectDeny {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	policy := domain.Policy{Subject: strings.TrimSpace(req.Subject), RoleID: strings.TrimSpace(req.RoleID), Domain: strings.TrimSpace(req.Domain), Method: strings.ToUpper(strings.TrimSpace(req.Method)), Path: strings.TrimSpace(req.Path), Effect: req.Effect}
	if err := h.service.SavePolicy(c.Request.Context(), policy); err != nil {
		writeServiceError(c, err)
		return
	}
	response.OK(c, policyResponse{Subject: policy.Subject, RoleID: policy.RoleID, Domain: policy.Domain, Method: policy.Method, Path: policy.Path, Effect: policy.Effect})
}

type dataScopeRequest struct {
	Subject  string       `json:"subject"`
	RoleID   string       `json:"roleId"`
	Domain   string       `json:"domain"`
	Resource string       `json:"resource"`
	Scope    domain.Scope `json:"scope"`
	IDs      []string     `json:"ids"`
}

type dataScopeResponse struct {
	Subject  string       `json:"subject,omitempty"`
	RoleID   string       `json:"roleId,omitempty"`
	Domain   string       `json:"domain,omitempty"`
	Resource string       `json:"resource"`
	Scope    domain.Scope `json:"scope"`
	IDs      []string     `json:"ids"`
}

func (h *Handler) listDataScopes(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	scopes, err := h.service.ListDataScopes(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	out := make([]dataScopeResponse, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, dataScopeResponse{Subject: scope.Subject, RoleID: scope.RoleID, Domain: scope.Domain, Resource: scope.Resource, Scope: scope.Scope, IDs: append([]string(nil), scope.IDs...)})
	}
	response.OK(c, out)
}

func (h *Handler) createDataScope(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	var req dataScopeRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Resource) == "" || req.Scope == "" || (strings.TrimSpace(req.Subject) == "" && strings.TrimSpace(req.RoleID) == "") {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	scope := domain.DataScope{Subject: strings.TrimSpace(req.Subject), RoleID: strings.TrimSpace(req.RoleID), Domain: strings.TrimSpace(req.Domain), Resource: strings.TrimSpace(req.Resource), Scope: req.Scope, IDs: append([]string(nil), req.IDs...)}
	if err := h.service.SaveDataScope(c.Request.Context(), scope); err != nil {
		writeServiceError(c, err)
		return
	}
	response.OK(c, dataScopeResponse{Subject: scope.Subject, RoleID: scope.RoleID, Domain: scope.Domain, Resource: scope.Resource, Scope: scope.Scope, IDs: scope.IDs})
}

func writeServiceError(c *gin.Context, err error) {
	if errors.Is(err, iamapp.ErrRepositoryMissing) || errors.Is(err, authdomain.ErrDependencyUnavailable) {
		response.Error(c, http.StatusServiceUnavailable, codeUnavailable, "dependency unavailable")
		return
	}
	if errors.Is(err, iamapp.ErrInvalidID) || errors.Is(err, domain.ErrInvalidPolicy) {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	response.Error(c, http.StatusInternalServerError, codeInternalError, "internal error")
}
