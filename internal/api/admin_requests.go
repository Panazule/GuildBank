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

// adminRequestHandler serves the officer request queue and the decision page.
// Approval issues stock through the same ledger path as a manual issue; denial
// only moves the request status.
type adminRequestHandler struct {
	admin  *service.AdminInventoryService
	views  *views
	logger *slog.Logger
	// now is injectable so the age bands are deterministic in tests. A nil
	// value uses the wall clock.
	now func() time.Time
}

func (h *adminRequestHandler) currentTime() time.Time {
	if h.now != nil {
		return h.now().UTC()
	}
	return time.Now().UTC()
}

// adminRequestFilterFrom parses the queue query. The default status is Pending
// so the queue opens on the work that needs doing; status=all removes it.
func adminRequestFilterFrom(c *gin.Context) (domain.RequestFilter, map[string]string) {
	filter := domain.RequestFilter{
		Status:   domain.RequestPending,
		Page:     1,
		PageSize: domain.RequestDefaultPageSize,
	}
	fields := make(map[string]string)
	switch raw := strings.TrimSpace(c.Query("status")); {
	case raw == "":
	case strings.EqualFold(raw, "all"):
		filter.Status = ""
	case domain.RequestStatus(raw).Valid():
		filter.Status = domain.RequestStatus(raw)
	default:
		fields["status"] = "status must be one of the approved values"
	}
	if raw := strings.TrimSpace(c.Query("kind")); raw != "" {
		if !domain.RequesterKind(raw).Valid() {
			fields["kind"] = "kind must be Member or Public"
		} else {
			filter.RequesterKind = domain.RequesterKind(raw)
		}
	}
	filter.CharacterName = domain.NormalizeCharacterName(c.Query("character"))
	filter.ItemName = strings.TrimSpace(c.Query("item"))
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		value, err := time.Parse("2006-01-02", raw)
		if err != nil {
			fields["from"] = "from must be a calendar date"
		} else {
			filter.From = value.UTC()
		}
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		value, err := time.Parse("2006-01-02", raw)
		if err != nil {
			fields["to"] = "to must be a calendar date"
		} else {
			// The date input names a whole day, so the bound is inclusive.
			filter.To = value.UTC().Add(24*time.Hour - time.Nanosecond)
		}
	}
	filter.Actionable = c.Query("actionable") == "1" || c.Query("actionable") == "true"
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > domain.RequestMaxPage {
			fields["page"] = "page must be between 1 and 10000"
		} else {
			filter.Page = value
		}
	}
	if raw := strings.TrimSpace(c.Query("pageSize")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > domain.RequestMaxPageSize {
			fields["pageSize"] = "pageSize must be between 1 and 200"
		} else {
			filter.PageSize = value
		}
	}
	return filter, fields
}

