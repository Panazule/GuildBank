package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

// registrationHandler serves public self-registration. Every flow is a plain
// GET navigation until the final same-origin POST: CSP form-action 'self'
// cancels a form submission's cross-origin redirect chain.
type registrationHandler struct {
	registration *service.RegistrationService
	auth         *service.AuthService
	state        *discordStateManager
	ticketSigner *registrationTicketSigner
	csrfMgr      *csrfManager
	views        *views
	logger       *slog.Logger
	secure       bool
}

// registrationForm is the only visitor-supplied field. accessLevel,
// accountStatus and origin are server-set constants.
type registrationForm struct {
	UserName string `json:"userName" form:"userName"`
}

// page is the landing page. It renders even when registration is unavailable, so
// the link never 404s, and it states which approval outcome will apply before the
// visitor consents.
func (h *registrationHandler) page(c *gin.Context) {
	available, cfg, err := h.availability(c)
	if err != nil {
		h.logger.Error("registration availability read failed", "request_id", GetRequestID(c), "error", err)
	}
	h.views.register(c, http.StatusOK, registerPageData{
		pageData: pageData{Title: "Create account", CSRF: CSRF(c), User: currentUserPointer(c),
			Notice: registrationNotice(c.Query("notice"))},
		Available:       available,
		GuildConfigured: h.registration.GuildConfigured(cfg),
	})
}

// availability reports whether the flow may run and returns the current Discord
// configuration for the explanatory text.
func (h *registrationHandler) availability(c *gin.Context) (bool, service.DiscordConfig, error) {
	cfg, err := h.registration.Config(c.Request.Context())
	if err != nil {
		return false, service.DiscordConfig{}, err
	}
	available, err := h.registration.Available(c.Request.Context())
	if err != nil {
		return false, cfg, err
	}
	return available, cfg, nil
}

// start mints a registration state and redirects to Discord. It is the only
// caller of the registration consent URL, so the wider `guilds` scope cannot leak
// into linking or sign-in (D-SR-13).
func (h *registrationHandler) start(c *gin.Context) {
	available, cfg, err := h.availability(c)
	if err != nil {
		h.logger.Error("registration availability read failed", "request_id", GetRequestID(c), "error", err)
		h.redirectLogin(c, "registration-provider")
		return
	}
	if !available {
		h.redirectLogin(c, "registration-unavailable")
		return
	}
	state := h.state.issue(service.DiscordStateRegister, 0, "", "", time.Now())
	if state == "" {
		h.logger.Error("registration state generation failed", "request_id", GetRequestID(c))
		h.redirectLogin(c, "registration-error")
		return
	}
	h.state.setCookie(c, discordRegisterStateCookie, state)
	c.Redirect(http.StatusSeeOther, h.registration.AuthorizeURL(cfg, state))
}

func (h *registrationHandler) redirectLogin(c *gin.Context, code string) {
	c.Redirect(http.StatusSeeOther, "/login?notice="+code)
}

// callback verifies the state, exchanges the code, checks the guild and issues
// the signed ticket. Every failure is a redirect to a fixed notice code; no
// provider text reaches the browser.
func (h *registrationHandler) callback(c *gin.Context) {
	stored, err := c.Cookie(discordRegisterStateCookie)
	h.state.clearCookie(c, discordRegisterStateCookie)
	submitted := c.Query("state")
	_, valid := h.state.verify(submitted, service.DiscordStateRegister, 0, "", time.Now())
	if err != nil || stored == "" || submitted == "" || stored != submitted || !valid {
		h.redirectLogin(c, "registration-state")
		return
	}
	if c.Query("error") != "" {
		h.redirectLogin(c, "registration-cancelled")
		return
	}
	code := c.Query("code")
	if code == "" {
		h.redirectLogin(c, "registration-state")
		return
	}

	ticket, err := h.registration.Verify(c.Request.Context(), code, GetRequestID(c))
	if err != nil {
		h.callbackFailure(c, err)
		return
	}
	token := h.ticketSigner.issue(ticket)
	if token == "" {
		h.logger.Error("registration ticket generation failed", "request_id", GetRequestID(c))
		h.redirectLogin(c, "registration-error")
		return
	}
	h.ticketSigner.setCookie(c, token)
	c.Redirect(http.StatusSeeOther, "/register/complete")
}

// callbackFailure maps a verification error onto a fixed login notice.
func (h *registrationHandler) callbackFailure(c *gin.Context, err error) {
	var claimed *service.RegistrationClaimedError
	var rate *domain.RateLimitError
	switch {
	case errors.As(err, &claimed):
		if claimed.Pending {
			h.redirectLogin(c, "registration-pending")
			return
		}
		h.redirectLogin(c, "registration-exists")
	case errors.As(err, &rate):
		h.redirectLogin(c, "registration-rate")
	case errors.Is(err, domain.ErrUpstream):
		h.logger.Warn("discord registration provider failure", "request_id", GetRequestID(c))
		h.redirectLogin(c, "registration-provider")
	case errors.Is(err, domain.ErrUnavailable):
		h.redirectLogin(c, "registration-unavailable")
	default:
		h.logger.Error("discord registration failed", "request_id", GetRequestID(c), "error", err)
		h.redirectLogin(c, "registration-error")
	}
}

