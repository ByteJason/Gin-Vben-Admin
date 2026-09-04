// Package commoncapabilities exposes the small, tenant-scoped management API
// shared by notification and media clients. It is deliberately adapter based:
// an absent application port is reported as a stable 503 rather than guessed.
package commoncapabilities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	mimepkg "mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	fileapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/file"
	mailapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/mail"
	notification "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/notification"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const basePath = "/api/admin/v1/common"
const maxMediaMultipartBytes int64 = 100 << 20
const maxMediaMultipartMemoryBytes int64 = 1 << 20

type Handler struct {
	Runtime *notification.Runtime
	Catalog fileapp.MediaCatalog
	Usage   fileapp.MediaUsageService
	Mail    *mailapp.Service
}

func NewHandler(runtime *notification.Runtime, catalog fileapp.MediaCatalog, usage ...fileapp.MediaUsageService) *Handler {
	var u fileapp.MediaUsageService
	if len(usage) > 0 {
		u = usage[0]
	}
	// Every management instance has one stable caller used for UI test sends.
	// Bootstrap also seeds it, while this small reconciliation keeps the direct
	// handler/test seam deterministic when a caller supplies a fresh runtime.
	if runtime != nil {
		if _, err := runtime.Caller("system.admin"); errors.Is(err, notification.ErrCallerNotFound) {
			_ = runtime.SetCaller(notification.Caller{Key: "system.admin", Name: "System administration", Module: "system", Enabled: true, SystemOwned: true})
		}
	}
	return &Handler{Runtime: runtime, Catalog: catalog, Usage: u}
}
func RegisterRoutes(r gin.IRouter, h *Handler) { RegisterRoutesOn(r.Group("/api/admin/v1"), h) }
func RegisterRoutesOn(r gin.IRouter, h *Handler) {
	// These paths mirror the versioned OpenAPI resource groups. /common is
	// retained as a compact compatibility alias for early clients.
	mountNotification(r.Group("/notification"), h)
	mountMedia(r.Group("/media"), h)
	mountNotification(r.Group("/common"), h)
	mountMedia(r.Group("/common"), h)
}
func mountNotification(g gin.IRouter, h *Handler) {
	g.GET("/callers", h.listCallers)
	g.POST("/callers", h.putCaller)
	g.GET("/callers/:id", h.getCaller)
	g.PATCH("/callers/:id", h.putCaller)
	g.PUT("/callers/:id", h.putCaller)
	g.DELETE("/callers/:id", h.deleteCaller)
	g.GET("/accounts", h.listAccounts)
	g.POST("/accounts", h.createAccount)
	g.PATCH("/accounts/:id", h.updateAccount)
	g.PUT("/accounts/:id", h.updateAccount)
	g.DELETE("/accounts/:id", h.deleteAccount)
	g.GET("/templates", h.listTemplates)
	g.POST("/templates", h.putTemplate)
	g.PATCH("/templates/:id", h.putTemplate)
	g.PUT("/templates/:id", h.putTemplate)
	g.DELETE("/templates/:id", h.deleteTemplate)
	g.POST("/templates/:id/publish", h.publishTemplate)
	g.POST("/templates/:id/test", h.testTemplate)
	g.GET("/verification-policies", h.listPolicies)
	g.PATCH("/verification-policies/:policy_key", h.putPolicy)
	g.PUT("/verification-policies/:policy_key", h.putPolicy)
	g.POST("/verification/challenges", h.issueChallenge)
	g.GET("/verification/challenges/:id", h.challengeStatus)
	g.POST("/verification/challenges/:id/verify", h.verifyChallenge)
}
func mountMedia(g gin.IRouter, h *Handler) {
	library := g.Group("/library")
	library.GET("", h.listMedia)
	library.POST("", h.uploadMedia)
	library.PATCH("/:id", h.patchMedia)
	library.PUT("/:id", h.patchMedia)
	library.GET("/:id", h.getMedia)
	library.GET("/:id/open", h.openMedia)
	library.GET("/:id/signed-url", h.signedURL)
	library.POST("/:id/signed-url", h.signedURL)
	library.DELETE("/:id", h.deleteMedia)
	g.GET("/resources/:id", h.getMedia)
	g.GET("/resources/:id/open", h.openMedia)
	g.GET("/resources/:id/signed-url", h.signedURL)
	g.POST("/resources/:id/signed-url", h.signedURL)
	g.GET("/categories", h.listCategories)
	g.POST("/categories", h.createCategory)
	g.PATCH("/categories/:id", h.updateCategory)
	g.PUT("/categories/:id", h.updateCategory)
	g.DELETE("/categories/:id", h.deleteCategory)
	g.GET("/usages", h.listUsageByQuery)
	g.POST("/usages/:id", h.attachUsage)
	g.DELETE("/usages/:id", h.detachUsage)
	g.GET("/library/:id/usage", h.listUsage)
}

func (h *Handler) unsupported(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
}
func scope(c *gin.Context) (tenant.Context, bool) {
	s, e := tenant.RequireContext(c.Request.Context())
	if e != nil {
		response.Error(c, 400, 10000, "invalid tenant context")
		return tenant.Context{}, false
	}
	return s, true
}
func idem(c *gin.Context) string { return strings.TrimSpace(c.GetHeader("Idempotency-Key")) }

type callerDTO struct {
	Key          string    `json:"key,omitempty"`
	CallerKey    string    `json:"callerKey,omitempty"`
	Name         *string   `json:"name"`
	Module       *string   `json:"module,omitempty"`
	Capabilities *[]string `json:"capabilities,omitempty"`
	Enabled      *bool     `json:"enabled"`
	// SystemOwned is output-only. Management requests cannot promote a caller
	// to a system resource; built-in reconciliation owns that bit.
	SystemOwned      bool            `json:"systemOwned,omitempty"`
	SMTPAccountIDs   *[]string       `json:"smtpAccountIds,omitempty"`
	DefaultAccountID *string         `json:"defaultAccountId,omitempty"`
	RoutingPolicy    *string         `json:"routingPolicy,omitempty"`
	Weights          *map[string]int `json:"weights,omitempty"`
}

type callerView struct {
	ID               string         `json:"id"`
	Key              string         `json:"key,omitempty"`
	CallerKey        string         `json:"callerKey"`
	Name             string         `json:"name"`
	Module           string         `json:"module,omitempty"`
	Capabilities     []string       `json:"capabilities,omitempty"`
	Enabled          bool           `json:"enabled"`
	SystemOwned      bool           `json:"systemOwned"`
	SMTPAccountIDs   []string       `json:"smtpAccountIds,omitempty"`
	DefaultAccountID string         `json:"defaultAccountId,omitempty"`
	RoutingPolicy    string         `json:"routingPolicy,omitempty"`
	Weights          map[string]int `json:"weights,omitempty"`
}

