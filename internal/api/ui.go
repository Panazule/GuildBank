package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/service"
)

// adminHomeCountTimeout bounds the two optional landing-page count reads so a
// slow or unavailable store cannot hold up the operational navigation page.
const adminHomeCountTimeout = 5 * time.Second

type uiHandler struct {
	admin *service.AdminInventoryService
	users *service.UserAdminService
	views *views
}

// adminHome is the single administration entry point. It groups every officer
// and administrator workflow behind one navigation link while keeping the
// individual routes and their authorization unchanged. Counts are advisory: a
// failed read leaves the link usable and says the count is unavailable.
func (h *uiHandler) adminHome(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	data := adminHomePageData{
		pageData: pageData{Title: "Administration", CSRF: CSRF(c), User: &session.User},
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminHomeCountTimeout)
	defer cancel()

	if h.admin != nil {
		if count, err := h.admin.PendingRequestCount(ctx); err == nil {
			data.PendingRequests = count
			data.RequestCountKnown = true
			// The shared navigation badge reuses the request count when one is
			// available, matching the queue and decision pages.
			data.pageData.PendingRequestCount = count
		}
	}
	if h.users != nil && session.User.CanManageUsers() {
		if count, err := h.users.PendingCount(ctx); err == nil {
			data.PendingAccounts = count
			data.AccountCountKnown = true
		}
	}

	h.views.adminHome(c, http.StatusOK, data)
}
