package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

const adminWriteTimeout = 60 * time.Second

type adminInventoryHandler struct {
	admin  *service.AdminInventoryService
	views  *views
	logger *slog.Logger
}

func nullableAdminText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func adminItemResponseOf(item domain.AdminInventoryItem) adminItemResponse {
	now := time.Now().UTC()
	return adminItemResponse{
		ID: item.ID, EQUID: item.EQUID, Name: item.Name, ItemType: item.ItemType,
		Quantity: item.Quantity, ClassString: item.ClassString, RaceString: item.RaceString,
		MetadataMissing: item.MetadataMissingFg,
		SourceLocation:  nullableAdminText(item.SourceLocation),
		LocationGroup:   item.LocationGroup, LocationSort: item.LocationSort,
		GivenTo:      nullableAdminText(item.GivenTo),
		Deleted:      item.DeletedFg,
		NameSort:     item.NameSort,
		DateAdded:    item.DateAdded.Format("2006-01-02"),
		DateOpen:     item.DateOpen.Format("2006-01-02"),
		DateBazaar:   item.DateBazaar.Format("2006-01-02"),
		Availability: service.Availability(now, item.DateOpen, item.DateBazaar),
		ModAt:        item.ModAt.UTC().Format(time.RFC3339Nano),
		ModBy:        item.ModBy, ModByUserID: item.ModByUserID,
	}
}

func adminWarningsOf(warnings []service.ItemWarning) []adminWarningResponse {
	out := make([]adminWarningResponse, 0, len(warnings))
	for _, warning := range warnings {
		out = append(out, adminWarningResponse{Code: warning.Code, Field: warning.Field, Detail: warning.Detail})
	}
	return out
}

// adminFilterFrom parses the documented admin list query parameters. Repeated
// parameters use their first value, matching the public list.
func adminFilterFrom(c *gin.Context) (domain.AdminInventoryFilter, map[string]string) {
	filter := domain.AdminInventoryFilter{
		Search:        strings.TrimSpace(c.Query("q")),
		GivenTo:       strings.TrimSpace(c.Query("givenTo")),
		Metadata:      domain.AdminMetadataFilter(strings.TrimSpace(c.Query("metadata"))),
		LocationGroup: strings.TrimSpace(c.Query("locationGroup")),
		ItemType:      domain.ItemType(strings.TrimSpace(c.Query("itemType"))),
		Status:        domain.AdminInventoryStatus(strings.TrimSpace(c.Query("status"))),
		Sort:          domain.AdminInventorySort(strings.TrimSpace(c.Query("sort"))),
		Page:          1, PageSize: service.AdminDefaultPageSize,
	}
	fields := make(map[string]string)
	if raw := strings.TrimSpace(c.Query("id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 1 {
			fields["id"] = "id must be a positive integer"
		} else {
			filter.ID = value
		}
	}
	if raw := strings.TrimSpace(c.Query("equid")); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || value < 1 || value > 4294967295 {
			fields["equid"] = "equid must be between 1 and 4294967295"
		} else {
			filter.EQUID = value
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
		if err != nil || value < 1 || value > service.AdminMaxPageSize {
			fields["pageSize"] = "pageSize must be between 1 and 200"
		} else {
			filter.PageSize = value
		}
	}
	return filter, fields
}

func (h *adminInventoryHandler) list(c *gin.Context) {
	filter, fields := adminFilterFrom(c)
	if len(fields) > 0 {
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"one or more fields are invalid", fields)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	page, err := h.admin.List(ctx, filter)
	if err != nil {
		h.respondAdminError(c, err)
		return
	}
	items := make([]adminItemResponse, 0, len(page.Records))
	for _, record := range page.Records {
		items = append(items, adminItemResponseOf(record))
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{"items": items},
		"meta": gin.H{
			"page": page.Page, "pageSize": page.PageSize, "total": page.Total,
			"totalPages": page.TotalPages, "status": page.Filter.Status, "sort": page.Filter.Sort,
		},
	})
}

