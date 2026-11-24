package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

// movementFormIdempotencyField is the hidden HTML field carrying the
// per-render idempotency key (OD-06). It shares the duplicate-key 409 semantics
// with the API's Idempotency-Key header.
const movementFormIdempotencyField = "idempotencyKey"

// newMovementIdempotencyKey renders one 128-bit key for a form. It is generated
// per form render so a double-click is retried under the same key, while a
// deliberate second action renders a fresh key.
func newMovementIdempotencyKey() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(buf[:])
}

func adminMovementViewOf(movement domain.InventoryMovement) adminMovementView {
	related := ""
	if movement.RelatedItemID != nil {
		related = strconv.FormatInt(*movement.RelatedItemID, 10)
	}
	return adminMovementView{
		MovementID: movement.MovementID, Type: string(movement.MovementType),
		Quantity: movement.Quantity, Recipient: movement.Recipient,
		ActorUserName:   movement.ActorUserName,
		OccurredAtValue: movement.OccurredAt.UTC().Format(time.RFC3339Nano),
		OccurredAtText:  movement.OccurredAt.UTC().Format("2006-01-02 15:04 UTC"),
		FromLocation:    movement.FromLocation, ToLocation: movement.ToLocation,
		RemainingQuantity: movement.RemainingQuantity, Note: movement.Note,
		CorrelationID: movement.CorrelationID, RelatedItemID: related,
	}
}

func adminMovementTypeOptions(selected string) []inventorySelectOption {
	out := []inventorySelectOption{{Value: "", Label: "All movements", Selected: selected == ""}}
	for _, movementType := range domain.MovementTypes {
		out = append(out, inventorySelectOption{
			Value: string(movementType), Label: string(movementType), Selected: string(movementType) == selected,
		})
	}
	return out
}

// adminMovementFormValuesFrom reads the shared issue/return form fields. The
// idempotency key and expected version are re-posted hidden fields.
func adminMovementFormValuesFrom(c *gin.Context) adminMovementFormValues {
	return adminMovementFormValues{
		Quantity:       strings.TrimSpace(c.PostForm("quantity")),
		Recipient:      strings.TrimSpace(c.PostForm("recipient")),
		Note:           strings.TrimSpace(c.PostForm("note")),
		ExpectedModAt:  strings.TrimSpace(c.PostForm("expectedModAt")),
		IdempotencyKey: strings.TrimSpace(c.PostForm(movementFormIdempotencyField)),
	}
}

// movementFormInput validates the shared fields and returns the cleaned
// quantity, note and expected version. The idempotency key is bounded by the
// service, matching the API header path.
func movementFormInput(values adminMovementFormValues, maxQuantity uint32) (uint32, string, time.Time, map[string]string) {
	fields := make(map[string]string)
	var quantity uint32
	raw := values.Quantity
	if raw == "" {
		fields["quantity"] = "Quantity is required"
	} else if value, err := strconv.ParseUint(raw, 10, 32); err != nil || value < 1 {
		fields["quantity"] = "Quantity must be at least 1"
	} else if value > uint64(maxQuantity) {
		fields["quantity"] = "Quantity cannot exceed the quantity on hand"
	} else {
		quantity = uint32(value)
	}
	var expected time.Time
	if values.ExpectedModAt == "" {
		fields["expectedModAt"] = "The form was missing its version token; reload the page"
	} else if value, ok := parseExpectedModAt(values.ExpectedModAt); !ok {
		fields["expectedModAt"] = "The version token is not a valid timestamp; reload the page"
	} else {
		expected = value
	}
	return quantity, values.Note, expected, fields
}

func (h *adminInventoryHandler) issuePage(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	item, err := h.admin.Get(c.Request.Context(), id)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	if item.DeletedFg {
		h.views.errorPage(c, http.StatusConflict, "This record is removed. Issue is available only for active records.")
		return
	}
	if strings.TrimSpace(item.GivenTo) != "" {
		h.views.errorPage(c, http.StatusConflict, "This record already carries a recipient. Issue is not available.")
		return
	}
	h.renderMovementForm(c, http.StatusOK, "admin_inventory_issue.html", false,
		adminItemViewOf(item), adminMovementFormValues{
			Quantity:       strconv.FormatUint(uint64(item.Quantity), 10),
			ExpectedModAt:  item.ModAt.UTC().Format(time.RFC3339Nano),
			IdempotencyKey: newMovementIdempotencyKey(),
		}, adminIssueModeText(item.Quantity), "", nil)
}

// adminIssueModeText explains the issue form's behaviour before an officer
// commits. The rule is quantity-based: any active lot with more than one unit on
// hand supports partial issue, and EQUID stackability metadata is advisory only.
func adminIssueModeText(onHand uint32) string {
	if onHand > 1 {
		return "This lot has more than one unit on hand: a smaller quantity reduces this record and keeps it active, and the full quantity issues the whole stack."
	}
	return "This lot has a single unit on hand: issuing it issues the whole lot and removes it from active inventory."
}

