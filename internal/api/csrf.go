package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	csrfContextKey = "csrf_token"
	csrfCookieName = "guildbank_csrf"
	csrfFieldName  = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
	csrfMaxAge     = 24 * 60 * 60
)

// csrfManager implements signed double-submit cookies. The cookie is HttpOnly;
// templates and the small setup script receive the same signed token. The HMAC
// secret is process-local, so a restart invalidates outstanding forms (safe).
type csrfManager struct {
	secret []byte
	secure bool
}

func newCSRFManager(secure bool) (*csrfManager, error) {
	secret := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		return nil, fmt.Errorf("api: csrf secret: %w", err)
	}
	return &csrfManager{secret: secret, secure: secure}, nil
}

func (m *csrfManager) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := m.readCookie(c)
		if !m.valid(token) {
			token = m.newToken()
			m.setCookie(c, token)
		}
		c.Set(csrfContextKey, token)

		if isSafeMethod(c.Request.Method) {
			c.Next()
			return
		}
		submitted := m.submittedToken(c)
		if submitted == "" || subtle.ConstantTimeCompare([]byte(token), []byte(submitted)) != 1 || !m.valid(submitted) {
			if wantsHTML(c) {
				writeHTMLStatus(c, http.StatusForbidden, "Request rejected", "The form security token is missing or expired. Reload the page and try again.")
			} else {
				writeError(c, http.StatusForbidden, "csrf_invalid", "CSRF token is missing or invalid")
			}
			return
		}
		c.Next()
	}
}

func (m *csrfManager) newToken() string {
	nonce := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		// crypto/rand failure is fatal for CSRF security; an empty token is
		// rejected by valid() and every unsafe request fails closed.
		return ""
	}
	return encodeToken(nonce) + "." + encodeToken(m.sign(nonce))
}

func (m *csrfManager) valid(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	nonce, err1 := base64.RawURLEncoding.DecodeString(parts[0])
	sig, err2 := base64.RawURLEncoding.DecodeString(parts[1])
	if err1 != nil || err2 != nil || len(nonce) != 32 {
		return false
	}
	return hmac.Equal(sig, m.sign(nonce))
}

func (m *csrfManager) sign(nonce []byte) []byte {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write(nonce)
	return mac.Sum(nil)
}

func (m *csrfManager) readCookie(c *gin.Context) string {
	value, err := c.Cookie(csrfCookieName)
	if err != nil {
		return ""
	}
	return value
}

func (m *csrfManager) setCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(csrfCookieName, token, csrfMaxAge, "/", "", m.secure, true)
}

func (m *csrfManager) submittedToken(c *gin.Context) string {
	if value := strings.TrimSpace(c.GetHeader(csrfHeaderName)); value != "" {
		return value
	}
	contentType := c.GetHeader("Content-Type")
	if strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
		if err != nil {
			return ""
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return ""
		}
		return values.Get(csrfFieldName)
	}
	return ""
}

func encodeToken(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// CSRF returns the signed token for the current request.
func CSRF(c *gin.Context) string {
	if v, ok := c.Get(csrfContextKey); ok {
		if token, ok := v.(string); ok {
			return token
		}
	}
	return ""
}

// rotateCSRF issues a fresh token after authentication state changes.
func rotateCSRF(c *gin.Context, manager *csrfManager) {
	token := manager.newToken()
	manager.setCookie(c, token)
	c.Set(csrfContextKey, token)
}
