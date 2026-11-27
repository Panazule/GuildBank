package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

type fakeRequestCharacters struct {
	byUser map[int64][]domain.Character
	err    error
}

func (f fakeRequestCharacters) List(ctx context.Context, userID int64) ([]domain.Character, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byUser[userID], nil
}

var requestTestNow = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

func newRequestTestService(store *fakeAdmin) *RequestService {
	characters := fakeRequestCharacters{byUser: map[int64][]domain.Character{
		7: {
			{ID: 1, UserID: 7, Name: "Icefear"},
			{ID: 2, UserID: 7, Name: "Banktoon"},
		},
	}}
	return NewRequestService(store, characters, func() time.Time { return requestTestNow }, nil)
}

func requestTestLot(store *fakeAdmin, quantity uint32) domain.AdminInventoryItem {
	lot := domain.AdminInventoryItem{
		ID: 1, EQUID: 100, Name: "Cloak of Flames", ItemType: domain.ItemTypeItem,
		Quantity: quantity, SourceLocation: "Bank Tab 2",
		ModAt: time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC),
	}
	store.items[lot.ID] = lot
	return lot
}

func memberRequestInput() domain.CreateRequestInput {
	return domain.CreateRequestInput{
		GuildBankItemID: 1,
		Quantity:        5,
		CharacterName:   "icefear",
		Note:            "for raid",
		Kind:            domain.RequesterMember,
		Actor:           domain.Actor{UserID: 7, UserName: "member1", Role: domain.RoleMember},
		CorrelationID:   "corr-1",
		SourceIP:        "127.0.0.1",
	}
}

func TestCreateRequestStoresSnapshotsAndAudit(t *testing.T) {
	store := newFakeAdmin()
	lot := requestTestLot(store, 9)
	service := newRequestTestService(store)

	request, err := service.Create(context.Background(), memberRequestInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if request.RequestID != 1 || request.Status != domain.RequestPending {
		t.Fatalf("request = %+v", request)
	}
	if request.EQUID != lot.EQUID || request.ItemName != lot.Name || request.OnHandAtCreate != 9 {
		t.Fatalf("snapshot = %+v", request)
	}
	// The profile spelling wins over the posted case.
	if request.CharacterName != "Icefear" {
		t.Fatalf("character = %q, want Icefear", request.CharacterName)
	}
	if request.RequesterUserID == nil || *request.RequesterUserID != 7 ||
		request.RequesterUserName != "member1" || request.CreatedByName != "member1" {
		t.Fatalf("requester snapshot = %+v", request)
	}
	if len(store.audits) != 1 {
		t.Fatalf("audits = %d", len(store.audits))
	}
	audit := store.audits[0]
	if audit.EventType != "REQUEST_CREATED" || audit.TableName != "InventoryRequest" ||
		audit.RecordID != "1" || audit.CorrelationID != "corr-1" || audit.UserID != 7 {
		t.Fatalf("audit = %+v", audit)
	}
	if store.committed != 1 {
		t.Fatalf("committed = %d", store.committed)
	}
}

func TestCreateRequestDuplicatePendingIsCaseInsensitive(t *testing.T) {
	store := newFakeAdmin()
	requestTestLot(store, 9)
	service := newRequestTestService(store)
	if _, err := service.Create(context.Background(), memberRequestInput()); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	input := memberRequestInput()
	input.CharacterName = "ICEFEAR"
	_, err := service.Create(context.Background(), input)
	if !errors.Is(err, domain.ErrRequestAlreadyPending) {
		t.Fatalf("second Create error = %v, want request_already_pending", err)
	}
	if len(store.requests) != 1 {
		t.Fatalf("requests = %d", len(store.requests))
	}
}

func TestCreateRequestAllowsSecondCharacterAndRetryAfterCancel(t *testing.T) {
	store := newFakeAdmin()
	requestTestLot(store, 9)
	service := newRequestTestService(store)
	ctx := context.Background()

	first, err := service.Create(ctx, memberRequestInput())
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	secondInput := memberRequestInput()
	secondInput.CharacterName = "Banktoon"
	if _, err := service.Create(ctx, secondInput); err != nil {
		t.Fatalf("second character Create: %v", err)
	}
	if len(store.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(store.requests))
	}

	if _, err := service.Cancel(ctx, domain.CancelRequestInput{
		RequestID: first.RequestID, Reason: "changed my mind",
		Actor:         domain.Actor{UserID: 7, UserName: "member1"},
		CorrelationID: "corr-cancel",
	}); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// Only Pending rows occupy uq_request_pending, so the same character may
	// ask again after the earlier request is terminal.
	if _, err := service.Create(ctx, memberRequestInput()); err != nil {
		t.Fatalf("Create after cancel: %v", err)
	}
}

