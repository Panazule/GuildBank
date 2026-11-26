package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

// usersHandler serves administrator account management and the signed-in
// account owner's own password change. Every route is classified in
// authorization_matrix_test.go.
type usersHandler struct {
	users  *service.UserAdminService
	views  *views
	logger *slog.Logger
}

type userResponse struct {
	ID                 int64      `json:"id"`
	UserName           string     `json:"userName"`
	AccessLevel        string     `json:"accessLevel"`
	Disabled           bool       `json:"disabled"`
	MustChangePassword bool       `json:"mustChangePassword"`
	LastLogin          *time.Time `json:"lastLogin,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	ModAt              time.Time  `json:"modAt"`
	ModBy              string     `json:"modBy,omitempty"`
}

func userResponseOf(u domain.User) userResponse {
	return userResponse{
		ID: u.ID, UserName: u.UserName, AccessLevel: u.AccessLevel,
		Disabled: u.Disabled, MustChangePassword: u.MustChangePassword,
		LastLogin: u.LastLogin, CreatedAt: u.CreatedAt.UTC(), ModAt: u.ModAt.UTC(),
		ModBy: u.ModBy,
	}
}

type createUserRequest struct {
	UserName        string `json:"userName" form:"userName"`
	AccessLevel     string `json:"accessLevel" form:"accessLevel"`
	Password        string `json:"password" form:"password"`
	ConfirmPassword string `json:"confirmPassword" form:"confirmPassword"`
}

func (r createUserRequest) input() domain.CreateUserInput {
	return domain.CreateUserInput{
		UserName: r.UserName, AccessLevel: r.AccessLevel,
		Password: r.Password, ConfirmPassword: r.ConfirmPassword,
	}
}

// updateUserRequest accepts a role and a status. JSON clients send `disabled`;
// the HTML form sends an explicit `status` value, because an unchecked checkbox
// cannot express "enable" unambiguously.
type updateUserRequest struct {
	AccessLevel *string `json:"accessLevel" form:"accessLevel"`
	Disabled    *bool   `json:"disabled" form:"disabled"`
	Status      string  `json:"-" form:"status"`
}

func (r updateUserRequest) desiredStatus() (bool, bool, *requestError) {
	if r.Disabled != nil {
		return *r.Disabled, true, nil
	}
	switch strings.TrimSpace(r.Status) {
	case "":
		return false, false, nil
	case "active":
		return false, true, nil
	case "disabled":
		return true, true, nil
	default:
		return false, false, validationRequestError(map[string]string{
			"status": "must be active or disabled"})
	}
}

type resetPasswordRequest struct {
	Password        string `json:"password" form:"password"`
	ConfirmPassword string `json:"confirmPassword" form:"confirmPassword"`
}

func (r resetPasswordRequest) input() domain.ResetPasswordInput {
	return domain.ResetPasswordInput{Password: r.Password, ConfirmPassword: r.ConfirmPassword}
}

type deleteUserRequest struct {
	ConfirmUserName string `json:"confirmUserName" form:"confirmUserName"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" form:"currentPassword"`
	NewPassword     string `json:"newPassword" form:"newPassword"`
	ConfirmPassword string `json:"confirmPassword" form:"confirmPassword"`
}

func (r changePasswordRequest) input() domain.ChangePasswordInput {
	return domain.ChangePasswordInput{
		CurrentPassword: r.CurrentPassword, NewPassword: r.NewPassword,
		ConfirmPassword: r.ConfirmPassword,
	}
}

// bindUserWrite decodes an account request from JSON under the strict
// administrative contract, or from an HTML form, which cannot send JSON.
func bindUserWrite(c *gin.Context, dst any) *requestError {
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		return decodeAdminJSON(c, dst)
	}
	if err := c.ShouldBind(dst); err != nil {
		return badRequest("the form could not be read")
	}
	return nil
}

// fail maps an account-administration error. A duplicate username is 409 with a
// field-level message so the create form can point at the name.
func (h *usersHandler) fail(c *gin.Context, err error) {
	if service.IsAlreadyExists(err) {
		writeErrorFields(c, http.StatusConflict, "conflict",
			"the request conflicts with current state",
			map[string]string{"userName": "that username is already in use"})
		return
	}
	var validation *domain.ValidationError
	if errors.As(err, &validation) {
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"one or more fields are invalid", validation.Fields)
		return
	}
	respondError(c, h.logger, err)
}

func (h *usersHandler) list(c *gin.Context) {
	users, err := h.users.List(c.Request.Context(), c.Query("q"))
	if err != nil {
		h.fail(c, err)
		return
	}
	out := make([]userResponse, 0, len(users))
	for _, user := range users {
		out = append(out, userResponseOf(user))
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"users": out}})
}

