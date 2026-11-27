package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

func (h *adminInventoryHandler) page(c *gin.Context) {
	filter, fields := adminFilterFrom(c)
	if len(fields) > 0 {
		h.views.errorPage(c, http.StatusUnprocessableEntity, adminValidationMessage(fields))
		return
	}
	if !h.admin.Ready() {
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The guild inventory is temporarily unavailable. Please try again after the database connection is restored.")
		return
	}
	ctx := c.Request.Context()
	page, err := h.admin.List(ctx, filter)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	groups, err := h.admin.LocationGroups(ctx)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	data := adminListPageOf(page, groups)
	data.pageData = pageData{Title: "Admin inventory", CSRF: CSRF(c), User: currentUserPointer(c)}
	data.Notice = adminNotice(c.Query("notice"))
	if c.Query("notice") == "bulk-removed" {
		data.Notice = adminBulkRemovedNotice(c.Query("count"))
	}
	h.views.adminInventoryList(c, http.StatusOK, data)
}

// adminBulkRemovedNotice renders the bounded, machine-generated result of a
// page-local bulk removal. The raw query value is parsed as a number; no text is
// reflected.
func adminBulkRemovedNotice(raw string) string {
	count, err := strconv.Atoi(raw)
	if err != nil || count < 1 || count > service.AdminMaxBulkRemove {
		return "The selected records were removed."
	}
	if count == 1 {
		return "1 record was removed."
	}
	return fmt.Sprintf("%d records were removed.", count)
}

func adminValidationMessage(fields map[string]string) string {
	message := ""
	for _, key := range sortedKeys(fields) {
		if message != "" {
			return message + " " + fields[key]
		}
		message = fields[key]
	}
	if message == "" {
		message = "One or more filters are invalid."
	}
	return message
}

func sortedKeys(fields map[string]string) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

// adminNotice maps the fixed redirect code to fixed presentation text.
func adminNotice(code string) string {
	switch code {
	case "created":
		return "The record was added."
	case "updated":
		return "The record was updated."
	case "unchanged":
		return "No editable value changed, so nothing was written."
	case "removed":
		return "The record was removed."
	case "restored":
		return "The record was restored."
	case "issued":
		return "The issue was recorded."
	case "returned":
		return "The return was recorded."
	case "returned-new":
		return "The return was recorded as a new active record."
	case "updated-removed":
		return "The record was updated and removed."
	case "bulk-removed":
		return "The selected records were removed."
	default:
		return ""
	}
}

func (h *adminInventoryHandler) respondAdminPageError(c *gin.Context, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		h.views.errorPage(c, http.StatusUnprocessableEntity, adminValidationMessage(validation.Fields))
	case errors.Is(err, service.ErrSettingsInvalid):
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The guild date settings are invalid; an administrator must correct GuildOpenDays and BazaarDays.")
	case errors.Is(err, domain.ErrUnavailable):
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The guild inventory is temporarily unavailable. Please try again after the database connection is restored.")
	case errors.Is(err, domain.ErrConflict):
		h.views.errorPage(c, http.StatusConflict,
			"One or more selected records changed since the page was loaded. Reload the inventory list and try again.")
	case errors.Is(err, domain.ErrNotFound):
		h.views.errorPage(c, http.StatusNotFound,
			"One or more selected records no longer exist. Reload the inventory list and try again.")
	default:
		h.logger.Error("admin inventory page failed", "request_id", GetRequestID(c), "error", err)
		h.views.errorPage(c, http.StatusInternalServerError, "The inventory could not be displayed.")
	}
}

func (h *adminInventoryHandler) newPage(c *gin.Context) {
	data := adminFormPageData{
		pageData: pageData{Title: "Add inventory record", CSRF: CSRF(c), User: currentUserPointer(c)},
		Ready:    h.admin.Ready(),
		Form:     adminFormValues{ItemType: string(domain.ItemTypeItem), Quantity: "1"},
	}
	h.views.adminInventoryForm(c, http.StatusOK, data)
}

