package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

// discordHandler serves account linking only. There is deliberately no Discord
// login route: a callback always attaches to an authenticated application
// session, so a Discord identity can never authenticate by itself.
type discordHandler struct {
	discord *service.DiscordService
	state   *discordStateManager
	views   *views
	logger  *slog.Logger
}

type discordIdentityResponse struct {
	Linked   bool       `json:"linked"`
	UserID   string     `json:"discordUserId,omitempty"`
	UserName string     `json:"discordUserName,omitempty"`
	LinkedAt *time.Time `json:"linkedAt,omitempty"`
}

func discordResponseOf(identity domain.DiscordIdentity) discordIdentityResponse {
	out := discordIdentityResponse{Linked: identity.Linked()}
	if identity.Linked() {
		out.UserID = identity.DiscordUserID
		out.UserName = identity.DiscordUserName
		out.LinkedAt = identity.LinkedAt
	}
	return out
}

// start begins the consent flow. The state value is bound to this account and
// session, then kept in a SameSite cookie so the callback can prove that the
// consent was requested here.
func (h *discordHandler) start(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	cfg, err := h.discord.Config(c.Request.Context())
	if err != nil {
		h.failPage(c, err)
		return
	}
	if !cfg.Ready() {
		h.unavailable(c)
		return
	}
	state := h.state.issue(session.User.ID, session.Session.ID, time.Now())
	if state == "" {
		h.failPage(c, errors.New("api: discord state generation failed"))
		return
	}
	h.state.setCookie(c, state)
	target := h.discord.AuthorizeURL(cfg, state)
	if wantsHTML(c) {
		c.Redirect(http.StatusSeeOther, target)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"authorizeUrl": target}})
}

// callback completes the flow. Every outcome is a redirect to the profile page
// with a fixed notice code, so no provider text reaches the page.
func (h *discordHandler) callback(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	stored, err := c.Cookie(discordStateCookieName)
	h.state.clearCookie(c)
	submitted := c.Query("state")
	if err != nil || stored == "" || submitted == "" || stored != submitted ||
		!h.state.verify(submitted, session.User.ID, session.Session.ID, time.Now()) {
		h.redirectNotice(c, "discord-state")
		return
	}
	if c.Query("error") != "" {
		h.redirectNotice(c, "discord-denied")
		return
	}
	code := c.Query("code")
	if code == "" {
		h.redirectNotice(c, "discord-state")
		return
	}
	switch _, err := h.discord.Link(c.Request.Context(), session.User, code,
		GetRequestID(c), c.ClientIP()); {
	case err == nil:
		h.logger.Info("discord identity linked", "user_id", session.User.ID, "request_id", GetRequestID(c))
		h.redirectNotice(c, "discord-linked")
	case errors.Is(err, domain.ErrAlreadyExists):
		h.redirectNotice(c, "discord-taken")
	case errors.Is(err, domain.ErrUpstream):
		h.redirectNotice(c, "discord-provider")
	case errors.Is(err, domain.ErrUnavailable):
		h.redirectNotice(c, "discord-unavailable")
	default:
		h.logger.Error("discord link failed", "user_id", session.User.ID,
			"request_id", GetRequestID(c), "error", err)
		h.redirectNotice(c, "discord-error")
	}
}

// unlink removes the identity from the signed-in account.
func (h *discordHandler) unlink(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if err := h.discord.Unlink(c.Request.Context(), session.User, GetRequestID(c), c.ClientIP()); err != nil {
		switch {
		case errors.Is(err, domain.ErrNotFound):
			writeError(c, http.StatusNotFound, "not_found", "no Discord account is linked")
		case errors.Is(err, domain.ErrUnavailable):
			writeError(c, http.StatusServiceUnavailable, "discord_unavailable", "Discord linking is not configured")
		default:
			respondError(c, h.logger, err)
		}
		return
	}
	c.Status(http.StatusNoContent)
}

// unlinkForm is the HTML adapter for DELETE, which a form cannot issue.
func (h *discordHandler) unlinkForm(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	if err := h.discord.Unlink(c.Request.Context(), session.User, GetRequestID(c), c.ClientIP()); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			h.redirectNotice(c, "discord-not-linked")
			return
		}
		h.logger.Error("discord unlink failed", "user_id", session.User.ID,
			"request_id", GetRequestID(c), "error", err)
		h.redirectNotice(c, "discord-error")
		return
	}
	h.redirectNotice(c, "discord-unlinked")
}

// identity reports the linked identity for API clients.
func (h *discordHandler) identity(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	identity, err := h.discord.Identity(c.Request.Context(), session.User.ID)
	if err != nil {
		if errors.Is(err, domain.ErrUnavailable) {
			writeError(c, http.StatusServiceUnavailable, "discord_unavailable", "Discord linking is not configured")
			return
		}
		respondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"discord": discordResponseOf(identity)}})
}

func (h *discordHandler) redirectNotice(c *gin.Context, code string) {
	c.Redirect(http.StatusSeeOther, "/account/profile?notice="+code)
}

func (h *discordHandler) unavailable(c *gin.Context) {
	if wantsHTML(c) {
		h.redirectNotice(c, "discord-unavailable")
		return
	}
	writeError(c, http.StatusServiceUnavailable, "discord_unavailable",
		"Discord linking is not enabled. An administrator must configure it in Settings.")
}

func (h *discordHandler) failPage(c *gin.Context, err error) {
	h.logger.Error("discord page failed", "request_id", GetRequestID(c), "error", err)
	if wantsHTML(c) {
		h.views.errorPage(c, http.StatusInternalServerError, "The Discord request could not be started.")
		return
	}
	writeError(c, http.StatusInternalServerError, "internal_error", "the request could not be completed")
}
