package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

type auditEntryResponse struct {
	ID            int64           `json:"id"`
	EventDate     string          `json:"eventDate"`
	UserID        *int64          `json:"userId"`
	UserName      string          `json:"userName"`
	EventType     string          `json:"eventType"`
	TableName     string          `json:"tableName"`
	RecordID      string          `json:"recordId"`
	CorrelationID string          `json:"correlationId"`
	SourceIP      string          `json:"sourceIP"`
	Notes         string          `json:"notes"`
	OldValues     json.RawMessage `json:"oldValues"`
	NewValues     json.RawMessage `json:"newValues"`
}

func auditEntryResponseOf(entry domain.AuditEntry) auditEntryResponse {
	var userID *int64
	if entry.UserID != 0 {
		value := entry.UserID
		userID = &value
	}
	oldValues := entry.OldValues
	if len(oldValues) == 0 {
		oldValues = json.RawMessage("{}")
	}
	newValues := entry.NewValues
	if len(newValues) == 0 {
		newValues = json.RawMessage("{}")
	}
	return auditEntryResponse{
		ID: entry.AuditID, EventDate: entry.EventDate.UTC().Format(time.RFC3339Nano),
		UserID: userID, UserName: entry.UserName, EventType: entry.EventType,
		TableName: entry.TableName, RecordID: entry.RecordID,
		CorrelationID: entry.CorrelationID, SourceIP: entry.SourceIP,
		Notes: entry.Notes, OldValues: oldValues, NewValues: newValues,
	}
}

// auditFilterFrom parses the documented audit query parameters. Dates are
// inclusive UTC calendar days.
func auditFilterFrom(c *gin.Context) (domain.AuditFilter, map[string]string) {
	filter := domain.AuditFilter{
		EventType:     strings.TrimSpace(c.Query("eventType")),
		User:          strings.TrimSpace(c.Query("user")),
		Table:         strings.TrimSpace(c.Query("table")),
		RecordID:      strings.TrimSpace(c.Query("recordId")),
		CorrelationID: strings.TrimSpace(c.Query("correlationId")),
		Page:          1, PageSize: service.AdminDefaultPageSize,
	}
	fields := make(map[string]string)
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		value, err := time.Parse("2006-01-02", raw)
		if err != nil {
			fields["from"] = "from must be YYYY-MM-DD"
		} else {
			value = value.UTC()
			filter.From = &value
		}
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		value, err := time.Parse("2006-01-02", raw)
		if err != nil {
			fields["to"] = "to must be YYYY-MM-DD"
		} else {
			value = value.UTC()
			filter.To = &value
		}
	}
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > service.AdminMaxPage {
			fields["page"] = "page must be between 1 and 10000"
		} else {
			filter.Page = value
		}
	}
	if raw := strings.TrimSpace(c.Query("pageSize")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > service.AdminAuditMaxPageSize {
			fields["pageSize"] = "pageSize must be between 1 and 200"
		} else {
			filter.PageSize = value
		}
	}
	return filter, fields
}

func (h *adminInventoryHandler) auditList(c *gin.Context) {
	filter, fields := auditFilterFrom(c)
	if len(fields) > 0 {
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"one or more fields are invalid", fields)
		return
	}
	page, err := h.admin.ListAudit(c.Request.Context(), filter)
	if err != nil {
		h.respondAdminError(c, err)
		return
	}
	entries := make([]auditEntryResponse, 0, len(page.Entries))
	for _, entry := range page.Entries {
		entries = append(entries, auditEntryResponseOf(entry))
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{"entries": entries},
		"meta": gin.H{
			"page": page.Page, "pageSize": page.PageSize, "total": page.Total,
			"totalPages": page.TotalPages,
		},
	})
}

func (h *adminInventoryHandler) auditEntry(c *gin.Context) {
	id, ok := parseAdminID(c, false)
	if !ok {
		return
	}
	entry, err := h.admin.GetAudit(c.Request.Context(), id)
	if err != nil {
		h.respondAdminError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"entry": auditEntryResponseOf(entry)}})
}

func auditURL(filter domain.AuditFilter, page int) string {
	values := url.Values{}
	if filter.EventType != "" {
		values.Set("eventType", filter.EventType)
	}
	if filter.User != "" {
		values.Set("user", filter.User)
	}
	if filter.Table != "" {
		values.Set("table", filter.Table)
	}
	if filter.RecordID != "" {
		values.Set("recordId", filter.RecordID)
	}
	if filter.CorrelationID != "" {
		values.Set("correlationId", filter.CorrelationID)
	}
	if filter.From != nil {
		values.Set("from", filter.From.Format("2006-01-02"))
	}
	if filter.To != nil {
		values.Set("to", filter.To.Format("2006-01-02"))
	}
	if filter.PageSize != 0 && filter.PageSize != service.AdminDefaultPageSize {
		values.Set("pageSize", strconv.Itoa(filter.PageSize))
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if encoded := values.Encode(); encoded != "" {
		return "/admin/audit?" + encoded
	}
	return "/admin/audit"
}

func (h *adminInventoryHandler) auditPage(c *gin.Context) {
	filter, fields := auditFilterFrom(c)
	if len(fields) > 0 {
		h.views.errorPage(c, http.StatusUnprocessableEntity, adminValidationMessage(fields))
		return
	}
	page, err := h.admin.ListAudit(c.Request.Context(), filter)
	if err != nil {
		h.respondAdminPageError(c, err)
		return
	}
	entries := make([]auditEntryView, 0, len(page.Entries))
	for _, entry := range page.Entries {
		entries = append(entries, auditEntryViewOf(entry))
	}
	data := adminAuditPageData{
		pageData: pageData{Title: "Audit", CSRF: CSRF(c), User: currentUserPointer(c)},
		Entries:  entries,
		Filter: auditFilterView{
			EventType: filter.EventType, User: filter.User, Table: filter.Table,
			RecordID: filter.RecordID, CorrelationID: filter.CorrelationID,
			From: auditDateText(filter.From), To: auditDateText(filter.To),
		},
		Total: page.Total, Page: page.Page, PageSize: page.PageSize,
		TotalPages: page.TotalPages, Empty: page.Total == 0,
		EventTypeOpts: auditEventTypeOptions(filter.EventType),
	}
	if page.TotalPages > 0 {
		if page.Page > 1 {
			previous := page.Page - 1
			if previous > page.TotalPages {
				previous = page.TotalPages
			}
			data.PrevURL = auditURL(page.Filter, previous)
			data.FirstURL = auditURL(page.Filter, 1)
		}
		if page.Page < page.TotalPages {
			data.NextURL = auditURL(page.Filter, page.Page+1)
			data.LastURL = auditURL(page.Filter, page.TotalPages)
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
				Page: candidate, URL: auditURL(page.Filter, candidate), Current: candidate == page.Page,
			})
		}
	}
	h.views.adminAudit(c, http.StatusOK, data)
}

func auditDateText(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format("2006-01-02")
}

func auditEventTypeOptions(selected string) []inventorySelectOption {
	out := []inventorySelectOption{{Value: "", Label: "All events", Selected: selected == ""}}
	for _, eventType := range service.AdminAuditEventTypes {
		out = append(out, inventorySelectOption{Value: eventType, Label: eventType, Selected: eventType == selected})
	}
	return out
}
