package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

const (
	// RequestMinePageSize is the member list page size. The design fixes it at
	// 25 (section 13.5) so a member's history stays one short page per screen.
	RequestMinePageSize = 25
)

// RequestCharacterSource is the account's character list. It is used for the
// server-side ownership re-validation on every member creation (design section
// 14.4); the dropdown is a convenience, never an authorization decision.
type RequestCharacterSource interface {
	List(ctx context.Context, userID int64) ([]domain.Character, error)
}

// RequestStore is the member-facing request persistence surface. It is a subset
// of AdminStore: the same guild store satisfies both, so a future approval can
// join the request transition to the issue transaction without a second factory.
type RequestStore interface {
	Ready() bool
	GetAdminInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error)
	GetRequest(ctx context.Context, requestID int64) (domain.InventoryRequest, error)
	RequestByIdempotencyKey(ctx context.Context, key string) (domain.InventoryRequest, error)
	ListRequests(ctx context.Context, filter domain.RequestFilter) (domain.RequestPage, error)
	CountPendingRequests(ctx context.Context, requesterUserID int64) (int, error)
	PendingRequestForCharacter(ctx context.Context, guildBankItemID int64, characterName string) (int64, error)
	PendingCharactersForLot(ctx context.Context, guildBankItemID, requesterUserID int64) ([]string, error)
	BeginAdminWrite(ctx context.Context) (AdminWriteTx, error)
}

// RequestService owns member request creation, the "my requests" reads and
// cancellation. Approval and denial are the officer flow and are deliberately
// absent here.
type RequestService struct {
	store      RequestStore
	characters RequestCharacterSource
	now        func() time.Time
	logger     *slog.Logger
}

// NewRequestService wires the member request workflow. A nil store produces a
// degraded service whose Ready() is false; a nil character source makes member
// creation unavailable rather than trusting an unvalidated name.
func NewRequestService(store RequestStore, characters RequestCharacterSource,
	now func() time.Time, logger *slog.Logger) *RequestService {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RequestService{store: store, characters: characters, now: now, logger: logger}
}

// Ready reports whether the guild store is available.
func (s *RequestService) Ready() bool {
	return s != nil && s.store != nil && s.store.Ready()
}

func (s *RequestService) requireReady() error {
	if !s.Ready() {
		return domain.ErrUnavailable
	}
	return nil
}

// ActiveLot returns the lot a request may target. A missing or removed lot is
// ErrRequestItemUnavailable (409 item_unavailable), never a 404: the member
// followed a link to a lot that existed when the page was rendered.
func (s *RequestService) ActiveLot(ctx context.Context, lotID int64) (domain.AdminInventoryItem, error) {
	if err := s.requireReady(); err != nil {
		return domain.AdminInventoryItem{}, err
	}
	if lotID <= 0 {
		return domain.AdminInventoryItem{}, fmt.Errorf("%w: invalid lot", domain.ErrRequestItemUnavailable)
	}
	lot, err := s.store.GetAdminInventory(ctx, lotID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.AdminInventoryItem{}, fmt.Errorf("%w: lot %d", domain.ErrRequestItemUnavailable, lotID)
		}
		return domain.AdminInventoryItem{}, err
	}
	if lot.DeletedFg {
		return domain.AdminInventoryItem{}, fmt.Errorf("%w: lot %d is removed", domain.ErrRequestItemUnavailable, lotID)
	}
	return lot, nil
}

// Characters returns the signed-in account's character list for the form's
// picker. An empty list means the form renders the free-text variant (design
// section 5.3), not that the member cannot request.
func (s *RequestService) Characters(ctx context.Context, userID int64) ([]domain.Character, error) {
	if s.characters == nil {
		return nil, domain.ErrUnavailable
	}
	return s.characters.List(ctx, userID)
}

// PendingCharacters lists the caller's own characters that already hold a
// Pending request for the lot, so the item page can say so instead of letting
// the member submit a duplicate (design section 7.1).
func (s *RequestService) PendingCharacters(ctx context.Context, lotID, userID int64) ([]string, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	if lotID <= 0 || userID <= 0 {
		return []string{}, nil
	}
	return s.store.PendingCharactersForLot(ctx, lotID, userID)
}