func (h *Handler) callerView(value notification.Caller) callerView {
	return h.callerViewFor(context.Background(), value)
}

func (h *Handler) callerViewFor(ctx context.Context, value notification.Caller) callerView {
	view := callerView{ID: value.Key, Key: value.Key, CallerKey: value.Key, Name: value.Name, Module: value.Module, Capabilities: append([]string(nil), value.Capabilities...), Enabled: value.Enabled, SystemOwned: value.SystemOwned}
	if h != nil && h.Mail != nil {
		if route, ok := h.Mail.CallerRouteFor(ctx, value.Key); ok {
			view.SMTPAccountIDs = append([]string(nil), route.AccountIDs...)
			view.DefaultAccountID = route.DefaultAccountID
			view.RoutingPolicy = string(route.Strategy)
			view.Weights = cloneIntMap(route.Weights)
		}
	}
	return view
}

func (h *Handler) listCallers(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	ctx := c.Request.Context()
	items := h.Runtime.ListCallersFor(ctx)
	views := make([]callerView, 0, len(items))
	for _, item := range items {
		views = append(views, h.callerViewFor(ctx, item))
	}
	response.OK(c, views)
}
func (h *Handler) getCaller(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	ctx := c.Request.Context()
	out, e := h.Runtime.CallerFor(ctx, c.Param("id"))
	if e != nil {
		writeNotificationError(c, e)
		return
	}
	response.OK(c, h.callerViewFor(ctx, out))
}
func (h *Handler) putCaller(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	var in callerDTO
	if c.ShouldBindJSON(&in) != nil {
		response.Error(c, 400, 10000, "invalid caller")
		return
	}
	if in.RoutingPolicy != nil {
		strategy := strings.TrimSpace(*in.RoutingPolicy)
		if strategy != string(mailapp.RoutingWeightedRandom) && strategy != string(mailapp.RoutingRoundRobin) {
			response.Error(c, 400, 10000, "invalid routing policy")
			return
		}
	}
	key := strings.TrimSpace(c.Param("id"))
	if key == "" {
		key = strings.TrimSpace(in.CallerKey)
		if key == "" {
			key = strings.TrimSpace(in.Key)
		}
	}
	if key == "" {
		response.Error(c, 400, 10000, "callerKey is required")
		return
	}
	ctx := c.Request.Context()
	existing, existingErr := h.Runtime.CallerFor(ctx, key)
	if existingErr != nil && !errors.Is(existingErr, notification.ErrCallerNotFound) {
		writeNotificationError(c, existingErr)
		return
	}
	if c.Param("id") != "" && errors.Is(existingErr, notification.ErrCallerNotFound) {
		writeNotificationError(c, notification.ErrCallerNotFound)
		return
	}
	value := existing
	value.Key = key
	if in.Name != nil {
		value.Name = *in.Name
	} else if value.Name == "" {
		value.Name = key
	}
	if in.Module != nil {
		value.Module = *in.Module
	}
	if in.Capabilities != nil {
		value.Capabilities = append([]string(nil), (*in.Capabilities)...)
	}
	if in.Enabled != nil {
		value.Enabled = *in.Enabled
	} else if errors.Is(existingErr, notification.ErrCallerNotFound) {
		value.Enabled = true
	}
	// Keep an existing system-owned marker; ignore the request body's value.
	if e := h.Runtime.SetCallerFor(ctx, value); e != nil {
		writeNotificationError(c, e)
		return
	}
	if h.Mail != nil {
		route, routeOK := h.Mail.CallerRouteFor(ctx, key)
		if in.SMTPAccountIDs != nil {
			route.AccountIDs = append([]string(nil), (*in.SMTPAccountIDs)...)
		}
		if in.DefaultAccountID != nil {
			route.DefaultAccountID = *in.DefaultAccountID
		}
		if in.RoutingPolicy != nil {
			route.Strategy = mailapp.RoutingPolicy(strings.TrimSpace(*in.RoutingPolicy))
			if route.Strategy != mailapp.RoutingWeightedRandom && route.Strategy != mailapp.RoutingRoundRobin {
				response.Error(c, 400, 10000, "invalid routing policy")
				return
			}
		}
		if in.Weights != nil {
			route.Weights = cloneIntMap(*in.Weights)
		}
		if routeOK || in.SMTPAccountIDs != nil || in.DefaultAccountID != nil || in.RoutingPolicy != nil || in.Weights != nil {
			h.Mail.SetCallerRouteFor(ctx, key, route)
		}
	}
	value, _ = h.Runtime.CallerFor(ctx, key)
	response.OK(c, h.callerViewFor(ctx, value))
}
func (h *Handler) deleteCaller(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	ctx := c.Request.Context()
	if e := h.Runtime.DeleteCallerFor(ctx, c.Param("id")); e != nil {
		writeNotificationError(c, e)
		return
	}
	if h.Mail != nil {
		h.Mail.DeleteCallerRouteFor(ctx, c.Param("id"))
	}
	response.OK(c, nil)
}

type templateDTO struct {
	Key           string                                 `json:"key,omitempty"`
	TemplateKey   string                                 `json:"templateKey,omitempty"`
	Purpose       string                                 `json:"purpose,omitempty"`
	DefaultLocale string                                 `json:"defaultLocale,omitempty"`
	Variables     []string                               `json:"-"`
	VariableMap   map[string]string                      `json:"-"`
	Locales       map[string]notification.TemplateLocale `json:"locales,omitempty"`
	Locale        string                                 `json:"locale,omitempty"`
	Subject       string                                 `json:"subject,omitempty"`
	Body          string                                 `json:"body,omitempty"`
	Enabled       *bool                                  `json:"enabled"`
	Published     *bool                                  `json:"published"`
}