func (h *adminInventoryHandler) detailPage(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	item, err := h.admin.Get(ctx, id)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	history, err := h.admin.ListAudit(ctx, domain.AuditFilter{
		Table: "GuildBankItems", RecordID: strconv.FormatInt(id, 10),
		Page: 1, PageSize: 20,
	})
	if err != nil {
		h.logger.Warn("record history unavailable", "request_id", GetRequestID(c), "error", err)
	}
	entries := make([]auditEntryView, 0, len(history.Entries))
	for _, entry := range history.Entries {
		entries = append(entries, auditEntryViewOf(entry))
	}
	nameWarning := ""
	if c.Query("nameWarning") == "1" {
		// Fixed advisory text: the record always stores the metadata name, so no
		// user-supplied value is reflected back into the page.
		nameWarning = "The name you entered differs from this item's metadata name. " +
			"The record is stored with the metadata name shown above, because item metadata always takes precedence."
	}
	pendingRows := make([]adminRequestRowView, 0, 4)
	pending, err := h.admin.PendingRequestsForLot(ctx, id, 10)
	if err != nil {
		h.logger.Warn("pending requests for lot unavailable", "request_id", GetRequestID(c), "error", err)
	}
	now := time.Now().UTC()
	for _, request := range pending {
		pendingRows = append(pendingRows, adminRequestRowOf(request, 0, now))
	}
	h.views.adminInventoryDetail(c, http.StatusOK, adminDetailPageData{
		pageData:           pageData{Title: item.Name, CSRF: CSRF(c), User: currentUserPointer(c)},
		Item:               adminItemViewOf(item),
		History:            entries,
		Notice:             adminNotice(c.Query("notice")),
		ReturnTo:           adminReturnTo(c),
		NameWarning:        nameWarning,
		PendingRequests:    pendingRows,
		PendingRequestsURL: "/admin/requests?item=" + url.QueryEscape(item.Name),
	})
}

func (h *adminInventoryHandler) editPage(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	item, err := h.admin.Get(c.Request.Context(), id)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	view := adminItemViewOf(item)
	h.views.adminInventoryForm(c, http.StatusOK, adminFormPageData{
		pageData: pageData{Title: "Edit " + item.Name, CSRF: CSRF(c), User: currentUserPointer(c)},
		Ready:    true,
		Edit:     true,
		Item:     view,
		Form:     adminFormValuesOf(view),
	})
}

func adminFormValuesOf(item adminItemView) adminFormValues {
	return adminFormValues{
		ItemType: item.ItemType, EQUID: strconv.FormatUint(item.EQUID, 10),
		Quantity:       strconv.FormatUint(uint64(item.Quantity), 10),
		SourceLocation: item.SourceLocation, GivenTo: item.GivenTo,
		DateOpen: item.DateOpenISO, DateBazaar: item.DateBazaarISO,
		ExpectedModAt: item.ModAtValue,
	}
}

func (h *adminInventoryHandler) removePage(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	item, err := h.admin.Get(c.Request.Context(), id)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	view := adminItemViewOf(item)
	if view.Deleted {
		h.views.errorPage(c, http.StatusConflict, "This record is already removed.")
		return
	}
	h.views.adminRemove(c, http.StatusOK, adminRemovePageData{
		pageData: pageData{Title: "Remove " + item.Name, CSRF: CSRF(c), User: currentUserPointer(c)},
		Item:     view,
		ReturnTo: adminReturnTo(c),
	})
}

// restorePage renders the Restore confirmation for a row the list may restore.
// It writes nothing; the POST goes to the existing restoreForm workflow. The
// guards mirror the action's eligibility so a crafted URL cannot reach a
// confirmation page for an active, issued or consolidated row.
func (h *adminInventoryHandler) restorePage(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	item, err := h.admin.Get(c.Request.Context(), id)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	view := adminItemViewOf(item)
	switch {
	case !view.Deleted:
		h.views.errorPage(c, http.StatusConflict, "This record is not removed.")
		return
	case strings.TrimSpace(item.GivenTo) != "":
		h.views.errorPage(c, http.StatusConflict, "This record is issued to someone; use Return instead of Restore.")
		return
	case item.ConsolidatedInto != nil:
		h.views.errorPage(c, http.StatusConflict, "This record was consolidated into another row and cannot be restored.")
		return
	}
	h.views.adminRestore(c, http.StatusOK, adminRestorePageData{
		pageData: pageData{Title: "Restore " + item.Name, CSRF: CSRF(c), User: currentUserPointer(c)},
		Item:     view,
		ReturnTo: adminReturnTo(c),
	})
}

