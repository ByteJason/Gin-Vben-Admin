// Package filehttp exposes the tenant-scoped local file-center HTTP seam.
package filehttp

import (
	"errors"
	"io"
	mimepkg "mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	fileapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/file"
	authdomain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const basePath = "/api/admin/v1/files"
const maxMultipartBytes int64 = 100 << 20

type Handler struct{ service *fileapp.Service }

func NewHandler(service *fileapp.Service) *Handler { return &Handler{service: service} }

func RegisterRoutes(r gin.IRouter, handler *Handler) { registerRoutes(r.Group(basePath), handler) }

func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	registerRoutes(group.Group("/files"), handler)
}

func registerRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		group.GET("/*path", disabled)
		group.POST("/*path", disabled)
		group.DELETE("/*path", disabled)
		return
	}
	group.GET("", handler.list)
	group.GET("/", handler.list)
	group.POST("/upload", handler.upload)
	group.GET("/categories", handler.listCategories)
	group.POST("/categories", handler.createCategory)
	group.PUT("/categories/:id", handler.updateCategory)
	group.PATCH("/categories/:id", handler.updateCategory)
	group.DELETE("/categories/:id", handler.deleteCategory)
	group.GET("/cleanup/dry-run", handler.cleanupDryRun)
	group.GET("/:id", handler.metadata)
	group.GET("/:id/download", handler.download)
	group.GET("/:id/preview", handler.preview)
	group.POST("/:id/signed-url", handler.signedURL)
	group.DELETE("/:id", handler.delete)
}

func (h *Handler) list(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	limit, err := boundedInt(c.Query("limit"), 50, 1, 200)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid limit")
		return
	}
	offset, err := boundedInt(c.Query("offset"), 0, 0, 1_000_000)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid offset")
		return
	}
	page, err := h.service.List(c.Request.Context(), fileapp.ListFilter{TenantID: scope.TenantID, OrgID: scope.Organization, OwnerID: strings.TrimSpace(c.Query("ownerId")), CategoryID: strings.TrimSpace(c.Query("categoryId")), Limit: limit, Offset: offset})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, page)
}

func (h *Handler) upload(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	if err := c.Request.ParseMultipartForm(maxMultipartBytes); err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid multipart upload")
		return
	}
	header, err := c.FormFile("file")
	if err != nil || header == nil {
		response.Error(c, http.StatusBadRequest, 10000, "file is required")
		return
	}
	if header.Size > maxMultipartBytes {
		response.Error(c, http.StatusRequestEntityTooLarge, 10000, "file is too large")
		return
	}
	reader, err := header.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "file is unreadable")
		return
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxMultipartBytes+1))
	if err != nil || int64(len(data)) > maxMultipartBytes {
		response.Error(c, http.StatusRequestEntityTooLarge, 10000, "file is too large")
		return
	}
	mime := strings.TrimSpace(header.Header.Get("Content-Type"))
	if parsed, _, parseErr := mimepkg.ParseMediaType(mime); parseErr == nil {
		mime = parsed
	}
	if mime == "" || mime == "application/octet-stream" {
		detected := http.DetectContentType(data)
		if detected != "" {
			mime = detected
		}
	}
	if parsed, _, parseErr := mimepkg.ParseMediaType(mime); parseErr == nil {
		mime = parsed
	}
	acl := fileapp.ACL(strings.TrimSpace(c.PostForm("acl")))
	item, err := h.service.Upload(c.Request.Context(), fileapp.UploadInput{Name: header.Filename, MIME: mime, Size: int64(len(data)), OwnerID: actorID(c), TenantID: scope.TenantID, OrgID: scope.Organization, ACL: acl, Data: data, CategoryID: strings.TrimSpace(c.PostForm("categoryId"))})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, item)
}

func (h *Handler) listCategories(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	response.OK(c, h.service.ListCategories(c.Request.Context(), scope.TenantID, scope.Organization))
}
func (h *Handler) createCategory(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	var in fileapp.CategoryInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid category")
		return
	}
	item, err := h.service.CreateCategory(c.Request.Context(), in, scope.TenantID, scope.Organization)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, item)
}
func (h *Handler) updateCategory(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	var in fileapp.CategoryInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid category")
		return
	}
	item, err := h.service.UpdateCategory(c.Request.Context(), c.Param("id"), in, scope.TenantID, scope.Organization)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, item)
}
func (h *Handler) deleteCategory(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	if err := h.service.DeleteCategory(c.Request.Context(), c.Param("id"), scope.TenantID, scope.Organization); err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *Handler) metadata(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	item, _, err := h.service.Download(c.Request.Context(), c.Param("id"), actorID(c), scope.TenantID, scope.Organization)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, item)
}