// Create validates and writes one Pending request plus its REQUEST_CREATED
// audit row in one short transaction. The lot is read without a lock: a request
// is a statement of intent, so a lot that changes a second later is handled at
// approval time rather than by holding a lock while a member fills in a form
// (design section 9.6).
func (s *RequestService) Create(ctx context.Context, in domain.CreateRequestInput) (domain.InventoryRequest, error) {
	if err := s.requireReady(); err != nil {
		return domain.InventoryRequest{}, err
	}
	in.CharacterName = domain.NormalizeCharacterName(in.CharacterName)
	if err := in.Validate(); err != nil {
		return domain.InventoryRequest{}, err
	}

	// A replayed form returns the original request rather than creating a
	// second row. The key is checked first so a retry is not refused by the
	// pending budget or the duplicate rule it already satisfied.
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		existing, err := s.store.RequestByIdempotencyKey(ctx, key)
		switch {
		case err == nil:
			if !requestOwnedBy(existing, in.Actor.UserID) {
				return domain.InventoryRequest{}, fmt.Errorf("%w: idempotency key belongs to another requester", domain.ErrConflict)
			}
			return existing, nil
		case !errors.Is(err, domain.ErrNotFound):
			return domain.InventoryRequest{}, err
		}
	}

	lot, err := s.ActiveLot(ctx, in.GuildBankItemID)
	if err != nil {
		return domain.InventoryRequest{}, err
	}
	if in.Quantity > lot.Quantity {
		return domain.InventoryRequest{}, fmt.Errorf(
			"%w: requested %d but %d is on hand", domain.ErrRequestInsufficientStock, in.Quantity, lot.Quantity)
	}
	if !in.ExpectedModAt.IsZero() && !sameModAt(lot.ModAt, in.ExpectedModAt) {
		return domain.InventoryRequest{}, fmt.Errorf("%w: lot %d", domain.ErrRequestStaleItem, lot.ID)
	}

	if in.Kind == domain.RequesterMember {
		name, err := s.ownedCharacterName(ctx, in.Actor.UserID, in.CharacterName)
		if err != nil {
			return domain.InventoryRequest{}, err
		}
		// Storing the profile spelling keeps the request and the ledger
		// Recipient identical to what the profile page shows.
		in.CharacterName = name
		count, err := s.store.CountPendingRequests(ctx, in.Actor.UserID)
		if err != nil {
			return domain.InventoryRequest{}, err
		}
		if count >= domain.RequestMaxPendingPerUser {
			return domain.InventoryRequest{}, fmt.Errorf(
				"%w: %d pending requests", domain.ErrRequestTooManyPending, count)
		}
	}

	pendingID, err := s.store.PendingRequestForCharacter(ctx, in.GuildBankItemID, in.CharacterName)
	if err != nil {
		return domain.InventoryRequest{}, err
	}
	if pendingID > 0 {
		return domain.InventoryRequest{}, fmt.Errorf(
			"%w: request %d already holds this character for the item", domain.ErrRequestAlreadyPending, pendingID)
	}

	// Identity and stock are snapshots taken from the lot the service read, so
	// a request can never claim an identity the lot contradicts.
	in.EQUID = lot.EQUID
	in.ItemName = lot.Name
	in.OnHandAtCreate = lot.Quantity
	in.CreatedAt = s.now().UTC()

	requestID, err := s.insert(ctx, in)
	if err != nil {
		// The engine enforces the same duplicate rules. A conflict here is a
		// lost race with a concurrent submission, not a server fault.
		if errors.Is(err, domain.ErrConflict) {
			if existing, lookupErr := s.store.RequestByIdempotencyKey(ctx, in.IdempotencyKey); lookupErr == nil &&
				requestOwnedBy(existing, in.Actor.UserID) {
				return existing, nil
			}
			return domain.InventoryRequest{}, fmt.Errorf("%w: a concurrent submission won the race", domain.ErrRequestAlreadyPending)
		}
		return domain.InventoryRequest{}, err
	}
	return s.store.GetRequest(ctx, requestID)
}