func createInputFromForm(c *gin.Context) (domain.CreateItemInput, adminFormValues, map[string]string) {
	values := adminFormValues{
		ItemType:       c.PostForm("itemType"),
		EQUID:          strings.TrimSpace(c.PostForm("equid")),
		Quantity:       strings.TrimSpace(c.PostForm("quantity")),
		SourceLocation: strings.TrimSpace(c.PostForm("sourceLocation")),
		GivenTo:        strings.TrimSpace(c.PostForm("givenTo")),
		Name:           strings.TrimSpace(c.PostForm("name")),
		DateOpen:       strings.TrimSpace(c.PostForm("dateOpen")),
		DateBazaar:     strings.TrimSpace(c.PostForm("dateBazaar")),
	}
	fields := make(map[string]string)
	in := domain.CreateItemInput{}
	switch strings.ToLower(values.ItemType) {
	case "item":
		in.ItemType = domain.ItemTypeItem
	case "spell":
		in.ItemType = domain.ItemTypeSpell
	default:
		fields["itemType"] = "Choose Item or Spell"
	}
	if values.EQUID == "" {
		// Name-first: the service resolves an exact item name. An omitted EQUID is
		// accepted here; the service reports a validation error only when neither
		// an EQUID nor a name is supplied.
	} else if value, err := strconv.ParseUint(values.EQUID, 10, 64); err != nil || value < 1 || value > 4294967295 {
		fields["equid"] = "EQUID must be 1-4294967295"
	} else if domain.IsUnresolvedEQUID(value) {
		fields["equid"] = "4294967295 is reserved for unresolved identities"
	} else {
		in.EQUID = value
	}
	if values.Quantity == "" {
		fields["quantity"] = "Quantity is required"
	} else if value, err := strconv.ParseUint(values.Quantity, 10, 32); err != nil || value < 1 {
		fields["quantity"] = "Quantity must be at least 1"
	} else {
		in.Quantity = uint32(value)
	}
	if values.SourceLocation != "" {
		in.SourceLocation = domain.OptionalString{Value: &values.SourceLocation, Set: true}
	} else {
		in.SourceLocation = domain.OptionalString{Set: true}
	}
	if values.GivenTo != "" {
		in.GivenTo = domain.OptionalString{Value: &values.GivenTo, Set: true}
	} else {
		in.GivenTo = domain.OptionalString{Set: true}
	}
	in.Name = values.Name
	in.DateOpen = optionalDateFromForm("dateOpen", values.DateOpen, false, fields)
	in.DateBazaar = optionalDateFromForm("dateBazaar", values.DateBazaar, false, fields)
	return in, values, fields
}

// optionalDateFromForm learns whether an empty date input means "absent"
// (create: use the guild default) or "reset" (edit: null).
func optionalDateFromForm(field, raw string, emptyMeansReset bool, fields map[string]string) domain.OptionalDate {
	if raw == "" {
		if emptyMeansReset {
			return domain.OptionalDate{Set: true}
		}
		return domain.OptionalDate{}
	}
	value, err := time.Parse("2006-01-02", raw)
	if err != nil {
		fields[field] = "Use YYYY-MM-DD or leave blank"
		return domain.OptionalDate{Set: true}
	}
	return domain.OptionalDate{Value: &value, Set: true}
}

