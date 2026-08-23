// Package audithttp exposes the read-only administration audit query seam.
package audithttp

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	auditapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/audit"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

type Handler struct{ service *auditapp.Service }

func NewHandler(service *auditapp.Service) *Handler { return &Handler{service: service} }

func RegisterRoutes(r gin.IRouter, handler *Handler) {
	auditGroup := r.Group("/api/admin/v1/audit")
	registerRoutes(auditGroup.Group("/events"), handler)
	registerRetentionRoute(auditGroup, handler)
}

// RegisterRoutesOn mounts audit routes below an already-prefixed router
// group. This keeps middleware composition separate from path ownership.
func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	auditGroup := group.Group("/audit")
	registerRoutes(auditGroup.Group("/events"), handler)
	registerRetentionRoute(auditGroup, handler)
}

func registerRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		group.GET("", disabled)
		group.GET("/export", disabled)
		group.GET("/retention/dry-run", disabled)
		return
	}
	group.GET("", handler.query)
	group.GET("/export", handler.export)
}

func registerRetentionRoute(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		group.GET("/retention/dry-run", disabled)
		return
	}
	group.GET("/retention/dry-run", handler.retentionDryRun)
}

func (h *Handler) query(c *gin.Context) {
	filter, err := parseFilter(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, err.Error())
		return
	}
	page, err := h.service.Query(c.Request.Context(), filter)
	if err != nil {
		if err == auditapp.ErrInvalidFilter {
			response.Error(c, http.StatusBadRequest, 10000, "invalid filter")
			return
		}
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
		return
	}
	response.OK(c, page)
}

func (h *Handler) export(c *gin.Context) {
	filter, err := parseFilter(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, err.Error())
		return
	}
	format := auditapp.ExportFormat(strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", string(auditapp.ExportFormatJSON)))))
	result, err := h.service.Export(c.Request.Context(), filter, format)
	if err != nil {
		if err == auditapp.ErrInvalidFilter || err == auditapp.ErrInvalidExportFormat {
			response.Error(c, http.StatusBadRequest, 10000, "invalid export")
			return
		}
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+result.Filename+`"`)
	if requestID := strings.TrimSpace(c.GetHeader("X-Request-ID")); requestID != "" {
		c.Header("X-Request-ID", requestID)
	}
	c.Data(http.StatusOK, result.ContentType, result.Data)
}

func (h *Handler) retentionDryRun(c *gin.Context) {
	days, err := queryInt(c, "days")
	if err != nil || days == 0 {
		days = 180
	}
	report, err := h.service.RetentionDryRun(c.Request.Context(), time.Now().UTC(), days)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid retention policy")
		return
	}
	response.OK(c, report)
}

func parseFilter(c *gin.Context) (auditapp.Filter, error) {
	filter := auditapp.Filter{
		ActorID: c.Query("actorId"), Action: c.Query("action"), Category: auditapp.Category(strings.TrimSpace(c.Query("category"))), Resource: c.Query("resource"),
		Outcome: c.Query("outcome"), RequestID: c.Query("requestId"), Limit: 0,
	}
	var err error
	filter.Limit, err = queryInt(c, "limit")
	if err != nil {
		return auditapp.Filter{}, errors.New("invalid limit")
	}
	filter.Offset, err = queryInt(c, "offset")
	if err != nil {
		return auditapp.Filter{}, errors.New("invalid offset")
	}
	if value := strings.TrimSpace(c.Query("from")); value != "" {
		filter.From, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return auditapp.Filter{}, errors.New("invalid from")
		}
	}
	if value := strings.TrimSpace(c.Query("to")); value != "" {
		filter.To, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return auditapp.Filter{}, errors.New("invalid to")
		}
	}
	return filter, nil
}

func queryInt(c *gin.Context, key string) (int, error) {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

func disabled(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
}