// insert writes the row and its audit event in one transaction so a request is
// never queued without its REQUEST_CREATED record.
func (s *RequestService) insert(ctx context.Context, in domain.CreateRequestInput) (int64, error) {
	tx, err := s.store.BeginAdminWrite(ctx)
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	requestID, err := tx.InsertRequest(ctx, in)
	if err != nil {
		return 0, err
	}
	entry, err := requestCreatedAudit(requestID, in)
	if err != nil {
		return 0, err
	}
	if err := tx.InsertAudit(ctx, entry); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	committed = true
	return requestID, nil
}

// ownedCharacterName re-validates ownership against the account's current list.
// An account with no profile characters has nothing to compare against, so the
// already-validated profile name is accepted (design section 5.3).
func (s *RequestService) ownedCharacterName(ctx context.Context, userID int64, posted string) (string, error) {
	if s.characters == nil {
		return "", domain.ErrUnavailable
	}
	characters, err := s.characters.List(ctx, userID)
	if err != nil {
		return "", err
	}
	posted = domain.NormalizeCharacterName(posted)
	if len(characters) == 0 {
		return posted, nil
	}
	for _, character := range characters {
		if strings.EqualFold(character.Name, posted) {
			return character.Name, nil
		}
	}
	return "", fmt.Errorf("%w: %q is not on this account", domain.ErrRequestCharacterInvalid, posted)
}

// ListMine reads one bounded page of the caller's own requests. The account
// scope is always applied from the session, never from the filter.
func (s *RequestService) ListMine(ctx context.Context, userID int64, filter domain.RequestFilter) (domain.RequestPage, error) {
	if err := s.requireReady(); err != nil {
		return domain.RequestPage{}, err
	}
	if userID <= 0 {
		return domain.RequestPage{}, domain.ErrUnauthorized
	}
	if filter.Status != "" && !filter.Status.Valid() {
		return domain.RequestPage{}, domain.NewValidationError("status", "status must be one of the approved values")
	}
	filter.RequesterUserID = userID
	filter.RequesterKind = domain.RequesterMember
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = RequestMinePageSize
	}
	return s.store.ListRequests(ctx, filter)
}

// GetMine reads one of the caller's own requests. Another account's ID is
// ErrNotFound, never a 403, so the page cannot be used to enumerate requests
// (design section 14.6).
func (s *RequestService) GetMine(ctx context.Context, userID, requestID int64) (domain.InventoryRequest, error) {
	if err := s.requireReady(); err != nil {
		return domain.InventoryRequest{}, err
	}
	if userID <= 0 {
		return domain.InventoryRequest{}, domain.ErrUnauthorized
	}
	if requestID <= 0 {
		return domain.InventoryRequest{}, domain.ErrNotFound
	}
	request, err := s.store.GetRequest(ctx, requestID)
	if err != nil {
		return domain.InventoryRequest{}, err
	}
	if request.RequesterUserID == nil || *request.RequesterUserID != userID {
		return domain.InventoryRequest{}, domain.ErrNotFound
	}
	return request, nil
}

