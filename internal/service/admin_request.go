package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Panazule/guildbank/internal/domain"
)

// requestApprovalIdempotencyKey builds the deterministic ledger key for an
// approval. A second approval of the same request can never write a second
// movement because the request row is locked first, and this key is the
// backstop if that guard is ever bypassed (design section 9.4).
func requestApprovalIdempotencyKey(requestID int64) string {
	return "req-" + strconv.FormatInt(requestID, 10)
}

// ListRequests reads one bounded page of the officer queue. The queue is
// oldest-first so the oldest request cannot starve; the member list keeps the
// repository's newest-first default.
func (s *AdminInventoryService) ListRequests(ctx context.Context, filter domain.RequestFilter) (domain.RequestPage, error) {
	if err := s.requireReady(); err != nil {
		return domain.RequestPage{}, err
	}
	if filter.Status != "" && !filter.Status.Valid() {
		return domain.RequestPage{}, domain.NewValidationError("status", "status must be one of the approved values")
	}
	if filter.RequesterKind != "" && !filter.RequesterKind.Valid() {
		return domain.RequestPage{}, domain.NewValidationError("requesterKind", "requesterKind must be one of the approved values")
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Page > domain.RequestMaxPage {
		return domain.RequestPage{}, domain.NewValidationError("page",
			fmt.Sprintf("page must be between 1 and %d", domain.RequestMaxPage))
	}
	if filter.PageSize <= 0 {
		filter.PageSize = domain.RequestDefaultPageSize
	}
	if filter.PageSize > domain.RequestMaxPageSize {
		return domain.RequestPage{}, domain.NewValidationError("pageSize",
			fmt.Sprintf("pageSize must be between 1 and %d", domain.RequestMaxPageSize))
	}
	if !filter.From.IsZero() && !filter.To.IsZero() && filter.From.After(filter.To) {
		return domain.RequestPage{}, domain.NewValidationError("from", "from must not be after to")
	}
	filter.OldestFirst = true
	return s.store.ListRequests(ctx, filter)
}

// GetRequest reads one request for the officer decision page. A missing row is
// ErrNotFound, which the handler renders as 404.
func (s *AdminInventoryService) GetRequest(ctx context.Context, requestID int64) (domain.InventoryRequest, error) {
	if err := s.requireReady(); err != nil {
		return domain.InventoryRequest{}, err
	}
	if requestID <= 0 {
		return domain.InventoryRequest{}, domain.ErrNotFound
	}
	return s.store.GetRequest(ctx, requestID)
}

// PendingRequestsForLot lists the oldest pending requests for the item-detail
// panel, bounded to a small read.
func (s *AdminInventoryService) PendingRequestsForLot(ctx context.Context, guildBankItemID int64, limit int) ([]domain.InventoryRequest, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	if guildBankItemID <= 0 {
		return []domain.InventoryRequest{}, nil
	}
	if limit <= 0 {
		limit = 10
	}
	return s.store.PendingRequestsForLot(ctx, guildBankItemID, limit)
}

// PendingRequestCount returns the size of the pending queue for the navigation
// badge. It reads one row so the count is cheap.
func (s *AdminInventoryService) PendingRequestCount(ctx context.Context) (int, error) {
	page, err := s.ListRequests(ctx, domain.RequestFilter{
		Status: domain.RequestPending, Page: 1, PageSize: 1,
	})
	if err != nil {
		return 0, err
	}
	return page.Total, nil
}

// ApproveRequest issues the requested quantity and completes the request in one
// transaction. Approval and issuance are the same act: there is no Approved
// state (design section 4.2).
func (s *AdminInventoryService) ApproveRequest(ctx context.Context, in domain.ApproveRequestInput) (domain.InventoryRequest, error) {
	if err := s.requireReady(); err != nil {
		return domain.InventoryRequest{}, err
	}
	if err := in.Validate(); err != nil {
		return domain.InventoryRequest{}, err
	}
	in.Note = strings.TrimSpace(in.Note)
	return s.withRequestDeadlockRetry("approve request", func() (domain.InventoryRequest, error) {
		return s.approveRequestAttempt(ctx, in)
	})
}

func (s *AdminInventoryService) approveRequestAttempt(ctx context.Context, in domain.ApproveRequestInput) (domain.InventoryRequest, error) {
	tx, err := s.store.BeginAdminWrite(ctx)
	if err != nil {
		return domain.InventoryRequest{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Lock order is request first, lot second, matching every request writer.
	request, err := tx.LockRequest(ctx, in.RequestID)
	if err != nil {
		return domain.InventoryRequest{}, mapRequestLookupError(err)
	}
	if request.Status != domain.RequestPending {
		// A replay of an approval that already completed resolves to the
		// outcome the caller asked for rather than an error (design section
		// 9.4). A denied or cancelled request is a different decision.
		if request.Status == domain.RequestCompleted && request.CompletedMovementID != nil {
			return s.store.GetRequest(ctx, request.RequestID)
		}
		return domain.InventoryRequest{}, fmt.Errorf(
			"%w: request %d is %s", domain.ErrRequestNotPending, request.RequestID, request.Status)
	}

	current, err := tx.LockInventory(ctx, request.GuildBankItemID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.InventoryRequest{}, fmt.Errorf(
				"%w: lot %d", domain.ErrRequestItemUnavailable, request.GuildBankItemID)
		}
		return domain.InventoryRequest{}, err
	}
	if current.DeletedFg {
		return domain.InventoryRequest{}, fmt.Errorf(
			"%w: lot %d is removed", domain.ErrRequestItemUnavailable, current.ID)
	}
	if strings.TrimSpace(current.GivenTo) != "" {
		return domain.InventoryRequest{}, fmt.Errorf(
			"%w: an active row cannot carry a recipient", domain.ErrConflict)
	}
	if !sameModAt(current.ModAt, in.ExpectedModAt) {
		return domain.InventoryRequest{}, fmt.Errorf(
			"%w: lot %d", domain.ErrRequestStaleItem, current.ID)
	}
	issued, err := resolveApproveQuantity(request, current, in.IssueQuantity)
	if err != nil {
		return domain.InventoryRequest{}, err
	}

	issueIn := domain.IssueItemInput{
		GuildBankItemID: current.ID,
		// The lot is already locked and its version checked, so the shared
		// issue path sees the same snapshot the quantity was resolved from.
		ExpectedModAt:  current.ModAt,
		Quantity:       issued,
		Recipient:      request.CharacterName,
		Note:           requestMovementNote(request, issued, in.Note),
		IdempotencyKey: requestApprovalIdempotencyKey(request.RequestID),
		RequestID:      request.RequestID,
		Actor:          in.Actor,
		// The whole lifecycle shares the creation correlation ID so the audit
		// viewer shows request, movement and completion together.
		CorrelationID: request.CorrelationID,
		SourceIP:      in.SourceIP,
	}
	_, movement, err := s.issueLocked(ctx, tx, issueIn)
	if err != nil {
		return domain.InventoryRequest{}, err
	}
	if err := tx.CompleteRequest(ctx, domain.CompleteRequestParams{
		RequestID: request.RequestID, MovementID: movement.MovementID,
		IssuedQuantity: issued, DecisionNote: in.Note,
		Actor: in.Actor, DecidedAt: movement.OccurredAt,
	}); err != nil {
		return domain.InventoryRequest{}, err
	}
	entry, err := requestCompletedAudit(request, in.Actor, issued, movement, in.Note, in.SourceIP)
	if err != nil {
		return domain.InventoryRequest{}, err
	}
	if err := tx.InsertAudit(ctx, entry); err != nil {
		return domain.InventoryRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.InventoryRequest{}, err
	}
	committed = true
	return s.store.GetRequest(ctx, request.RequestID)
}

// resolveApproveQuantity applies the two approved shapes of an approval. A lot
// that covers the request issues the requested quantity automatically; a short
// lot issues a prompted quantity between 1 and what is on hand. A posted
// reduction while the lot covers the request is a 422, because the UI never
// offers one and honouring it would be an undocumented second under-issue path
// (design section 4.4).
func resolveApproveQuantity(request domain.InventoryRequest,
	current domain.AdminInventoryItem, posted uint32) (uint32, error) {
	onHand := current.Quantity
	if onHand == 0 {
		return 0, fmt.Errorf("%w: lot %d is empty", domain.ErrRequestInsufficientStock, current.ID)
	}
	requested := request.RequestedQuantity
	if posted == 0 {
		// No quantity was posted, which means the officer submitted the
		// covered form. If stock has since fallen below the request, the form
		// they saw no longer matches reality: refuse rather than silently
		// issuing less, and let the handler re-render the short-stock shape
		// for an explicit decision (design section 8.4).
		if onHand < requested {
			return 0, fmt.Errorf(
				"%w: requested %d but only %d is on hand", domain.ErrRequestInsufficientStock, requested, onHand)
		}
		return requested, nil
	}
	if onHand >= requested {
		return 0, domain.NewValidationError("issuedQuantity",
			"a reduced issued quantity is not offered while the item covers the request")
	}
	if posted > onHand {
		return 0, fmt.Errorf("%w: issued %d but %d is on hand",
			domain.ErrRequestInsufficientStock, posted, onHand)
	}
	return posted, nil
}

// requestMovementNote builds the ledger note. The prefix names the request, a
// short issue states both numbers first, and the officer's note wins over the
// requester's so the ledger records why the action was taken. The whole value is
// bounded to the movement column.
func requestMovementNote(request domain.InventoryRequest, issued uint32, officerNote string) string {
	prefix := "request #" + strconv.FormatInt(request.RequestID, 10) + ": "
	if issued < request.RequestedQuantity {
		prefix += strconv.FormatUint(uint64(issued), 10) + " of " +
			strconv.FormatUint(uint64(request.RequestedQuantity), 10) + " — "
	}
	text := strings.TrimSpace(officerNote)
	if text == "" {
		text = strings.TrimSpace(request.Note)
	}
	return truncateRequestRunes(prefix+text, domain.RequestMaxNoteRunes)
}

func truncateRequestRunes(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

// requestCompletedAudit builds the REQUEST_COMPLETED payload. It carries both
// quantities and the movement ID so a short issue is self-explanatory in the
// log, and reuses the request's correlation ID (design sections 10.1 and 10.4).
func requestCompletedAudit(request domain.InventoryRequest, actor domain.Actor,
	issued uint32, movement domain.InventoryMovement, note, sourceIP string) (domain.AuditEntry, error) {
	oldValues, err := json.Marshal(map[string]any{"status": string(domain.RequestPending)})
	if err != nil {
		return domain.AuditEntry{}, err
	}
	payload := map[string]any{
		"status":            string(domain.RequestCompleted),
		"requestedQuantity": request.RequestedQuantity,
		"issuedQuantity":    issued,
		"movementId":        movement.MovementID,
		"remainingQuantity": movement.RemainingQuantity,
	}
	if text := strings.TrimSpace(note); text != "" {
		payload["note"] = text
	}
	newValues, err := json.Marshal(payload)
	if err != nil {
		return domain.AuditEntry{}, err
	}
	return domain.AuditEntry{
		EventDate: movement.OccurredAt, UserID: actor.UserID, UserName: actor.UserName,
		EventType: "REQUEST_COMPLETED", TableName: "InventoryRequest",
		RecordID: strconv.FormatInt(request.RequestID, 10), OldValues: oldValues,
		NewValues: newValues, CorrelationID: request.CorrelationID, SourceIP: sourceIP,
	}, nil
}

// DenyRequest refuses one Pending request with a required reason. It never
// touches inventory and never writes a movement.
func (s *AdminInventoryService) DenyRequest(ctx context.Context, in domain.DenyRequestInput) (domain.InventoryRequest, error) {
	if err := s.requireReady(); err != nil {
		return domain.InventoryRequest{}, err
	}
	if err := in.Validate(); err != nil {
		return domain.InventoryRequest{}, err
	}
	reason := strings.TrimSpace(in.Reason)
	return s.withRequestDeadlockRetry("deny request", func() (domain.InventoryRequest, error) {
		return s.denyRequestAttempt(ctx, in, reason)
	})
}

func (s *AdminInventoryService) denyRequestAttempt(ctx context.Context,
	in domain.DenyRequestInput, reason string) (domain.InventoryRequest, error) {
	tx, err := s.store.BeginAdminWrite(ctx)
	if err != nil {
		return domain.InventoryRequest{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	request, err := tx.LockRequest(ctx, in.RequestID)
	if err != nil {
		return domain.InventoryRequest{}, mapRequestLookupError(err)
	}
	if request.Status != domain.RequestPending {
		return domain.InventoryRequest{}, fmt.Errorf(
			"%w: request %d is %s", domain.ErrRequestNotPending, request.RequestID, request.Status)
	}
	decidedAt := s.now().UTC()
	if err := tx.DecideRequest(ctx, domain.DecideRequestParams{
		RequestID: request.RequestID, Status: domain.RequestDenied,
		DecisionNote: reason, Actor: in.Actor, DecidedAt: decidedAt,
	}); err != nil {
		return domain.InventoryRequest{}, err
	}
	entry, err := requestDecisionAudit(request, domain.RequestDenied, reason,
		in.Actor, decidedAt, in.SourceIP)
	if err != nil {
		return domain.InventoryRequest{}, err
	}
	if err := tx.InsertAudit(ctx, entry); err != nil {
		return domain.InventoryRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.InventoryRequest{}, err
	}
	committed = true
	return s.store.GetRequest(ctx, request.RequestID)
}

// mapRequestLookupError turns a missing request into ErrNotFound and leaves
// every other store error untouched.
func mapRequestLookupError(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrNotFound
	}
	return err
}

// withRequestDeadlockRetry retries one deadlocked request decision once, using
// the same attempt closure so the retry repeats the identical command.
func (s *AdminInventoryService) withRequestDeadlockRetry(operation string,
	attempt func() (domain.InventoryRequest, error)) (domain.InventoryRequest, error) {
	result, err := attempt()
	var retry *domain.RetryableError
	if err != nil && errors.As(err, &retry) {
		s.logger.Warn("request write deadlock; retrying once", "operation", operation)
		result, err = attempt()
	}
	return result, err
}
