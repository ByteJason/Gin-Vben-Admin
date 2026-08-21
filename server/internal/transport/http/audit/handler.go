// Package audithttp exposes the read-only administration audit query seam.
package audithttp

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	auditapp "example.com/gin-vben-admin/server/internal/application/audit"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const basePath = "/api/admin/v1/audit/events"

type Handler struct{ service *auditapp.Service }

func NewHandler(service *auditapp.Service) *Handler { return &Handler{service: service} }

func RegisterRoutes(r gin.IRouter, handler *Handler) {
	group := r.Group(basePath)
	registerRoutes(group, handler)
}

// RegisterRoutesOn mounts audit routes below an already-prefixed router
// group. This keeps middleware composition separate from path ownership.
func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	registerRoutes(group.Group("/audit/events"), handler)
}

func registerRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		group.GET("", disabled)
		return
	}
	group.GET("", handler.query)
}

func (h *Handler) query(c *gin.Context) {
	filter := auditapp.Filter{
		ActorID: c.Query("actorId"), Action: c.Query("action"), Resource: c.Query("resource"),
		Outcome: c.Query("outcome"), RequestID: c.Query("requestId"), Limit: 0,
	}
	var err error
	filter.Limit, err = queryInt(c, "limit")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid limit")
		return
	}
	filter.Offset, err = queryInt(c, "offset")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid offset")
		return
	}
	if value := strings.TrimSpace(c.Query("from")); value != "" {
		filter.From, err = time.Parse(time.RFC3339, value)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 10000, "invalid from")
			return
		}
	}
	if value := strings.TrimSpace(c.Query("to")); value != "" {
		filter.To, err = time.Parse(time.RFC3339, value)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 10000, "invalid to")
			return
		}
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
