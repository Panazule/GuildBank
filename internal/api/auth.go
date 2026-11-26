package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

type authHandler struct {
	auth    *service.AuthService
	views   *views
	logger  *slog.Logger
	secure  bool
	csrfMgr *csrfManager
}

type loginRequest struct {
	UserName string `json:"userName" form:"UserName"`
	Password string `json:"password" form:"Password"`
	Next     string `json:"next" form:"next"`
}

type publicUser struct {
	ID                 int64      `json:"id"`
	UserName           string     `json:"userName"`
	AccessLevel        string     `json:"accessLevel"`
	LastLogin          *time.Time `json:"lastLogin,omitempty"`
	Disabled           bool       `json:"disabled"`
	MustChangePassword bool       `json:"mustChangePassword"`
}

func toPublicUser(u domain.User) publicUser {
	return publicUser{
		ID:                 u.ID,
		UserName:           u.UserName,
		AccessLevel:        u.AccessLevel,
		LastLogin:          u.LastLogin,
		Disabled:           u.Disabled,
		MustChangePassword: u.MustChangePassword,
	}
}

type sessionResponse struct {
	User              publicUser `json:"user"`
	ExpiresAt         time.Time  `json:"expiresAt"`
	AbsoluteExpiresAt time.Time  `json:"absoluteExpiresAt"`
}

// csrf issues a token for API clients. The browser can also use it as the
// first request before any form is loaded.
func (h *authHandler) csrf(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"csrfToken": CSRF(c)}})
}

func (h *authHandler) loginPage(c *gin.Context) {
	if token := sessionToken(c); token != "" {
		if session, err := h.auth.Authenticate(c.Request.Context(), token); err == nil {
			if session.User.MustChangePassword {
				c.Redirect(http.StatusSeeOther, "/account/password")
				return
			}
			redirectAfterLogin(c, c.Query("next"))
			return
		}
		clearSessionCookie(c, h.secure)
	}
	h.views.login(c, http.StatusOK, loginPageData{
		pageData: pageData{Title: "Sign in", CSRF: CSRF(c)},
		Next:     safeNext(c.Query("next")),
	})
}

func (h *authHandler) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBind(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "username and password are required")
		return
	}
	result, err := h.auth.Login(c.Request.Context(), service.LoginInput{
		UserName:  req.UserName,
		Password:  req.Password,
		ClientIP:  c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.loginFailure(c, req, err)
		return
	}
	h.establish(c, result, req.Next)
}

func (h *authHandler) establish(c *gin.Context, result service.LoginResult, next string) {
	maxAge := int(time.Until(result.AbsoluteExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	setSessionCookie(c, result.Token, maxAge, h.secure)
	rotateCSRF(c, h.csrfMgr)

	if wantsHTML(c) {
		// A temporary password must be replaced before anything else is
		// reachable, so the flag overrides both next and the admin default.
		if result.User.MustChangePassword {
			c.Redirect(http.StatusSeeOther, "/account/password")
			return
		}
		redirectAfterLogin(c, next)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": sessionResponse{
		User:              toPublicUser(result.User),
		ExpiresAt:         result.ExpiresAt,
		AbsoluteExpiresAt: result.AbsoluteExpiresAt,
	}})
}

func (h *authHandler) loginFailure(c *gin.Context, req loginRequest, err error) {
	var rate *domain.RateLimitError
	if errors.As(err, &rate) {
		h.logger.Warn("login rate limited", "username", req.UserName, "client_ip", c.ClientIP())
		if wantsHTML(c) {
			seconds := int(rate.RetryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", strconv.Itoa(seconds))
			h.renderLoginError(c, http.StatusTooManyRequests, req.Next, "Too many failed attempts. Wait 15 minutes before trying again.")
			return
		}
		respondError(c, h.logger, err)
		return
	}
	if errors.Is(err, domain.ErrUnauthorized) {
		h.logger.Warn("login rejected", "username", req.UserName, "client_ip", c.ClientIP())
		if wantsHTML(c) {
			h.renderLoginError(c, http.StatusUnauthorized, req.Next, "Invalid username or password.")
			return
		}
		writeError(c, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	respondError(c, h.logger, err)
}

func (h *authHandler) renderLoginError(c *gin.Context, status int, next, message string) {
	h.views.login(c, status, loginPageData{
		pageData: pageData{Title: "Sign in", CSRF: CSRF(c), Error: message},
		Next:     safeNext(next),
	})
}

func (h *authHandler) logout(c *gin.Context) {
	token := sessionToken(c)
	if err := h.auth.Logout(c.Request.Context(), token); err != nil {
		h.logger.Error("logout failed", "request_id", GetRequestID(c), "error", err)
	}
	clearSessionCookie(c, h.secure)
	rotateCSRF(c, h.csrfMgr)
	if wantsHTML(c) {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *authHandler) session(c *gin.Context) {
	principal, ok := CurrentSession(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": sessionResponse{
		User:              toPublicUser(principal.User),
		ExpiresAt:         principal.ExpiresAt,
		AbsoluteExpiresAt: principal.AbsoluteExpiresAt,
	}})
}