// form renders the username page from a valid ticket. The Discord identity is
// shown for confirmation and travels only inside the signed cookie.
func (h *registrationHandler) form(c *gin.Context) {
	ticket, ok := h.ticketSigner.ticket(c, time.Now())
	if !ok {
		h.ticketSigner.clearCookie(c)
		c.Redirect(http.StatusSeeOther, "/register?notice=registration-expired")
		return
	}
	h.renderForm(c, http.StatusOK, ticket, registrationForm{}, "", nil)
}

func (h *registrationHandler) renderForm(c *gin.Context, status int, ticket domain.RegistrationTicket,
	form registrationForm, message string, fields map[string]string) {
	h.views.registerComplete(c, status, registerCompletePageData{
		pageData: pageData{
			Title: "Choose a username", CSRF: CSRF(c), User: currentUserPointer(c),
			Error: message, ErrorFields: fields,
		},
		Form:            form,
		DiscordName:     ticket.DiscordUserName,
		Member:          ticket.Approved(),
		GuildConfigured: ticket.GuildCheck != domain.GuildCheckNotConfigured,
	})
}

// registrationNotice maps a machine-written query code onto fixed text. Nothing
// from the query string is reflected.
func registrationNotice(code string) string {
	switch code {
	case "registration-expired":
		return "That registration expired. Start again from the Discord consent."
	case "registration-unavailable":
		return "Self-registration is not enabled. Ask an officer for an account."
	}
	return ""
}

// complete creates the account. A session is issued only for an account that
// reports IsEnabled(), which is the same predicate every other gate uses.
func (h *registrationHandler) complete(c *gin.Context) {
	ticket, ok := h.ticketSigner.ticket(c, time.Now())
	if !ok {
		h.ticketSigner.clearCookie(c)
		c.Redirect(http.StatusSeeOther, "/register?notice=registration-expired")
		return
	}
	var form registrationForm
	if rerr := bindUserWrite(c, &form); rerr != nil {
		h.renderForm(c, rerr.status, ticket, form, rerr.message, rerr.fields)
		return
	}

	created, err := h.registration.Complete(c.Request.Context(), ticket, form.UserName,
		GetRequestID(c), c.ClientIP())
	if err != nil {
		h.completeFailure(c, ticket, form, err)
		return
	}

	h.ticketSigner.clearCookie(c)
	h.logger.Info("account self-registered", "user_id", created.ID,
		"account_status", string(created.Status()), "request_id", GetRequestID(c))

	if !created.IsEnabled() {
		// Pending: no session is created at all, so one rule decides.
		h.redirectLogin(c, "registration-pending")
		return
	}
	result, err := h.auth.IssueSession(c.Request.Context(), created, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.logger.Error("registration session issuance failed", "user_id", created.ID,
			"request_id", GetRequestID(c), "error", err)
		h.redirectLogin(c, "registration-error")
		return
	}
	h.establish(c, result)
}

// establish installs the session cookie and rotates the CSRF token, mirroring
// authHandler.establish for the approved registration path.
func (h *registrationHandler) establish(c *gin.Context, result service.LoginResult) {
	maxAge := int(time.Until(result.AbsoluteExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	setSessionCookie(c, result.Token, maxAge, h.secure)
	rotateCSRF(c, h.csrfMgr)
	c.Redirect(http.StatusSeeOther, "/")
}

func (h *registrationHandler) completeFailure(c *gin.Context, ticket domain.RegistrationTicket,
	form registrationForm, err error) {
	var claimed *service.RegistrationClaimedError
	var validation *domain.ValidationError
	var rate *domain.RateLimitError
	switch {
	case errors.As(err, &claimed):
		h.ticketSigner.clearCookie(c)
		if claimed.Pending {
			h.redirectLogin(c, "registration-pending")
			return
		}
		h.redirectLogin(c, "registration-exists")
	case errors.As(err, &rate):
		seconds := int(rate.RetryAfter.Seconds())
		if seconds < 1 {
			seconds = 1
		}
		c.Header("Retry-After", strconv.Itoa(seconds))
		h.renderForm(c, http.StatusTooManyRequests, ticket, form,
			"That username could not be saved. Wait a few minutes before trying again.", nil)
	case errors.Is(err, domain.ErrAlreadyExists):
		// The ticket stays valid so the visitor can simply pick another name.
		h.renderForm(c, http.StatusConflict, ticket, form, "Check the highlighted field.",
			map[string]string{"userName": "that username is already in use"})
	case errors.As(err, &validation):
		h.renderForm(c, http.StatusUnprocessableEntity, ticket, form,
			"Check the highlighted fields.", validation.Fields)
	case errors.Is(err, domain.ErrUnavailable):
		h.ticketSigner.clearCookie(c)
		h.redirectLogin(c, "registration-unavailable")
	case errors.Is(err, domain.ErrUnauthorized):
		h.ticketSigner.clearCookie(c)
		h.redirectLogin(c, "registration-state")
	default:
		h.logger.Error("registration completion failed", "request_id", GetRequestID(c), "error", err)
		h.renderForm(c, http.StatusInternalServerError, ticket, form,
			"The account could not be created.", nil)
	}
}