// UnmarshalJSON accepts both the compact public shape (templateKey/locale/
// subject/body) and the richer internal shape (locales map, variables list).
// This keeps older management clients working while the generated OpenAPI
// client adopts the same endpoint.
func (d *templateDTO) UnmarshalJSON(raw []byte) error {
	var value struct {
		Key, TemplateKey, Purpose, DefaultLocale, Locale, Subject, Body string
		Variables                                                       json.RawMessage
		Locales                                                         map[string]notification.TemplateLocale
		Enabled, Published                                              *bool
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	d.Key, d.TemplateKey, d.Purpose, d.DefaultLocale, d.Locale, d.Subject, d.Body = value.Key, value.TemplateKey, value.Purpose, value.DefaultLocale, value.Locale, value.Subject, value.Body
	d.Locales, d.Enabled, d.Published = value.Locales, value.Enabled, value.Published
	if len(value.Variables) > 0 {
		if err := json.Unmarshal(value.Variables, &d.Variables); err != nil {
			if err := json.Unmarshal(value.Variables, &d.VariableMap); err != nil {
				return err
			}
			for key := range d.VariableMap {
				d.Variables = append(d.Variables, key)
			}
		}
	}
	return nil
}

type templateView struct {
	ID            string                                 `json:"id"`
	Key           string                                 `json:"key,omitempty"`
	TemplateKey   string                                 `json:"templateKey"`
	Purpose       string                                 `json:"purpose,omitempty"`
	DefaultLocale string                                 `json:"defaultLocale,omitempty"`
	Locale        string                                 `json:"locale,omitempty"`
	Subject       string                                 `json:"subject,omitempty"`
	Body          string                                 `json:"body,omitempty"`
	Variables     []string                               `json:"variables,omitempty"`
	Locales       map[string]notification.TemplateLocale `json:"locales,omitempty"`
	Generation    string                                 `json:"generation,omitempty"`
	Enabled       bool                                   `json:"enabled"`
	Published     bool                                   `json:"published"`
}

func templateToView(value notification.Template) templateView {
	view := templateView{ID: value.Key, Key: value.Key, TemplateKey: value.Key, Purpose: value.Purpose, DefaultLocale: value.DefaultLocale, Variables: append([]string(nil), value.Variables...), Locales: cloneLocales(value.Locales), Generation: value.Generation, Enabled: value.Enabled, Published: value.Published}
	view.Locale = value.DefaultLocale
	if locale, ok := value.Locales[value.DefaultLocale]; ok {
		view.Subject, view.Body = locale.Subject, locale.Body
	}
	return view
}

func (h *Handler) listTemplates(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	items := h.Runtime.ListTemplatesFor(c.Request.Context())
	views := make([]templateView, 0, len(items))
	for _, item := range items {
		views = append(views, templateToView(item))
	}
	response.OK(c, views)
}
func (h *Handler) putTemplate(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	var in templateDTO
	if c.ShouldBindJSON(&in) != nil {
		response.Error(c, 400, 10000, "invalid template")
		return
	}
	key := strings.TrimSpace(c.Param("id"))
	if key == "" {
		key = strings.TrimSpace(in.TemplateKey)
		if key == "" {
			key = strings.TrimSpace(in.Key)
		}
	}
	if key == "" {
		response.Error(c, 400, 10000, "templateKey is required")
		return
	}
	ctx := c.Request.Context()
	existing, existingErr := h.Runtime.TemplateFor(ctx, key)
	if existingErr != nil && !errors.Is(existingErr, notification.ErrTemplateNotFound) {
		writeNotificationError(c, existingErr)
		return
	}
	if c.Param("id") != "" && errors.Is(existingErr, notification.ErrTemplateNotFound) {
		writeNotificationError(c, notification.ErrTemplateNotFound)
		return
	}
	value := existing
	value.Key = key
	if in.Purpose != "" {
		value.Purpose = in.Purpose
	} else if value.Purpose == "" {
		value.Purpose = key
	}
	if in.DefaultLocale != "" {
		value.DefaultLocale = in.DefaultLocale
	}
	if in.Variables != nil {
		value.Variables = append([]string(nil), in.Variables...)
	}
	locales := cloneLocales(value.Locales)
	if in.Locales != nil {
		if locales == nil {
			locales = make(map[string]notification.TemplateLocale)
		}
		for locale, variant := range in.Locales {
			if strings.TrimSpace(variant.Locale) == "" {
				variant.Locale = locale
			}
			locales[locale] = variant
		}
	}
	if len(locales) == 0 && (in.Locale != "" || in.Subject != "" || in.Body != "") {
		locale := in.Locale
		if locale == "" {
			locale = in.DefaultLocale
		}
		if locale == "" {
			locale = "zh-CN"
		}
		if locales == nil {
			locales = make(map[string]notification.TemplateLocale)
		}
		locales[locale] = notification.TemplateLocale{Locale: locale, Subject: in.Subject, Body: in.Body}
	} else if in.Locale != "" || in.Subject != "" || in.Body != "" {
		locale := in.Locale
		if locale == "" {
			locale = in.DefaultLocale
		}
		if locale == "" {
			locale = value.DefaultLocale
		}
		if locale == "" {
			locale = "zh-CN"
		}
		if locales == nil {
			locales = make(map[string]notification.TemplateLocale)
		}
		variant := locales[locale]
		variant.Locale = locale
		if in.Subject != "" {
			variant.Subject = in.Subject
		}
		if in.Body != "" {
			variant.Body = in.Body
		}
		locales[locale] = variant
	}
	value.Locales = locales
	if in.Enabled != nil {
		value.Enabled = *in.Enabled
	} else if errors.Is(existingErr, notification.ErrTemplateNotFound) {
		value.Enabled = true
	}
	if in.Published != nil {
		value.Published = *in.Published
	}
	if e := h.Runtime.SetTemplateFor(ctx, value); e != nil {
		writeNotificationError(c, e)
		return
	}
	stored, _ := h.Runtime.TemplateFor(ctx, key)
	response.OK(c, templateToView(stored))
}
func (h *Handler) deleteTemplate(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	if e := h.Runtime.DeleteTemplateFor(c.Request.Context(), c.Param("id")); e != nil {
		writeNotificationError(c, e)
		return
	}
	response.OK(c, nil)
}
func (h *Handler) publishTemplate(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	t, e := h.Runtime.TemplateFor(c.Request.Context(), c.Param("id"))
	if e != nil {
		writeNotificationError(c, e)
		return
	}
	t.Published = true
	if e = h.Runtime.SetTemplateFor(c.Request.Context(), t); e != nil {
		writeNotificationError(c, e)
		return
	}
	response.OK(c, templateToView(t))
}
func (h *Handler) testTemplate(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	var in struct {
		CallerKey  string                   `json:"callerKey"`
		Recipient  string                   `json:"recipient"`
		Recipients []notification.Recipient `json:"recipients"`
		Purpose    string                   `json:"purpose"`
		Variables  map[string]string        `json:"variables"`
		Locale     string                   `json:"locale"`
	}
	if c.ShouldBindJSON(&in) != nil {
		response.Error(c, 400, 10000, "invalid test request")
		return
	}
	if strings.TrimSpace(in.Recipient) != "" && len(in.Recipients) == 0 {
		in.Recipients = []notification.Recipient{{Address: in.Recipient, Kind: "to"}}
	}
	// Admin test sends are designed to be one-click from the management UI.
	// Fill omitted declared variables with deterministic, locale-aware sample
	// values while retaining every value supplied by the operator. Production
	// NotificationService calls remain strict and still require their declared
	// variables explicitly.
	templateValue, e := h.Runtime.TemplateFor(c.Request.Context(), c.Param("id"))
	if e != nil {
		writeNotificationError(c, e)
		return
	}
	if strings.TrimSpace(in.Locale) == "" {
		metadata := notification.ContextMetadataFromContext(c.Request.Context())
		in.Locale = requestLocale(firstNonEmpty(metadata.Locale, c.GetHeader("Accept-Language"), templateValue.DefaultLocale))
	}
	in.Variables = fillTemplateTestVariables(templateValue, in.Locale, in.Variables)
	// The path identifies the template under test and the management caller is
	// installed by the trusted adapter; body caller/purpose fields cannot switch
	// the capability or redirect the send.
	ctx := h.managementContext(c)
	out, e := h.Runtime.Send(ctx, notification.NotificationRequest{Purpose: c.Param("id"), Recipients: in.Recipients, Variables: in.Variables, Locale: in.Locale, IdempotencyKey: idem(c), Mode: notification.SendModeAdminTest})
	if e != nil {
		writeNotificationError(c, e)
		return
	}
	response.OK(c, out)
}

func requestLocale(value string) string {
	return strings.TrimSpace(strings.Split(value, ",")[0])
}

func fillTemplateTestVariables(value notification.Template, locale string, provided map[string]string) map[string]string {
	result := make(map[string]string, len(value.Variables)+len(provided))
	for key, item := range provided {
		result[key] = item
	}
	for _, name := range value.Variables {
		if _, exists := result[name]; exists {
			continue
		}
		result[name] = sampleTemplateVariable(name, locale)
	}
	return result
}

func sampleTemplateVariable(name, locale string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(key, "code") || strings.Contains(key, "otp") || strings.Contains(key, "token") {
		return "123456"
	}
	if strings.Contains(key, "expire") || strings.Contains(key, "ttl") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en") {
			return "10 minutes"
		}
		return "10 分钟"
	}
	if strings.Contains(key, "email") {
		return "user@example.test"
	}
	if strings.Contains(key, "location") || strings.Contains(key, "ip") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en") {
			return "Sample location"
		}
		return "示例地点"
	}
	if strings.Contains(key, "name") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en") {
			return "Sample User"
		}
		return "示例用户"
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en") {
		return "Sample value"
	}
	return "示例值"
}

