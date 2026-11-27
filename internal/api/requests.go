package api

import (
	"context"
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

const requestWriteTimeout = 5 * time.Second

// requestHandler serves the member request workflow: create, own list, own
// detail and cancellation. Officer decisions are the Phase 4 admin handler; the
// public form is Phase 5. Both will reuse the same service.
type requestHandler struct {
	requests *service.RequestService
	views    *views
	logger   *slog.Logger
}

// newForm renders the member request form. The character control is a dropdown
// when the account has profile characters and a free-text input otherwise
// (design section 5.6), chosen from one view model.
func (h *requestHandler) newForm(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	lotID, err := strconv.ParseInt(strings.TrimSpace(c.Query("item")), 10, 64)
	if err != nil || lotID <= 0 {
		h.views.errorPage(c, http.StatusNotFound, "The requested inventory record does not exist.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestWriteTimeout)
	defer cancel()

	lot, err := h.requests.ActiveLot(ctx, lotID)
	if err != nil {
		h.respondFormFailure(c, err)
		return
	}
	if lot.Quantity == 0 {
		h.views.errorPage(c, http.StatusConflict,
			"This item is out of stock, so it cannot be requested right now.")
		return
	}
	characters, err := h.requests.Characters(ctx, session.User.ID)
	if err != nil {
		h.respondPageFailure(c, err)
		return
	}
	data := requestFormPageOf(lot, characters, requestFormValues{
		ItemID: lot.ID, Quantity: 1,
		ExpectedModAt:  lot.ModAt.UTC().Format(time.RFC3339Nano),
		IdempotencyKey: newMovementIdempotencyKey(),
	})
	data.pageData = pageData{
		Title: "Request " + lot.Name, CSRF: CSRF(c), User: &session.User,
	}
	h.views.requestForm(c, http.StatusOK, data)
}

// create validates and writes one member request. Success is a 303 to the
// member receipt (the detail page), so a refresh never resubmits.
func (h *requestHandler) create(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	values := requestFormValues{
		ItemID:         parseRequestInt64(c.PostForm("item")),
		Quantity:       parseRequestUint32(c.PostForm("quantity")),
		CharacterName:  strings.TrimSpace(c.PostForm("characterName")),
		Note:           strings.TrimSpace(c.PostForm("note")),
		ExpectedModAt:  strings.TrimSpace(c.PostForm("expectedModAt")),
		IdempotencyKey: strings.TrimSpace(c.PostForm("idempotencyKey")),
	}
	in := domain.CreateRequestInput{
		GuildBankItemID: values.ItemID,
		Quantity:        values.Quantity,
		CharacterName:   values.CharacterName,
		Note:            values.Note,
		Kind:            domain.RequesterMember,
		Actor:           requestActor(session),
		IdempotencyKey:  values.IdempotencyKey,
		CorrelationID:   GetRequestID(c),
		SourceIP:        c.ClientIP(),
	}
	if values.ExpectedModAt != "" {
		parsed, ok := parseExpectedModAt(values.ExpectedModAt)
		if !ok {
			h.renderFormError(c, values, http.StatusUnprocessableEntity,
				"Check the highlighted fields.",
				map[string]string{"expectedModAt": "the item version is not a valid timestamp"})
			return
		}
		in.ExpectedModAt = parsed
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestWriteTimeout)
	defer cancel()
	request, err := h.requests.Create(ctx, in)
	if err != nil {
		h.failCreate(c, values, err)
		return
	}
	c.Redirect(http.StatusSeeOther,
		"/my/requests/"+strconv.FormatInt(request.RequestID, 10)+"?notice=created")
}

// failCreate re-renders the form for an outcome the member can correct. A lot
// that vanished is an error page; everything else keeps the member's input and
// reloads the current item and character list.
func (h *requestHandler) failCreate(c *gin.Context, values requestFormValues, err error) {
	status, message := http.StatusUnprocessableEntity, "Check the highlighted fields."
	fields := map[string]string{}
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		fields = validation.Fields
	case errors.Is(err, domain.ErrRequestItemUnavailable):
		h.respondFormFailure(c, err)
		return
	case errors.Is(err, domain.ErrRequestInsufficientStock):
		status, message = http.StatusConflict, "The quantity is above what is on hand now."
	case errors.Is(err, domain.ErrRequestStaleItem):
		status, message = http.StatusConflict, "The stock changed since you loaded this page. Review the item and submit again."
	case errors.Is(err, domain.ErrRequestAlreadyPending):
		status, message = http.StatusConflict, "That character already has a pending request for this item."
	case errors.Is(err, domain.ErrRequestTooManyPending):
		status, message = http.StatusTooManyRequests, "You already have the maximum number of pending requests."
	case errors.Is(err, domain.ErrConflict):
		status, message = http.StatusConflict, "That form submission could not be completed. Reload the page and try again."
	case errors.Is(err, domain.ErrRequestCharacterInvalid):
		message = "That character is not on your profile."
		fields["characterName"] = message
	case errors.Is(err, domain.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		h.respondPageFailure(c, err)
		return
	default:
		h.logger.Error("request creation failed", "request_id", GetRequestID(c), "error", err)
		h.views.errorPage(c, http.StatusInternalServerError, "The request could not be created.")
		return
	}
	h.renderFormError(c, values, status, message, fields)
}

// renderFormError reloads the lot and character list and renders the form with
// the failure attached. The expected item version is refreshed so the member
// can resubmit against the state they now see.
func (h *requestHandler) renderFormError(c *gin.Context, values requestFormValues,
	status int, message string, fields map[string]string) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestWriteTimeout)
	defer cancel()
	lot, err := h.requests.ActiveLot(ctx, values.ItemID)
	if err != nil {
		h.respondFormFailure(c, err)
		return
	}
	if lot.Quantity == 0 {
		h.views.errorPage(c, http.StatusConflict,
			"This item is out of stock, so it cannot be requested right now.")
		return
	}
	characters, err := h.requests.Characters(ctx, session.User.ID)
	if err != nil {
		h.respondPageFailure(c, err)
		return
	}
	// The form is re-rendered against the state the member now sees, so a stale
	// submission is corrected by one resubmit rather than failing twice.
	values.ExpectedModAt = lot.ModAt.UTC().Format(time.RFC3339Nano)
	data := requestFormPageOf(lot, characters, values)
	data.pageData = pageData{
		Title: "Request " + lot.Name, CSRF: CSRF(c), User: &session.User,
		Error: message, ErrorFields: fields,
	}
	h.views.requestForm(c, status, data)
}

// mine lists the signed-in account's own requests, newest first, with an
// optional status filter.
func (h *requestHandler) mine(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	filter := domain.RequestFilter{Status: domain.RequestStatus(strings.TrimSpace(c.Query("status")))}
	if page, err := strconv.Atoi(strings.TrimSpace(c.Query("page"))); err == nil && page > 0 {
		filter.Page = page
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestWriteTimeout)
	defer cancel()
	page, err := h.requests.ListMine(ctx, session.User.ID, filter)
	if err != nil {
		h.respondPageFailure(c, err)
		return
	}
	data := requestsMinePageOf(page)
	data.pageData = pageData{
		Title: "My requests", CSRF: CSRF(c), User: &session.User,
		Notice: requestNotice(c.Query("notice")),
	}
	h.views.requestsMine(c, http.StatusOK, data)
}

// mineDetail is the member receipt and detail page. Another account's ID is a
// 404 through the service, so this page cannot be used to enumerate requests.
func (h *requestHandler) mineDetail(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	requestID, ok := parseRequestID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestWriteTimeout)
	defer cancel()
	request, err := h.requests.GetMine(ctx, session.User.ID, requestID)
	if err != nil {
		h.respondDetailFailure(c, err)
		return
	}
	data := requestDetailPageOf(request)
	data.pageData = pageData{
		Title: "Request #" + strconv.FormatInt(request.RequestID, 10),
		CSRF:  CSRF(c), User: &session.User,
		Notice: requestNotice(c.Query("notice")),
	}
	h.views.requestMineDetail(c, http.StatusOK, data)
}

