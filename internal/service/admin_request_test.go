package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func officerActor() domain.Actor {
	return domain.Actor{UserID: 2, UserName: "officer1", Role: domain.RoleOfficer}
}

// seedAdminRequest inserts one Pending member request for lot 1.
func seedAdminRequest(store *fakeAdmin, requestID int64, requested uint32, createdAt time.Time) domain.InventoryRequest {
	requester := int64(7)
	request := domain.InventoryRequest{
		RequestID: requestID, GuildBankItemID: 1, EQUID: 100, ItemName: "Cloak of Flames",
		RequestedQuantity: requested, OnHandAtCreate: requested, CharacterName: "Icefear",
		RequesterKind: domain.RequesterMember, RequesterUserID: &requester,
		RequesterUserName: "member1", Note: "for raid", Status: domain.RequestPending,
		CorrelationID: "corr-1", IdempotencyKey: "create-1", CreatedAt: createdAt,
		CreatedByUserID: &requester, CreatedByName: "member1",
	}
	store.requests = append(store.requests, request)
	if store.nextRequestID <= requestID {
		store.nextRequestID = requestID + 1
	}
	return request
}

func TestApproveRequestIssuesAndCompletesAtomically(t *testing.T) {
	ctx := context.Background()
	store := newFakeAdmin()
	lot := requestTestLot(store, 5)
	request := seedAdminRequest(store, 99, 5, requestTestNow.Add(-48*time.Hour))
	svc := testAdminService(store, nil, requestTestNow)

	updated, err := svc.ApproveRequest(ctx, domain.ApproveRequestInput{
		RequestID: request.RequestID, ExpectedModAt: lot.ModAt,
		Actor: officerActor(), CorrelationID: "corr-approve", SourceIP: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	if updated.Status != domain.RequestCompleted || updated.IssuedQuantity == nil || *updated.IssuedQuantity != 5 {
		t.Fatalf("request = %+v", updated)
	}
	if updated.CompletedMovementID == nil || *updated.CompletedMovementID != 1 ||
		updated.DecidedByName != "officer1" || updated.DecidedAt == nil {
		t.Fatalf("decision snapshot = %+v", updated)
	}
	if len(store.movements) != 1 {
		t.Fatalf("movements = %d, want 1", len(store.movements))
	}
	movement := store.movements[0]
	if movement.MovementType != domain.MovementIssue || movement.Quantity != 5 ||
		movement.RemainingQuantity != 0 || movement.Recipient != "Icefear" {
		t.Fatalf("movement = %+v", movement)
	}
	if movement.RequestID == nil || *movement.RequestID != request.RequestID {
		t.Fatalf("movement request link = %+v", movement.RequestID)
	}
	if movement.IdempotencyKey != "req-99" || movement.CorrelationID != "corr-1" {
		t.Fatalf("movement identity = %+v", movement)
	}
	if movement.Note != "request #99: for raid" {
		t.Fatalf("movement note = %q", movement.Note)
	}
	item := store.items[lot.ID]
	if !item.DeletedFg || item.GivenTo != "Icefear" {
		t.Fatalf("lot after issue = %+v", item)
	}
	if len(store.audits) != 2 || store.audits[0].EventType != "ITEM_ISSUED" ||
		store.audits[1].EventType != "REQUEST_COMPLETED" {
		t.Fatalf("audits = %+v", store.audits)
	}
	completed := store.audits[1]
	if completed.TableName != "InventoryRequest" || completed.RecordID != "99" ||
		completed.CorrelationID != "corr-1" || completed.UserName != "officer1" {
		t.Fatalf("completion audit = %+v", completed)
	}
	var payload map[string]any
	if err := json.Unmarshal(completed.NewValues, &payload); err != nil {
		t.Fatalf("decode completion audit: %v", err)
	}
	if payload["status"] != "Completed" || payload["requestedQuantity"] != float64(5) ||
		payload["issuedQuantity"] != float64(5) || payload["movementId"] != float64(1) {
		t.Fatalf("completion payload = %v", payload)
	}
}

func TestApproveRequestShortIssueRecordsReducedQuantity(t *testing.T) {
	ctx := context.Background()
	store := newFakeAdmin()
	lot := requestTestLot(store, 4)
	request := seedAdminRequest(store, 7, 5, requestTestNow.Add(-time.Hour))
	svc := testAdminService(store, nil, requestTestNow)

	updated, err := svc.ApproveRequest(ctx, domain.ApproveRequestInput{
		RequestID: request.RequestID, ExpectedModAt: lot.ModAt, IssueQuantity: 4,
		Actor: officerActor(), CorrelationID: "corr-approve",
	})
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	if !updated.ShortIssued() || updated.IssuedQuantity == nil || *updated.IssuedQuantity != 4 {
		t.Fatalf("request = %+v", updated)
	}
	if store.movements[0].Quantity != 4 {
		t.Fatalf("movement quantity = %d", store.movements[0].Quantity)
	}
	if store.movements[0].Note != "request #7: 4 of 5 — for raid" {
		t.Fatalf("movement note = %q", store.movements[0].Note)
	}
	if !store.items[lot.ID].DeletedFg {
		t.Fatal("issuing the whole on-hand stack must soft-delete the lot")
	}
}

func TestApproveRequestRejectsSilentShortIssueFromCoveredForm(t *testing.T) {
	ctx := context.Background()
	store := newFakeAdmin()
	lot := requestTestLot(store, 2)
	request := seedAdminRequest(store, 5, 5, requestTestNow)
	svc := testAdminService(store, nil, requestTestNow)

	_, err := svc.ApproveRequest(ctx, domain.ApproveRequestInput{
		RequestID: request.RequestID, ExpectedModAt: lot.ModAt,
		Actor: officerActor(), CorrelationID: "corr-approve",
	})
	if !errors.Is(err, domain.ErrRequestInsufficientStock) {
		t.Fatalf("err = %v, want ErrRequestInsufficientStock", err)
	}
	if store.requests[0].Status != domain.RequestPending || len(store.movements) != 0 {
		t.Fatal("a covered-form submission must not issue a silent partial amount")
	}
}

func TestApproveRequestRejectsReducedQuantityWhenStockCovers(t *testing.T) {
	ctx := context.Background()
	store := newFakeAdmin()
	lot := requestTestLot(store, 5)
	request := seedAdminRequest(store, 3, 5, requestTestNow)
	svc := testAdminService(store, nil, requestTestNow)

	_, err := svc.ApproveRequest(ctx, domain.ApproveRequestInput{
		RequestID: request.RequestID, ExpectedModAt: lot.ModAt, IssueQuantity: 3,
		Actor: officerActor(), CorrelationID: "corr-approve",
	})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["issuedQuantity"] == "" {
		t.Fatalf("err = %v, want issuedQuantity validation", err)
	}
	if store.requests[0].Status != domain.RequestPending || len(store.movements) != 0 {
		t.Fatalf("state changed: %+v movements=%d", store.requests[0], len(store.movements))
	}
}

func TestApproveRequestRejectsEmptyStockAndStaleLot(t *testing.T) {
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		store := newFakeAdmin()
		lot := requestTestLot(store, 0)
		request := seedAdminRequest(store, 11, 5, requestTestNow)
		svc := testAdminService(store, nil, requestTestNow)
		_, err := svc.ApproveRequest(ctx, domain.ApproveRequestInput{
			RequestID: request.RequestID, ExpectedModAt: lot.ModAt,
			Actor: officerActor(), CorrelationID: "corr-approve",
		})
		if !errors.Is(err, domain.ErrRequestInsufficientStock) {
			t.Fatalf("err = %v, want ErrRequestInsufficientStock", err)
		}
		if store.requests[0].Status != domain.RequestPending || len(store.movements) != 0 {
			t.Fatal("an empty lot must leave the request pending and write nothing")
		}
	})

	t.Run("stale", func(t *testing.T) {
		store := newFakeAdmin()
		lot := requestTestLot(store, 5)
		request := seedAdminRequest(store, 12, 5, requestTestNow)
		svc := testAdminService(store, nil, requestTestNow)
		_, err := svc.ApproveRequest(ctx, domain.ApproveRequestInput{
			RequestID: request.RequestID, ExpectedModAt: lot.ModAt.Add(time.Minute),
			Actor: officerActor(), CorrelationID: "corr-approve",
		})
		if !errors.Is(err, domain.ErrRequestStaleItem) {
			t.Fatalf("err = %v, want ErrRequestStaleItem", err)
		}
		if store.requests[0].Status != domain.RequestPending || len(store.movements) != 0 {
			t.Fatal("a stale version must leave the request pending and write nothing")
		}
	})
}

