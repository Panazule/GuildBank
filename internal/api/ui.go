package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type uiHandler struct {
	views *views
}

func (h *uiHandler) adminRoot(c *gin.Context) {
	c.Redirect(http.StatusSeeOther, "/admin/settings")
}