type policyDTO struct {
	CallerKey             *string `json:"callerKey"`
	Purpose               *string `json:"purpose"`
	Length                *int    `json:"length"`
	CodeLength            *int    `json:"codeLength"`
	Charset               *string `json:"charset"`
	TTLSeconds            *int    `json:"ttlSeconds"`
	MaxFailures           *int    `json:"maxFailures"`
	ResendAfterSeconds    *int    `json:"resendAfterSeconds"`
	ResendIntervalSeconds *int    `json:"resendIntervalSeconds"`
	HourlyLimit           *int    `json:"hourlyLimit"`
	MaxSendsPerHour       *int    `json:"maxSendsPerHour"`
}

type policyView struct {
	PolicyKey             string `json:"policyKey"`
	Key                   string `json:"key,omitempty"`
	CallerKey             string `json:"callerKey,omitempty"`
	Purpose               string `json:"purpose,omitempty"`
	CodeLength            int    `json:"codeLength"`
	Charset               string `json:"charset"`
	TTLSeconds            int    `json:"ttlSeconds"`
	MaxFailures           int    `json:"maxFailures"`
	ResendIntervalSeconds int    `json:"resendIntervalSeconds"`
	HourlyLimit           int    `json:"hourlyLimit"`
}

func policyToView(value notification.VerificationPolicy) policyView {
	charset := value.Charset
	switch charset {
	case notification.DefaultVerificationCharset:
		charset = "numeric"
	case "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ":
		charset = "alphanumeric"
	}
	return policyView{PolicyKey: value.Key, Key: value.Key, CallerKey: value.CallerKey, Purpose: value.Purpose, CodeLength: value.Length, Charset: charset, TTLSeconds: int(value.TTL / time.Second), MaxFailures: value.MaxFailures, ResendIntervalSeconds: int(value.ResendAfter / time.Second), HourlyLimit: value.HourlyLimit}
}

func (h *Handler) putPolicy(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	var in policyDTO
	if c.ShouldBindJSON(&in) != nil {
		response.Error(c, 400, 10000, "invalid policy")
		return
	}
	ctx := c.Request.Context()
	key := strings.TrimSpace(c.Param("policy_key"))
	existing, existingErr := h.Runtime.VerificationPolicyFor(ctx, key)
	if existingErr != nil && !errors.Is(existingErr, notification.ErrPolicyNotFound) {
		writeNotificationError(c, existingErr)
		return
	}
	p := existing
	p.Key = key
	if in.CallerKey != nil {
		p.CallerKey = strings.TrimSpace(*in.CallerKey)
	}
	if in.Purpose != nil {
		p.Purpose = strings.TrimSpace(*in.Purpose)
	}
	if in.Length != nil {
		p.Length = *in.Length
	}
	if in.CodeLength != nil {
		p.Length = *in.CodeLength
	}
	if in.Charset != nil {
		p.Charset = *in.Charset
	}
	if in.TTLSeconds != nil {
		p.TTL = time.Duration(*in.TTLSeconds) * time.Second
	}
	if in.MaxFailures != nil {
		p.MaxFailures = *in.MaxFailures
	}
	if in.ResendAfterSeconds != nil {
		p.ResendAfter = time.Duration(*in.ResendAfterSeconds) * time.Second
	}
	if in.ResendIntervalSeconds != nil {
		p.ResendAfter = time.Duration(*in.ResendIntervalSeconds) * time.Second
	}
	if in.HourlyLimit != nil {
		p.HourlyLimit = *in.HourlyLimit
	}
	if in.MaxSendsPerHour != nil {
		p.HourlyLimit = *in.MaxSendsPerHour
	}
	if e := h.Runtime.SetVerificationPolicyFor(ctx, p); e != nil {
		writeNotificationError(c, e)
		return
	}
	stored, _ := h.Runtime.VerificationPolicyFor(ctx, key)
	response.OK(c, policyToView(stored))
}
func (h *Handler) listPolicies(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	items := h.Runtime.ListVerificationPoliciesFor(c.Request.Context())
	views := make([]policyView, 0, len(items))
	for _, item := range items {
		views = append(views, policyToView(item))
	}
	response.OK(c, views)
}

