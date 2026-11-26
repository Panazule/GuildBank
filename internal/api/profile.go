package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

// profileHandler serves the signed-in account's own profile. Every query is
// scoped to the session user; no handler accepts a user ID.
type profileHandler struct {
	characters *service.CharacterService
	discord    *service.DiscordService
	views      *views
	logger     *slog.Logger
}

type characterRequest struct {
	Name string `json:"characterName" form:"characterName"`
}

type characterResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

func characterResponseOf(c domain.Character) characterResponse {
	return characterResponse{ID: c.ID, Name: c.Name, CreatedAt: c.CreatedAt.UTC()}
}

func characterViews(characters []domain.Character) []characterView {
	out := make([]characterView, 0, len(characters))
	for _, character := range characters {
		out = append(out, characterView{
			ID: character.ID, Name: character.Name,
			CreatedText: character.CreatedAt.UTC().Format("2006-01-02"),
		})
	}
	return out
}

func (h *profileHandler) page(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	characters, err := h.characters.List(c.Request.Context(), session.User.ID)
	if err != nil {
		h.pageError(c, err)
		return
	}
	data := profilePageData{
		pageData: pageData{
			Title: "Profile", CSRF: CSRF(c), User: &session.User,
		},
		UserName:      session.User.UserName,
		RoleLabel:     userRoleLabel(session.User.Role()),
		Characters:    characterViews(characters),
		MaxCharacters: domain.CharacterMaxPerUser,
		Discord:       h.discordSection(c, session.User.ID),
	}
	data.Notice = profileNotice(c.Query("notice"))
	h.views.accountProfile(c, http.StatusOK, data)
}

// discordSection reads the linking state for the profile card. Every failure is
// logged and rendered as "not available": the page must never break because the
// provider or its configuration is missing.
func (h *profileHandler) discordSection(c *gin.Context, userID int64) discordSectionView {
	cfg, err := h.discord.Config(c.Request.Context())
	if err != nil {
		h.logger.Error("discord configuration read failed", "request_id", GetRequestID(c), "error", err)
		return discordSectionView{}
	}
	section := discordSectionView{Available: cfg.Ready(), GuildID: cfg.GuildID}
	if !section.Available {
		return section
	}
	identity, err := h.discord.Identity(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("discord identity read failed", "request_id", GetRequestID(c), "error", err)
		return section
	}
	if identity.Linked() {
		section.Linked = true
		section.UserName = identity.DiscordUserName
		if identity.LinkedAt != nil {
			section.LinkedText = identity.LinkedAt.UTC().Format("2006-01-02")
		}
	}
	return section
}

func (h *profileHandler) list(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	characters, err := h.characters.List(c.Request.Context(), session.User.ID)
	if err != nil {
		h.fail(c, err)
		return
	}
	out := make([]characterResponse, 0, len(characters))
	for _, character := range characters {
		out = append(out, characterResponseOf(character))
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"characters": out}})
}

func (h *profileHandler) add(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	var req characterRequest
	if rerr := bindUserWrite(c, &req); rerr != nil {
		if wantsHTML(c) {
			h.renderFormError(c, &domain.ValidationError{Fields: rerr.fields}, rerr.message)
			return
		}
		writeRequestError(c, rerr)
		return
	}
	character, err := h.characters.Add(c.Request.Context(), session.User,
		domain.AddCharacterInput{Name: req.Name}, GetRequestID(c), c.ClientIP())
	if err != nil {
		h.fail(c, err)
		return
	}
	if wantsHTML(c) {
		c.Redirect(http.StatusSeeOther, "/account/profile?notice=character-added")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{"character": characterResponseOf(character)}})
}

