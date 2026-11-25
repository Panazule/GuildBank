package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// adminInventoryListPath is the single destination a carried navigation-context
// value may point at. Restricting it here means a crafted link can never turn
// the context into an open redirect.
const adminInventoryListPath = "/admin/inventory"

// adminReturnToParam carries the originating inventory-list URL through the
// Issue, Return, Remove, Restore and Bulk Remove flows so the officer lands back
// on the same filtered view.
const adminReturnToParam = "returnTo"

// adminSafeReturnTo accepts only a relative /admin/inventory URL. Anything else
// (absolute or protocol-relative hosts, a different path, or a fragment) is
// rejected and yields "", so the caller falls back to the record page.
func adminSafeReturnTo(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Fragment != "" {
		return ""
	}
	if parsed.Path != adminInventoryListPath {
		return ""
	}
	if parsed.RawQuery == "" {
		return adminInventoryListPath
	}
	return adminInventoryListPath + "?" + parsed.RawQuery
}

// adminReturnTo reads the carry-through value from the query string (GET form
// pages and links).
func adminReturnTo(c *gin.Context) string {
	return adminSafeReturnTo(c.Query(adminReturnToParam))
}

// adminReturnToForm reads the carry-through value from a submitted form.
func adminReturnToForm(c *gin.Context) string {
	return adminSafeReturnTo(c.PostForm(adminReturnToParam))
}

// adminNoticeRedirect sends the officer back to the preserved list view when a
// context was carried, otherwise to the record page, always attaching one fixed
// notice code.
func adminNoticeRedirect(c *gin.Context, target, fallback, notice string) {
	if target == "" {
		c.Redirect(http.StatusSeeOther, fallback+"?notice="+notice)
		return
	}
	parsed, err := url.Parse(target)
	if err != nil {
		c.Redirect(http.StatusSeeOther, fallback+"?notice="+notice)
		return
	}
	query := parsed.Query()
	query.Set("notice", notice)
	parsed.RawQuery = query.Encode()
	c.Redirect(http.StatusSeeOther, parsed.String())
}

// adminBulkRemovedRedirect returns the officer to the preserved list view (or
// the plain list) with the bounded bulk-removal notice.
func adminBulkRemovedRedirect(c *gin.Context, target string, removed int) {
	count := strconv.Itoa(removed)
	fallback := adminInventoryListPath + "?notice=bulk-removed&count=" + count
	if target == "" {
		c.Redirect(http.StatusSeeOther, fallback)
		return
	}
	parsed, err := url.Parse(target)
	if err != nil {
		c.Redirect(http.StatusSeeOther, fallback)
		return
	}
	query := parsed.Query()
	query.Set("notice", "bulk-removed")
	query.Set("count", count)
	parsed.RawQuery = query.Encode()
	c.Redirect(http.StatusSeeOther, parsed.String())
}
