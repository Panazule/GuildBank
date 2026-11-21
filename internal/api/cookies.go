package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	sessionCookieName = "guildbank_session"
	sessionContextKey = "auth_session"
)

func setSessionCookie(c *gin.Context, token string, maxAge int, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, token, maxAge, "/", "", secure, true)
}

func clearSessionCookie(c *gin.Context, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookieName, "", -1, "/", "", secure, true)
}

func sessionToken(c *gin.Context) string {
	token, err := c.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return token
}

func wantsHTML(c *gin.Context) bool {
	return strings.Contains(strings.ToLower(c.GetHeader("Accept")), "text/html")
}

// safeNext accepts only a local absolute path. It prevents open redirects via
// login?next=https://evil.example or protocol-relative URLs.
func safeNext(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return ""
	}
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" {
		return ""
	}
	return u.RequestURI()
}

func redirectAfterLogin(c *gin.Context, next string) {
	if target := safeNext(next); target != "" {
		c.Redirect(http.StatusSeeOther, target)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/settings")
}

func writeHTMLStatus(c *gin.Context, status int, title, message string) {
	c.Abort()
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(status, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>%s</title></head><body><h1>%s</h1><p>%s</p></body></html>`,
		templateEscape(title), templateEscape(title), templateEscape(message))
}

func templateEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;")
	return r.Replace(s)
}