func (h *adminInventoryHandler) issueForm(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	item, err := h.admin.Get(c.Request.Context(), id)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	values := adminMovementFormValuesFrom(c)
	quantity, note, expected, fields := movementFormInput(values, item.Quantity)
	recipient := values.Recipient
	if recipient == "" {
		fields["recipient"] = "Recipient is required"
	}
	if len(fields) > 0 {
		h.renderMovementForm(c, http.StatusUnprocessableEntity, "admin_inventory_issue.html", true,
			adminItemViewOf(item), values, adminIssueModeText(item.Quantity),
			"The issue was not recorded. Correct the highlighted fields.", fields)
		return
	}
	in := domain.IssueItemInput{
		GuildBankItemID: id, ExpectedModAt: expected, Quantity: quantity,
		Recipient: recipient, Note: note, IdempotencyKey: values.IdempotencyKey,
	}
	in.Actor, in.CorrelationID, in.SourceIP = actor, GetRequestID(c), c.ClientIP()
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	result, err := h.admin.Issue(ctx, in)
	if err != nil {
		h.renderMovementFormError(c, "admin_inventory_issue.html", adminItemViewOf(item), values,
			adminIssueModeText(item.Quantity), err)
		return
	}
	h.logger.Info("admin inventory issued (html)", "request_id", GetRequestID(c),
		"user_id", actor.UserID, "item_id", id, "quantity", quantity)
	c.Redirect(http.StatusSeeOther,
		"/admin/inventory/"+strconv.FormatInt(result.Item.ID, 10)+"?notice=issued")
}

func (h *adminInventoryHandler) returnPage(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	item, err := h.admin.Get(c.Request.Context(), id)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	mode, available := adminReturnModeText(item)
	if !available {
		h.views.errorPage(c, http.StatusConflict, mode)
		return
	}
	h.renderMovementForm(c, http.StatusOK, "admin_inventory_return.html", false,
		adminItemViewOf(item), adminMovementFormValues{
			Quantity:       strconv.FormatUint(uint64(item.Quantity), 10),
			ExpectedModAt:  item.ModAt.UTC().Format(time.RFC3339Nano),
			IdempotencyKey: newMovementIdempotencyKey(),
		}, mode, "", nil)
}

// adminReturnModeText states what a return will do and whether it is available
// at all for the current record state.
func adminReturnModeText(item domain.AdminInventoryItem) (string, bool) {
	if !item.DeletedFg {
		if strings.TrimSpace(item.GivenTo) != "" {
			return "This record carries a recipient but is active. Remove it before a return.", false
		}
		return "This record is active: the returned quantity is added to it.", true
	}
	if strings.TrimSpace(item.GivenTo) == "" {
		return "This record was removed administratively, so it cannot be returned. Restore it instead.", false
	}
	return "Returning the full issued quantity restores this record and clears the recipient. A smaller quantity creates a new active record and leaves this one in history.", true
}

func (h *adminInventoryHandler) returnForm(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	item, err := h.admin.Get(c.Request.Context(), id)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	mode, available := adminReturnModeText(item)
	if !available {
		h.views.errorPage(c, http.StatusConflict, mode)
		return
	}
	values := adminMovementFormValuesFrom(c)
	quantity, note, expected, fields := movementFormInput(values, item.Quantity)
	if len(fields) > 0 {
		h.renderMovementForm(c, http.StatusUnprocessableEntity, "admin_inventory_return.html", true,
			adminItemViewOf(item), values, mode,
			"The return was not recorded. Correct the highlighted fields.", fields)
		return
	}
	in := domain.ReturnItemInput{
		GuildBankItemID: id, ExpectedModAt: expected, Quantity: quantity,
		Recipient: values.Recipient, Note: note, IdempotencyKey: values.IdempotencyKey,
	}
	in.Actor, in.CorrelationID, in.SourceIP = actor, GetRequestID(c), c.ClientIP()
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	result, err := h.admin.Return(ctx, in)
	if err != nil {
		h.renderMovementFormError(c, "admin_inventory_return.html", adminItemViewOf(item), values,
			mode, err)
		return
	}
	h.logger.Info("admin inventory returned (html)", "request_id", GetRequestID(c),
		"user_id", actor.UserID, "item_id", id, "quantity", quantity)
	notice := "returned"
	if result.CreatedCount > 0 {
		notice = "returned-new"
	}
	c.Redirect(http.StatusSeeOther,
		"/admin/inventory/"+strconv.FormatInt(result.Item.ID, 10)+"?notice="+notice)
}