// queue lists the officer request queue, oldest first.
func (h *adminRequestHandler) queue(c *gin.Context) {
	filter, fields := adminRequestFilterFrom(c)
	if len(fields) > 0 {
		h.views.errorPage(c, http.StatusUnprocessableEntity, adminValidationMessage(fields))
		return
	}
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	if !h.admin.Ready() {
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The request queue is temporarily unavailable. Please try again after the database connection is restored.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	page, err := h.admin.ListRequests(ctx, filter)
	if err != nil {
		h.respondRequestPageError(c, err)
		return
	}
	pending, _ := h.pendingRequestCount(ctx)
	data := adminRequestsPageOf(page, session.User.ID, h.currentTime())
	data.pageData = pageData{
		Title: "Requests", CSRF: CSRF(c), User: &session.User,
		PendingRequestCount: pending,
		Notice:              adminRequestNotice(c.Query("notice")),
	}
	h.views.adminRequests(c, http.StatusOK, data)
}

// pendingRequestCount reads the navigation badge count. A failure is not fatal
// to the page: the queue itself still renders without a badge.
func (h *adminRequestHandler) pendingRequestCount(ctx context.Context) (int, error) {
	return h.admin.PendingRequestCount(ctx)
}

// adminRequestNotice maps the fixed redirect codes to fixed text. No notice
// echoes user-entered content.
func adminRequestNotice(code string) string {
	switch code {
	case "completed":
		return "The request was completed and the item issued."
	case "short-issued":
		return "The request was completed with a short issue: fewer units were issued than requested."
	case "denied":
		return "The request was denied. The reason is visible to the requester."
	case "insufficient-stock":
		return "The stock changed before the approval could be recorded. Review the current quantity and decide again."
	case "already-decided":
		return "Another officer already decided this request."
	case "stale-item":
		return "The item changed since this page was loaded. Review the current lot state and approve again."
	}
	return ""
}

// detail renders the decision page: request, live lot, approve and deny forms,
// and the request's audit history.
func (h *adminRequestHandler) detail(c *gin.Context) {
	requestID, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	data, err := h.loadDetail(ctx, requestID, session.User.ID, session.User.Role())
	if err != nil {
		h.respondRequestPageError(c, err)
		return
	}
	pending, _ := h.pendingRequestCount(ctx)
	data.pageData = pageData{
		Title: "Request #" + strconv.FormatInt(requestID, 10), CSRF: CSRF(c),
		User: &session.User, PendingRequestCount: pending,
		Notice: adminRequestNotice(c.Query("notice")),
	}
	h.views.adminRequestDetail(c, http.StatusOK, data)
}

// loadDetail reads the request, its live lot and its audit history. A missing
// lot is not an error: the page explains that nothing is on hand and offers
// only Deny.
func (h *adminRequestHandler) loadDetail(ctx context.Context, requestID, selfUserID int64,
	role domain.Role) (adminRequestDetailPageData, error) {
	request, err := h.admin.GetRequest(ctx, requestID)
	if err != nil {
		return adminRequestDetailPageData{}, err
	}
	lot, lotErr := h.admin.Get(ctx, request.GuildBankItemID)
	lotFound := lotErr == nil
	if lotErr != nil && !errors.Is(lotErr, domain.ErrNotFound) {
		return adminRequestDetailPageData{}, lotErr
	}
	history, err := h.admin.ListAudit(ctx, domain.AuditFilter{
		Table: "InventoryRequest", RecordID: strconv.FormatInt(requestID, 10),
		Page: 1, PageSize: 20, EventTypes: service.AuditEventTypesFor(role),
	})
	if err != nil {
		// History is informative; the decision must still be possible if the
		// audit read alone fails.
		h.logger.Warn("request history unavailable", "inventory_request_id", requestID, "error", err)
	}
	entries := make([]auditEntryView, 0, len(history.Entries))
	for _, entry := range history.Entries {
		entries = append(entries, auditEntryViewOf(entry))
	}
	return adminRequestDetailOf(request, lot, lotFound, entries, selfUserID, h.currentTime()), nil
}

// approve issues the requested quantity (or the reduced short-stock quantity)
// and completes the request. Success is a 303 to the completed decision page.
func (h *adminRequestHandler) approve(c *gin.Context) {
	requestID, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	fields := make(map[string]string)
	var expected time.Time
	if raw := strings.TrimSpace(c.PostForm("expectedModAt")); raw == "" {
		fields["expectedModAt"] = "The form was missing its version token; reload the page"
	} else if value, valid := parseExpectedModAt(raw); !valid {
		fields["expectedModAt"] = "The version token is not a valid timestamp; reload the page"
	} else {
		expected = value
	}
	var issueQuantity uint32
	if raw := strings.TrimSpace(c.PostForm("issuedQuantity")); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || value < 1 {
			fields["issuedQuantity"] = "Issue quantity must be at least 1"
		} else {
			issueQuantity = uint32(value)
		}
	}
	note := strings.TrimSpace(c.PostForm("note"))
	if len(fields) > 0 {
		h.renderDecisionError(c, requestID, http.StatusUnprocessableEntity,
			"The approval was not recorded. Correct the highlighted fields.", fields)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	request, err := h.admin.ApproveRequest(ctx, domain.ApproveRequestInput{
		RequestID: requestID, ExpectedModAt: expected, IssueQuantity: issueQuantity,
		Note: note, Actor: actor, CorrelationID: GetRequestID(c), SourceIP: c.ClientIP(),
	})
	if err != nil {
		h.failDecision(c, requestID, err)
		return
	}
	h.logger.Info("request approved", "request_id", GetRequestID(c), "user_id", actor.UserID,
		"inventory_request_id", requestID, "issued", request.IssuedQuantity)
	notice := "completed"
	if request.ShortIssued() {
		notice = "short-issued"
	}
	c.Redirect(http.StatusSeeOther,
		"/admin/requests/"+strconv.FormatInt(requestID, 10)+"?notice="+notice)
}

// deny records the required reason and makes the request terminal.
func (h *adminRequestHandler) deny(c *gin.Context) {
	requestID, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	reason := strings.TrimSpace(c.PostForm("reason"))
	if reason == "" {
		h.renderDecisionError(c, requestID, http.StatusUnprocessableEntity,
			"A denial reason is required.", map[string]string{"reason": "Reason is required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	if _, err := h.admin.DenyRequest(ctx, domain.DenyRequestInput{
		RequestID: requestID, Reason: reason, Actor: actor,
		CorrelationID: GetRequestID(c), SourceIP: c.ClientIP(),
	}); err != nil {
		h.failDecision(c, requestID, err)
		return
	}
	h.logger.Info("request denied", "request_id", GetRequestID(c), "user_id", actor.UserID,
		"inventory_request_id", requestID)
	c.Redirect(http.StatusSeeOther,
		"/admin/requests/"+strconv.FormatInt(requestID, 10)+"?notice=denied")
}

// failDecision maps a decision failure onto the decision page. A changed lot is
// re-rendered against its current state so the officer decides again instead of
// retrying a stale form.
func (h *adminRequestHandler) failDecision(c *gin.Context, requestID int64, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		h.renderDecisionError(c, requestID, http.StatusUnprocessableEntity,
			"The action was not recorded. Correct the highlighted fields.", validation.Fields)
	case errors.Is(err, domain.ErrRequestInsufficientStock):
		h.renderDecisionError(c, requestID, http.StatusConflict,
			"The stock changed. Only the quantity shown below remained.", nil)
	case errors.Is(err, domain.ErrRequestStaleItem):
		h.renderDecisionError(c, requestID, http.StatusConflict,
			"The item changed since this page was loaded. Review the current lot state and approve again.", nil)
	case errors.Is(err, domain.ErrRequestNotPending):
		h.renderDecisionError(c, requestID, http.StatusConflict,
			"Another officer already decided this request.", nil)
	case errors.Is(err, domain.ErrRequestItemUnavailable):
		h.renderDecisionError(c, requestID, http.StatusConflict,
			"This item is no longer available, so the request cannot be approved.", nil)
	case errors.Is(err, domain.ErrConflict):
		h.renderDecisionError(c, requestID, http.StatusConflict,
			"This request changed since the page was loaded. Reload and try again.", nil)
	default:
		h.respondRequestPageError(c, err)
	}
}

// renderDecisionError reloads the decision page and renders it with a banner.
func (h *adminRequestHandler) renderDecisionError(c *gin.Context, requestID int64,
	status int, message string, fields map[string]string) {
	session, ok := CurrentSession(c)
	if !ok {
		unauthorized(c)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	data, err := h.loadDetail(ctx, requestID, session.User.ID, session.User.Role())
	if err != nil {
		h.respondRequestPageError(c, err)
		return
	}
	pending, _ := h.pendingRequestCount(ctx)
	data.pageData = pageData{
		Title: "Request #" + strconv.FormatInt(requestID, 10), CSRF: CSRF(c),
		User: &session.User, PendingRequestCount: pending,
		Error: message, ErrorFields: fields,
	}
	h.views.adminRequestDetail(c, status, data)
}

// respondRequestPageError maps store failures onto the shared error page.
func (h *adminRequestHandler) respondRequestPageError(c *gin.Context, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		h.views.errorPage(c, http.StatusUnprocessableEntity, adminValidationMessage(validation.Fields))
	case errors.Is(err, domain.ErrNotFound):
		h.views.errorPage(c, http.StatusNotFound, "The requested request does not exist.")
	case errors.Is(err, domain.ErrUnavailable), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled):
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The request queue is temporarily unavailable. Please try again after the database connection is restored.")
	default:
		h.logger.Error("admin request page failed", "request_id", GetRequestID(c), "error", err)
		h.views.errorPage(c, http.StatusInternalServerError, "The request could not be displayed.")
	}
}
