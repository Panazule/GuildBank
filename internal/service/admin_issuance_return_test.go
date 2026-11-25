package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func issuanceTestItem(id int64, quantity uint32, deleted bool, givenTo string) domain.AdminInventoryItem {
	return domain.AdminInventoryItem{
		ID: id, EQUID: 100, Name: "Ant's Potion", ItemType: domain.ItemTypeItem,
		Quantity: quantity, DeletedFg: deleted, GivenTo: givenTo,
		SourceLocation: "General1-Slot1", LocationGroup: "General 1", LocationSort: "general\x010000000001",
		NameSort:   "ants potion",
		DateAdded:  time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		DateOpen:   time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC),
		DateBazaar: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		ModAt:      time.Date(2026, 11, 24, 11, 0, 0, 0, time.UTC),
		ModBy:      "admin",
	}
}

func issueMovement(movementID, lotID int64, quantity uint32, recipient string) domain.InventoryMovement {
	return domain.InventoryMovement{
		MovementID: movementID, GuildBankItemID: lotID, EQUID: 100,
		MovementType: domain.MovementIssue, Quantity: quantity, Recipient: recipient,
		ActorUserName: "admin", OccurredAt: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC),
	}
}

// TestReturnIssuanceClosesPartialIssueOnActiveLot proves a movement-targeted
// return of a partial issue adds the units back to the same active lot and
// attributes the RETURN to the addressed ISSUE (D1).
func TestReturnIssuanceClosesPartialIssueOnActiveLot(t *testing.T) {
	store := newFakeAdmin()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	item := issuanceTestItem(1, 4, false, "")
	store.items[item.ID] = item
	store.movements = []domain.InventoryMovement{issueMovement(1, item.ID, 1, "Joe")}
	svc := testAdminService(store, nil, now)

	in := domain.ReturnIssuanceInput{MovementID: 1, ExpectedModAt: item.ModAt, Quantity: 1, Recipient: "Joe"}
	in.Actor = testActor()
	result, err := svc.ReturnIssuance(context.Background(), in)
	if err != nil {
		t.Fatalf("ReturnIssuance: %v", err)
	}
	if result.Item.Quantity != 5 || store.items[item.ID].Quantity != 5 {
		t.Fatalf("quantity = %d / stored %d, want 5", result.Item.Quantity, store.items[item.ID].Quantity)
	}
	if len(store.movements) != 2 {
		t.Fatalf("movements = %d, want 2", len(store.movements))
	}
	ret := store.movements[1]
	if ret.MovementType != domain.MovementReturn || ret.RelatedMovementID == nil || *ret.RelatedMovementID != 1 {
		t.Fatalf("return movement = %+v, want RelatedMovementID 1", ret)
	}
	if outstanding := store.movementOutstanding(1); outstanding != 0 {
		t.Fatalf("outstanding after full return = %d, want 0", outstanding)
	}
}

// TestReturnIssuanceRestoresWholeStackIssue proves a full return of a
// whole-stack issue restores the lot and clears the recipient (OD-02).
func TestReturnIssuanceRestoresWholeStackIssue(t *testing.T) {
	store := newFakeAdmin()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	item := issuanceTestItem(2, 5, true, "Joe")
	store.items[item.ID] = item
	store.movements = []domain.InventoryMovement{issueMovement(2, item.ID, 5, "Joe")}
	svc := testAdminService(store, nil, now)

	in := domain.ReturnIssuanceInput{MovementID: 2, ExpectedModAt: item.ModAt, Quantity: 5, Recipient: "Joe"}
	in.Actor = testActor()
	if _, err := svc.ReturnIssuance(context.Background(), in); err != nil {
		t.Fatalf("ReturnIssuance: %v", err)
	}
	restored := store.items[item.ID]
	if restored.DeletedFg || restored.GivenTo != "" || restored.Quantity != 5 {
		t.Fatalf("restored = %+v", restored)
	}
}

// TestReturnIssuanceOnRemovedLotCreatesNewActiveLot proves decision D6: a
// return against an administratively removed lot lands the units in a new
// active lot and leaves the removed lot removed.
func TestReturnIssuanceOnRemovedLotCreatesNewActiveLot(t *testing.T) {
	store := newFakeAdmin()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	item := issuanceTestItem(3, 2, true, "")
	store.items[item.ID] = item
	store.movements = []domain.InventoryMovement{issueMovement(3, item.ID, 2, "Joe")}
	svc := testAdminService(store, nil, now)

	in := domain.ReturnIssuanceInput{MovementID: 3, ExpectedModAt: item.ModAt, Quantity: 2, Recipient: "Joe"}
	in.Actor = testActor()
	result, err := svc.ReturnIssuance(context.Background(), in)
	if err != nil {
		t.Fatalf("ReturnIssuance: %v", err)
	}
	if result.CreatedCount != 1 {
		t.Fatalf("CreatedCount = %d, want 1", result.CreatedCount)
	}
	if !store.items[item.ID].DeletedFg {
		t.Fatalf("removed lot became %+v, want it to stay removed", store.items[item.ID])
	}
	created := store.items[result.Item.ID]
	if created.DeletedFg || created.Quantity != 2 || created.GivenTo != "" {
		t.Fatalf("created lot = %+v", created)
	}
}

// TestReturnIssuanceRejectsOverReturn proves the outstanding quantity is the
// hard upper bound.
func TestReturnIssuanceRejectsOverReturn(t *testing.T) {
	store := newFakeAdmin()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	item := issuanceTestItem(1, 4, false, "")
	store.items[item.ID] = item
	store.movements = []domain.InventoryMovement{issueMovement(1, item.ID, 1, "Joe")}
	svc := testAdminService(store, nil, now)

	in := domain.ReturnIssuanceInput{MovementID: 1, ExpectedModAt: item.ModAt, Quantity: 2}
	in.Actor = testActor()
	_, err := svc.ReturnIssuance(context.Background(), in)
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["quantity"] == "" {
		t.Fatalf("err = %v, want a quantity validation error", err)
	}
}

// TestLotLevelReturnAttributesFIFOIssue proves the existing lot-level return
// attributes the RETURN to the earliest outstanding ISSUE movement so the
// issued row leaves the Issued view.
func TestLotLevelReturnAttributesFIFOIssue(t *testing.T) {
	store := newFakeAdmin()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	item := issuanceTestItem(1, 3, false, "")
	store.items[item.ID] = item
	store.movements = []domain.InventoryMovement{issueMovement(1, item.ID, 2, "Joe")}
	svc := testAdminService(store, nil, now)

	in := domain.ReturnItemInput{GuildBankItemID: item.ID, ExpectedModAt: item.ModAt, Quantity: 2}
	in.Actor = testActor()
	if _, err := svc.Return(context.Background(), in); err != nil {
		t.Fatalf("Return: %v", err)
	}
	ret := store.movements[1]
	if ret.RelatedMovementID == nil || *ret.RelatedMovementID != 1 {
		t.Fatalf("return movement = %+v, want FIFO attribution to 1", ret)
	}
	if outstanding := store.movementOutstanding(1); outstanding != 0 {
		t.Fatalf("outstanding = %d, want 0", outstanding)
	}
}
