// Package dictionaryhttp exposes tenant-scoped dictionary administration APIs.
package dictionaryhttp

import (
	"errors"
	"net/http"
	"strings"

	dictionaryapp "example.com/gin-vben-admin/server/internal/application/dictionary"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const basePath = "/api/admin/v1/dictionaries"

type Handler struct{ service *dictionaryapp.Service }

func NewHandler(service *dictionaryapp.Service) *Handler { return &Handler{service: service} }
func RegisterRoutes(r gin.IRouter, handler *Handler)     { registerRoutes(r.Group(basePath), handler) }
func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	registerRoutes(group.Group("/dictionaries"), handler)
}

func registerRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
			group.Handle(method, "/*path", disabled)
		}
		return
	}
	group.GET("", handler.listTypes)
	group.POST("", handler.createType)
	group.PATCH("/types/:code", handler.updateType)
	group.DELETE("/types/:code", handler.deleteType)
	group.GET("/:type/items", handler.listItems)
	group.POST("/:type/items", handler.createItem)
	group.POST("/:type/items/import", handler.importItems)
	group.PATCH("/:type/items/:id", handler.updateItem)
	group.DELETE("/:type/items/:id", handler.deleteItem)
}

type listQuery struct {
	IncludeDisabled bool `form:"includeDisabled"`
}

func (h *Handler) listTypes(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	var query listQuery
	_ = c.ShouldBindQuery(&query)
	items, err := h.service.ListTypes(c.Request.Context(), dictionaryapp.ListOptions{Locale: locale(c), IncludeDisabled: query.IncludeDisabled})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, items)
}

func (h *Handler) createType(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	var input dictionaryapp.TypeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid dictionary type", "dictionary.type.invalid", nil)
		return
	}
	item, err := h.service.CreateType(c.Request.Context(), input)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, item)
}

func (h *Handler) updateType(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	var input dictionaryapp.TypeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid dictionary type", "dictionary.type.invalid", nil)
		return
	}
	item, err := h.service.UpdateType(c.Request.Context(), c.Param("code"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, item)
}

func (h *Handler) deleteType(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	if err := h.service.DeleteType(c.Request.Context(), c.Param("code")); err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *Handler) listItems(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	var query listQuery
	_ = c.ShouldBindQuery(&query)
	items, err := h.service.ListItems(c.Request.Context(), c.Param("type"), dictionaryapp.ListOptions{Locale: locale(c), IncludeDisabled: query.IncludeDisabled})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, items)
}

func (h *Handler) createItem(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	var input dictionaryapp.ItemInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid dictionary item", "dictionary.item.invalid", nil)
		return
	}
	item, err := h.service.CreateItem(c.Request.Context(), c.Param("type"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, item)
}

type importRequest struct {
	Items []dictionaryapp.ItemInput `json:"items"`
}

func (h *Handler) importItems(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	var request importRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid dictionary import", "dictionary.import.invalid", nil)
		return
	}
	items, err := h.service.ImportItems(c.Request.Context(), c.Param("type"), request.Items)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

func (h *Handler) updateItem(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	var input dictionaryapp.ItemInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid dictionary item", "dictionary.item.invalid", nil)
		return
	}
	item, err := h.service.UpdateItem(c.Request.Context(), c.Param("type"), c.Param("id"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, item)
}

func (h *Handler) deleteItem(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	if err := h.service.DeleteItem(c.Request.Context(), c.Param("type"), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, nil)
}

func locale(c *gin.Context) string {
	if value := strings.TrimSpace(c.Query("locale")); value != "" {
		return value
	}
	header := strings.TrimSpace(c.GetHeader("Accept-Language"))
	if index := strings.IndexByte(header, ','); index >= 0 {
		header = header[:index]
	}
	if index := strings.IndexByte(header, ';'); index >= 0 {
		header = header[:index]
	}
	return header
}

func scopeOK(c *gin.Context) bool {
	if _, err := tenant.RequireContext(c.Request.Context()); err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid tenant context", "tenant.context.invalid", nil)
		return false
	}
	return true
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, dictionaryapp.ErrSystemReadOnly):
		response.ErrorWithMessageKey(c, http.StatusForbidden, 30000, "system dictionary is read-only", "dictionary.system.readOnly", nil)
	case errors.Is(err, dictionaryapp.ErrTypeNotFound), errors.Is(err, dictionaryapp.ErrItemNotFound):
		response.ErrorWithMessageKey(c, http.StatusNotFound, 10001, "dictionary record not found", "dictionary.record.notFound", nil)
	case errors.Is(err, dictionaryapp.ErrTypeConflict), errors.Is(err, dictionaryapp.ErrItemConflict):
		response.ErrorWithMessageKey(c, http.StatusConflict, 10010, "dictionary record already exists", "dictionary.record.conflict", nil)
	case errors.Is(err, dictionaryapp.ErrInvalidType), errors.Is(err, dictionaryapp.ErrInvalidItem), errors.Is(err, dictionaryapp.ErrImportLimit):
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid dictionary request", "dictionary.request.invalid", nil)
	case errors.Is(err, dictionaryapp.ErrRepositoryMissing):
		response.ErrorWithMessageKey(c, http.StatusServiceUnavailable, 40001, "dictionary dependency unavailable", "dictionary.dependency.unavailable", nil)
	default:
		response.ErrorWithMessageKey(c, http.StatusInternalServerError, 50000, "internal error", "error.internal", nil)
	}
}

func disabled(c *gin.Context) {
	response.ErrorWithMessageKey(c, http.StatusServiceUnavailable, 40001, "dictionary capability unavailable", "dictionary.capability.unavailable", nil)
}