func (h *Handler) issueChallenge(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	var in notification.IssueRequest
	if c.ShouldBindJSON(&in) != nil {
		response.Error(c, 400, 10000, "invalid verification request")
		return
	}
	if key := idem(c); key != "" && strings.TrimSpace(in.IdempotencyKey) == "" {
		in.IdempotencyKey = key
	}
	out, err := h.Runtime.Issue(c.Request.Context(), in)
	if err != nil {
		writeNotificationError(c, err)
		return
	}
	response.OK(c, out)
}

func (h *Handler) challengeStatus(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	out, err := h.Runtime.ChallengeStatusFor(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeNotificationError(c, err)
		return
	}
	response.OK(c, out)
}

func (h *Handler) verifyChallenge(c *gin.Context) {
	if h == nil || h.Runtime == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	var in notification.VerifyRequest
	if c.ShouldBindJSON(&in) != nil {
		response.Error(c, 400, 10000, "invalid verification request")
		return
	}
	in.ChallengeID = c.Param("id")
	if key := idem(c); key != "" && strings.TrimSpace(in.IdempotencyKey) == "" {
		in.IdempotencyKey = key
	}
	if err := h.Runtime.Verify(c.Request.Context(), in); err != nil {
		writeNotificationError(c, err)
		return
	}
	response.OK(c, gin.H{"verified": true})
}

func (h *Handler) listAccounts(c *gin.Context) {
	if h == nil || h.Mail == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	out, e := h.Mail.ListAccounts(c.Request.Context())
	if e != nil {
		writeMailError(c, e)
		return
	}
	response.OK(c, out)
}

type accountDTO struct {
	Name        *string `json:"name"`
	Enabled     *bool   `json:"enabled"`
	Host        *string `json:"host"`
	Port        *int    `json:"port"`
	Username    *string `json:"username"`
	Password    *string `json:"password"`
	Weight      *int    `json:"weight"`
	FromEmail   *string `json:"fromEmail"`
	FromName    *string `json:"fromName"`
	ImplicitTLS *bool   `json:"implicitTls"`
}

func (h *Handler) accountInput(c *gin.Context, existing *mailapp.Account) (mailapp.AccountInput, bool) {
	var dto accountDTO
	if c.ShouldBindJSON(&dto) != nil {
		response.Error(c, 400, 10000, "invalid smtp account")
		return mailapp.AccountInput{}, false
	}
	in := mailapp.AccountInput{Enabled: true, Weight: 1, Port: 587}
	if existing != nil {
		in = mailapp.AccountInput{Name: existing.Name, Enabled: existing.Enabled, Host: existing.Host, Port: existing.Port, Username: existing.Username, Weight: existing.Weight, FromEmail: existing.FromEmail, FromName: existing.FromName, ImplicitTLS: existing.ImplicitTLS}
	}
	if dto.Name != nil {
		in.Name = *dto.Name
	}
	if dto.Enabled != nil {
		in.Enabled = *dto.Enabled
	}
	if dto.Host != nil {
		in.Host = *dto.Host
	}
	if dto.Port != nil {
		in.Port = *dto.Port
	}
	if dto.Username != nil {
		in.Username = *dto.Username
	}
	if dto.Password != nil {
		in.Password = *dto.Password
	}
	if dto.Weight != nil {
		in.Weight = *dto.Weight
	}
	if dto.FromEmail != nil {
		in.FromEmail = *dto.FromEmail
	}
	if dto.FromName != nil {
		in.FromName = *dto.FromName
	}
	if dto.ImplicitTLS != nil {
		in.ImplicitTLS = *dto.ImplicitTLS
	}
	return in, true
}
func (h *Handler) createAccount(c *gin.Context) {
	if h == nil || h.Mail == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	in, ok := h.accountInput(c, nil)
	if !ok {
		return
	}
	out, e := h.Mail.CreateAccount(c.Request.Context(), in)
	if e != nil {
		writeMailError(c, e)
		return
	}
	response.OK(c, out)
}
func (h *Handler) updateAccount(c *gin.Context) {
	if h == nil || h.Mail == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	existing, e := h.Mail.GetAccount(c.Request.Context(), c.Param("id"))
	if e != nil {
		writeMailError(c, e)
		return
	}
	in, ok := h.accountInput(c, &existing)
	if !ok {
		return
	}
	out, e := h.Mail.UpdateAccount(c.Request.Context(), c.Param("id"), in)
	if e != nil {
		writeMailError(c, e)
		return
	}
	response.OK(c, out)
}
func (h *Handler) deleteAccount(c *gin.Context) {
	if h == nil || h.Mail == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	if e := h.Mail.DeleteAccount(c.Request.Context(), c.Param("id")); e != nil {
		writeMailError(c, e)
		return
	}
	response.OK(c, nil)
}

