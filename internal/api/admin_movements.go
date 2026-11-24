package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

// movementIdempotencyHeader is the approved transport for the request
// idempotency key (OD-06). The HTML layer uses a hidden field instead.
const movementIdempotencyHeader = "Idempotency-Key"

// movementRequest is the shared issue/return DTO. Quantity and expectedModAt are
// required; recipient is required for ISSUE and optional for RETURN. The
// remaining ledger columns are listed so a client that sends one receives a
// named 422 instead of a generic unknown-field 400.
type movementRequest struct {
	Quantity          decimalUint      `json:"quantity"`
	Recipient         *string          `json:"recipient"`
	Note              *string          `json:"note"`
	ExpectedModAt     *string          `json:"expectedModAt"`
	ID                *json.RawMessage `json:"id"`
	GuildBankItemID   *json.RawMessage `json:"guildBankItemId"`
	MovementType      *json.RawMessage `json:"movementType"`
	OccurredAt        *json.RawMessage `json:"occurredAt"`
	ActorUserID       *json.RawMessage `json:"actorUserId"`
	ActorUserName     *json.RawMessage `json:"actorUserName"`
	CorrelationID     *json.RawMessage `json:"correlationId"`
	IdempotencyKey    *json.RawMessage `json:"idempotencyKey"`
	FromLocation      *json.RawMessage `json:"fromLocation"`
	ToLocation        *json.RawMessage `json:"toLocation"`
	RemainingQuantity *json.RawMessage `json:"remainingQuantity"`
	RelatedItemID     *json.RawMessage `json:"relatedItemId"`
	SourceIP          *json.RawMessage `json:"sourceIp"`
}

// movementResponse is the safe ledger projection. It never exposes the
// idempotency key or source address.
type movementResponse struct {
	MovementID        int64               `json:"movementId"`
	GuildBankItemID   int64               `json:"guildBankItemId"`
	EQUID             uint64              `json:"equid"`
	Type              domain.MovementType `json:"type"`
	Quantity          uint32              `json:"quantity"`
	Recipient         *string             `json:"recipient"`
	ActorUserID       *int64              `json:"actorUserId"`
	ActorUserName     string              `json:"actorUserName"`
	OccurredAt        string              `json:"occurredAt"`
	FromLocation      *string             `json:"fromLocation"`
	ToLocation        *string             `json:"toLocation"`
	RemainingQuantity uint32              `json:"remainingQuantity"`
	RelatedItemID     *int64              `json:"relatedItemId"`
	Note              *string             `json:"note"`
	CorrelationID     string              `json:"correlationId"`
}

func movementResponseOf(movement domain.InventoryMovement) movementResponse {
	return movementResponse{
		MovementID: movement.MovementID, GuildBankItemID: movement.GuildBankItemID,
		EQUID: movement.EQUID, Type: movement.MovementType, Quantity: movement.Quantity,
		Recipient: nullableAdminText(movement.Recipient), ActorUserID: movement.ActorUserID,
		ActorUserName:     movement.ActorUserName,
		OccurredAt:        movement.OccurredAt.UTC().Format(time.RFC3339Nano),
		FromLocation:      nullableAdminText(movement.FromLocation),
		ToLocation:        nullableAdminText(movement.ToLocation),
		RemainingQuantity: movement.RemainingQuantity, RelatedItemID: movement.RelatedItemID,
		Note: nullableAdminText(movement.Note), CorrelationID: movement.CorrelationID,
	}
}

// movementWriteResponse builds the shared issue/return envelope. currentOnHand is
// the lot's active on-hand quantity after the write: zero for a whole-stack
// issue and the reduced quantity for a partial issue.
func movementWriteResponse(result service.AdminWriteResult) gin.H {
	body := gin.H{
		"item":          adminItemResponseOf(result.Item),
		"currentOnHand": currentOnHandOf(result.Item),
	}
	if result.Movement != nil {
		body["movement"] = movementResponseOf(*result.Movement)
	}
	return gin.H{"data": body}
}

