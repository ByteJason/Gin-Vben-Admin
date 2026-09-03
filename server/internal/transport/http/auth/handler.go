// Package authhttp exposes the management authentication HTTP seam.
package authhttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	appauth "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	httpmiddleware "github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/middleware"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const (
	authPath            = "/api/admin/v1/auth"
	codeBadRequest      = 10000
	codeCredentials     = 10002
	codeToken           = 10003
	codeRateLimited     = 10004
	codeCaptcha         = 10005
	codeUnauthenticated = 20000
)

type Handler struct {
	service  appauth.AuthService
	recovery appauth.AccountRecoveryService
	sessions appauth.SessionManagementService
	config   config.AuthConfig
	limiter  appauth.RateLimiter
	captcha  appauth.CaptchaProvider
	risk     appauth.CaptchaRiskStore
}

// Service exposes the transport's authentication port for composing other
// protected management route groups without duplicating token middleware.
func (h *Handler) Service() appauth.AuthService {
	if h == nil {
		return nil
	}
	return h.service
}

func NewHandler(service appauth.AuthService, cfg config.AuthConfig, limiters ...appauth.RateLimiter) *Handler {
	var limiter appauth.RateLimiter
	if len(limiters) > 0 {
		limiter = limiters[0]
	}
	return &Handler{service: service, config: cfg, limiter: limiter}
}

func (h *Handler) SetCaptchaProvider(provider appauth.CaptchaProvider) {
	if h != nil {
		h.captcha = provider
	}
}

// SetCaptchaRiskStore wires the bounded failed-login counter used to trigger
// captcha challenges without coupling HTTP handlers to Redis or memory.
func (h *Handler) SetCaptchaRiskStore(store appauth.CaptchaRiskStore) {
	if h != nil {
		h.risk = store
	}
}

// SetAccountRecovery wires the optional registration/password recovery seam.
func (h *Handler) SetAccountRecovery(recovery appauth.AccountRecoveryService) {
	if h != nil {
		h.recovery = recovery
	}
}

// SetSessionManager wires user-scoped device-session listing and revocation.
func (h *Handler) SetSessionManager(manager appauth.SessionManagementService) {
	if h != nil {
		h.sessions = manager
	}
}

// RegisterRoutes installs login, refresh, and logout. A nil handler is a
// deliberate disabled seam that returns a safe dependency error rather than
// accidentally accepting credentials.
func RegisterRoutes(r gin.IRouter, handler *Handler, policies ...httpmiddleware.TenantPolicy) {
	group := r.Group(authPath)
	if handler == nil || !handler.config.Enabled || handler.service == nil {
		group.GET("/captcha", disabled)
		group.POST("/login", disabled)
		group.POST("/refresh", disabled)
		group.POST("/logout", disabled)
		group.POST("/register", disabled)
		group.POST("/password/reset/request", disabled)
		group.POST("/password/reset", disabled)
		group.GET("/sessions", disabled)
		group.DELETE("/sessions/:id", disabled)
		return
	}
	policy := httpmiddleware.TenantPolicy{Mode: "single", DefaultTenantID: "default"}
	if len(policies) > 0 {
		policy = policies[0]
	}
	group.Use(httpmiddleware.TenantContext(policy))
	group.GET("/captcha", handler.issueCaptcha)
	group.POST("/login", handler.login)
	group.POST("/refresh", handler.refresh)
	group.POST("/logout", handler.logout)
	if handler.config.RegistrationEnabled && handler.recovery != nil {
		group.POST("/register", handler.register)
	} else {
		group.POST("/register", disabled)
	}
	if handler.recovery != nil {
		group.POST("/password/reset/request", handler.requestPasswordReset)
		group.POST("/password/reset", handler.resetPassword)
	} else {
		group.POST("/password/reset/request", disabled)
		group.POST("/password/reset", disabled)
	}
	if handler.sessions != nil {
		group.GET("/sessions", Middleware(handler.service), handler.listSessions)
		group.DELETE("/sessions/:id", Middleware(handler.service), handler.revokeSession)
	} else {
		group.GET("/sessions", disabled)
		group.DELETE("/sessions/:id", disabled)
	}
}