// Cancel withdraws one of the caller's own Pending requests. The guarded
// Pending transition means a second cancel is a no-op with request_not_pending
// rather than a second audit row (design sections 3.3 and 14.7).
func (s *RequestService) Cancel(ctx context.Context, in domain.CancelRequestInput) (domain.InventoryRequest, error) {
	if err := s.requireReady(); err != nil {
		return domain.InventoryRequest{}, err
	}
	if err := in.Validate(); err != nil {
		return domain.InventoryRequest{}, err
	}
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
		return domain.InventoryRequest{}, err
	}
	// "Own" is resolved from the stored requester, never from a posted value.
	if request.RequesterUserID == nil || *request.RequesterUserID != in.Actor.UserID {
		return domain.InventoryRequest{}, domain.ErrNotFound
	}
	if request.Status != domain.RequestPending {
		return domain.InventoryRequest{}, fmt.Errorf(
			"%w: request %d is %s", domain.ErrRequestNotPending, request.RequestID, request.Status)
	}

	decidedAt := s.now().UTC()
	reason := strings.TrimSpace(in.Reason)
	if err := tx.DecideRequest(ctx, domain.DecideRequestParams{
		RequestID: request.RequestID, Status: domain.RequestCancelled,
		DecisionNote: reason, Actor: in.Actor, DecidedAt: decidedAt,
	}); err != nil {
		return domain.InventoryRequest{}, err
	}
	entry, err := requestDecisionAudit(request, domain.RequestCancelled, reason,
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

// requestOwnedBy reports whether the row belongs to the supplied account. Public
// rows have no account and only match a zero actor.
func requestOwnedBy(request domain.InventoryRequest, userID int64) bool {
	if request.RequesterUserID == nil {
		return userID == 0
	}
	return *request.RequesterUserID == userID
}

// requestCreatedAudit builds the REQUEST_CREATED payload. It records the item,
// quantity, character and kind; the free-text note is included but no source IP
// beyond the standard audit column (design sections 10.1 and 14.3).
func requestCreatedAudit(requestID int64, in domain.CreateRequestInput) (domain.AuditEntry, error) {
	payload := map[string]any{
		"itemId":        in.GuildBankItemID,
		"equid":         in.EQUID,
		"itemName":      in.ItemName,
		"quantity":      in.Quantity,
		"characterName": domain.NormalizeCharacterName(in.CharacterName),
		"requesterKind": string(in.Kind),
	}
	if note := strings.TrimSpace(in.Note); note != "" {
		payload["note"] = note
	}
	if in.Kind == domain.RequesterPublic {
		if contact := strings.TrimSpace(in.ContactDiscord); contact != "" {
			payload["contactDiscord"] = contact
		}
	}
	newValues, err := json.Marshal(payload)
	if err != nil {
		return domain.AuditEntry{}, err
	}
	userID, userName := in.Actor.UserID, in.Actor.UserName
	if in.Kind == domain.RequesterPublic {
		userID, userName = 0, domain.RequestPublicActorName
	}
	return domain.AuditEntry{
		EventDate: in.CreatedAt, UserID: userID, UserName: userName,
		EventType: "REQUEST_CREATED", TableName: "InventoryRequest",
		RecordID: strconv.FormatInt(requestID, 10), OldValues: json.RawMessage("{}"),
		NewValues: newValues, CorrelationID: in.CorrelationID, SourceIP: in.SourceIP,
	}, nil
}

// requestDecisionAudit builds the REQUEST_CANCELLED (or, in Phase 4,
// REQUEST_DENIED) payload. It reuses the request's creation correlation ID so
// the audit viewer shows the whole lifecycle in one list (design section 10.4).
func requestDecisionAudit(request domain.InventoryRequest, status domain.RequestStatus,
	reason string, actor domain.Actor, at time.Time, sourceIP string) (domain.AuditEntry, error) {
	eventType := "REQUEST_CANCELLED"
	if status == domain.RequestDenied {
		eventType = "REQUEST_DENIED"
	}
	oldValues, err := json.Marshal(map[string]any{"status": string(domain.RequestPending)})
	if err != nil {
		return domain.AuditEntry{}, err
	}
	payload := map[string]any{"status": string(status)}
	if reason != "" {
		payload["reason"] = reason
	}
	newValues, err := json.Marshal(payload)
	if err != nil {
		return domain.AuditEntry{}, err
	}
	return domain.AuditEntry{
		EventDate: at, UserID: actor.UserID, UserName: actor.UserName,
		EventType: eventType, TableName: "InventoryRequest",
		RecordID: strconv.FormatInt(request.RequestID, 10), OldValues: oldValues,
		NewValues: newValues, CorrelationID: request.CorrelationID, SourceIP: sourceIP,
	}, nil
}
