// Package settingshttp exposes the versioned settings administration seam.
package settingshttp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	settingsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	authdomain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const (
	basePath              = "/api/admin/v1/settings"
	observabilityBasePath = "/api/admin/v1/observability/settings"
)

type ActorResolver func(*gin.Context) settingsapp.Actor

type Handler struct {
	service *settingsapp.Service
	actor   ActorResolver
}

func NewHandler(service *settingsapp.Service, resolvers ...ActorResolver) *Handler {
	var resolver ActorResolver
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	}
	return &Handler{service: service, actor: resolver}
}

func RegisterRoutes(r gin.IRouter, handler *Handler) {
	group := r.Group(basePath)
	// RegisterRoutes is retained for embedders that still rely on the legacy
	// per-key contract. The application composition root uses RegisterRoutesOn,
	// which mounts the module-only System Settings surface below.
	registerRoutes(group, handler)
	registerObservabilityRoutes(r.Group(observabilityBasePath), handler)
}

// RegisterRoutesOn mounts settings routes below an already-prefixed router
// group. The application composition root uses this seam to attach the
// shared admin authentication middleware without duplicating /api/admin/v1.
func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	registerSystemRoutes(group.Group("/settings"), handler)
	registerObservabilityRoutes(group.Group("/observability/settings"), handler)
}

// registerSystemRoutes is the production System Settings contract. It keeps
// the read/write key seam required by branding and dictionary consumers, but
// does not expose per-field history, rollback, or connection-test actions.
// All administrative edits go through an atomic module request.
func registerSystemRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		group.GET("/*path", disabled)
		group.PUT("/*path", disabled)
		group.POST("/*path", disabled)
		return
	}
	group.GET("", handler.listDefinitions)
	group.GET("/", handler.listDefinitions)
	group.GET("/modules", handler.listModules)
	group.GET("/modules/:module", handler.getModule)
	group.PUT("/modules/:module", handler.updateModule)
	group.POST("/modules/:module/validate", handler.validateModule)
	group.POST("/modules/:module/reset", handler.resetModule)
	group.POST("/modules/:module/clear-credentials", handler.clearCredentials)
	// Compatibility reads/writes are intentionally limited to consumers such
	// as the media branding picker; retired mail keys are rejected by handlers.
	group.GET("/:key", handler.get)
	group.PUT("/:key", handler.update)
}

func registerRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		group.GET("/*path", disabled)
		group.PUT("/*path", disabled)
		group.POST("/*path", disabled)
		return
	}
	group.GET("", handler.listDefinitions)
	group.GET("/", handler.listDefinitions)
	group.GET("/modules", handler.listModules)
	group.GET("/modules/:module", handler.getModule)
	group.PUT("/modules/:module", handler.updateModule)
	group.POST("/modules/:module/validate", handler.validateModule)
	group.POST("/modules/:module/reset", handler.resetModule)
	group.POST("/modules/:module/clear-credentials", handler.clearCredentials)
	group.GET("/:key", handler.get)
	group.GET("/:key/history", handler.history)
	group.PUT("/:key", handler.update)
	group.POST("/:key/test", handler.testConnection)
	group.POST("/:key/rollback", handler.rollback)
}

func registerObservabilityRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		group.GET("/:key", disabled)
		group.PUT("/:key", disabled)
		return
	}
	group.GET("/:key", handler.getObservability)
	group.PUT("/:key", handler.updateObservability)
}

type updateRequest struct {
	Value           json.RawMessage `json:"value"`
	ExpectedVersion int64           `json:"expectedVersion"`
	RequestID       string          `json:"requestId"`
}

type rollbackRequest struct {
	Version         int64  `json:"version"`
	ExpectedVersion int64  `json:"expectedVersion"`
	RequestID       string `json:"requestId"`
}

type connectionTestRequest struct {
	Value json.RawMessage `json:"value"`
}

type moduleUpdateRequest struct {
	Values           map[string]json.RawMessage `json:"values"`
	ExpectedRevision int64                      `json:"expectedRevision"`
	RequestID        string                     `json:"requestId"`
	hasRevision      bool                       `json:"-"`
}

// UnmarshalJSON keeps the canonical module request closed over its documented
// fields. In particular, resetKeys is deliberately not accepted on save or
// validate; restoring defaults is a separate module operation.
func (r *moduleUpdateRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("module request must be an object")
	}
	for key := range fields {
		switch key {
		case "values", "expectedRevision", "requestId":
		default:
			return fmt.Errorf("unknown module request field %q", key)
		}
	}
	type plain moduleUpdateRequest
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = moduleUpdateRequest(decoded)
	_, r.hasRevision = fields["expectedRevision"]
	return nil
}

type moduleResetRequest struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	RequestID        string `json:"requestId"`
	hasRevision      bool   `json:"-"`
}