func (h *usersHandler) create(c *gin.Context) {
	var req createUserRequest
	if rerr := bindUserWrite(c, &req); rerr != nil {
		writeRequestError(c, rerr)
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	created, err := h.users.Create(c.Request.Context(), req.input(), actor, GetRequestID(c), c.ClientIP())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{"user": userResponseOf(created)}})
}

func (h *usersHandler) update(c *gin.Context) {
	id, ok := parseAdminID(c, false)
	if !ok {
		return
	}
	var req updateUserRequest
	if rerr := bindUserWrite(c, &req); rerr != nil {
		writeRequestError(c, rerr)
		return
	}
	disabled, hasStatus, rerr := req.desiredStatus()
	if rerr != nil {
		writeRequestError(c, rerr)
		return
	}
	if req.AccessLevel == nil && !hasStatus {
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"one or more fields are invalid",
			map[string]string{"accessLevel": "send accessLevel, disabled or status"})
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if req.AccessLevel != nil {
		if _, err := h.users.SetRole(ctx, id, *req.AccessLevel, actor, GetRequestID(c), c.ClientIP()); err != nil {
			h.fail(c, err)
			return
		}
	}
	if hasStatus {
		if _, err := h.users.SetDisabled(ctx, id, disabled, actor, GetRequestID(c), c.ClientIP()); err != nil {
			h.fail(c, err)
			return
		}
	}
	updated, err := h.users.Get(ctx, id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"user": userResponseOf(updated)}})
}

func (h *usersHandler) resetPassword(c *gin.Context) {
	id, ok := parseAdminID(c, false)
	if !ok {
		return
	}
	var req resetPasswordRequest
	if rerr := bindUserWrite(c, &req); rerr != nil {
		writeRequestError(c, rerr)
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	updated, err := h.users.ResetPassword(c.Request.Context(), id, req.input(), actor, GetRequestID(c), c.ClientIP())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"user": userResponseOf(updated)}})
}