func (h *Handler) download(c *gin.Context) { h.serveObject(c, false) }

func (h *Handler) preview(c *gin.Context) { h.serveObject(c, true) }

func (h *Handler) serveObject(c *gin.Context, inline bool) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	item, object, err := h.service.Download(c.Request.Context(), c.Param("id"), actorID(c), scope.TenantID, scope.Organization)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Content-Type", item.MIME)
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	c.Header("Content-Disposition", disposition+`; filename="`+safeFilename(item.Name)+`"`)
	c.Data(http.StatusOK, item.MIME, object.Data)
}

func (h *Handler) signedURL(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	var request struct {
		TTLSeconds int `json:"ttlSeconds"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			response.Error(c, http.StatusBadRequest, 10000, "invalid request")
			return
		}
	}
	if request.TTLSeconds == 0 {
		request.TTLSeconds = 900
	}
	if request.TTLSeconds < 1 || request.TTLSeconds > 86400 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid URL TTL")
		return
	}
	// authorize against the tenant before asking the provider to sign.
	if _, err := h.service.Authorize(c.Param("id"), actorID(c), scope.TenantID, scope.Organization); err != nil {
		writeError(c, err)
		return
	}
	url, err := h.service.SignedURLFor(c.Request.Context(), c.Param("id"), actorID(c), scope.TenantID, scope.Organization, time.Duration(request.TTLSeconds)*time.Second)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, gin.H{"url": url, "expiresIn": request.TTLSeconds})
}

func (h *Handler) delete(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	if err := h.service.DeleteFile(c.Request.Context(), c.Param("id"), actorID(c), scope.TenantID, scope.Organization); err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *Handler) cleanupDryRun(c *gin.Context) {
	scope, ok := requestScope(c)
	if !ok {
		return
	}
	seconds, err := boundedInt(c.Query("ageSeconds"), 180*24*60*60, 1, 10*365*24*60*60)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid cleanup age")
		return
	}
	report, err := h.service.CleanupDryRun(c.Request.Context(), time.Duration(seconds)*time.Second, scope.TenantID, scope.Organization)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, report)
}

func requestScope(c *gin.Context) (tenant.Context, bool) {
	scope, err := tenant.RequireContext(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid tenant context")
		return tenant.Context{}, false
	}
	return scope, true
}

func actorID(c *gin.Context) string {
	if value, ok := c.Get("auth_claims"); ok {
		if claims, ok := value.(authdomain.Claims); ok {
			return strings.TrimSpace(claims.Subject)
		}
	}
	return strings.TrimSpace(c.GetHeader("X-Actor-ID"))
}

func boundedInt(raw string, fallback, min, max int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, errors.New("invalid integer")
	}
	return value, nil
}

func safeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	if name == "" {
		return "download"
	}
	return name
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, fileapp.ErrFileNotFound):
		response.Error(c, http.StatusNotFound, 10001, "file not found")
	case errors.Is(err, fileapp.ErrAccessDenied):
		response.Error(c, http.StatusForbidden, 30000, "forbidden")
	case errors.Is(err, fileapp.ErrCategoryNotFound):
		response.Error(c, http.StatusNotFound, 10001, "category not found")
	case errors.Is(err, fileapp.ErrCategoryAccessDenied):
		response.Error(c, http.StatusForbidden, 30000, "forbidden")
	case errors.Is(err, fileapp.ErrCategoryNotEmpty):
		response.Error(c, http.StatusConflict, 10000, "category is not empty")
	case errors.Is(err, fileapp.ErrInvalidCategory):
		response.Error(c, http.StatusBadRequest, 10000, "invalid category")
	case errors.Is(err, fileapp.ErrFileTooLarge):
		response.Error(c, http.StatusRequestEntityTooLarge, 10000, "file is too large")
	case errors.Is(err, fileapp.ErrMIMETypeNotAllowed):
		response.Error(c, http.StatusUnsupportedMediaType, 10000, "file MIME type is not allowed")
	case errors.Is(err, fileapp.ErrInvalidUpload):
		response.Error(c, http.StatusBadRequest, 10000, "invalid file")
	case errors.Is(err, fileapp.ErrStorageRead):
		response.Error(c, http.StatusNotImplemented, 40001, "file preview unavailable")
	default:
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
	}
}

func disabled(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
}