func (h *adminInventoryHandler) getItem(c *gin.Context) {
	id, ok := parseAdminID(c, false)
	if !ok {
		return
	}
	item, err := h.admin.Get(c.Request.Context(), id)
	if err != nil {
		h.respondAdminError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"item": adminItemResponseOf(item)}})
}

// parseAdminID reads :id. A malformed or non-positive ID is 404 for both API
// and HTML, never a name search.
func parseAdminID(c *gin.Context, htmlResponse bool) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		if htmlResponse {
			writeHTMLStatus(c, http.StatusNotFound, "Not found", "The requested inventory record does not exist.")
		} else {
			writeError(c, http.StatusNotFound, "not_found", "inventory item not found")
		}
		return 0, false
	}
	return id, true
}

func createInputFrom(req createItemRequest) (domain.CreateItemInput, *requestError) {
	fields := make(map[string]string)
	in := domain.CreateItemInput{}
	if req.ItemType == nil || strings.TrimSpace(*req.ItemType) == "" {
		fields["itemType"] = "itemType is required"
	} else {
		switch strings.ToLower(strings.TrimSpace(*req.ItemType)) {
		case "item":
			in.ItemType = domain.ItemTypeItem
		case "spell":
			in.ItemType = domain.ItemTypeSpell
		default:
			fields["itemType"] = "itemType must be Item or Spell"
		}
	}
	if !req.EQUID.Set {
		fields["equid"] = "equid is required"
	} else if req.EQUID.Value > 4294967295 {
		fields["equid"] = "equid must be between 1 and 4294967295"
	} else {
		in.EQUID = req.EQUID.Value
	}
	if !req.Quantity.Set {
		fields["quantity"] = "quantity is required"
	} else if req.Quantity.Value > uint64(service.AdminMaxSpellQuantity) {
		fields["quantity"] = "quantity exceeds the storage range"
	} else {
		in.Quantity = uint32(req.Quantity.Value)
	}
	if req.Name != nil {
		in.Name = *req.Name
	}
	in.SourceLocation = parseOptionalString("sourceLocation", req.SourceLocation, fields)
	in.GivenTo = parseOptionalString("givenTo", req.GivenTo, fields)
	in.DateOpen = parseOptionalDate("dateOpen", req.DateOpen, fields)
	in.DateBazaar = parseOptionalDate("dateBazaar", req.DateBazaar, fields)
	for field, message := range immutableFields([]struct {
		name string
		raw  *json.RawMessage
	}{
		{"id", req.ID}, {"dateAdded", req.DateAdded}, {"classString", req.ClassString},
		{"raceString", req.RaceString}, {"metadataMissing", req.MetadataMissing},
		{"locationGroup", req.LocationGroup}, {"locationSort", req.LocationSort},
		{"nameSort", req.NameSort}, {"deleted", req.Deleted}, {"modAt", req.ModAt},
		{"modBy", req.ModBy}, {"modByUserId", req.ModByUserID},
		{"availability", req.Availability},
	}) {
		fields[field] = message
	}
	if len(fields) > 0 {
		return in, validationRequestError(fields)
	}
	return in, nil
}

func (h *adminInventoryHandler) create(c *gin.Context) {
	var req createItemRequest
	if err := decodeAdminJSON(c, &req); err != nil {
		writeRequestError(c, err)
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	in, reqErr := createInputFrom(req)
	if reqErr != nil {
		writeRequestError(c, reqErr)
		return
	}
	in.Actor, in.CorrelationID, in.SourceIP = actor, GetRequestID(c), c.ClientIP()
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	result, err := h.admin.Create(ctx, in)
	if err != nil {
		h.respondAdminError(c, err)
		return
	}
	h.logger.Info("admin inventory created", "request_id", GetRequestID(c),
		"user_id", actor.UserID, "created", result.CreatedCount)
	c.Header("Location", "/api/v1/admin/items/"+strconv.FormatInt(result.Item.ID, 10))
	c.Header("Cache-Control", "no-store")
	body := gin.H{
		"item": adminItemResponseOf(result.Item), "createdCount": result.CreatedCount,
		"warnings": adminWarningsOf(result.Warnings),
	}
	if result.CreatedCount > 0 {
		body["createdIds"] = result.CreatedIDs
	}
	c.JSON(http.StatusCreated, gin.H{"data": body})
}

func (h *adminInventoryHandler) update(c *gin.Context) {
	id, ok := parseAdminID(c, false)
	if !ok {
		return
	}
	var req updateItemRequest
	if err := decodeAdminJSON(c, &req); err != nil {
		writeRequestError(c, err)
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	in, reqErr := updateInputFrom(id, req)
	if reqErr != nil {
		writeRequestError(c, reqErr)
		return
	}
	in.Actor, in.CorrelationID, in.SourceIP = actor, GetRequestID(c), c.ClientIP()
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	result, err := h.admin.Update(ctx, in)
	if err != nil {
		h.respondAdminError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"item": adminItemResponseOf(result.Item), "changed": result.Changed,
		"warnings": adminWarningsOf(result.Warnings),
	}})
}