type clearCredentialsRequest struct {
	Keys             []string `json:"keys"`
	ExpectedRevision int64    `json:"expectedRevision"`
	RequestID        string   `json:"requestId"`
	hasKeys          bool     `json:"-"`
	hasRevision      bool     `json:"-"`
}

func (r *clearCredentialsRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("clear credentials request must be an object")
	}
	for key := range fields {
		switch key {
		case "keys", "expectedRevision", "requestId":
		default:
			return fmt.Errorf("unknown clear credentials field %q", key)
		}
	}
	if _, ok := fields["keys"]; !ok {
		return errors.New("keys is required")
	}
	if _, ok := fields["expectedRevision"]; !ok {
		return errors.New("expectedRevision is required")
	}
	type plain clearCredentialsRequest
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = clearCredentialsRequest(decoded)
	r.hasKeys, r.hasRevision = true, true
	return nil
}

func (r *moduleResetRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("module reset request must be an object")
	}
	for key := range fields {
		switch key {
		case "expectedRevision", "requestId":
		default:
			return fmt.Errorf("unknown module reset field %q", key)
		}
	}
	if _, ok := fields["expectedRevision"]; !ok {
		return errors.New("expectedRevision is required")
	}
	type plain moduleResetRequest
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = moduleResetRequest(decoded)
	r.hasRevision = true
	return nil
}

func (h *Handler) actorFor(c *gin.Context) settingsapp.Actor {
	if h != nil && h.actor != nil {
		return h.actor(c)
	}
	if value, ok := c.Get("auth_claims"); ok {
		if claims, ok := value.(authdomain.Claims); ok {
			return settingsapp.Actor{ID: claims.Subject}
		}
	}
	return settingsapp.Actor{}
}

func (h *Handler) listDefinitions(c *gin.Context) {
	items, err := h.service.Definitions(c.Request.Context(), h.actorFor(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, items)
}

func (h *Handler) listModules(c *gin.Context) {
	items, err := h.service.Modules(c.Request.Context(), h.actorFor(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, items)
}

func (h *Handler) getModule(c *gin.Context) {
	module := strings.TrimSpace(c.Param("module"))
	view, err := h.service.GetModule(c.Request.Context(), h.actorFor(c), module)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, view)
}

func (h *Handler) updateModule(c *gin.Context) {
	var request moduleUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Values == nil || !request.hasRevision || request.ExpectedRevision < 0 || len(strings.TrimSpace(request.RequestID)) > 128 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid module request")
		return
	}
	requestID := strings.TrimSpace(request.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(c.GetHeader("X-Request-ID"))
	}
	if len(requestID) > 128 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid module request")
		return
	}
	result, err := h.service.SaveModule(c.Request.Context(), h.actorFor(c), settingsapp.ModuleUpdateInput{
		Module: strings.ToLower(strings.TrimSpace(c.Param("module"))), Values: request.Values,
		ExpectedRevision: request.ExpectedRevision, RequestID: requestID,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (h *Handler) validateModule(c *gin.Context) {
	var request moduleUpdateRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil || request.ExpectedRevision < 0 || len(strings.TrimSpace(request.RequestID)) > 128 {
			response.Error(c, http.StatusBadRequest, 10000, "invalid module request")
			return
		}
	}
	if len(strings.TrimSpace(request.RequestID)) == 0 && len(strings.TrimSpace(c.GetHeader("X-Request-ID"))) > 128 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid module request")
		return
	}
	result, err := h.service.ValidateModule(c.Request.Context(), h.actorFor(c), settingsapp.ModuleUpdateInput{
		Module: strings.ToLower(strings.TrimSpace(c.Param("module"))), Values: request.Values, ExpectedRevision: request.ExpectedRevision,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (h *Handler) resetModule(c *gin.Context) {
	var request moduleResetRequest
	if c.Request.ContentLength == 0 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid module request")
		return
	}
	if err := c.ShouldBindJSON(&request); err != nil || !request.hasRevision || request.ExpectedRevision < 0 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid module request")
		return
	}
	requestID := strings.TrimSpace(request.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(c.GetHeader("X-Request-ID"))
	}
	if len(requestID) > 128 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid module request")
		return
	}
	result, err := h.service.ResetModule(c.Request.Context(), h.actorFor(c), strings.ToLower(strings.TrimSpace(c.Param("module"))), request.ExpectedRevision, requestID)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (h *Handler) clearCredentials(c *gin.Context) {
	var request clearCredentialsRequest
	if c.Request.ContentLength == 0 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid module request")
		return
	}
	if err := c.ShouldBindJSON(&request); err != nil || !request.hasKeys || !request.hasRevision || len(request.Keys) == 0 || request.ExpectedRevision < 0 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid module request")
		return
	}
	requestID := strings.TrimSpace(request.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(c.GetHeader("X-Request-ID"))
	}
	if len(requestID) > 128 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid module request")
		return
	}
	result, err := h.service.ClearCredentials(c.Request.Context(), h.actorFor(c), settingsapp.ClearCredentialsInput{
		Module: strings.ToLower(strings.TrimSpace(c.Param("module"))), Keys: request.Keys,
		ExpectedRevision: request.ExpectedRevision, RequestID: requestID,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (h *Handler) get(c *gin.Context) {
	h.getKey(c, c.Param("key"))
}

func (h *Handler) getObservability(c *gin.Context) {
	key := c.Param("key")
	if !settingsapp.IsObservabilitySettingKey(key) {
		writeError(c, settingsapp.ErrSettingNotFound)
		return
	}
	h.getKey(c, key)
}

func (h *Handler) getKey(c *gin.Context, key string) {
	if settingsapp.IsRetiredSettingKey(key) {
		writeError(c, settingsapp.ErrSettingNotFound)
		return
	}
	setting, err := h.service.Get(c.Request.Context(), h.actorFor(c), key)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, setting)
}

func (h *Handler) history(c *gin.Context) {
	if settingsapp.IsRetiredSettingKey(c.Param("key")) {
		writeError(c, settingsapp.ErrSettingNotFound)
		return
	}
	items, err := h.service.History(c.Request.Context(), h.actorFor(c), c.Param("key"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, items)
}

func (h *Handler) update(c *gin.Context) {
	h.updateKey(c, c.Param("key"))
}

func (h *Handler) updateObservability(c *gin.Context) {
	key := c.Param("key")
	if !settingsapp.IsObservabilitySettingKey(key) {
		writeError(c, settingsapp.ErrSettingNotFound)
		return
	}
	h.updateKey(c, key)
}

func (h *Handler) updateKey(c *gin.Context, key string) {
	if settingsapp.IsRetiredSettingKey(key) {
		writeError(c, settingsapp.ErrSettingNotFound)
		return
	}
	var request updateRequest
	if err := c.ShouldBindJSON(&request); err != nil || len(request.Value) == 0 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid request")
		return
	}
	requestID := request.RequestID
	if strings.TrimSpace(requestID) == "" {
		requestID = c.GetHeader("X-Request-ID")
	}
	requestID = strings.TrimSpace(requestID)
	if len(requestID) > 128 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid request")
		return
	}
	setting, err := h.service.Update(c.Request.Context(), h.actorFor(c), settingsapp.UpdateInput{Key: key, Value: request.Value, ExpectedVersion: request.ExpectedVersion, RequestID: requestID})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, setting)
}