func (h *usersHandler) remove(c *gin.Context) {
	id, ok := parseAdminID(c, false)
	if !ok {
		return
	}
	var req deleteUserRequest
	if rerr := bindUserWrite(c, &req); rerr != nil {
		writeRequestError(c, rerr)
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	if err := h.users.Delete(c.Request.Context(), id, domain.DeleteUserInput{ConfirmUserName: req.ConfirmUserName},
		actor, GetRequestID(c), c.ClientIP()); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *usersHandler) page(c *gin.Context) {
	filter := strings.TrimSpace(c.Query("q"))
	ctx := c.Request.Context()
	users, err := h.users.List(ctx, filter)
	if err != nil {
		h.pageError(c, err)
		return
	}
	enabledAdmins, err := h.users.EnabledAdmins(ctx)
	if err != nil {
		h.pageError(c, err)
		return
	}
	actorID := int64(0)
	if session, ok := CurrentSession(c); ok {
		actorID = session.User.ID
	}
	rows := make([]userRowView, 0, len(users))
	for _, user := range users {
		rows = append(rows, userRowViewOf(user, actorID, enabledAdmins))
	}
	data := usersPageData{
		pageData: pageData{Title: "Accounts", CSRF: CSRF(c), User: currentUserPointer(c)},
		Users:    rows, Filter: filter, Total: len(rows), EnabledAdmins: enabledAdmins,
	}
	data.Notice = userNotice(c.Query("notice"))
	h.views.adminUsers(c, http.StatusOK, data)
}

func (h *usersHandler) newPage(c *gin.Context) {
	h.renderCreate(c, http.StatusOK, userFormValues{AccessLevel: string(domain.RoleMember)}, nil, "")
}

func (h *usersHandler) createForm(c *gin.Context) {
	var req createUserRequest
	if rerr := bindUserWrite(c, &req); rerr != nil {
		h.renderCreate(c, rerr.status, userFormValues{UserName: req.UserName, AccessLevel: req.AccessLevel},
			rerr.fields, rerr.message)
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	form := userFormValues{UserName: req.UserName, AccessLevel: req.AccessLevel}
	created, err := h.users.Create(c.Request.Context(), req.input(), actor, GetRequestID(c), c.ClientIP())
	if err != nil {
		status, fields, message := userFormFailure(err)
		h.renderCreate(c, status, form, fields, message)
		return
	}
	c.Redirect(http.StatusSeeOther, accountDetailURL(created.ID, "created"))
}

// userFormFailure maps an account error onto a form re-render.
func userFormFailure(err error) (int, map[string]string, string) {
	if service.IsAlreadyExists(err) {
		return http.StatusConflict, map[string]string{"userName": "that username is already in use"},
			"Check the highlighted field."
	}
	var validation *domain.ValidationError
	if errors.As(err, &validation) {
		return http.StatusUnprocessableEntity, validation.Fields, "Check the highlighted fields."
	}
	if errors.Is(err, domain.ErrConflict) {
		return http.StatusConflict, nil, err.Error()
	}
	return http.StatusInternalServerError, nil, "The account could not be saved."
}

func (h *usersHandler) renderCreate(c *gin.Context, status int, form userFormValues,
	fields map[string]string, message string) {
	selected := domain.Role(form.AccessLevel)
	if !selected.Valid() {
		selected = domain.RoleMember
	}
	h.views.adminUserCreate(c, status, userCreatePageData{
		pageData: pageData{
			Title: "New account", CSRF: CSRF(c), User: currentUserPointer(c),
			Error: message, ErrorFields: fields,
		},
		Form:  form,
		Roles: userRoleOptions(selected),
	})
}

func (h *usersHandler) detailPage(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	user, err := h.users.Get(ctx, id)
	if err != nil {
		h.pageError(c, err)
		return
	}
	enabledAdmins, err := h.users.EnabledAdmins(ctx)
	if err != nil {
		h.pageError(c, err)
		return
	}
	h.renderDetail(c, http.StatusOK, user, enabledAdmins, "", nil)
}

func (h *usersHandler) renderDetail(c *gin.Context, status int, user domain.User,
	enabledAdmins int, message string, fields map[string]string) {
	actorID := int64(0)
	if session, ok := CurrentSession(c); ok {
		actorID = session.User.ID
	}
	row := userRowViewOf(user, actorID, enabledAdmins)
	data := userDetailPageData{
		pageData: pageData{
			Title: user.UserName, CSRF: CSRF(c), User: currentUserPointer(c),
			Error: message, ErrorFields: fields,
		},
		Row: row, Roles: userRoleOptions(row.Role), EnabledAdmins: enabledAdmins,
		Form: userFormValues{UserName: user.UserName, AccessLevel: user.AccessLevel},
	}
	data.Notice = userNotice(c.Query("notice"))
	h.views.adminUserDetail(c, status, data)
}

func (h *usersHandler) detailFailure(c *gin.Context, id int64, err error, fields map[string]string) {
	if errors.Is(err, domain.ErrNotFound) {
		h.views.errorPage(c, http.StatusNotFound, "That account does not exist.")
		return
	}
	user, getErr := h.users.Get(c.Request.Context(), id)
	if getErr != nil {
		h.pageError(c, getErr)
		return
	}
	enabledAdmins, getErr := h.users.EnabledAdmins(c.Request.Context())
	if getErr != nil {
		h.pageError(c, getErr)
		return
	}
	status, validationFields, message := userFormFailure(err)
	if len(fields) > 0 {
		status, validationFields = http.StatusUnprocessableEntity, fields
		message = "Check the highlighted fields."
	}
	h.renderDetail(c, status, user, enabledAdmins, message, validationFields)
}

func (h *usersHandler) updateForm(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	var req updateUserRequest
	if rerr := bindUserWrite(c, &req); rerr != nil {
		h.detailFailure(c, id, &domain.ValidationError{Fields: rerr.fields}, rerr.fields)
		return
	}
	disabled, hasStatus, rerr := req.desiredStatus()
	if rerr != nil {
		h.detailFailure(c, id, &domain.ValidationError{Fields: rerr.fields}, rerr.fields)
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if req.AccessLevel != nil {
		if _, err := h.users.SetRole(ctx, id, *req.AccessLevel, actor, GetRequestID(c), c.ClientIP()); err != nil {
			h.detailFailure(c, id, err, nil)
			return
		}
	}
	if hasStatus {
		if _, err := h.users.SetDisabled(ctx, id, disabled, actor, GetRequestID(c), c.ClientIP()); err != nil {
			h.detailFailure(c, id, err, nil)
			return
		}
	}
	c.Redirect(http.StatusSeeOther, accountDetailURL(id, "updated"))
}

func (h *usersHandler) resetForm(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	var req resetPasswordRequest
	if rerr := bindUserWrite(c, &req); rerr != nil {
		h.detailFailure(c, id, &domain.ValidationError{Fields: rerr.fields}, rerr.fields)
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	if _, err := h.users.ResetPassword(c.Request.Context(), id, req.input(), actor,
		GetRequestID(c), c.ClientIP()); err != nil {
		h.detailFailure(c, id, err, nil)
		return
	}
	c.Redirect(http.StatusSeeOther, accountDetailURL(id, "password-reset"))
}

func (h *usersHandler) deleteForm(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	var req deleteUserRequest
	if rerr := bindUserWrite(c, &req); rerr != nil {
		h.detailFailure(c, id, &domain.ValidationError{Fields: rerr.fields}, rerr.fields)
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	if err := h.users.Delete(c.Request.Context(), id,
		domain.DeleteUserInput{ConfirmUserName: req.ConfirmUserName}, actor,
		GetRequestID(c), c.ClientIP()); err != nil {
		h.detailFailure(c, id, err, nil)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/users?notice=deleted")
}

func accountDetailURL(id int64, notice string) string {
	return "/admin/users/" + strconv.FormatInt(id, 10) + "?notice=" + notice
}

// pageError renders a page-level failure. A missing row is 404; an unavailable
// store keeps its documented 503.
func (h *usersHandler) pageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.views.errorPage(c, http.StatusNotFound, "That account does not exist.")
	case errors.Is(err, domain.ErrUnavailable):
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"Account storage is unavailable. Please try again once the application database is ready.")
	default:
		h.logger.Error("account page failed", "request_id", GetRequestID(c), "error", err)
		h.views.errorPage(c, http.StatusInternalServerError, "The account page could not be loaded.")
	}
}

func (h *usersHandler) passwordPage(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	notice := ""
	if c.Query("changed") == "1" {
		notice = "Password updated. Other sessions for this account were signed out."
	}
	h.views.accountPassword(c, http.StatusOK, accountPasswordPageData{
		pageData: pageData{
			Title: "Change password", CSRF: CSRF(c), User: &session.User, Notice: notice,
		},
		UserName: session.User.UserName,
		Forced:   session.User.MustChangePassword,
	})
}

func (h *usersHandler) changePassword(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	var req changePasswordRequest
	if rerr := bindUserWrite(c, &req); rerr != nil {
		h.passwordFailure(c, session.User, rerr.fields, rerr.message)
		return
	}
	updated, err := h.users.ChangeOwnPassword(c.Request.Context(), session.User, session.Session.ID,
		req.input(), GetRequestID(c), c.ClientIP())
	if err != nil {
		var validation *domain.ValidationError
		if errors.As(err, &validation) {
			h.passwordFailure(c, session.User, validation.Fields, "Check the highlighted fields.")
			return
		}
		respondError(c, h.logger, err)
		return
	}
	if wantsHTML(c) {
		c.Redirect(http.StatusSeeOther, "/account/password?changed=1")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"user": userResponseOf(updated)}})
}

func (h *usersHandler) passwordFailure(c *gin.Context, user domain.User,
	fields map[string]string, message string) {
	if wantsHTML(c) {
		h.views.accountPassword(c, http.StatusUnprocessableEntity, accountPasswordPageData{
			pageData: pageData{
				Title: "Change password", CSRF: CSRF(c), User: &user,
				Error: message, ErrorFields: fields,
			},
			UserName: user.UserName, Forced: user.MustChangePassword,
		})
		return
	}
	writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
		"one or more fields are invalid", fields)
}

