// Package iamhttp exposes the protected management RBAC API.
package iamhttp

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	iamapp "example.com/gin-vben-admin/server/internal/application/iam"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	domain "example.com/gin-vben-admin/server/internal/domain/iam"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	authhttp "example.com/gin-vben-admin/server/internal/transport/http/auth"
	httpmiddleware "example.com/gin-vben-admin/server/internal/transport/http/middleware"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const (
	basePath          = "/api/admin/v1/iam"
	codeBadRequest    = 10000
	codeNotFound      = 10001
	codeUserConflict  = 10011
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
func RegisterRoutes(r gin.IRouter, handler *Handler, policies ...httpmiddleware.TenantPolicy) {
	group := r.Group(basePath)
	if handler == nil || handler.service == nil || handler.auth == nil {
		registerDisabled(group)
		return
	}
	policy := httpmiddleware.TenantPolicy{Mode: "single", DefaultTenantID: "default"}
	if len(policies) > 0 {
		policy = policies[0]
	}
	group.Use(authhttp.Middleware(handler.auth), httpmiddleware.TenantContext(policy))
	group.GET("/me", handler.currentUser)
	group.GET("/users", handler.listUsers)
	group.GET("/users/:id", handler.getUser)
	group.POST("/users", handler.createUser)
	group.PATCH("/users/:id", handler.updateUser)
	group.DELETE("/users/:id", handler.deleteUser)
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
	r.Group("/api/admin/v1").Use(authhttp.Middleware(handler.auth), httpmiddleware.TenantContext(policy)).GET("/menu/all", handler.listMenus)
}

func registerDisabled(group *gin.RouterGroup) {
	for _, path := range []string{"/me", "/users", "/users/:id", "/roles", "/menus", "/permissions", "/policies", "/data-scopes"} {
		group.GET(path, disabled)
		group.POST(path, disabled)
		group.PATCH(path, disabled)
		group.DELETE(path, disabled)
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
	scope, err := tenant.RequireContext(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid tenant context")
		return domain.User{}, false
	}
	allowed, err := h.service.Authorize(c.Request.Context(), domain.Subject{UserID: user.ID, RoleIDs: user.RoleIDs, Domain: scope.TenantID}, domain.Request{
		Domain: scope.TenantID,
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

type userCreateRequest struct {
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	OrgID    string `json:"orgId"`
	Active   *bool  `json:"active"`
	Password string `json:"password"`
}

type userUpdateRequest struct {
	Username *string `json:"username"`
	Nickname *string `json:"nickname"`
	Avatar   *string `json:"avatar"`
	Email    *string `json:"email"`
	Phone    *string `json:"phone"`
	OrgID    *string `json:"orgId"`
	Active   *bool   `json:"active"`
}

type userResponse struct {
	ID                string     `json:"id"`
	Username          string     `json:"username"`
	DisplayName       string     `json:"displayName"`
	Nickname          string     `json:"nickname"`
	Avatar            string     `json:"avatar"`
	Email             string     `json:"email"`
	Phone             string     `json:"phone"`
	LastLoginIP       string     `json:"lastLoginIp"`
	LastLoginAt       *time.Time `json:"lastLoginAt,omitempty"`
	PasswordChangedAt *time.Time `json:"passwordChangedAt,omitempty"`
	TenantID          string     `json:"tenantId"`
	OrgID             string     `json:"orgId"`
	Active            bool       `json:"active"`
	Status            string     `json:"status"`
	RoleIDs           []string   `json:"roleIds"`
}

type userPageResponse struct {
	Items    []userResponse `json:"items"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
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
	query, err := parseUserListQuery(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid user query")
		return
	}
	page, err := h.service.ListUsersPage(c.Request.Context(), query)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	out := userPageResponse{Items: make([]userResponse, 0, len(page.Items)), Total: page.Total, Page: page.Page, PageSize: page.PageSize}
	for _, user := range page.Items {
		out.Items = append(out.Items, responseFromUser(user))
	}
	response.OK(c, out)
}

func (h *Handler) getUser(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	user, err := h.service.GetUser(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.OK(c, responseFromUser(user))
}

func parseUserListQuery(c *gin.Context) (domain.UserListQuery, error) {
	query := domain.UserListQuery{Keyword: c.Query("keyword"), Status: c.Query("status"), RoleID: c.Query("roleId"), OrgID: c.Query("orgId"), Sort: c.Query("sort")}
	var err error
	if value := strings.TrimSpace(c.Query("page")); value != "" {
		query.Page, err = strconv.Atoi(value)
		if err != nil {
			return domain.UserListQuery{}, err
		}
	}
	if value := strings.TrimSpace(c.Query("pageSize")); value != "" {
		query.PageSize, err = strconv.Atoi(value)
		if err != nil {
			return domain.UserListQuery{}, err
		}
	}
	return query, nil
}

func responseFromUser(user domain.User) userResponse {
	return userResponse{
		ID: user.ID, Username: user.Username, DisplayName: user.DisplayName,
		Nickname: user.Nickname, Avatar: user.Avatar, Email: user.Email, Phone: user.Phone,
		LastLoginIP: user.LastLoginIP, LastLoginAt: timePointer(user.LastLoginAt),
		PasswordChangedAt: timePointer(user.PasswordChangedAt), TenantID: user.TenantID, OrgID: user.OrgID,
		Active: user.Active, Status: userStatus(user.Active), RoleIDs: append([]string(nil), user.RoleIDs...),
	}
}

func userStatus(active bool) string {
	if active {
		return "active"
	}
	return "disabled"
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func (h *Handler) createUser(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	var req userCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	user, err := h.service.CreateUser(c.Request.Context(), iamapp.UserCreateInput{
		Username: req.Username, Nickname: req.Nickname, Avatar: req.Avatar, Email: req.Email,
		Phone: req.Phone, OrgID: req.OrgID, Active: req.Active, Password: req.Password,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.OK(c, responseFromUser(user))
}

func (h *Handler) updateUser(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	var req userUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	user, err := h.service.UpdateUser(c.Request.Context(), c.Param("id"), iamapp.UserUpdateInput{
		Username: req.Username, Nickname: req.Nickname, Avatar: req.Avatar, Email: req.Email,
		Phone: req.Phone, OrgID: req.OrgID, Active: req.Active,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.OK(c, responseFromUser(user))
}

func (h *Handler) deleteUser(c *gin.Context) {
	if !h.guard(c) {
		return
	}
	if _, err := h.service.DeleteUser(c.Request.Context(), c.Param("id")); err != nil {
		writeServiceError(c, err)
		return
	}
	response.OK(c, nil)
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
	if errors.Is(err, domain.ErrUserConflict) {
		response.Error(c, http.StatusConflict, codeUserConflict, "user already exists")
		return
	}
	if errors.Is(err, domain.ErrResourceNotFound) {
		response.Error(c, http.StatusNotFound, codeNotFound, "resource not found")
		return
	}
	if errors.Is(err, iamapp.ErrRepositoryMissing) || errors.Is(err, authdomain.ErrDependencyUnavailable) {
		response.Error(c, http.StatusServiceUnavailable, codeUnavailable, "dependency unavailable")
		return
	}
	if errors.Is(err, iamapp.ErrPasswordHasherMissing) {
		response.Error(c, http.StatusServiceUnavailable, codeUnavailable, "dependency unavailable")
		return
	}
	if errors.Is(err, iamapp.ErrInvalidID) || errors.Is(err, iamapp.ErrInvalidUserQuery) || errors.Is(err, iamapp.ErrInvalidUser) || errors.Is(err, domain.ErrInvalidPolicy) {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	if errors.Is(err, tenant.ErrTenantContextMissing) || errors.Is(err, tenant.ErrInvalidTenantID) || errors.Is(err, tenant.ErrInvalidOrganization) {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid tenant context")
		return
	}
	if errors.Is(err, tenant.ErrCrossTenant) || errors.Is(err, tenant.ErrOrganizationDenied) {
		response.Error(c, http.StatusForbidden, codeForbidden, "forbidden")
		return
	}
	response.Error(c, http.StatusInternalServerError, codeInternalError, "internal error")
}