type loginRequest struct {
	// Username remains a wire-compatible alias for older clients. New clients
	// may send identifier + identifierType so username/email semantics are
	// explicit without introducing a new API generation.
	Username       string `json:"username,omitempty"`
	Identifier     string `json:"identifier,omitempty"`
	IdentifierType string `json:"identifierType,omitempty"`
	Password       string `json:"password"`
	CaptchaID      string `json:"captchaId,omitempty"`
	Captcha        string `json:"captcha,omitempty"`
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type passwordResetRequest struct {
	Username string `json:"username,omitempty"`
	Token    string `json:"token,omitempty"`
	Password string `json:"password,omitempty"`
}

type sessionData struct {
	ID         string    `json:"id"`
	DeviceID   string    `json:"deviceId"`
	DeviceName string    `json:"deviceName"`
	IPAddress  string    `json:"ipAddress"`
	UserAgent  string    `json:"userAgent"`
	ExpiresAt  time.Time `json:"expiresAt"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	Revoked    bool      `json:"revoked"`
}

type tokenData struct {
	AccessToken string `json:"accessToken"`
	TokenType   string `json:"tokenType"`
	ExpiresIn   int64  `json:"expiresIn"`
}

func (h *Handler) login(c *gin.Context) {
	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Password == "" {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	identifier, identifierType, err := canonicalLoginIdentifier(request)
	if err != nil {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	if request.IdentifierType != "" && request.IdentifierType != string(identifierType) {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	if !h.checkRateLimit(c, identifier) {
		return
	}
	ctx := requestContext(c)
	riskKey := captchaRiskKey(ctx, identifier)
	required, riskErr := h.captchaRequired(ctx, riskKey)
	if riskErr != nil {
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
		return
	}
	// Once a client presents either half of an issued image challenge, treat
	// the challenge as required even when the account is still below the risk
	// threshold. This keeps the rendered login form and the HTTP boundary in
	// agreement and prevents a captchaId-only request from bypassing verify.
	if strings.TrimSpace(request.CaptchaID) != "" || strings.TrimSpace(request.Captcha) != "" {
		required = true
	}
	if !h.verifyCaptcha(c, ctx, request, required) {
		return
	}
	pair, err := h.service.Login(ctx, identifier, request.Password)
	if err != nil {
		if errors.Is(err, authdomain.ErrInvalidCredentials) && h.risk != nil {
			if riskErr := h.risk.RecordFailure(ctx, riskKey, h.config.CaptchaRiskWindow); riskErr != nil {
				response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
				return
			}
		}
		handleAuthError(c, err, true)
		return
	}
	if h.risk != nil {
		if riskErr := h.risk.Reset(ctx, riskKey); riskErr != nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
			return
		}
	}
	h.setRefreshCookie(c, pair.RefreshToken, false)
	response.OK(c, tokenData{AccessToken: pair.AccessToken, TokenType: "Bearer", ExpiresIn: pair.ExpiresIn})
}

func canonicalLoginIdentifier(request loginRequest) (string, authdomain.IdentifierType, error) {
	identifier := strings.TrimSpace(request.Identifier)
	username := strings.TrimSpace(request.Username)
	if identifier != "" && username != "" {
		canonicalUsername, _, usernameErr := authdomain.NormalizeIdentifier(username)
		canonicalIdentifier, _, identifierErr := authdomain.NormalizeIdentifier(identifier)
		if usernameErr != nil || identifierErr != nil || canonicalUsername != canonicalIdentifier {
			return "", "", authdomain.ErrInvalidIdentifier
		}
	}
	value := identifier
	if value == "" {
		value = username
	}
	return authdomain.NormalizeIdentifier(value)
}

func (h *Handler) register(c *gin.Context) {
	var request registerRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	if err := h.recovery.Register(requestContext(c), strings.TrimSpace(request.Username), request.Password); err != nil {
		handleRecoveryError(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *Handler) requestPasswordReset(c *gin.Context) {
	var request passwordResetRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.Username) == "" {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	if err := h.recovery.RequestPasswordReset(requestContext(c), strings.TrimSpace(request.Username)); err != nil {
		handleRecoveryError(c, err)
		return
	}
	// Existing and missing accounts deliberately receive the same success body.
	response.OK(c, nil)
}

func (h *Handler) resetPassword(c *gin.Context) {
	var request passwordResetRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.Token) == "" || request.Password == "" {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	if err := h.recovery.ResetPassword(requestContext(c), strings.TrimSpace(request.Token), request.Password); err != nil {
		handleRecoveryError(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *Handler) listSessions(c *gin.Context) {
	claims, ok := verifiedClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, codeUnauthenticated, "unauthenticated")
		return
	}
	sessions, err := h.sessions.ListSessions(requestContext(c), claims.Subject)
	if err != nil {
		handleSessionError(c, err)
		return
	}
	items := make([]sessionData, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, sessionData{
			ID: session.ID, DeviceID: session.DeviceID, DeviceName: session.DeviceName,
			IPAddress: session.IPAddress, UserAgent: session.UserAgent, ExpiresAt: session.ExpiresAt,
			CreatedAt: session.CreatedAt, LastSeenAt: session.LastSeenAt, Revoked: session.Revoked,
		})
	}
	response.OK(c, items)
}

func (h *Handler) revokeSession(c *gin.Context) {
	claims, ok := verifiedClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, codeUnauthenticated, "unauthenticated")
		return
	}
	sessionID := strings.TrimSpace(c.Param("id"))
	if sessionID == "" {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return
	}
	if err := h.sessions.RevokeSession(requestContext(c), claims.Subject, sessionID); err != nil {
		handleSessionError(c, err)
		return
	}
	response.OK(c, nil)
}

func verifiedClaims(c *gin.Context) (authdomain.Claims, bool) {
	claims, ok := c.Get("auth_claims")
	if !ok {
		return authdomain.Claims{}, false
	}
	value, ok := claims.(authdomain.Claims)
	return value, ok && strings.TrimSpace(value.Subject) != ""
}

func (h *Handler) verifyCaptcha(c *gin.Context, ctx context.Context, request loginRequest, required bool) bool {
	if !required {
		return true
	}
	if h.captcha == nil {
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
		return false
	}
	if strings.TrimSpace(request.CaptchaID) == "" || strings.TrimSpace(request.Captcha) == "" {
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
		return false
	}
	if err := h.captcha.Verify(ctx, request.CaptchaID, request.Captcha); err != nil {
		switch {
		case errors.Is(err, appauth.ErrCaptchaInvalid), errors.Is(err, appauth.ErrCaptchaExpired):
			response.Error(c, http.StatusBadRequest, codeCaptcha, "invalid captcha")
		default:
			response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
		}
		return false
	}
	return true
}

func (h *Handler) captchaRequired(ctx context.Context, key string) (bool, error) {
	if h == nil {
		return false, nil
	}
	if h.config.CaptchaEnabled {
		return true, nil
	}
	// An explicitly injected provider remains enabled for backwards-compatible
	// tests and adapters when no risk store is configured. Runtime wiring uses
	// the config flag and risk store instead.
	if h.risk == nil {
		return h.captcha != nil, nil
	}
	return h.risk.Requires(ctx, key, h.config.CaptchaRiskThreshold, h.config.CaptchaRiskWindow)
}

func captchaRiskKey(ctx context.Context, identifier string) string {
	metadata := appauth.RequestMetadataFromContext(ctx)
	if strings.TrimSpace(metadata.IPAddress) == "" {
		return strings.TrimSpace(identifier)
	}
	return strings.TrimSpace(identifier) + "|" + strings.TrimSpace(metadata.IPAddress)
}

func (h *Handler) issueCaptcha(c *gin.Context) {
	if h == nil || h.captcha == nil {
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
		return
	}
	challenge, err := h.captcha.Issue(requestContext(c))
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
		return
	}
	response.OK(c, challenge)
}

func (h *Handler) checkRateLimit(c *gin.Context, username string) bool {
	if h.limiter == nil {
		return true
	}
	remote := c.Request.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	for _, key := range []string{"account:" + strings.TrimSpace(username), "ip:" + remote} {
		allowed, err := h.limiter.Allow(contextOrBackground(c), key, h.config.RateLimitMaxAttempts, h.config.RateLimitWindow)
		if err != nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
			return false
		}
		if !allowed {
			retryAfter := int(h.config.RateLimitWindow.Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			response.Error(c, http.StatusTooManyRequests, codeRateLimited, "too many authentication attempts")
			return false
		}
	}
	return true
}

func contextOrBackground(c *gin.Context) context.Context {
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		return c.Request.Context()
	}
	return context.Background()
}

// requestContext carries correlation and device attributes from the HTTP
// boundary into application ports. Values are bounded by the application
// metadata helper before they can reach durable session/audit storage.
func requestContext(c *gin.Context) context.Context {
	ctx := contextOrBackground(c)
	// Preserve metadata installed by the bearer middleware (principal, locale,
	// trace and trusted caller) while refreshing transport-specific headers.
	metadata := appauth.RequestMetadataFromContext(ctx)
	if c == nil {
		return appauth.WithRequestMetadata(ctx, metadata)
	}
	if value, ok := c.Get("request_id"); ok {
		metadata.RequestID, _ = value.(string)
	}
	if metadata.RequestID == "" {
		metadata.RequestID = c.GetHeader("X-Request-ID")
	}
	metadata.DeviceID = c.GetHeader("X-Device-ID")
	metadata.DeviceName = c.GetHeader("X-Device-Name")
	metadata.JSFingerprint = c.GetHeader("X-JS-Fingerprint")
	metadata.UserAgent = c.GetHeader("User-Agent")
	if c.Request != nil {
		metadata.IPAddress = c.Request.RemoteAddr
		if host, _, err := net.SplitHostPort(metadata.IPAddress); err == nil {
			metadata.IPAddress = host
		}
	}
	return appauth.WithRequestMetadata(ctx, metadata)
}

func (h *Handler) refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(h.cookieName())
	if err != nil || strings.TrimSpace(refreshToken) == "" {
		response.Error(c, http.StatusUnauthorized, codeUnauthenticated, "unauthenticated")
		return
	}
	pair, err := h.service.Refresh(requestContext(c), refreshToken)
	if err != nil {
		handleAuthError(c, err, false)
		return
	}
	h.setRefreshCookie(c, pair.RefreshToken, false)
	response.OK(c, tokenData{AccessToken: pair.AccessToken, TokenType: "Bearer", ExpiresIn: pair.ExpiresIn})
}

func (h *Handler) logout(c *gin.Context) {
	refreshToken, err := c.Cookie(h.cookieName())
	if err != nil || strings.TrimSpace(refreshToken) == "" {
		response.Error(c, http.StatusUnauthorized, codeUnauthenticated, "unauthenticated")
		return
	}
	if logoutService, ok := h.service.(appauth.RefreshLogoutService); ok {
		err = logoutService.LogoutWithRefreshToken(requestContext(c), refreshToken)
	} else {
		// Implementations that expose only AuthService cannot safely derive a
		// session ID from an opaque refresh token, so reject rather than guess.
		err = authdomain.ErrInvalidToken
	}
	if err != nil {
		handleAuthError(c, err, false)
		return
	}
	h.setRefreshCookie(c, "", true)
	response.OK(c, nil)
}

func disabled(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
}

func (h *Handler) cookieName() string {
	if strings.TrimSpace(h.config.RefreshCookieName) == "" {
		return "refresh_token"
	}
	return h.config.RefreshCookieName
}

func (h *Handler) setRefreshCookie(c *gin.Context, value string, clear bool) {
	maxAge := int(h.config.RefreshTTL.Seconds())
	expires := time.Now().Add(h.config.RefreshTTL)
	if clear {
		maxAge = -1
		expires = time.Unix(1, 0)
		value = ""
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     h.cookieName(),
		Value:    value,
		Path:     authPath,
		MaxAge:   maxAge,
		Expires:  expires,
		HttpOnly: true,
		Secure:   h.config.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

func handleAuthError(c *gin.Context, err error, login bool) {
	switch {
	case errors.Is(err, authdomain.ErrInvalidCredentials), errors.Is(err, authdomain.ErrAccountLocked):
		response.Error(c, http.StatusUnauthorized, codeCredentials, "invalid credentials")
	case errors.Is(err, authdomain.ErrInvalidToken):
		response.Error(c, http.StatusUnauthorized, codeToken, "invalid token")
	case errors.Is(err, authdomain.ErrRefreshReplay), errors.Is(err, authdomain.ErrSessionRevoked), errors.Is(err, authdomain.ErrSessionNotFound):
		response.Error(c, http.StatusUnauthorized, codeUnauthenticated, "unauthenticated")
	case errors.Is(err, authdomain.ErrDependencyUnavailable):
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
	default:
		if login {
			// Unknown login failures are treated as an unavailable dependency;
			// credential mismatches use the explicit branch above.
			response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
			return
		}
		response.Error(c, http.StatusInternalServerError, 50000, "internal error")
	}
}

func handleRecoveryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authdomain.ErrInvalidAccount):
		response.Error(c, http.StatusBadRequest, codeBadRequest, "invalid request")
	case errors.Is(err, authdomain.ErrUserAlreadyExists):
		response.Error(c, http.StatusUnprocessableEntity, 10001, "validation failed")
	case errors.Is(err, authdomain.ErrPasswordResetInvalid):
		response.Error(c, http.StatusUnauthorized, codeCredentials, "invalid credentials")
	case errors.Is(err, authdomain.ErrDependencyUnavailable):
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
	default:
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
	}
}

func handleSessionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authdomain.ErrSessionNotFound), errors.Is(err, authdomain.ErrSessionRevoked):
		response.Error(c, http.StatusUnauthorized, codeUnauthenticated, "unauthenticated")
	case errors.Is(err, authdomain.ErrDependencyUnavailable):
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
	default:
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
	}
}

// Middleware protects a route with a bearer access token and stores verified
// claims in the request context.
func Middleware(service appauth.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
			c.Abort()
			return
		}
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) || strings.TrimSpace(strings.TrimPrefix(header, prefix)) == "" {
			response.Error(c, http.StatusUnauthorized, codeUnauthenticated, "unauthenticated")
			c.Abort()
			return
		}
		claims, err := service.VerifyAccess(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
		if err != nil {
			response.Error(c, http.StatusUnauthorized, codeToken, "invalid token")
			c.Abort()
			return
		}
		c.Set("auth_claims", claims)
		// Install the verified principal and request correlation metadata before
		// downstream tenant/IAM/common-capability middleware runs.  The token
		// subject is the only trusted principal source; caller and locale values
		// already installed by an earlier trusted adapter are preserved.
		ctx := contextOrBackground(c)
		metadata := appauth.RequestMetadataFromContext(ctx)
		metadata.PrincipalID = claims.Subject
		if metadata.RequestID == "" {
			if value, ok := c.Get("request_id"); ok {
				metadata.RequestID, _ = value.(string)
			}
		}
		if metadata.RequestID == "" {
			metadata.RequestID = c.GetHeader(httpmiddleware.RequestIDHeader)
		}
		if metadata.TraceID == "" {
			metadata.TraceID = metadata.RequestID
		}
		if metadata.Locale == "" {
			// Keep locale parsing deliberately small at this boundary. The
			// notification runtime owns the complete fallback chain.
			if value := strings.TrimSpace(c.GetHeader("Accept-Language")); value != "" {
				metadata.Locale = strings.TrimSpace(strings.Split(value, ",")[0])
			}
		}
		ctx = appauth.WithRequestMetadata(ctx, metadata)
		ctx = appauth.WithCapabilityMetadata(ctx, metadata.CallerKey, metadata.Locale, metadata.TraceID, metadata.PrincipalID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
