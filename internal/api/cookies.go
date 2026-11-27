package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
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

// redirectAfterLogin sends the account to the page it asked for, or to the
// inventory landing page. Inventory is the application's home for every role;
// no administration page is ever the default. Callers must send a flagged
// account to /account/password first: this helper never inspects a session,
// because login runs before the session middleware.
func redirectAfterLogin(c *gin.Context, user domain.User, next string) {
	if target := authorizedNext(user, next); target != "" {
		c.Redirect(http.StatusSeeOther, target)
		return
	}
	c.Redirect(http.StatusSeeOther, "/")
}

// authorizedNext returns the stored destination when it is safe and available to
// this role, and "" otherwise. A destination that the role cannot use therefore
// lands on the inventory page rather than on a Forbidden page.
func authorizedNext(user domain.User, next string) string {
	target := safeNext(next)
	if target == "" {
		return ""
	}
	if required, restricted := minimumRoleForPage(target); restricted && !user.Role().AtLeast(required) {
		return ""
	}
	return target
}

// minimumRoleForPage mirrors the route groups for the destinations a login can
// carry. Only page prefixes are listed: an unauthenticated API call answers 401
// without a `next` value.
func minimumRoleForPage(target string) (domain.Role, bool) {
	parsed, err := url.Parse(target)
	if err != nil {
		return "", false
	}
	path := parsed.Path
	switch {
	case pageWithin(path, "/admin/settings"):
		return domain.RoleAdmin, true
	case path == "/admin", pageWithin(path, "/admin/inventory"),
		pageWithin(path, "/admin/imports"), pageWithin(path, "/admin/audit"),
		pageWithin(path, "/admin/users"):
		return domain.RoleOfficer, true
	}
	return "", false
}

// pageWithin reports whether path is root or one of its descendants.
func pageWithin(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
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