func (h *profileHandler) remove(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	id, ok := characterIDParam(c)
	if !ok {
		return
	}
	if err := h.characters.Remove(c.Request.Context(), session.User, id, GetRequestID(c), c.ClientIP()); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// removeForm is the HTML adapter for DELETE, which a form cannot issue.
func (h *profileHandler) removeForm(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	id, ok := characterIDParam(c)
	if !ok {
		return
	}
	if err := h.characters.Remove(c.Request.Context(), session.User, id, GetRequestID(c), c.ClientIP()); err != nil {
		h.fail(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/account/profile?notice=character-removed")
}

func characterIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		if wantsHTML(c) {
			c.AbortWithStatus(http.StatusNotFound)
			return 0, false
		}
		writeError(c, http.StatusNotFound, "not_found", "resource not found")
		return 0, false
	}
	return id, true
}

// fail maps a character error. A duplicate name is 409 with a field message so
// the profile form can point at the input; the cap is 409 with its own text.
func (h *profileHandler) fail(c *gin.Context, err error) {
	if wantsHTML(c) {
		h.renderFormError(c, err, "")
		return
	}
	switch {
	case errors.Is(err, domain.ErrAlreadyExists):
		writeErrorFields(c, http.StatusConflict, "conflict", "the request conflicts with current state",
			map[string]string{"characterName": "that character name is already registered"})
	case errors.Is(err, domain.ErrConflict):
		writeErrorFields(c, http.StatusConflict, "conflict", "the request conflicts with current state",
			map[string]string{"characterName": fmt.Sprintf("an account may have at most %d characters",
				domain.CharacterMaxPerUser)})
	default:
		respondError(c, h.logger, err)
	}
}

// renderFormError re-renders the profile page with the failure attached to the
// character field.
func (h *profileHandler) renderFormError(c *gin.Context, err error, message string) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	characters, listErr := h.characters.List(c.Request.Context(), session.User.ID)
	if listErr != nil {
		h.pageError(c, listErr)
		return
	}
	status := http.StatusUnprocessableEntity
	fields := map[string]string{}
	switch {
	case errors.Is(err, domain.ErrAlreadyExists):
		status = http.StatusConflict
		fields["characterName"] = "that character name is already registered"
	case errors.Is(err, domain.ErrConflict):
		status = http.StatusConflict
		fields["characterName"] = fmt.Sprintf("an account may have at most %d characters",
			domain.CharacterMaxPerUser)
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	default:
		var validation *domain.ValidationError
		if errors.As(err, &validation) {
			fields = validation.Fields
		} else {
			h.pageError(c, err)
			return
		}
	}
	if message == "" {
		message = "Check the highlighted field."
	}
	h.views.accountProfile(c, status, profilePageData{
		pageData: pageData{
			Title: "Profile", CSRF: CSRF(c), User: &session.User,
			Error: message, ErrorFields: fields,
		},
		UserName:      session.User.UserName,
		RoleLabel:     userRoleLabel(session.User.Role()),
		Characters:    characterViews(characters),
		MaxCharacters: domain.CharacterMaxPerUser,
		Discord:       h.discordSection(c, session.User.ID),
	})
}

func (h *profileHandler) pageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		unauthorized(c)
	case errors.Is(err, domain.ErrUnavailable):
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"Profile storage is unavailable. Please try again once the application database is ready.")
	default:
		h.logger.Error("profile page failed", "request_id", GetRequestID(c), "error", err)
		h.views.errorPage(c, http.StatusInternalServerError, "The profile page could not be loaded.")
	}
}

// profileNotice maps a machine-written query code onto fixed text. Discord
// notices are fixed strings so no provider message is reflected into the page.
func profileNotice(code string) string {
	switch code {
	case "character-added":
		return "Character added."
	case "character-removed":
		return "Character removed."
	case "discord-linked":
		return "Discord account linked."
	case "discord-unlinked":
		return "Discord account unlinked."
	case "discord-taken":
		return "That Discord account is already linked to a different account."
	case "discord-state":
		return "The Discord authorization expired or did not belong to this browser. Start the link again."
	case "discord-denied":
		return "Discord authorization was cancelled."
	case "discord-provider":
		return "Discord could not be reached. Try again shortly."
	case "discord-unavailable":
		return "Discord linking is not enabled."
	case "discord-not-linked":
		return "No Discord account is linked."
	case "discord-error":
		return "The Discord link could not be completed."
	}
	return ""
}