func (h *Handler) listMedia(c *gin.Context) {
	if h == nil || h.Catalog == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit < 1 || limit > 200 {
		response.Error(c, 400, 10000, "invalid limit")
		return
	}
	includeDescendants, _ := strconv.ParseBool(c.DefaultQuery("includeDescendants", "false"))
	offset := 0
	if rawOffset := strings.TrimSpace(c.Query("offset")); rawOffset != "" {
		parsed, parseErr := strconv.Atoi(rawOffset)
		if parseErr != nil || parsed < 0 {
			response.Error(c, 400, 10000, "invalid offset")
			return
		}
		offset = parsed
	}
	mime := strings.TrimSpace(c.Query("mime"))
	if mime == "" {
		// MediaListParams used by the shared clients calls the exact MIME
		// selector mimeExact; retain it as a compatibility alias alongside the
		// OpenAPI-facing mime parameter.
		mime = strings.TrimSpace(c.Query("mimeExact"))
	}
	if mime == "" {
		mime = strings.TrimSpace(c.Query("mimeFamily"))
	}
	filter := fileapp.MediaFilter{CategoryID: c.Query("categoryId"), OwnerID: c.Query("ownerId"), Cursor: c.Query("cursor"), Offset: offset, Limit: limit, Status: fileapp.MediaStatus(c.Query("status")), ScopeType: fileapp.ScopeType(c.Query("scopeType")), IncludeDescendants: includeDescendants}
	if strings.HasSuffix(strings.ToLower(mime), "/*") {
		filter.MIMEFamily = mime
	} else {
		filter.MIMEExact = mime
	}
	out, e := h.Catalog.List(c.Request.Context(), filter)
	if e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, out)
}
func (h *Handler) uploadMedia(c *gin.Context) {
	if h == nil || h.Catalog == nil {
		h.unsupported(c)
		return
	}
	s, ok := scope(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxMediaMultipartBytes+maxMediaMultipartMemoryBytes)
	defer func() {
		if c.Request.MultipartForm != nil {
			_ = c.Request.MultipartForm.RemoveAll()
		}
	}()
	if err := c.Request.ParseMultipartForm(maxMediaMultipartMemoryBytes); err != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		response.Error(c, status, 10000, "invalid multipart upload")
		return
	}
	f, e := c.FormFile("file")
	if e != nil {
		response.Error(c, 400, 10000, "file is required")
		return
	}
	r, e := f.Open()
	if e != nil {
		response.Error(c, 400, 10000, "file is unreadable")
		return
	}
	defer r.Close()
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		name = f.Filename
	}
	mime := strings.TrimSpace(c.PostForm("mime"))
	if mime == "" {
		mime = f.Header.Get("Content-Type")
	}
	if parsed, _, parseErr := mimepkg.ParseMediaType(mime); parseErr == nil {
		mime = parsed
	}
	metadata := map[string]string{}
	if raw := strings.TrimSpace(c.PostForm("metadata")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
			response.Error(c, 400, 10000, "invalid metadata")
			return
		}
	}
	if selectable := strings.TrimSpace(c.PostForm("selectable")); selectable != "" {
		metadata["selectable"] = selectable
	}
	out, e := h.Catalog.Upload(c.Request.Context(), fileapp.UploadInput{Reader: r, Size: f.Size, Name: name, MIME: mime, ACL: fileapp.ACL(strings.TrimSpace(c.PostForm("acl"))), TenantID: s.TenantID, OrgID: s.Organization, CategoryID: c.PostForm("categoryId"), IdempotencyKey: idem(c), Metadata: metadata})
	if e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, out)
}
func (h *Handler) getMedia(c *gin.Context) {
	if h == nil || h.Catalog == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	out, e := h.Catalog.Get(c.Request.Context(), c.Param("id"))
	if e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, out)
}
func (h *Handler) patchMedia(c *gin.Context) {
	if h == nil || h.Catalog == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	var in struct {
		Name       *string              `json:"name"`
		CategoryID *string              `json:"categoryId"`
		Status     *fileapp.MediaStatus `json:"status"`
		Metadata   map[string]string    `json:"metadata"`
	}
	if c.ShouldBindJSON(&in) != nil {
		response.Error(c, 400, 10000, "invalid media patch")
		return
	}
	updater, ok := h.Catalog.(interface {
		UpdateResource(context.Context, fileapp.ResourceID, fileapp.ResourcePatch) (fileapp.ResourceRef, error)
	})
	if !ok {
		h.unsupported(c)
		return
	}
	var category *fileapp.CategoryID
	if in.CategoryID != nil {
		value := fileapp.CategoryID(strings.TrimSpace(*in.CategoryID))
		category = &value
	}
	out, e := updater.UpdateResource(c.Request.Context(), c.Param("id"), fileapp.ResourcePatch{Name: in.Name, CategoryID: category, Status: in.Status, Metadata: in.Metadata, IdempotencyKey: idem(c)})
	if e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, out)
}
func (h *Handler) openMedia(c *gin.Context) {
	if h == nil || h.Catalog == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	ref, err := h.Catalog.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeFileError(c, err)
		return
	}
	options, ranged, rangeErr := parseMediaRange(c.GetHeader("Range"), ref.Size)
	if rangeErr != nil {
		c.Header("Accept-Ranges", "bytes")
		c.Header("Content-Range", fmt.Sprintf("bytes */%d", ref.Size))
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	obj, err := h.Catalog.Open(c.Request.Context(), c.Param("id"), options)
	if err != nil {
		writeFileError(c, err)
		return
	}
	defer obj.Close()
	contentType := safeContentType(ref.MIME)
	c.Header("Accept-Ranges", "bytes")
	status, length := http.StatusOK, ref.Size
	if ranged {
		start, end := rangeBounds(options, ref.Size)
		length = end - start + 1
		status = http.StatusPartialContent
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, ref.Size))
	}
	c.DataFromReader(status, length, contentType, obj, nil)
}

func safeContentType(value string) string {
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, "\r\n\x00") {
		return "application/octet-stream"
	}
	parsed, _, err := mimepkg.ParseMediaType(value)
	if err != nil || parsed == "" {
		return "application/octet-stream"
	}
	return parsed
}

// parseMediaRange supports one RFC 9110 byte range. Multiple ranges are
// rejected rather than silently returning a misleading multipart response;
// callers can retry with a single range or the full representation.
func parseMediaRange(value string, size int64) (fileapp.OpenOptions, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fileapp.OpenOptions{}, false, nil
	}
	if size < 0 || !strings.HasPrefix(strings.ToLower(value), "bytes=") {
		return fileapp.OpenOptions{}, true, errors.New("invalid media range")
	}
	spec := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "bytes="))
	if spec == "" || strings.Contains(spec, ",") {
		return fileapp.OpenOptions{}, true, errors.New("invalid media range")
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 || size == 0 {
		return fileapp.OpenOptions{}, true, errors.New("invalid media range")
	}
	left, right := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if left == "" {
		count, err := strconv.ParseInt(right, 10, 64)
		if err != nil || count <= 0 {
			return fileapp.OpenOptions{}, true, errors.New("invalid media range")
		}
		if count > size {
			count = size
		}
		start := size - count
		end := size - 1
		return fileapp.OpenOptions{RangeStart: &start, RangeEnd: &end}, true, nil
	}
	start, err := strconv.ParseInt(left, 10, 64)
	if err != nil || start < 0 || start >= size {
		return fileapp.OpenOptions{}, true, errors.New("invalid media range")
	}
	end := size - 1
	if right != "" {
		parsed, parseErr := strconv.ParseInt(right, 10, 64)
		if parseErr != nil || parsed < start {
			return fileapp.OpenOptions{}, true, errors.New("invalid media range")
		}
		if parsed < end {
			end = parsed
		}
	}
	return fileapp.OpenOptions{RangeStart: &start, RangeEnd: &end}, true, nil
}