func TestCreateRequestEnforcesQuantityLotAndBudget(t *testing.T) {
	store := newFakeAdmin()
	requestTestLot(store, 4)
	service := newRequestTestService(store)
	ctx := context.Background()

	input := memberRequestInput()
	input.Quantity = 5
	if _, err := service.Create(ctx, input); !errors.Is(err, domain.ErrRequestInsufficientStock) {
		t.Fatalf("above on-hand error = %v", err)
	}

	stale := memberRequestInput()
	stale.Quantity = 4
	stale.ExpectedModAt = requestTestNow
	if _, err := service.Create(ctx, stale); !errors.Is(err, domain.ErrRequestStaleItem) {
		t.Fatalf("stale error = %v", err)
	}

	store.items[1] = domain.AdminInventoryItem{ID: 1, EQUID: 100, Name: "Removed", DeletedFg: true}
	if _, err := service.Create(ctx, memberRequestInput()); !errors.Is(err, domain.ErrRequestItemUnavailable) {
		t.Fatalf("removed lot error = %v", err)
	}

	// Restore a valid lot and seed the full budget with other pending rows.
	requestTestLot(store, 9)
	for i := 0; i < domain.RequestMaxPendingPerUser; i++ {
		userID := int64(7)
		store.requests = append(store.requests, domain.InventoryRequest{
			RequestID: int64(100 + i), GuildBankItemID: int64(10 + i),
			Status: domain.RequestPending, RequesterUserID: &userID,
		})
	}
	if _, err := service.Create(ctx, memberRequestInput()); !errors.Is(err, domain.ErrRequestTooManyPending) {
		t.Fatalf("budget error = %v", err)
	}
}

func TestCreateRequestRejectsCharacterNotOnAccount(t *testing.T) {
	store := newFakeAdmin()
	requestTestLot(store, 9)
	service := newRequestTestService(store)
	input := memberRequestInput()
	input.CharacterName = "Someoneelse"
	if _, err := service.Create(context.Background(), input); !errors.Is(err, domain.ErrRequestCharacterInvalid) {
		t.Fatalf("error = %v, want character_invalid", err)
	}
	if len(store.requests) != 0 {
		t.Fatalf("requests = %d", len(store.requests))
	}
}

func TestCreateRequestIdempotencyReplayReturnsOriginal(t *testing.T) {
	store := newFakeAdmin()
	requestTestLot(store, 9)
	service := newRequestTestService(store)
	input := memberRequestInput()
	input.IdempotencyKey = "form-key-1"

	first, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	replay := memberRequestInput()
	replay.IdempotencyKey = "form-key-1"
	replay.Quantity = 1
	second, err := service.Create(context.Background(), replay)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if second.RequestID != first.RequestID || len(store.requests) != 1 {
		t.Fatalf("replay created a second row: %+v", second)
	}
}

