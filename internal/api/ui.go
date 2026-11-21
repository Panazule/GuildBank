package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

type uiHandler struct {
	auth  *service.AuthService
	views *views
}

func (h *uiHandler) home(c *gin.Context) {
	var user *domain.User
	if token := sessionToken(c); token != "" {
		if principal, err := h.auth.Authenticate(c.Request.Context(), token); err == nil {
			user = &principal.User
		}
	}
	h.views.home(c, homePageData{pageData: pageData{
		Title: "Aeternum Guild Bank",
		CSRF:  CSRF(c),
		User:  user,
	}})
}

func (h *uiHandler) adminRoot(c *gin.Context) {
	c.Redirect(http.StatusSeeOther, "/admin/settings")
}

func (h *uiHandler) errorPage(c *gin.Context, status int, message string) {
	h.views.errorPage(c, status, message)
}