func (h *Handler) rollback(c *gin.Context) {
	if settingsapp.IsRetiredSettingKey(c.Param("key")) {
		writeError(c, settingsapp.ErrSettingNotFound)
		return
	}
	var request rollbackRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Version <= 0 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid request")
		return
	}
	requestID := request.RequestID
	if strings.TrimSpace(requestID) == "" {
		requestID = c.GetHeader("X-Request-ID")
	}
	requestID = strings.TrimSpace(requestID)
	if len(requestID) > 128 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid request")
		return
	}
	setting, err := h.service.Rollback(c.Request.Context(), h.actorFor(c), settingsapp.RollbackInput{Key: c.Param("key"), Version: request.Version, ExpectedVersion: request.ExpectedVersion, RequestID: requestID})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, setting)
}

func (h *Handler) testConnection(c *gin.Context) {
	var request connectionTestRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			response.Error(c, http.StatusBadRequest, 10000, "invalid request")
			return
		}
	}
	requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
	if requestID == "" {
		requestID = newRequestID()
	}
	result, err := h.service.TestConnection(c.Request.Context(), h.actorFor(c), c.Param("key"), requestID, request.Value)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, result)
}

func newRequestID() string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "settings-test"
	}
	return "settings-" + hex.EncodeToString(bytes[:])
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, settingsapp.ErrPermissionDenied):
		response.Error(c, http.StatusForbidden, 30000, "forbidden")
	case errors.Is(err, settingsapp.ErrModuleNotFound), errors.Is(err, settingsapp.ErrSettingNotFound):
		response.Error(c, http.StatusNotFound, 10001, "setting not found")
	case errors.Is(err, settingsapp.ErrModuleRevisionConflict), errors.Is(err, settingsapp.ErrVersionConflict):
		response.Error(c, http.StatusConflict, 10010, "version conflict")
	case errors.Is(err, settingsapp.ErrSettingLocked):
		response.Error(c, http.StatusConflict, 10011, "setting is managed by deployment configuration")
	case errors.Is(err, settingsapp.ErrInvalidSetting):
		response.Error(c, http.StatusBadRequest, 10000, "invalid setting")
	case strings.Contains(err.Error(), "repository unavailable"):
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
	default:
		response.Error(c, http.StatusInternalServerError, 50000, "internal error")
	}
}

func disabled(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
}