func currentOnHandOf(item domain.AdminInventoryItem) uint32 {
	if item.DeletedFg {
		return 0
	}
	return item.Quantity
}

// movementInputFields validates the shared request fields and returns the
// cleaned quantity, recipient, note and expected version.
func movementInputFields(req movementRequest, requireRecipient bool) (uint32, string, string, time.Time, map[string]string) {
	fields := make(map[string]string)
	var quantity uint32
	if !req.Quantity.Set {
		fields["quantity"] = "quantity is required"
	} else if req.Quantity.Value < 1 {
		fields["quantity"] = "quantity must be at least 1"
	} else if req.Quantity.Value > uint64(service.AdminMaxSpellQuantity) {
		fields["quantity"] = "quantity exceeds the storage range"
	} else {
		quantity = uint32(req.Quantity.Value)
	}
	recipient := ""
	if req.Recipient != nil {
		recipient = strings.TrimSpace(*req.Recipient)
	}
	if requireRecipient && recipient == "" {
		fields["recipient"] = "recipient is required"
	}
	note := ""
	if req.Note != nil {
		note = strings.TrimSpace(*req.Note)
	}
	var expected time.Time
	if req.ExpectedModAt == nil || strings.TrimSpace(*req.ExpectedModAt) == "" {
		fields["expectedModAt"] = "expectedModAt is required"
	} else if value, ok := parseExpectedModAt(*req.ExpectedModAt); !ok {
		fields["expectedModAt"] = "expectedModAt must be RFC 3339 in UTC"
	} else {
		expected = value
	}
	for field, message := range immutableFields([]struct {
		name string
		raw  *json.RawMessage
	}{
		{"id", req.ID}, {"guildBankItemId", req.GuildBankItemID},
		{"movementType", req.MovementType}, {"occurredAt", req.OccurredAt},
		{"actorUserId", req.ActorUserID}, {"actorUserName", req.ActorUserName},
		{"correlationId", req.CorrelationID}, {"idempotencyKey", req.IdempotencyKey},
		{"fromLocation", req.FromLocation}, {"toLocation", req.ToLocation},
		{"remainingQuantity", req.RemainingQuantity}, {"relatedItemId", req.RelatedItemID},
		{"sourceIp", req.SourceIP},
	}) {
		fields[field] = message
	}
	return quantity, recipient, note, expected, fields
}

func issueInputFrom(id int64, req movementRequest) (domain.IssueItemInput, *requestError) {
	quantity, recipient, note, expected, fields := movementInputFields(req, true)
	if len(fields) > 0 {
		return domain.IssueItemInput{}, validationRequestError(fields)
	}
	return domain.IssueItemInput{
		GuildBankItemID: id, ExpectedModAt: expected, Quantity: quantity,
		Recipient: recipient, Note: note,
	}, nil
}

func returnInputFrom(id int64, req movementRequest) (domain.ReturnItemInput, *requestError) {
	quantity, recipient, note, expected, fields := movementInputFields(req, false)
	if len(fields) > 0 {
		return domain.ReturnItemInput{}, validationRequestError(fields)
	}
	return domain.ReturnItemInput{
		GuildBankItemID: id, ExpectedModAt: expected, Quantity: quantity,
		Recipient: recipient, Note: note,
	}, nil
}

// movementIdempotencyKey reads the approved header. Length and character bounds
// are enforced by the service so an over-long key is a named 422 rather than a
// silently truncated retry key.
func movementIdempotencyKey(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader(movementIdempotencyHeader))
}