func TestApproveRequestReplayReturnsCompletedWithoutSecondIssue(t *testing.T) {
	ctx := context.Background()
	store := newFakeAdmin()
	lot := requestTestLot(store, 5)
	request := seedAdminRequest(store, 21, 5, requestTestNow)
	svc := testAdminService(store, nil, requestTestNow)
	in := domain.ApproveRequestInput{
		RequestID: request.RequestID, ExpectedModAt: lot.ModAt,
		Actor: officerActor(), CorrelationID: "corr-approve",
	}
	if _, err := svc.ApproveRequest(ctx, in); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	// The lot's ModAt moved when the first approval issued it; a replay is
	// resolved from the completed request before the lot is consulted.
	replayed, err := svc.ApproveRequest(ctx, in)
	if err != nil {
		t.Fatalf("replayed approve: %v", err)
	}
	if replayed.Status != domain.RequestCompleted {
		t.Fatalf("replay status = %s", replayed.Status)
	}
	if len(store.movements) != 1 || len(store.audits) != 2 {
		t.Fatalf("replay wrote rows: %d movements, %d audits", len(store.movements), len(store.audits))
	}
}

func TestDenyRequestRequiresReasonAndWritesNoMovement(t *testing.T) {
	ctx := context.Background()
	store := newFakeAdmin()
	requestTestLot(store, 5)
	request := seedAdminRequest(store, 31, 5, requestTestNow)
	svc := testAdminService(store, nil, requestTestNow)

	_, err := svc.DenyRequest(ctx, domain.DenyRequestInput{
		RequestID: request.RequestID, Reason: "  ", Actor: officerActor(), CorrelationID: "corr-deny",
	})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["reason"] == "" {
		t.Fatalf("blank reason err = %v", err)
	}
	denied, err := svc.DenyRequest(ctx, domain.DenyRequestInput{
		RequestID: request.RequestID, Reason: "no stock this lockout", Actor: officerActor(),
		CorrelationID: "corr-deny", SourceIP: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("DenyRequest: %v", err)
	}
	if denied.Status != domain.RequestDenied || denied.DecisionNote != "no stock this lockout" ||
		denied.DecidedByName != "officer1" {
		t.Fatalf("request = %+v", denied)
	}
	if len(store.movements) != 0 {
		t.Fatalf("denial wrote %d movements", len(store.movements))
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "REQUEST_DENIED" ||
		store.audits[0].CorrelationID != "corr-1" {
		t.Fatalf("audits = %+v", store.audits)
	}
	if _, err := svc.DenyRequest(ctx, domain.DenyRequestInput{
		RequestID: request.RequestID, Reason: "again", Actor: officerActor(), CorrelationID: "corr-deny",
	}); !errors.Is(err, domain.ErrRequestNotPending) {
		t.Fatalf("second deny err = %v, want ErrRequestNotPending", err)
	}
	if len(store.audits) != 1 {
		t.Fatalf("second deny wrote an audit row: %+v", store.audits)
	}
}

func TestListRequestsIsOldestFirstAndActionableFilter(t *testing.T) {
	ctx := context.Background()
	store := newFakeAdmin()
	requestTestLot(store, 5)
	seedAdminRequest(store, 1, 5, requestTestNow.Add(-72*time.Hour))
	seedAdminRequest(store, 2, 5, requestTestNow.Add(-time.Hour))
	store.items[1] = domain.AdminInventoryItem{}
	svc := testAdminService(store, nil, requestTestNow)

	page, err := svc.ListRequests(ctx, domain.RequestFilter{Status: domain.RequestPending})
	if err != nil {
		t.Fatalf("ListRequests: %v", err)
	}
	if len(page.Requests) != 2 || page.Requests[0].RequestID != 1 {
		t.Fatalf("oldest-first order = %+v", page.Requests)
	}
	actionable, err := svc.ListRequests(ctx, domain.RequestFilter{
		Status: domain.RequestPending, Actionable: true,
	})
	if err != nil {
		t.Fatalf("actionable ListRequests: %v", err)
	}
	if actionable.Total != 0 {
		t.Fatalf("actionable total = %d, want 0", actionable.Total)
	}
	count, err := svc.PendingRequestCount(ctx)
	if err != nil || count != 2 {
		t.Fatalf("PendingRequestCount = %d, %v", count, err)
	}
}
