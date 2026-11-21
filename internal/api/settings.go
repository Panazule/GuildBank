package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

type settingsHandler struct {
	settings *service.SettingsService
	views    *views
	logger   *slog.Logger
}

type settingResponse struct {
	Key      string             `json:"key"`
	Value    string             `json:"value"`
	Type     domain.SettingType `json:"type"`
	IsSecret bool               `json:"isSecret"`
}

type settingsPayload struct {
	Settings map[string]string `json:"settings"`
}

func (h *settingsHandler) page(c *gin.Context) {
	rows, err := h.settings.List(c.Request.Context())
	if err != nil {
		respondError(c, h.logger, err)
		return
	}
	notice := ""
	if c.Query("saved") == "1" {
		notice = "Settings saved."
	}
	h.render(c, http.StatusOK, rows, nil, "", notice, formValues(rows))
}

func (h *settingsHandler) get(c *gin.Context) {
	rows, err := h.settings.List(c.Request.Context())
	if err != nil {
		respondError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": redactedSettings(rows)})
}

func (h *settingsHandler) update(c *gin.Context) {
	updates, ok := h.bindUpdates(c)
	if !ok {
		return
	}
	principal, _ := CurrentSession(c)
	rows, err := h.settings.Update(c.Request.Context(), updates, principal.User.UserName)
	if err != nil {
		var validation *domain.ValidationError
		if errors.As(err, &validation) {
			if wantsHTML(c) {
				form := formValues(rowsOrCurrent(c, h.settings, updates))
				h.render(c, http.StatusUnprocessableEntity, rows, validation.Fields, "Check the highlighted settings.", "", form)
				return
			}
			respondError(c, h.logger, err)
			return
		}
		respondError(c, h.logger, err)
		return
	}
	h.logger.Info("settings updated", "user_id", principal.User.ID, "request_id", GetRequestID(c))
	if wantsHTML(c) {
		c.Redirect(http.StatusSeeOther, "/admin/settings?saved=1")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": redactedSettings(rows)})
}

func (h *settingsHandler) bindUpdates(c *gin.Context) (map[string]string, bool) {
	contentType := c.GetHeader("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var payload settingsPayload
		if err := c.ShouldBindJSON(&payload); err != nil || payload.Settings == nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "settings object is required")
			return nil, false
		}
		return payload.Settings, true
	}
	if err := c.Request.ParseForm(); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "the settings form could not be read")
		return nil, false
	}
	updates := make(map[string]string, len(service.SettingDefinitions))
	for _, def := range service.SettingDefinitions {
		if values, ok := c.Request.PostForm[def.Key]; ok {
			updates[def.Key] = values[0]
		}
	}
	return updates, true
}

func (h *settingsHandler) render(c *gin.Context, status int, rows []domain.Setting, fields map[string]string, message, notice string, form map[string]string) {
	h.views.settings(c, status, settingsPageData{
		pageData: pageData{
			Title:       "Settings",
			CSRF:        CSRF(c),
			User:        currentUserPointer(c),
			Error:       message,
			Notice:      notice,
			ErrorFields: fields,
		},
		Settings: settingViews(rows),
		Form:     form,
	})
}

func settingViews(rows []domain.Setting) []settingView {
	out := make([]settingView, 0, len(service.SettingDefinitions))
	for _, def := range service.SettingDefinitions {
		value := ""
		configured := false
		for _, row := range rows {
			if row.Key == def.Key {
				value = row.Value
				configured = value != ""
				break
			}
		}
		if def.Secret {
			value = ""
		}
		out = append(out, settingView{
			Key: def.Key, Label: def.Label, Value: value, Type: def.Type,
			Secret: def.Secret, Configured: configured, Help: def.Help,
		})
	}
	return out
}

func redactedSettings(rows []domain.Setting) []settingResponse {
	out := make([]settingResponse, 0, len(rows))
	for _, row := range rows {
		value := row.Value
		if row.IsSecret {
			value = ""
		}
		out = append(out, settingResponse{Key: row.Key, Value: value, Type: row.Type, IsSecret: row.IsSecret})
	}
	return out
}

func formValues(rows []domain.Setting) map[string]string {
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		if !row.IsSecret {
			out[row.Key] = row.Value
		}
	}
	return out
}

func rowsOrCurrent(c *gin.Context, s *service.SettingsService, updates map[string]string) []domain.Setting {
	rows, err := s.List(c.Request.Context())
	if err != nil {
		return nil
	}
	for key, value := range updates {
		if def, ok := service.Definition(key); ok && def.Secret {
			continue
		}
		found := false
		for i := range rows {
			if rows[i].Key == key {
				rows[i].Value = value
				found = true
				break
			}
		}
		if !found {
			rows = append(rows, domain.Setting{Key: key, Value: value})
		}
	}
	return rows
}

func currentUserPointer(c *gin.Context) *domain.User {
	principal, ok := CurrentSession(c)
	if !ok {
		return nil
	}
	return &principal.User
}