func TestCancelRequestIsOwnerOnlyAndGuarded(t *testing.T) {
	store := newFakeAdmin()
	requestTestLot(store, 9)
	service := newRequestTestService(store)
	ctx := context.Background()
	request, err := service.Create(ctx, memberRequestInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Another member sees a 404, never a 403: the page must not confirm that
	// somebody else's request exists.
	_, err = service.Cancel(ctx, domain.CancelRequestInput{
		RequestID:     request.RequestID,
		Actor:         domain.Actor{UserID: 8, UserName: "other"},
		CorrelationID: "corr-other",
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("other member cancel error = %v, want not_found", err)
	}

	cancelled, err := service.Cancel(ctx, domain.CancelRequestInput{
		RequestID: request.RequestID, Reason: "no longer needed",
		Actor:         domain.Actor{UserID: 7, UserName: "member1"},
		CorrelationID: "corr-cancel", SourceIP: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if cancelled.Status != domain.RequestCancelled || cancelled.DecisionNote != "no longer needed" ||
		cancelled.DecidedAt == nil || cancelled.DecidedByName != "member1" {
		t.Fatalf("cancelled = %+v", cancelled)
	}
	if len(store.movements) != 0 {
		t.Fatalf("cancel wrote %d movements, want none", len(store.movements))
	}
	if len(store.audits) != 2 {
		t.Fatalf("audits = %d, want 2", len(store.audits))
	}
	audit := store.audits[1]
	if audit.EventType != "REQUEST_CANCELLED" || audit.RecordID != "1" ||
		audit.CorrelationID != request.CorrelationID || audit.UserID != 7 {
		t.Fatalf("cancel audit = %+v", audit)
	}

	// A second cancel is a no-op with request_not_pending, not a second audit.
	_, err = service.Cancel(ctx, domain.CancelRequestInput{
		RequestID:     request.RequestID,
		Actor:         domain.Actor{UserID: 7, UserName: "member1"},
		CorrelationID: "corr-again",
	})
	if !errors.Is(err, domain.ErrRequestNotPending) {
		t.Fatalf("second cancel error = %v, want request_not_pending", err)
	}
}

func TestCancelRequestRejectsTerminalRequest(t *testing.T) {
	store := newFakeAdmin()
	requestTestLot(store, 9)
	service := newRequestTestService(store)
	request, err := service.Create(context.Background(), memberRequestInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	store.requests[0].Status = domain.RequestCompleted

	_, err = service.Cancel(context.Background(), domain.CancelRequestInput{
		RequestID:     request.RequestID,
		Actor:         domain.Actor{UserID: 7, UserName: "member1"},
		CorrelationID: "corr-cancel",
	})
	if !errors.Is(err, domain.ErrRequestNotPending) {
		t.Fatalf("error = %v, want request_not_pending", err)
	}
}

func TestListAndGetMineAreScopedToTheAccount(t *testing.T) {
	store := newFakeAdmin()
	requestTestLot(store, 9)
	service := newRequestTestService(store)
	ctx := context.Background()
	mine, err := service.Create(ctx, memberRequestInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	otherUser := int64(8)
	store.requests = append(store.requests, domain.InventoryRequest{
		RequestID: 99, GuildBankItemID: 1, ItemName: "Cloak of Flames",
		RequestedQuantity: 1, CharacterName: "Otherchar", Status: domain.RequestPending,
		RequesterKind: domain.RequesterMember, RequesterUserID: &otherUser,
		CreatedAt: requestTestNow,
	})

	page, err := service.ListMine(ctx, 7, domain.RequestFilter{})
	if err != nil {
		t.Fatalf("ListMine: %v", err)
	}
	if page.Total != 1 || len(page.Requests) != 1 || page.Requests[0].RequestID != mine.RequestID {
		t.Fatalf("page = %+v", page)
	}
	if page.PageSize != RequestMinePageSize {
		t.Fatalf("page size = %d", page.PageSize)
	}

	if _, err := service.GetMine(ctx, 8, mine.RequestID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetMine other account error = %v", err)
	}
	if _, err := service.GetMine(ctx, 7, mine.RequestID); err != nil {
		t.Fatalf("GetMine own: %v", err)
	}

	// An invalid status is a 422 field error, not a silently widened read.
	if _, err := service.ListMine(ctx, 7, domain.RequestFilter{Status: "Nope"}); err == nil {
		t.Fatal("ListMine accepted an invalid status")
	}
}