func updateInputFromForm(c *gin.Context, id int64) (domain.UpdateItemInput, adminFormValues, map[string]string) {
	values := adminFormValues{
		ItemType:       c.PostForm("itemType"),
		EQUID:          strings.TrimSpace(c.PostForm("equid")),
		Quantity:       strings.TrimSpace(c.PostForm("quantity")),
		SourceLocation: strings.TrimSpace(c.PostForm("sourceLocation")),
		GivenTo:        strings.TrimSpace(c.PostForm("givenTo")),
		Name:           strings.TrimSpace(c.PostForm("name")),
		DateOpen:       strings.TrimSpace(c.PostForm("dateOpen")),
		DateBazaar:     strings.TrimSpace(c.PostForm("dateBazaar")),
		ExpectedModAt:  strings.TrimSpace(c.PostForm("expectedModAt")),
	}
	fields := make(map[string]string)
	in := domain.UpdateItemInput{ID: id}
	in.ExpectedModAt, _ = parseExpectedModAt(values.ExpectedModAt)
	if values.ExpectedModAt == "" {
		fields["expectedModAt"] = "The form was missing its version token; reload the page"
	} else if in.ExpectedModAt.IsZero() {
		fields["expectedModAt"] = "The version token is not a valid timestamp; reload the page"
	}
	if values.Quantity == "" {
		fields["quantity"] = "Quantity is required"
	} else if value, err := strconv.ParseUint(values.Quantity, 10, 32); err != nil || value < 1 {
		fields["quantity"] = "Quantity must be at least 1"
	} else {
		in.Quantity = uint32(value)
	}
	if values.SourceLocation != "" {
		in.SourceLocation = domain.OptionalString{Value: &values.SourceLocation, Set: true}
	} else {
		in.SourceLocation = domain.OptionalString{Set: true}
	}
	if values.GivenTo != "" {
		in.GivenTo = domain.OptionalString{Value: &values.GivenTo, Set: true}
	} else {
		in.GivenTo = domain.OptionalString{Set: true}
	}
	in.DateOpen = optionalDateFromForm("dateOpen", values.DateOpen, true, fields)
	in.DateBazaar = optionalDateFromForm("dateBazaar", values.DateBazaar, true, fields)
	return in, values, fields
}

func (h *adminInventoryHandler) createForm(c *gin.Context) {
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	in, values, fields := createInputFromForm(c)
	if len(fields) > 0 {
		h.renderForm(c, http.StatusUnprocessableEntity, false, values, domain.AdminInventoryItem{},
			"The record was not added. Correct the highlighted fields.", fields, false)
		return
	}
	in.Actor, in.CorrelationID, in.SourceIP = actor, GetRequestID(c), c.ClientIP()
	result, err := h.admin.Create(c.Request.Context(), in)
	if err != nil {
		h.renderFormError(c, false, values, domain.AdminInventoryItem{}, err)
		return
	}
	target := "/admin/inventory/" + strconv.FormatInt(result.Item.ID, 10) + "?notice=created"
	if adminHasWarning(result.Warnings, service.ItemWarningNameMismatch) {
		// Non-blocking advisory: the row was created with the metadata name.
		target += "&nameWarning=1"
	}
	c.Redirect(http.StatusSeeOther, target)
}

func (h *adminInventoryHandler) updateForm(c *gin.Context) {
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
	in, values, fields := updateInputFromForm(c, id)
	if len(fields) > 0 {
		h.renderForm(c, http.StatusUnprocessableEntity, true, values, item,
			"The record was not updated. Correct the highlighted fields.", fields, false)
		return
	}
	in.Actor, in.CorrelationID, in.SourceIP = actor, GetRequestID(c), c.ClientIP()
	if c.PostForm("action") == "save-remove" {
		in.Remove = true
	}
	result, err := h.admin.Update(c.Request.Context(), in)
	if err != nil {
		h.renderFormError(c, true, values, item, err)
		return
	}
	notice := "updated"
	if in.Remove {
		notice = "removed"
		if result.FieldsChanged {
			notice = "updated-removed"
		}
	} else if !result.Changed {
		notice = "unchanged"
	}
	c.Redirect(http.StatusSeeOther,
		"/admin/inventory/"+strconv.FormatInt(id, 10)+"?notice="+notice)
}