// renderMovementForm renders an Issue or Return form with its banner, field
// errors and the pre-submit description of the action. A re-render after a
// validation failure keeps the officer's idempotency key so a retry stays
// idempotent.
func (h *adminInventoryHandler) renderMovementForm(c *gin.Context, status int, name string, retain bool,
	item adminItemView, values adminMovementFormValues, mode string,
	banner string, fields map[string]string) {
	if !retain && values.IdempotencyKey == "" {
		values.IdempotencyKey = newMovementIdempotencyKey()
	}
	title := "Issue " + item.Name
	if name == "admin_inventory_return.html" {
		title = "Return " + item.Name
	}
	h.views.adminMovementForm(c, status, name, adminMovementFormPageData{
		pageData: pageData{
			Title: title, CSRF: CSRF(c), User: currentUserPointer(c),
			Error: banner, ErrorFields: fields,
		},
		Ready: h.admin.Ready(), Item: item, Form: values, Mode: mode,
		PartialSupported: item.Quantity > 1,
	})
}

// renderMovementFormError maps a service failure onto the form or the shared
// error page. A stale version and a duplicate idempotency key are both 409.
func (h *adminInventoryHandler) renderMovementFormError(c *gin.Context, name string, item adminItemView,
	values adminMovementFormValues, mode string, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		h.renderMovementForm(c, http.StatusUnprocessableEntity, name, true, item, values, mode,
			"The action was not recorded. Correct the highlighted fields.", validation.Fields)
	case errors.Is(err, domain.ErrConflict):
		h.renderMovementForm(c, http.StatusConflict, name, true, item, values, mode,
			"This record changed since the form was loaded, or the request was already submitted. Reload the record and try again.", nil)
	default:
		h.respondAdminPageError(c, err)
	}
}

// movementHistoryURL rebuilds the history URL with the active filters.
func movementHistoryURL(itemID int64, filter domain.MovementFilter, page int) string {
	values := url.Values{}
	if filter.MovementType != "" {
		values.Set("movementType", string(filter.MovementType))
	}
	if filter.Recipient != "" {
		values.Set("recipient", filter.Recipient)
	}
	if filter.CorrelationID != "" {
		values.Set("correlationId", filter.CorrelationID)
	}
	if filter.PageSize != 0 && filter.PageSize != service.AdminDefaultPageSize {
		values.Set("pageSize", strconv.Itoa(filter.PageSize))
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	base := "/admin/inventory/" + strconv.FormatInt(itemID, 10) + "/movements"
	if encoded := values.Encode(); encoded != "" {
		return base + "?" + encoded
	}
	return base
}

func (h *adminInventoryHandler) movementsPage(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	if !h.admin.Ready() {
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The guild inventory is temporarily unavailable. Please try again after the database connection is restored.")
		return
	}
	item, err := h.admin.Get(c.Request.Context(), id)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	filter, fields := movementFilterFrom(c, id)
	if len(fields) > 0 {
		h.views.errorPage(c, http.StatusUnprocessableEntity, adminValidationMessage(fields))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	page, err := h.admin.ListMovements(ctx, filter)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	movements := make([]adminMovementView, 0, len(page.Movements))
	for _, movement := range page.Movements {
		movements = append(movements, adminMovementViewOf(movement))
	}
	data := adminMovementHistoryPageData{
		pageData: pageData{Title: "Movement history · " + item.Name, CSRF: CSRF(c), User: currentUserPointer(c)},
		Ready:    true, Item: adminItemViewOf(item), Movements: movements,
		Filter: adminMovementFilterView{
			MovementType: string(filter.MovementType), Recipient: filter.Recipient,
			CorrelationID: filter.CorrelationID, PageSize: strconv.Itoa(filter.PageSize),
		},
		TypeOpts: adminMovementTypeOptions(string(filter.MovementType)),
		Total:    page.Total, Page: page.Page, PageSize: page.PageSize,
		TotalPages: page.TotalPages, Empty: page.Total == 0,
	}
	if page.TotalPages > 0 {
		if page.Page > 1 {
			previous := page.Page - 1
			if previous > page.TotalPages {
				previous = page.TotalPages
			}
			data.PrevURL = movementHistoryURL(id, page.Filter, previous)
			data.FirstURL = movementHistoryURL(id, page.Filter, 1)
		}
		if page.Page < page.TotalPages {
			data.NextURL = movementHistoryURL(id, page.Filter, page.Page+1)
			data.LastURL = movementHistoryURL(id, page.Filter, page.TotalPages)
		}
		start := page.Page - 2
		if start < 1 {
			start = 1
		}
		end := start + 4
		if end > page.TotalPages {
			end = page.TotalPages
			start = end - 4
			if start < 1 {
				start = 1
			}
		}
		for candidate := start; candidate <= end; candidate++ {
			data.PageLinks = append(data.PageLinks, inventoryPageLink{
				Page: candidate, URL: movementHistoryURL(id, page.Filter, candidate), Current: candidate == page.Page,
			})
		}
	}
	h.views.adminMovementHistory(c, http.StatusOK, data)
}