func rangeBounds(options fileapp.OpenOptions, size int64) (int64, int64) {
	start, end := int64(0), size-1
	if options.RangeStart != nil {
		start = *options.RangeStart
	}
	if options.RangeEnd != nil {
		end = *options.RangeEnd
	}
	return start, end
}
func (h *Handler) signedURL(c *gin.Context) {
	if h == nil || h.Catalog == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	var in struct {
		Purpose    string `json:"purpose"`
		TTLSeconds int    `json:"ttlSeconds"`
	}
	in.Purpose = strings.TrimSpace(c.Query("purpose"))
	if value := c.Query("ttlSeconds"); value != "" {
		in.TTLSeconds, _ = strconv.Atoi(value)
	}
	if c.Request.Method == http.MethodPost || (in.Purpose == "" && c.Request.ContentLength > 0) {
		var body struct {
			Purpose    string `json:"purpose"`
			TTLSeconds int    `json:"ttlSeconds"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			response.Error(c, 400, 10000, "invalid request")
			return
		}
		if in.Purpose == "" {
			in.Purpose = strings.TrimSpace(body.Purpose)
		}
		if in.TTLSeconds == 0 {
			in.TTLSeconds = body.TTLSeconds
		}
	}
	if in.TTLSeconds == 0 {
		in.TTLSeconds = 900
	}
	if in.TTLSeconds < 1 || in.TTLSeconds > 3600 {
		response.Error(c, 400, 10000, "invalid URL TTL")
		return
	}
	if in.Purpose != string(fileapp.URLPurposePreview) && in.Purpose != string(fileapp.URLPurposeDownload) {
		response.Error(c, 400, 10000, "purpose must be preview or download")
		return
	}
	out, err := h.Catalog.SignedURL(c.Request.Context(), c.Param("id"), fileapp.URLRequest{Purpose: fileapp.URLPurpose(in.Purpose), TTL: time.Duration(in.TTLSeconds) * time.Second})
	if err != nil {
		writeFileError(c, err)
		return
	}
	response.OK(c, out)
}

func (h *Handler) deleteMedia(c *gin.Context) {
	if h == nil || h.Catalog == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	options := fileapp.DeleteOptions{IdempotencyKey: idem(c), Force: strings.EqualFold(strings.TrimSpace(c.Query("force")), "true"), Confirmation: strings.TrimSpace(c.Query("confirmation"))}
	if e := h.Catalog.Delete(c.Request.Context(), c.Param("id"), options); e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, nil)
}
func (h *Handler) listCategories(c *gin.Context) {
	if h == nil || h.Catalog == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	includeDescendants, _ := strconv.ParseBool(c.DefaultQuery("includeDescendants", "false"))
	out, e := h.Catalog.ListCategories(c.Request.Context(), fileapp.CategoryFilter{ParentID: c.Query("parentId"), ScopeType: fileapp.ScopeType(c.Query("scopeType")), IncludeDescendants: includeDescendants})
	if e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, out)
}
func (h *Handler) createCategory(c *gin.Context) {
	if h == nil || h.Catalog == nil {
		h.unsupported(c)
		return
	}
	s, ok := scope(c)
	if !ok {
		return
	}
	var in fileapp.CategoryInput
	if c.ShouldBindJSON(&in) != nil {
		response.Error(c, 400, 10000, "invalid category")
		return
	}
	if !s.PlatformAdmin {
		// A normal tenant caller cannot choose a scope from JSON. Platform
		// administrators may leave these fields empty for a system category or
		// explicitly target a tenant/org selected in the management UI.
		in.TenantID = s.TenantID
		in.OrgID = s.Organization
	}
	in.IdempotencyKey = idem(c)
	out, e := h.Catalog.CreateCategory(c.Request.Context(), in)
	if e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, out)
}
func (h *Handler) updateCategory(c *gin.Context) {
	if h == nil || h.Catalog == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	var in fileapp.CategoryPatch
	if c.ShouldBindJSON(&in) != nil {
		response.Error(c, 400, 10000, "invalid category")
		return
	}
	in.IdempotencyKey = idem(c)
	out, e := h.Catalog.UpdateCategory(c.Request.Context(), c.Param("id"), in)
	if e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, out)
}
func (h *Handler) deleteCategory(c *gin.Context) {
	if h == nil || h.Catalog == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	e := h.Catalog.DeleteCategory(c.Request.Context(), fileapp.CategoryDeleteRequest{ID: c.Param("id"), IdempotencyKey: idem(c)})
	if e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, nil)
}
func (h *Handler) listUsage(c *gin.Context) {
	if h == nil || h.Usage == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	out, e := h.Usage.ListByResource(c.Request.Context(), c.Param("id"))
	if e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, out)
}
func (h *Handler) listUsageByQuery(c *gin.Context) {
	resourceID := strings.TrimSpace(c.Query("resourceId"))
	if resourceID == "" {
		response.Error(c, 400, 10000, "resourceId is required")
		return
	}
	if h == nil || h.Usage == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	out, e := h.Usage.ListByResource(c.Request.Context(), resourceID)
	if e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, out)
}
func (h *Handler) attachUsage(c *gin.Context) {
	if h == nil || h.Usage == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	var in fileapp.UsageInput
	if c.ShouldBindJSON(&in) != nil {
		response.Error(c, 400, 10000, "invalid usage")
		return
	}
	in.ResourceID = c.Param("id")
	in.IdempotencyKey = idem(c)
	out, e := h.Usage.Attach(c.Request.Context(), in)
	if e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, out)
}
func (h *Handler) detachUsage(c *gin.Context) {
	if h == nil || h.Usage == nil {
		h.unsupported(c)
		return
	}
	if _, ok := scope(c); !ok {
		return
	}
	if e := h.Usage.Detach(c.Request.Context(), fileapp.DetachRequest{UsageID: c.Param("id"), IdempotencyKey: idem(c)}); e != nil {
		writeFileError(c, e)
		return
	}
	response.OK(c, nil)
}

func writeNotificationError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, tenant.ErrCrossTenant), errors.Is(e, tenant.ErrOrganizationDenied), errors.Is(e, tenant.ErrTenantRequired):
		response.Error(c, 403, 30000, "forbidden")
	case errors.Is(e, notification.ErrCallerNotFound), errors.Is(e, notification.ErrTemplateNotFound), errors.Is(e, notification.ErrPolicyNotFound):
		response.Error(c, 404, 10001, "resource not found")
	case errors.Is(e, notification.ErrCallerDisabled), errors.Is(e, notification.ErrTemplateUnpublished):
		response.Error(c, 409, 10000, "resource unavailable")
	case errors.Is(e, notification.ErrCallerSystemOwned):
		response.Error(c, 409, 10000, "system resource cannot be deleted")
	case errors.Is(e, notification.ErrInvalidPolicy):
		response.Error(c, 400, 10000, "invalid verification policy")
	case errors.Is(e, notification.ErrIdempotencyConflict):
		response.Error(c, 409, 10000, "idempotency conflict")
	case errors.Is(e, notification.ErrInvalidRecipient), errors.Is(e, notification.ErrTemplateVariableMissing), errors.Is(e, notification.ErrTemplateVariableInvalid), errors.Is(e, notification.ErrInvalidMessage):
		messageKey, params := notificationValidationError(e)
		response.ErrorWithMessageKey(c, 400, 10000, "invalid notification request", messageKey, params)
	case errors.Is(e, notification.ErrVerificationRateLimited):
		response.Error(c, 429, 40002, "verification rate limited")
	case errors.Is(e, notification.ErrVerificationExpired), errors.Is(e, notification.ErrVerificationLocked), errors.Is(e, notification.ErrVerificationConsumed), errors.Is(e, notification.ErrVerificationCodeIncorrect), errors.Is(e, notification.ErrVerificationNotActive), errors.Is(e, notification.ErrVerificationNotFound):
		response.Error(c, 409, 40003, "verification challenge unavailable")
	default:
		response.Error(c, 503, 40001, "dependency unavailable")
	}
}

func notificationValidationError(err error) (string, map[string]any) {
	switch {
	case errors.Is(err, notification.ErrTemplateVariableMissing):
		return "notification.template.variableMissing", map[string]any{"variable": notificationErrorSuffix(err)}
	case errors.Is(err, notification.ErrTemplateVariableInvalid):
		return "notification.template.variableInvalid", map[string]any{"variable": notificationErrorSuffix(err)}
	case errors.Is(err, notification.ErrInvalidRecipient):
		return "notification.recipient.invalid", nil
	default:
		return "notification.request.invalid", nil
	}
}

func notificationErrorSuffix(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if index := strings.LastIndex(message, ": "); index >= 0 {
		return strings.TrimSpace(message[index+2:])
	}
	return ""
}

func writeMailError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, tenant.ErrCrossTenant), errors.Is(e, tenant.ErrOrganizationDenied):
		response.Error(c, 403, 30000, "forbidden")
	case errors.Is(e, mailapp.ErrAccountNotFound), errors.Is(e, mailapp.ErrMessageNotFound):
		response.Error(c, 404, 10001, "resource not found")
	case errors.Is(e, mailapp.ErrAccountConflict):
		response.Error(c, 409, 10010, "SMTP account already exists")
	case errors.Is(e, mailapp.ErrInvalidAccount), errors.Is(e, mailapp.ErrInvalidSend):
		response.Error(c, 400, 10000, "invalid smtp request")
	case errors.Is(e, mailapp.ErrPermissionDenied):
		response.Error(c, 403, 30000, "forbidden")
	case errors.Is(e, mailapp.ErrDeliveryFailed):
		response.Error(c, 502, 40001, "email delivery failed")
	default:
		response.Error(c, 503, 40001, "dependency unavailable")
	}
}

func writeFileError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, fileapp.ErrFileNotFound), errors.Is(e, fileapp.ErrCategoryNotFound), errors.Is(e, fileapp.ErrUsageNotFound):
		response.Error(c, 404, 10001, "resource not found")
	case errors.Is(e, fileapp.ErrAccessDenied), errors.Is(e, fileapp.ErrCategoryAccessDenied):
		response.Error(c, 403, 30000, "forbidden")
	case errors.Is(e, fileapp.ErrMediaConflict), errors.Is(e, fileapp.ErrObjectExists):
		response.Error(c, 409, 10000, "idempotency conflict")
	case errors.Is(e, fileapp.ErrMediaInUse):
		response.Error(c, 409, 10002, "media resource is in use")
	case errors.Is(e, fileapp.ErrMediaNotReady), errors.Is(e, fileapp.ErrMediaTypeNotAllowed):
		response.Error(c, 409, 10003, "media resource unavailable")
	case errors.Is(e, fileapp.ErrInvalidMediaCursor), errors.Is(e, fileapp.ErrInvalidUsage), errors.Is(e, fileapp.ErrInvalidCategory):
		response.Error(c, 400, 10000, "invalid request")
	case errors.Is(e, fileapp.ErrFileTooLarge):
		response.Error(c, http.StatusRequestEntityTooLarge, 10000, "media upload is too large")
	case errors.Is(e, fileapp.ErrMIMETypeNotAllowed):
		response.Error(c, http.StatusUnsupportedMediaType, 10000, "media type is not allowed")
	case errors.Is(e, fileapp.ErrSignedURLUnsupported), errors.Is(e, fileapp.ErrStorageRead):
		response.Error(c, http.StatusNotImplemented, 40001, "file preview unavailable")
	case errors.Is(e, fileapp.ErrInvalidUpload):
		response.Error(c, 400, 10000, "invalid media upload")
	case errors.Is(e, fileapp.ErrCategoryNotEmpty):
		response.Error(c, 409, 10002, "category is not empty")
	default:
		response.Error(c, 503, 40001, "dependency unavailable")
	}
}

func cloneIntMap(values map[string]int) map[string]int {
	if values == nil {
		return nil
	}
	result := make(map[string]int, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func cloneLocales(values map[string]notification.TemplateLocale) map[string]notification.TemplateLocale {
	if values == nil {
		return nil
	}
	result := make(map[string]notification.TemplateLocale, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

// managementContext installs the server-owned management caller for template
// test sends. The request body can provide recipient/variables only; caller
// identity and locale come from this trusted adapter/header metadata.
func (h *Handler) managementContext(c *gin.Context) context.Context {
	ctx := c.Request.Context()
	metadata := notification.ContextMetadataFromContext(ctx)
	metadata.CallerKey = "system.admin"
	if metadata.Locale == "" {
		if locale := strings.TrimSpace(c.GetHeader("Accept-Language")); locale != "" {
			metadata.Locale = requestLocale(locale)
		}
	}
	return notification.WithContextMetadata(ctx, metadata)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