// renderForm renders the create or edit form with an error banner and optional
// field errors.
func (h *adminInventoryHandler) renderForm(c *gin.Context, status int, edit bool,
	values adminFormValues, item domain.AdminInventoryItem, banner string,
	fields map[string]string, conflict bool) {
	h.renderFormCandidates(c, status, edit, values, item, banner, fields, conflict, nil)
}

// renderFormCandidates renders the form and, for an ambiguous item name, the
// bounded candidate list so the officer chooses an identity instead of the
// server guessing one.
func (h *adminInventoryHandler) renderFormCandidates(c *gin.Context, status int, edit bool,
	values adminFormValues, item domain.AdminInventoryItem, banner string,
	fields map[string]string, conflict bool, candidates []domain.Item) {
	title := "Add inventory record"
	if edit {
		title = "Edit " + item.Name
	}
	h.views.adminInventoryForm(c, status, adminFormPageData{
		pageData: pageData{
			Title: title, CSRF: CSRF(c), User: currentUserPointer(c),
			Error: banner, ErrorFields: fields,
		},
		Ready: true, Edit: edit, Item: adminItemViewOf(item), Form: values,
		Conflict: conflict, Candidates: itemCandidatesOf(candidates),
	})
}

// renderFormError maps a service failure onto the form or the shared error page.
func (h *adminInventoryHandler) renderFormError(c *gin.Context, edit bool,
	values adminFormValues, item domain.AdminInventoryItem, err error) {
	var ambiguous *domain.ItemNameAmbiguousError
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &ambiguous):
		h.renderFormCandidates(c, http.StatusUnprocessableEntity, edit, values, item,
			"That item name matches more than one item. Choose the intended item.",
			map[string]string{"equid": "choose one of the matching items"}, false, ambiguous.Candidates)
	case errors.As(err, &validation):
		h.renderForm(c, http.StatusUnprocessableEntity, edit, values, item,
			"The record was not saved. Correct the highlighted fields.", validation.Fields, false)
	case errors.Is(err, domain.ErrConflict):
		h.renderForm(c, http.StatusConflict, edit, values, item,
			"This record changed since the form was loaded. Reload the edit page and try again.", nil, true)
	default:
		h.respondAdminPageError(c, err)
	}
}

func (h *adminInventoryHandler) removeForm(c *gin.Context) {
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
	expected, valid := parseExpectedModAt(c.PostForm("expectedModAt"))
	if !valid {
		h.views.adminRemove(c, http.StatusUnprocessableEntity, adminRemovePageData{
			pageData: pageData{Title: "Remove " + item.Name, CSRF: CSRF(c), User: currentUserPointer(c),
				Error: "The form was missing its version token; reload the page."},
			Item:     adminItemViewOf(item),
			ReturnTo: adminReturnToForm(c),
		})
		return
	}
	if _, err := h.admin.Remove(c.Request.Context(), id, expected, actor, GetRequestID(c), c.ClientIP()); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			h.views.adminRemove(c, http.StatusConflict, adminRemovePageData{
				pageData: pageData{Title: "Remove " + item.Name, CSRF: CSRF(c), User: currentUserPointer(c),
					Error: "This record changed since the page was loaded. Reload and try again."},
				Item:     adminItemViewOf(item),
				ReturnTo: adminReturnToForm(c),
			})
			return
		}
		h.respondAdminPageError(c, err)
		return
	}
	adminNoticeRedirect(c, adminReturnToForm(c), "/admin/inventory/"+strconv.FormatInt(id, 10), "removed")
}

func (h *adminInventoryHandler) restoreForm(c *gin.Context) {
	id, ok := parseAdminID(c, true)
	if !ok {
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	expected, valid := parseExpectedModAt(c.PostForm("expectedModAt"))
	if !valid {
		h.views.errorPage(c, http.StatusUnprocessableEntity,
			"The form was missing its version token; reload the record page and try again.")
		return
	}
	if _, err := h.admin.Restore(c.Request.Context(), id, expected, actor, GetRequestID(c), c.ClientIP()); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			h.views.errorPage(c, http.StatusConflict,
				"This record changed since the page was loaded. Reload the record page and try again.")
			return
		}
		h.respondAdminPageError(c, err)
		return
	}
	adminNoticeRedirect(c, adminReturnToForm(c), "/admin/inventory/"+strconv.FormatInt(id, 10), "restored")
}