func (h *adminInventoryHandler) issue(c *gin.Context) {
	id, ok := parseAdminID(c, false)
	if !ok {
		return
	}
	var req movementRequest
	if err := decodeAdminJSON(c, &req); err != nil {
		writeRequestError(c, err)
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	in, reqErr := issueInputFrom(id, req)
	if reqErr != nil {
		writeRequestError(c, reqErr)
		return
	}
	in.Actor, in.CorrelationID, in.SourceIP = actor, GetRequestID(c), c.ClientIP()
	in.IdempotencyKey = movementIdempotencyKey(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	result, err := h.admin.Issue(ctx, in)
	if err != nil {
		h.respondAdminError(c, err)
		return
	}
	h.logger.Info("admin inventory issued", "request_id", GetRequestID(c),
		"user_id", actor.UserID, "item_id", id, "quantity", in.Quantity,
		"partial", !result.Item.DeletedFg)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, movementWriteResponse(result))
}

func (h *adminInventoryHandler) returnItem(c *gin.Context) {
	id, ok := parseAdminID(c, false)
	if !ok {
		return
	}
	var req movementRequest
	if err := decodeAdminJSON(c, &req); err != nil {
		writeRequestError(c, err)
		return
	}
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	in, reqErr := returnInputFrom(id, req)
	if reqErr != nil {
		writeRequestError(c, reqErr)
		return
	}
	in.Actor, in.CorrelationID, in.SourceIP = actor, GetRequestID(c), c.ClientIP()
	in.IdempotencyKey = movementIdempotencyKey(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminWriteTimeout)
	defer cancel()
	result, err := h.admin.Return(ctx, in)
	if err != nil {
		h.respondAdminError(c, err)
		return
	}
	h.logger.Info("admin inventory returned", "request_id", GetRequestID(c),
		"user_id", actor.UserID, "item_id", id, "quantity", in.Quantity)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, movementWriteResponse(result))
}

// movementFilterFrom parses the documented history query parameters. The lot ID
// always comes from the path; the remaining filters are optional.
func movementFilterFrom(c *gin.Context, lotID int64) (domain.MovementFilter, map[string]string) {
	filter := domain.MovementFilter{
		GuildBankItemID: lotID,
		MovementType:    domain.MovementType(strings.TrimSpace(c.Query("movementType"))),
		Recipient:       strings.TrimSpace(c.Query("recipient")),
		CorrelationID:   strings.TrimSpace(c.Query("correlationId")),
		Page:            1, PageSize: service.AdminDefaultPageSize,
	}
	fields := make(map[string]string)
	if filter.MovementType != "" && !filter.MovementType.Valid() {
		fields["movementType"] = "movementType must be one of the approved values"
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
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			fields["from"] = "from must be RFC 3339 in UTC"
		} else {
			filter.From = value.UTC()
		}
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			fields["to"] = "to must be RFC 3339 in UTC"
		} else {
			filter.To = value.UTC()
		}
	}
	if !filter.From.IsZero() && !filter.To.IsZero() && filter.From.After(filter.To) {
		fields["from"] = "from must not be after to"
	}
	return filter, fields
}

func (h *adminInventoryHandler) movements(c *gin.Context) {
	id, ok := parseAdminID(c, false)
	if !ok {
		return
	}
	filter, fields := movementFilterFrom(c, id)
	if len(fields) > 0 {
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"one or more fields are invalid", fields)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	page, err := h.admin.ListMovements(ctx, filter)
	if err != nil {
		h.respondAdminError(c, err)
		return
	}
	movements := make([]movementResponse, 0, len(page.Movements))
	for _, movement := range page.Movements {
		movements = append(movements, movementResponseOf(movement))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{"movements": movements},
		"meta": gin.H{
			"guildBankItemId": page.Filter.GuildBankItemID,
			"page":            page.Page, "pageSize": page.PageSize, "total": page.Total,
			"totalPages": page.TotalPages, "movementType": page.Filter.MovementType,
			"recipient": page.Filter.Recipient, "correlationId": page.Filter.CorrelationID,
		},
	})
}

func (h *adminInventoryHandler) movement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("movementID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusNotFound, "not_found", "movement not found")
		return
	}
	movement, err := h.admin.GetMovement(c.Request.Context(), id)
	if err != nil {
		h.respondAdminError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"movement": movementResponseOf(movement)}})
}