// userRowViewOf builds the shared account presentation. locked marks the last
// enabled Administrator, whose role, status and delete controls must stay
// disabled in the UI as well as in the store.
func userRowViewOf(u domain.User, actorID int64, enabledAdmins int) userRowView {
	locked := u.Role() == domain.RoleAdmin && !u.Disabled && enabledAdmins <= 1
	row := userRowView{
		ID: u.ID, UserName: u.UserName, Role: u.Role(), RoleLabel: userRoleLabel(u.Role()),
		Disabled: u.Disabled, MustChangePassword: u.MustChangePassword,
		LastLoginText: userTimeText(u.LastLogin),
		CreatedText:   u.CreatedAt.UTC().Format("2006-01-02"),
		ModBy:         u.ModBy, IsSelf: u.ID == actorID, Locked: locked,
	}
	row.CanDisable = !row.IsSelf && !locked
	row.CanDelete = !row.IsSelf && !locked
	return row
}

func userRoleLabel(role domain.Role) string {
	switch role {
	case domain.RoleAdmin:
		return "Administrator"
	case domain.RoleOfficer:
		return "Officer"
	default:
		return "Member"
	}
}

func userRoleOptions(selected domain.Role) []roleSelectOption {
	out := make([]roleSelectOption, 0, len(domain.Roles))
	for _, role := range domain.Roles {
		out = append(out, roleSelectOption{
			Value: string(role), Label: userRoleLabel(role), Selected: role == selected,
		})
	}
	return out
}

func userTimeText(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "—"
	}
	return value.UTC().Format("2006-01-02 15:04 UTC")
}

// userNotice maps a machine-written query code onto fixed text. Nothing from the
// query string is reflected.
func userNotice(code string) string {
	switch code {
	case "created":
		return "The account was created. The user must change the temporary password at the next sign-in."
	case "updated":
		return "The account was updated."
	case "password-reset":
		return "The temporary password was set. Every session for that account was signed out."
	case "deleted":
		return "The account was deleted."
	}
	return ""
}