// cancel withdraws one of the caller's own Pending requests.
func (h *requestHandler) cancel(c *gin.Context) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	requestID, ok := parseRequestID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), requestWriteTimeout)
	defer cancel()
	_, err := h.requests.Cancel(ctx, domain.CancelRequestInput{
		RequestID: requestID, Reason: strings.TrimSpace(c.PostForm("reason")),
		Actor: requestActor(session), CorrelationID: GetRequestID(c),
		SourceIP: c.ClientIP(),
	})
	if err != nil {
		if errors.Is(err, domain.ErrRequestNotPending) {
			c.Redirect(http.StatusSeeOther,
				"/my/requests/"+strconv.FormatInt(requestID, 10)+"?notice=already-decided")
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			h.respondDetailFailure(c, err)
			return
		}
		var validation *domain.ValidationError
		if errors.As(err, &validation) {
			request, loadErr := h.requests.GetMine(ctx, session.User.ID, requestID)
			if loadErr != nil {
				h.respondDetailFailure(c, loadErr)
				return
			}
			data := requestDetailPageOf(request)
			data.pageData = pageData{
				Title: "Request #" + strconv.FormatInt(request.RequestID, 10),
				CSRF:  CSRF(c), User: &session.User,
				Error: "Check the highlighted fields.", ErrorFields: validation.Fields,
			}
			h.views.requestMineDetail(c, http.StatusUnprocessableEntity, data)
			return
		}
		h.logger.Error("request cancellation failed", "request_id", GetRequestID(c), "error", err)
		h.views.errorPage(c, http.StatusInternalServerError, "The request could not be cancelled.")
		return
	}
	c.Redirect(http.StatusSeeOther,
		"/my/requests/"+strconv.FormatInt(requestID, 10)+"?notice=cancelled")
}

