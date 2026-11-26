package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

const (
	principalKey = "principal"
)

// SetupGate enforces D-02 before any application route: while no administrator
// exists only setup, health, static assets and the CSRF bootstrap are reachable.
// Once one exists, every /setup route returns 410.
func SetupGate(setup *service.SetupService, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if exemptFromSetupGate(path) {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		state, err := setup.Status(ctx)
		cancel()
		if err != nil {
			logger.Error("setup gate probe failed", "path", path, "error", err)
			writeError(c, http.StatusInternalServerError, "internal_error", "setup state unavailable")
			return
		}
		if state == domain.SetupComplete {
			if path == "/setup" || strings.HasPrefix(path, "/setup/") {
				if wantsHTML(c) {
					writeHTMLStatus(c, http.StatusGone, "Setup complete", "An administrator account already exists; the setup wizard is permanently closed.")
				} else {
					writeError(c, http.StatusGone, "setup_closed", "setup is already complete")
				}
				return
			}
			c.Next()
			return
		}
		if state != domain.SetupPending {
			writeError(c, http.StatusInternalServerError, "internal_error", "setup state unknown")
			return
		}
		if path == "/setup" || strings.HasPrefix(path, "/setup/") || path == "/api/v1/auth/csrf" {
			c.Next()
			return
		}
		if strings.HasPrefix(path, "/api/") {
			writeError(c, http.StatusServiceUnavailable, "setup_required", "the application is in setup mode")
			return
		}
		c.Redirect(http.StatusFound, "/setup")
		c.Abort()
	}
}

func exemptFromSetupGate(path string) bool {
	return path == "/healthz" || path == "/readyz" || strings.HasPrefix(path, "/static/")
}

// RequireAuth resolves the session cookie, attaches the principal and clears
// invalid cookies. It never authorizes; RequireAdmin follows it.
func RequireAuth(auth *service.AuthService, secure bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := sessionToken(c)
		if token == "" {
			unauthorized(c)
			return
		}
		session, err := auth.Authenticate(c.Request.Context(), token)
		if err != nil {
			if errors.Is(err, domain.ErrUnauthorized) {
				clearSessionCookie(c, secure)
				unauthorized(c)
				return
			}
			_ = c.Error(err)
			writeError(c, http.StatusInternalServerError, "internal_error", "session validation failed")
			return
		}
		c.Set(principalKey, session)
		c.Next()
	}
}

// RequireRole enforces the approved role hierarchy (Member < Officer < Admin)
// on an already-authenticated request. It never authorizes a missing principal:
// RequireAuth must run first.
func RequireRole(min domain.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		session, ok := CurrentSession(c)
		if !ok {
			unauthorized(c)
			return
		}
		if !session.User.Role().AtLeast(min) {
			if wantsHTML(c) {
				writeHTMLStatus(c, http.StatusForbidden, "Forbidden",
					"You do not have permission to view this page.")
				return
			}
			writeError(c, http.StatusForbidden, "forbidden", "insufficient permissions")
			return
		}
		c.Next()
	}
}

// RequireAdmin keeps the pre-role call site working; it is RequireRole(Admin).
func RequireAdmin() gin.HandlerFunc { return RequireRole(domain.RoleAdmin) }

func unauthorized(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		writeError(c, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	next := safeNext(c.Request.URL.RequestURI())
	target := "/login"
	if next != "" && next != "/login" {
		target += "?next=" + urlQueryEscape(next)
	}
	c.Redirect(http.StatusSeeOther, target)
	c.Abort()
}

func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}

// CurrentSession returns the authenticated principal attached by RequireAuth.
func CurrentSession(c *gin.Context) (service.AuthenticatedSession, bool) {
	v, ok := c.Get(principalKey)
	if !ok {
		return service.AuthenticatedSession{}, false
	}
	session, ok := v.(service.AuthenticatedSession)
	return session, ok
}

// SecurityHeaders applies conservative defaults to every application route.
// Embedded static assets are registered before this middleware and may cache;
// application pages and APIs are never stored.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy",
			"default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; object-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'self'")
		c.Header("Cache-Control", "no-store")
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, requestBodyLimit(c))
		}
		c.Next()
	}
}

func requestBodyLimit(c *gin.Context) int64 {
	path := c.Request.URL.Path
	if path == "/api/v1/imports" && c.Request.Method == http.MethodPost {
		return 10 << 20
	}
	if strings.HasPrefix(path, "/api/v1/imports/") && strings.HasSuffix(path, "/selection") &&
		c.Request.Method == http.MethodPut {
		return 2 << 20
	}
	return 1 << 20
}
