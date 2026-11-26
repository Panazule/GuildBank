package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type uiHandler struct {
	views *views
}

// adminRoot sends each signed-in account to the administration area it may use:
// Administrators land on Settings, Officers on the inventory workspace.
func (h *uiHandler) adminRoot(c *gin.Context) {
	if session, ok := CurrentSession(c); ok && session.User.CanManageSettings() {
		c.Redirect(http.StatusSeeOther, "/admin/settings")
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/inventory")
}
