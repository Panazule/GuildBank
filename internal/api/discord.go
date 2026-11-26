package api

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
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
	auth    *service.AuthService
	state   *discordStateManager
	csrfMgr *csrfManager
	views   *views
	logger  *slog.Logger
	secure  bool
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
	state := h.state.issue(service.DiscordScopeLink, session.User.ID, session.Session.ID, "", time.Now())
	if state == "" {
		h.failPage(c, errors.New("api: discord state generation failed"))
		return
	}
	h.state.setCookie(c, discordLinkStateCookie, state)
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
	stored, err := c.Cookie(discordLinkStateCookie)
	h.state.clearCookie(c, discordLinkStateCookie)
	submitted := c.Query("state")
	_, valid := h.state.verify(submitted, service.DiscordScopeLink, session.User.ID, session.Session.ID, time.Now())
	if err != nil || stored == "" || submitted == "" || stored != submitted || !valid {
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

// loginStart begins Discord sign-in from the login page. It is public: no
// session is required and no account is ever created by the flow.
func (h *discordHandler) loginStart(c *gin.Context) {
	cfg, err := h.discord.Config(c.Request.Context())
	if err != nil {
		h.logger.Error("discord configuration read failed", "request_id", GetRequestID(c), "error", err)
		h.loginNotice(c, "discord-error", "")
		return
	}
	if !cfg.Ready() {
		h.loginNotice(c, "discord-unavailable", "")
		return
	}
	// An account that is already signed in keeps its session; there is nothing
	// to gain from a second credential round trip.
	if token := sessionToken(c); token != "" {
		if session, authErr := h.auth.Authenticate(c.Request.Context(), token); authErr == nil {
			if session.User.MustChangePassword {
				c.Redirect(http.StatusSeeOther, "/account/password")
				return
			}
			redirectAfterLogin(c, session.User, c.Query("next"))
			return
		}
		clearSessionCookie(c, h.secure)
	}

	state := h.state.issue(service.DiscordScopeLogin, 0, "", safeNext(c.Query("next")), time.Now())
	if state == "" {
		h.logger.Error("discord login state generation failed", "request_id", GetRequestID(c))
		h.loginNotice(c, "discord-error", "")
		return
	}
	h.state.setCookie(c, discordLoginStateCookie, state)
	c.Redirect(http.StatusSeeOther, h.discord.LoginAuthorizeURL(cfg, state))
}

// loginCallback completes Discord sign-in. Every outcome is a redirect to the
// login page with a fixed notice; no provider text reaches the browser.
func (h *discordHandler) loginCallback(c *gin.Context) {
	stored, err := c.Cookie(discordLoginStateCookie)
	h.state.clearCookie(c, discordLoginStateCookie)
	submitted := c.Query("state")
	parsed, valid := h.state.verify(submitted, service.DiscordScopeLogin, 0, "", time.Now())
	next := ""
	if err != nil || stored == "" || submitted == "" || stored != submitted || !valid {
		h.loginNotice(c, "discord-state", next)
		return
	}
	// The destination travels inside the signed state, so a callback cannot
	// redirect somewhere the browser did not ask for.
	next = safeNext(parsed.Next)
	if c.Query("error") != "" {
		h.loginNotice(c, "discord-cancelled", next)
		return
	}
	code := c.Query("code")
	if code == "" {
		h.loginNotice(c, "discord-state", next)
		return
	}

	result, err := h.discord.Login(c.Request.Context(), code, c.ClientIP(),
		c.Request.UserAgent(), GetRequestID(c))
	if err != nil {
		var rate *domain.RateLimitError
		var stateErr *domain.AccountStateError
		switch {
		case errors.As(err, &rate):
			h.logger.Warn("discord login rate limited", "client_ip", c.ClientIP())
			h.loginNotice(c, "discord-rate", next)
		case errors.As(err, &stateErr):
			// The account exists and the identity is linked; only its state
			// blocks sign-in. This case must come before the ErrUnauthorized
			// branch, which AccountStateError also satisfies (D-SR-14).
			h.logger.Warn("discord login refused", "client_ip", c.ClientIP(),
				"status", stateErr.Status.String(), "disabled", stateErr.Disabled)
			if stateErr.Disabled {
				h.loginNotice(c, "discord-disabled", next)
			} else {
				h.loginNotice(c, "discord-pending", next)
			}
		case errors.Is(err, domain.ErrUnauthorized):
			// Neutral: an unknown or unlinked Discord account is identical to
			// the caller, and nothing was created or changed.
			h.logger.Warn("discord login rejected", "client_ip", c.ClientIP())
			h.loginNotice(c, "discord-unlinked", next)
		case errors.Is(err, domain.ErrUpstream):
			h.loginNotice(c, "discord-provider", next)
		case errors.Is(err, domain.ErrUnavailable):
			h.loginNotice(c, "discord-unavailable", next)
		default:
			h.logger.Error("discord login failed", "request_id", GetRequestID(c), "error", err)
			h.loginNotice(c, "discord-error", next)
		}
		return
	}
	h.completeLogin(c, result, next)
}

// completeLogin mirrors authHandler.establish for a browser redirect: the same
// session cookie, the same CSRF rotation and the same MustChangePassword
// precedence over the requested destination.
func (h *discordHandler) completeLogin(c *gin.Context, result service.LoginResult, next string) {
	maxAge := int(time.Until(result.AbsoluteExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	setSessionCookie(c, result.Token, maxAge, h.secure)
	rotateCSRF(c, h.csrfMgr)
	h.logger.Info("discord sign-in", "user_id", result.User.ID, "request_id", GetRequestID(c))
	if result.User.MustChangePassword {
		c.Redirect(http.StatusSeeOther, "/account/password")
		return
	}
	redirectAfterLogin(c, result.User, next)
}

func (h *discordHandler) loginNotice(c *gin.Context, code, next string) {
	target := "/login?notice=" + code
	if next = safeNext(next); next != "" {
		target += "&next=" + url.QueryEscape(next)
	}
	c.Redirect(http.StatusSeeOther, target)
}

// discordLoginNotice turns a notice code into fixed text for the login page.
func discordLoginNotice(code string) string {
	switch code {
	case "discord-unlinked":
		return "No GuildBank account is linked to that Discord account. Sign in with your username and password, then link Discord from your profile."
	case "discord-pending":
		return "Your GuildBank account exists but is waiting for an administrator to approve it. You can sign in once it has been approved."
	case "discord-disabled":
		return "Your GuildBank account is disabled. Contact an administrator."
	case "discord-cancelled":
		return "Discord sign-in was cancelled."
	case "discord-state":
		return "The Discord sign-in expired or did not belong to this browser. Try again."
	case "discord-provider":
		return "Discord could not be reached. Try again shortly."
	case "discord-unavailable":
		return "Discord sign-in is not enabled."
	case "discord-rate":
		return "Too many Discord sign-in attempts. Wait a few minutes before trying again."
	case "discord-error":
		return "Discord sign-in could not be completed."
	}
	return ""
}
