package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

type setupHandler struct {
	setup  *service.SetupService
	auth   *authHandler
	views  *views
	logger *slog.Logger
}

func (h *setupHandler) page(c *gin.Context) {
	form := h.setup.DefaultSetupInput()
	h.render(c, http.StatusOK, form, nil, "", "")
}

func (h *setupHandler) validateAdmin(c *gin.Context) {
	h.validateStep(c, h.setup.ValidateAdmin)
}

func (h *setupHandler) validateGuild(c *gin.Context) {
	h.validateStep(c, h.setup.ValidateGuild)
}

func (h *setupHandler) validateStep(c *gin.Context, validate func(service.SetupInput) error) {
	form, ok := h.bind(c)
	if !ok {
		return
	}
	if err := validate(form); err != nil {
		respondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": true}})
}

func (h *setupHandler) testDatabase(c *gin.Context) {
	form, ok := h.bind(c)
	if !ok {
		return
	}
	if err := h.setup.TestDatabase(c.Request.Context(), form); err != nil {
		var validation *domain.ValidationError
		if errors.As(err, &validation) {
			respondError(c, h.logger, err)
			return
		}
		h.logger.Warn("setup database test failed", "request_id", GetRequestID(c), "error", err)
		c.JSON(http.StatusServiceUnavailable, errorEnvelope{Error: errorBody{
			Code:      "database_unavailable",
			Message:   "The MariaDB connection could not be verified. Check the host, port, database and credentials.",
			RequestID: GetRequestID(c),
		}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": true}})
}

func (h *setupHandler) complete(c *gin.Context) {
	form, ok := h.bind(c)
	if !ok {
		return
	}
	user, err := h.setup.Complete(c.Request.Context(), form)
	if err != nil {
		h.completeFailure(c, form, err)
		return
	}
	result, err := h.auth.auth.IssueSession(c.Request.Context(), user, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.logger.Error("setup session creation failed", "request_id", GetRequestID(c), "error", err)
		respondError(c, h.logger, err)
		return
	}
	h.logger.Info("setup completed", "user_id", user.ID, "user_name", user.UserName, "request_id", GetRequestID(c))
	h.auth.establish(c, result, "/")
}

func (h *setupHandler) completeFailure(c *gin.Context, form service.SetupInput, err error) {
	var validation *domain.ValidationError
	if errors.As(err, &validation) {
		if wantsHTML(c) {
			scrubPasswords(&form)
			h.render(c, http.StatusUnprocessableEntity, form, validation.Fields, "Check the highlighted fields.", "")
			return
		}
		respondError(c, h.logger, err)
		return
	}
	if errors.Is(err, domain.ErrUnavailable) {
		h.logger.Error("setup database validation failed", "request_id", GetRequestID(c), "error", err)
		if wantsHTML(c) {
			scrubPasswords(&form)
			h.render(c, http.StatusServiceUnavailable, form, nil, "The MariaDB connection could not be verified. No administrator was created.", "")
			return
		}
		respondError(c, h.logger, err)
		return
	}
	if errors.Is(err, domain.ErrSetupClosed) {
		if wantsHTML(c) {
			writeHTMLStatus(c, http.StatusGone, "Setup complete", "An administrator already exists; refresh and sign in.")
			return
		}
		respondError(c, h.logger, err)
		return
	}
	respondError(c, h.logger, err)
}

func (h *setupHandler) bind(c *gin.Context) (service.SetupInput, bool) {
	var form service.SetupInput
	if err := c.ShouldBind(&form); err != nil {
		writeErrorFields(c, http.StatusBadRequest, "invalid_request", "the setup form could not be read", nil)
		return form, false
	}
	return form, true
}

func (h *setupHandler) render(c *gin.Context, status int, form service.SetupInput, fields map[string]string, message, notice string) {
	h.views.setup(c, status, setupPageData{
		pageData: pageData{Title: "Setup", CSRF: CSRF(c), Error: message, Notice: notice, ErrorFields: fields},
		Form:     form,
	})
}

func scrubPasswords(form *service.SetupInput) {
	form.Password = ""
	form.ConfirmPassword = ""
	form.MariaDBPassword = ""
}
