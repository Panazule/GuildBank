package api

import (
	"errors"
	"net/http"
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
	h.views.adminInventoryList(c, http.StatusOK, data)
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
	h.views.adminInventoryDetail(c, http.StatusOK, adminDetailPageData{
		pageData: pageData{Title: item.Name, CSRF: CSRF(c), User: currentUserPointer(c)},
		Item:     adminItemViewOf(item),
		History:  entries,
		Notice:   adminNotice(c.Query("notice")),
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
		ExpectedModAt:    item.ModAtValue,
		QuantityReadOnly: item.ItemType == string(domain.ItemTypeItem),
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
		fields["equid"] = "EQUID is required"
	} else if value, err := strconv.ParseUint(values.EQUID, 10, 64); err != nil || value < 1 || value > 4294967295 {
		fields["equid"] = "EQUID must be 1-4294967295"
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
		values.QuantityReadOnly = in.ItemType == domain.ItemTypeItem
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
	c.Redirect(http.StatusSeeOther,
		"/admin/inventory/"+strconv.FormatInt(result.Item.ID, 10)+"?notice=created")
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
	current := adminItemViewOf(item)
	in, values, fields := updateInputFromForm(c, id)
	values.QuantityReadOnly = current.ItemType == string(domain.ItemTypeItem)
	if len(fields) > 0 {
		h.renderForm(c, http.StatusUnprocessableEntity, true, values, item,
			"The record was not updated. Correct the highlighted fields.", fields, false)
		return
	}
	in.Actor, in.CorrelationID, in.SourceIP = actor, GetRequestID(c), c.ClientIP()
	result, err := h.admin.Update(c.Request.Context(), in)
	if err != nil {
		h.renderFormError(c, true, values, item, err)
		return
	}
	notice := "updated"
	if !result.Changed {
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
	title := "Add inventory record"
	if edit {
		title = "Edit " + item.Name
	}
	h.views.adminInventoryForm(c, status, adminFormPageData{
		pageData: pageData{
			Title: title, CSRF: CSRF(c), User: currentUserPointer(c),
			Error: banner, ErrorFields: fields,
		},
		Ready: true, Edit: edit, Item: adminItemViewOf(item), Form: values, Conflict: conflict,
	})
}

// renderFormError maps a service failure onto the form or the shared error page.
func (h *adminInventoryHandler) renderFormError(c *gin.Context, edit bool,
	values adminFormValues, item domain.AdminInventoryItem, err error) {
	var validation *domain.ValidationError
	switch {
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
			Item: adminItemViewOf(item),
		})
		return
	}
	if _, err := h.admin.Remove(c.Request.Context(), id, expected, actor, GetRequestID(c), c.ClientIP()); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			h.views.adminRemove(c, http.StatusConflict, adminRemovePageData{
				pageData: pageData{Title: "Remove " + item.Name, CSRF: CSRF(c), User: currentUserPointer(c),
					Error: "This record changed since the page was loaded. Reload and try again."},
				Item: adminItemViewOf(item),
			})
			return
		}
		h.respondAdminPageError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/inventory/"+strconv.FormatInt(id, 10)+"?notice=removed")
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
	c.Redirect(http.StatusSeeOther, "/admin/inventory/"+strconv.FormatInt(id, 10)+"?notice=restored")
}
