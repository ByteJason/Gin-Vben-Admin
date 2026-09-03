// Package mailhttp exposes tenant-scoped SMTP account and delivery routes.
package mailhttp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	mailapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/mail"
	authdomain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const basePath = "/api/admin/v1/mail"

type Handler struct{ service *mailapp.Service }

func NewHandler(service *mailapp.Service) *Handler { return &Handler{service: service} }

func RegisterRoutes(r gin.IRouter, handler *Handler) { registerRoutes(r.Group(basePath), handler) }

func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	registerRoutes(group.Group("/mail"), handler)
}

func registerRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		group.GET("/*path", disabled)
		group.POST("/*path", disabled)
		group.PUT("/*path", disabled)
		group.DELETE("/*path", disabled)
		return
	}
	group.GET("/accounts", handler.listAccounts)
	group.POST("/accounts", handler.createAccount)
	group.PUT("/accounts/:id", handler.updateAccount)
	group.POST("/accounts/:id/test", handler.testAccount)
	group.DELETE("/accounts/:id", handler.deleteAccount)
	group.GET("/messages", handler.listMessages)
	group.POST("/messages", handler.sendMessage)
	group.GET("/messages/:id", handler.getMessage)
}

func (h *Handler) listAccounts(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	items, err := h.service.ListAccounts(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, items)
}

func (h *Handler) createAccount(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	var input mailapp.AccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid request")
		return
	}
	account, err := h.service.CreateAccount(c.Request.Context(), input)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, account)
}

func (h *Handler) updateAccount(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	var input mailapp.AccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid request")
		return
	}
	account, err := h.service.UpdateAccount(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, account)
}

func (h *Handler) testAccount(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
	if requestID == "" {
		requestID = "mail-test"
	}
	result, err := h.service.TestAccount(c.Request.Context(), c.Param("id"), requestID)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (h *Handler) deleteAccount(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	if err := h.service.DeleteAccount(c.Request.Context(), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *Handler) listMessages(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	from, err := queryTime(c.Query("from"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid from time")
		return
	}
	to, err := queryTime(c.Query("to"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid to time")
		return
	}
	page, err := h.service.ListMessages(c.Request.Context(), mailapp.MessageFilter{
		AccountID: strings.TrimSpace(c.Query("accountId")),
		CallerKey: strings.TrimSpace(c.Query("callerKey")),
		From:      from,
		Keyword:   strings.TrimSpace(c.Query("keyword")),
		Limit:     queryInt(c.Query("limit"), 50),
		Offset:    queryInt(c.Query("offset"), 0),
		Source:    strings.TrimSpace(c.Query("source")),
		Status:    strings.TrimSpace(c.Query("status")),
		To:        to,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, page)
}

func (h *Handler) sendMessage(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	var input mailapp.SendInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid request")
		return
	}
	ctx := mailapp.WithSender(c.Request.Context(), actorID(c))
	message, err := h.service.Send(ctx, input)
	if err != nil {
		// A persisted failed record is still returned so operators can inspect
		// the stable status/error code without receiving the body.
		if message.ID != "" {
			response.Write(c, http.StatusBadGateway, 40001, "email delivery failed", message)
			return
		}
		writeError(c, err)
		return
	}
	response.OK(c, message)
}

func (h *Handler) getMessage(c *gin.Context) {
	if _, ok := requestScope(c); !ok {
		return
	}
	includeBody := strings.EqualFold(strings.TrimSpace(c.Query("includeBody")), "true")
	message, err := h.service.GetMessage(c.Request.Context(), c.Param("id"), includeBody)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, message)
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

func queryInt(value string, fallback int) int {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func queryTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, mailapp.ErrAccountConflict):
		response.Error(c, http.StatusConflict, 10010, "SMTP account already exists")
	case errors.Is(err, mailapp.ErrAccountNotFound), errors.Is(err, mailapp.ErrMessageNotFound):
		response.Error(c, http.StatusNotFound, 10001, "mail record not found")
	case errors.Is(err, mailapp.ErrPermissionDenied):
		response.Error(c, http.StatusForbidden, 30000, "forbidden")
	case errors.Is(err, mailapp.ErrInvalidAccount), errors.Is(err, mailapp.ErrInvalidSend):
		response.Error(c, http.StatusBadRequest, 10000, "invalid mail request")
	case errors.Is(err, mailapp.ErrDeliveryFailed):
		response.Error(c, http.StatusBadGateway, 40001, "email delivery failed")
	default:
		response.Error(c, http.StatusServiceUnavailable, 40001, "mail dependency unavailable")
	}
}

func disabled(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 40001, "mail capability unavailable")
}