// bulkTargetsFrom reads the page-local bulk-removal selection. Each checkbox
// value is the opaque "<id>|<modAt>" token rendered by the list, so a stale page
// can never remove a record it did not display.
func bulkTargetsFrom(c *gin.Context) ([]domain.RemoveItemTarget, []string, map[string]string) {
	raw := c.PostFormArray("remove")
	fields := make(map[string]string)
	targets := make([]domain.RemoveItemTarget, 0, len(raw))
	tokens := make([]string, 0, len(raw))
	switch {
	case len(raw) == 0:
		fields["remove"] = "Select at least one record to remove."
		return nil, nil, fields
	case len(raw) > service.AdminMaxBulkRemove:
		fields["remove"] = fmt.Sprintf("Select at most %d records.", service.AdminMaxBulkRemove)
		return nil, nil, fields
	}
	seen := make(map[int64]bool, len(raw))
	for _, value := range raw {
		idPart, modPart, ok := strings.Cut(value, "|")
		if !ok {
			fields["remove"] = "One selection token is malformed; reload the inventory list."
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSpace(idPart), 10, 64)
		if err != nil || id <= 0 {
			fields["remove"] = "One selection token has an invalid ID; reload the inventory list."
			continue
		}
		expected, valid := parseExpectedModAt(modPart)
		if !valid {
			fields["remove"] = "One selection token has an invalid version; reload the inventory list."
			continue
		}
		if seen[id] {
			fields["remove"] = "A record was selected more than once; reload the inventory list."
			continue
		}
		seen[id] = true
		targets = append(targets, domain.RemoveItemTarget{ID: id, ExpectedModAt: expected})
		tokens = append(tokens, value)
	}
	if len(fields) > 0 {
		return nil, nil, fields
	}
	return targets, tokens, nil
}

// bulkRemovePreview validates the page-local selection and renders the removal
// confirmation. It writes nothing.
func (h *adminInventoryHandler) bulkRemovePreview(c *gin.Context) {
	if !h.admin.Ready() {
		h.views.errorPage(c, http.StatusServiceUnavailable,
			"The guild inventory is temporarily unavailable. Please try again after the database connection is restored.")
		return
	}
	targets, tokens, fields := bulkTargetsFrom(c)
	if len(fields) > 0 {
		h.views.errorPage(c, http.StatusUnprocessableEntity, adminValidationMessage(fields))
		return
	}
	items, err := h.admin.PreviewRemoveMany(c.Request.Context(), targets)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	views := make([]adminItemView, 0, len(items))
	for _, item := range items {
		views = append(views, adminItemViewOf(item))
	}
	h.views.adminBulkRemove(c, http.StatusOK, adminBulkRemovePageData{
		pageData: pageData{Title: "Remove records", CSRF: CSRF(c), User: currentUserPointer(c)},
		Items:    views, Targets: tokens,
		ReturnTo: adminReturnToForm(c),
	})
}

// bulkRemoveForm applies the previewed selection in one all-or-nothing
// transaction. The service re-validates every version token under lock.
func (h *adminInventoryHandler) bulkRemoveForm(c *gin.Context) {
	targets, _, fields := bulkTargetsFrom(c)
	if len(fields) > 0 {
		h.views.errorPage(c, http.StatusUnprocessableEntity, adminValidationMessage(fields))
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	result, err := h.admin.RemoveMany(ctx, targets, actor, GetRequestID(c), c.ClientIP())
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	h.logger.Info("admin inventory bulk removed", "request_id", GetRequestID(c),
		"user_id", actor.UserID, "removed", result.Removed)
	adminBulkRemovedRedirect(c, adminReturnToForm(c), result.Removed)
}