// respondFormFailure maps a lot or store failure on the create form. A removed
// lot is the documented 409; an unavailable store is 503.
func (h *requestHandler) respondFormFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrRequestItemUnavailable), errors.Is(err, domain.ErrNotFound):
		h.views.errorPage(c, http.StatusConflict,
			"This item is no longer available, so it cannot be requested.")
	case errors.Is(err, domain.ErrUnavailable), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled):
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The request workflow is temporarily unavailable. Please try again shortly.")
	default:
		h.respondPageFailure(c, err)
	}
}

// respondDetailFailure maps a missing or another account's request to 404.
func (h *requestHandler) respondDetailFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.views.errorPage(c, http.StatusNotFound, "The requested request does not exist.")
	case errors.Is(err, domain.ErrUnavailable), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled):
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The request workflow is temporarily unavailable. Please try again shortly.")
	default:
		h.respondPageFailure(c, err)
	}
}

func (h *requestHandler) respondPageFailure(c *gin.Context, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		h.views.errorPage(c, http.StatusUnprocessableEntity, adminValidationMessage(validation.Fields))
	case errors.Is(err, domain.ErrUnavailable), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled):
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The request workflow is temporarily unavailable. Please try again shortly.")
	default:
		h.logger.Error("request page failed", "request_id", GetRequestID(c), "error", err)
		h.views.errorPage(c, http.StatusInternalServerError, "The request page could not be loaded.")
	}
}

// parseRequestID reads the :id parameter, rendering the member-facing 404.
func parseRequestID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeHTMLStatus(c, http.StatusNotFound, "Not found", "The requested request does not exist.")
		return 0, false
	}
	return id, true
}

// parseRequestInt64 parses a positive form identifier. An invalid value becomes
// zero so the domain validation reports the field.
func parseRequestInt64(raw string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

// parseRequestUint32 parses a form number. An invalid value becomes zero so the
// domain validation reports the field instead of the handler guessing.
func parseRequestUint32(raw string) uint32 {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(value)
}

// requestActor builds the actor snapshot from the authenticated session.
func requestActor(session service.AuthenticatedSession) domain.Actor {
	return domain.Actor{
		UserID: session.User.ID, UserName: session.User.UserName,
		IsAdmin: session.User.IsAdmin(), Role: session.User.Role(),
	}
}

// requestNotice maps a machine-written query code onto fixed text. No notice
// echoes user-entered content (design section 13.7).
func requestNotice(code string) string {
	switch code {
	case "created":
		return "Request submitted. An officer will review it."
	case "cancelled":
		return "Request cancelled."
	case "already-decided":
		return "That request was already decided, so it could not be cancelled."
	}
	return ""
}