func updateInputFrom(id int64, req updateItemRequest) (domain.UpdateItemInput, *requestError) {
	fields := make(map[string]string)
	in := domain.UpdateItemInput{ID: id}
	if req.ExpectedModAt == nil || strings.TrimSpace(*req.ExpectedModAt) == "" {
		fields["expectedModAt"] = "expectedModAt is required"
	} else if value, ok := parseExpectedModAt(*req.ExpectedModAt); !ok {
		fields["expectedModAt"] = "expectedModAt must be RFC 3339 in UTC"
	} else {
		in.ExpectedModAt = value
	}
	if !req.Quantity.Set {
		fields["quantity"] = "quantity is required"
	} else if req.Quantity.Value > uint64(service.AdminMaxSpellQuantity) {
		fields["quantity"] = "quantity exceeds the storage range"
	} else {
		in.Quantity = uint32(req.Quantity.Value)
	}
	in.SourceLocation = parseOptionalString("sourceLocation", req.SourceLocation, fields)
	in.GivenTo = parseOptionalString("givenTo", req.GivenTo, fields)
	in.DateOpen = parseOptionalDate("dateOpen", req.DateOpen, fields)
	in.DateBazaar = parseOptionalDate("dateBazaar", req.DateBazaar, fields)
	for field, message := range immutableFields([]struct {
		name string
		raw  *json.RawMessage
	}{
		{"id", req.ID}, {"equid", req.EQUID}, {"name", req.Name}, {"itemType", req.ItemType},
		{"dateAdded", req.DateAdded}, {"classString", req.ClassString},
		{"raceString", req.RaceString}, {"metadataMissing", req.MetadataMissing},
		{"locationGroup", req.LocationGroup}, {"locationSort", req.LocationSort},
		{"nameSort", req.NameSort}, {"modBy", req.ModBy}, {"modByUserId", req.ModByUserID},
	}) {
		fields[field] = message
	}
	if len(fields) > 0 {
		return in, validationRequestError(fields)
	}
	return in, nil
}

func (h *adminInventoryHandler) remove(c *gin.Context) {
	h.changeState(c, true)
}

func (h *adminInventoryHandler) restore(c *gin.Context) {
	h.changeState(c, false)
}

func (h *adminInventoryHandler) changeState(c *gin.Context, deleteRow bool) {
	id, ok := parseAdminID(c, false)
	if !ok {
		return
	}
	raw := strings.TrimSpace(c.Query("expectedModAt"))
	if raw == "" {
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"one or more fields are invalid",
			map[string]string{"expectedModAt": "expectedModAt is required"})
		return
	}
	expected, ok := parseExpectedModAt(raw)
	if !ok {
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"one or more fields are invalid",
			map[string]string{"expectedModAt": "expectedModAt must be RFC 3339 in UTC"})
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	var result service.AdminWriteResult
	var err error
	if deleteRow {
		result, err = h.admin.Remove(ctx, id, expected, actor, GetRequestID(c), c.ClientIP())
	} else {
		result, err = h.admin.Restore(ctx, id, expected, actor, GetRequestID(c), c.ClientIP())
	}
	if err != nil {
		h.respondAdminError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"item": adminItemResponseOf(result.Item), "deleted": result.Item.DeletedFg,
	}})
}
