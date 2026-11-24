package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

func issuedItem() domain.AdminInventoryItem {
	item := seededItem()
	item.Quantity = 10
	item.GivenTo = ""
	return item
}

func TestAdminIssuePartialReducesLotAndWritesMovement(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	store.items[item.ID] = item
	// Partial issue is governed by the on-hand quantity, so a non-stackable
	// classification must not block it.
	store.stackability = map[uint64]domain.Stackability{item.EQUID: domain.StackabilityNonStackable}
	svc := testAdminService(store, nil, testClock())

	result, err := svc.Issue(context.Background(), domain.IssueItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: item.ModAt, Quantity: 4,
		Recipient: "Alice", IdempotencyKey: "issue-1", Actor: testActor(),
		CorrelationID: "req-issue",
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if result.Item.Quantity != 6 || result.Item.DeletedFg {
		t.Fatalf("result = %+v, want active quantity 6", result.Item)
	}
	if store.items[item.ID].Quantity != 6 {
		t.Fatalf("stored quantity = %d, want 6", store.items[item.ID].Quantity)
	}
	if len(store.movements) != 1 {
		t.Fatalf("movements = %d, want 1", len(store.movements))
	}
	movement := store.movements[0]
	if movement.MovementType != domain.MovementIssue || movement.Quantity != 4 ||
		movement.RemainingQuantity != 6 || movement.Recipient != "Alice" ||
		movement.IdempotencyKey != "issue-1" || movement.CorrelationID != "req-issue" {
		t.Fatalf("movement = %+v", movement)
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "ITEM_ISSUED" {
		t.Fatalf("audits = %+v, want one ITEM_ISSUED", store.audits)
	}
	if store.committed != 1 || store.rolled != 0 {
		t.Fatalf("committed/rolled = %d/%d, want 1/0", store.committed, store.rolled)
	}
}

func TestAdminIssueFullStackSoftDeletesAndRecordsRecipient(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	item.Quantity = 5
	store.items[item.ID] = item
	svc := testAdminService(store, nil, testClock())

	result, err := svc.Issue(context.Background(), domain.IssueItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: item.ModAt, Quantity: 5,
		Recipient: "Alice", Actor: testActor(),
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !result.Item.DeletedFg || result.Item.GivenTo != "Alice" || result.Item.Quantity != 5 {
		t.Fatalf("result = %+v, want deleted whole-issued lot", result.Item)
	}
	if len(store.movements) != 1 || store.movements[0].RemainingQuantity != 0 {
		t.Fatalf("movements = %+v, want one zero-remaining ISSUE", store.movements)
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "ITEM_ISSUED" {
		t.Fatalf("audits = %+v, want one ITEM_ISSUED", store.audits)
	}
}

func TestAdminIssuePartialAllowedByQuantityNotMetadata(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	item.Quantity = 5
	store.items[item.ID] = item
	// A non-stackable classification must not block a partial issue: the rule is
	// the active lot quantity currently on hand, which is 5 here.
	store.stackability = map[uint64]domain.Stackability{item.EQUID: domain.StackabilityNonStackable}
	svc := testAdminService(store, nil, testClock())

	result, err := svc.Issue(context.Background(), domain.IssueItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: item.ModAt, Quantity: 2,
		Recipient: "Alice", Actor: testActor(),
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if result.Item.Quantity != 3 || result.Item.DeletedFg || result.Item.GivenTo != "" {
		t.Fatalf("result = %+v, want active quantity 3 and no recipient", result.Item)
	}
	if len(store.movements) != 1 || store.movements[0].MovementType != domain.MovementIssue ||
		store.movements[0].Quantity != 2 || store.movements[0].RemainingQuantity != 3 {
		t.Fatalf("movements = %+v", store.movements)
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "ITEM_ISSUED" {
		t.Fatalf("audits = %+v, want one ITEM_ISSUED", store.audits)
	}
	if store.committed != 1 || store.rolled != 0 {
		t.Fatalf("committed/rolled = %d/%d, want 1/0", store.committed, store.rolled)
	}
}

func TestAdminIssueRejectsActiveRowWithRecipient(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	item.GivenTo = "Alice"
	store.items[item.ID] = item
	svc := testAdminService(store, nil, testClock())

	_, err := svc.Issue(context.Background(), domain.IssueItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: item.ModAt, Quantity: 1,
		Recipient: "Bob", Actor: testActor(),
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if store.rolled != 1 || len(store.movements) != 0 {
		t.Fatalf("rolled/movements = %d/%d, want 1/0", store.rolled, len(store.movements))
	}
}

func TestAdminIssueStaleTokenConflicts(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	store.items[item.ID] = item
	svc := testAdminService(store, nil, testClock())

	_, err := svc.Issue(context.Background(), domain.IssueItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: item.ModAt.Add(-1), Quantity: 1,
		Recipient: "Alice", Actor: testActor(),
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestAdminIssueRollsBackWhenAuditFails(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	store.items[item.ID] = item
	store.failAudit = true
	svc := testAdminService(store, nil, testClock())

	_, err := svc.Issue(context.Background(), domain.IssueItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: item.ModAt, Quantity: 3,
		Recipient: "Alice", Actor: testActor(),
	})
	if err == nil {
		t.Fatal("issue succeeded despite audit failure")
	}
	if store.rolled != 1 || store.committed != 0 {
		t.Fatalf("rolled/committed = %d/%d, want 1/0", store.rolled, store.committed)
	}
	if store.items[item.ID].Quantity != 10 || len(store.movements) != 0 || len(store.audits) != 0 {
		t.Fatalf("rolled back state = item %+v movements %d audits %d",
			store.items[item.ID], len(store.movements), len(store.audits))
	}
}

func TestAdminReturnToActiveIncreasesLot(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	item.Quantity = 5
	store.items[item.ID] = item
	svc := testAdminService(store, nil, testClock())

	result, err := svc.Return(context.Background(), domain.ReturnItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: item.ModAt, Quantity: 3,
		Recipient: "Alice", Note: "partial return", Actor: testActor(),
	})
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	if result.Item.Quantity != 8 || result.Item.DeletedFg {
		t.Fatalf("result = %+v, want active quantity 8", result.Item)
	}
	if len(store.movements) != 1 || store.movements[0].MovementType != domain.MovementReturn ||
		store.movements[0].Quantity != 3 || store.movements[0].RemainingQuantity != 8 {
		t.Fatalf("movements = %+v", store.movements)
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "ITEM_RETURNED" {
		t.Fatalf("audits = %+v, want one ITEM_RETURNED", store.audits)
	}
}

func TestAdminReturnWholeIssuedRestoresRow(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	item.DeletedFg = true
	item.GivenTo = "Alice"
	store.items[item.ID] = item
	svc := testAdminService(store, nil, testClock())

	result, err := svc.Return(context.Background(), domain.ReturnItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: item.ModAt, Quantity: 10,
		Recipient: "Alice", Actor: testActor(),
	})
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	if result.Item.DeletedFg || result.Item.GivenTo != "" || result.Item.Quantity != 10 {
		t.Fatalf("result = %+v, want restored active lot without recipient", result.Item)
	}
	if len(store.movements) != 1 || store.movements[0].MovementType != domain.MovementReturn {
		t.Fatalf("movements = %+v, want one RETURN", store.movements)
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "ITEM_RETURNED" {
		t.Fatalf("audits = %+v, want one ITEM_RETURNED", store.audits)
	}
}
func TestAdminReturnPartialIssuedCreatesNewLot(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	item.DeletedFg = true
	item.GivenTo = "Alice"
	store.items[item.ID] = item
	svc := testAdminService(store, nil, testClock())

	result, err := svc.Return(context.Background(), domain.ReturnItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: item.ModAt, Quantity: 4,
		Recipient: "Alice", Actor: testActor(),
	})
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	if result.CreatedCount != 1 || len(result.CreatedIDs) != 1 {
		t.Fatalf("created = %d/%v, want 1/1", result.CreatedCount, result.CreatedIDs)
	}
	if !store.items[item.ID].DeletedFg || store.items[item.ID].Quantity != 10 {
		t.Fatalf("original = %+v, want deleted historical lot", store.items[item.ID])
	}
	newItem := store.items[result.CreatedIDs[0]]
	if newItem.DeletedFg || newItem.Quantity != 4 || newItem.GivenTo != "" {
		t.Fatalf("new lot = %+v, want active quantity 4", newItem)
	}
	if len(store.movements) != 1 || store.movements[0].RelatedItemID == nil ||
		*store.movements[0].RelatedItemID != item.ID {
		t.Fatalf("movements = %+v, want RETURN referencing original", store.movements)
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "ITEM_RETURNED" {
		t.Fatalf("audits = %+v, want one ITEM_RETURNED", store.audits)
	}
}

func TestAdminReturnRejectsAdministrativelyRemovedRow(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	item.DeletedFg = true
	item.GivenTo = ""
	store.items[item.ID] = item
	svc := testAdminService(store, nil, testClock())

	_, err := svc.Return(context.Background(), domain.ReturnItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: item.ModAt, Quantity: 1,
		Actor: testActor(),
	})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["guildBankItemId"] == "" {
		t.Fatalf("err = %v, want guildBankItemId validation", err)
	}
	if len(store.movements) != 0 || len(store.audits) != 0 {
		t.Fatalf("movements/audits = %d/%d, want 0/0", len(store.movements), len(store.audits))
	}
}

func TestAdminOnHandSumsActiveRowsOnly(t *testing.T) {
	store := newFakeAdmin()
	first := issuedItem()
	first.Quantity = 5
	second := issuedItem()
	second.ID = 2
	second.Quantity = 3
	removed := issuedItem()
	removed.ID = 3
	removed.Quantity = 100
	removed.DeletedFg = true
	store.items[first.ID] = first
	store.items[second.ID] = second
	store.items[removed.ID] = removed
	svc := testAdminService(store, nil, testClock())

	total, err := svc.OnHand(context.Background(), domain.AdminInventoryFilter{})
	if err != nil {
		t.Fatalf("on hand: %v", err)
	}
	if total != 8 {
		t.Fatalf("on hand = %d, want 8", total)
	}
}

func TestAdminListAndGetMovement(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	store.items[item.ID] = item
	svc := testAdminService(store, nil, testClock())

	if _, err := svc.Issue(context.Background(), domain.IssueItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: item.ModAt, Quantity: 2,
		Recipient: "Alice", Actor: testActor(), CorrelationID: "req-list",
	}); err != nil {
		t.Fatalf("issue: %v", err)
	}
	page, err := svc.ListMovements(context.Background(), domain.MovementFilter{
		GuildBankItemID: item.ID, MovementType: domain.MovementIssue,
	})
	if err != nil {
		t.Fatalf("list movements: %v", err)
	}
	if page.Total != 1 || len(page.Movements) != 1 {
		t.Fatalf("page = %+v, want one movement", page)
	}
	movement, err := svc.GetMovement(context.Background(), page.Movements[0].MovementID)
	if err != nil {
		t.Fatalf("get movement: %v", err)
	}
	if movement.CorrelationID != "req-list" || movement.MovementType != domain.MovementIssue {
		t.Fatalf("movement = %+v", movement)
	}
}

func TestAdminIssueIdempotencyKeyConflictRollsBack(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	store.items[item.ID] = item
	// No classification is registered, so the fake reports unknown: partial
	// issue must still succeed because it is quantity-based.
	svc := testAdminService(store, nil, testClock())

	first, err := svc.Issue(context.Background(), domain.IssueItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: item.ModAt, Quantity: 2,
		Recipient: "Alice", IdempotencyKey: "same-key", Actor: testActor(),
	})
	if err != nil {
		t.Fatalf("first issue: %v", err)
	}
	_, err = svc.Issue(context.Background(), domain.IssueItemInput{
		GuildBankItemID: item.ID, ExpectedModAt: first.Item.ModAt, Quantity: 2,
		Recipient: "Alice", IdempotencyKey: "same-key", Actor: testActor(),
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second issue err = %v, want ErrConflict", err)
	}
	if store.items[item.ID].Quantity != 8 {
		t.Fatalf("quantity = %d, want 8 after rollback", store.items[item.ID].Quantity)
	}
	if len(store.movements) != 1 || len(store.audits) != 1 {
		t.Fatalf("movements/audits = %d/%d, want 1/1", len(store.movements), len(store.audits))
	}
}

func TestAdminRestoreUsesReceiveForAdministrativeRemoval(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	store.items[item.ID] = item
	svc := testAdminService(store, nil, testClock())

	removed, err := svc.Remove(context.Background(), item.ID, item.ModAt, testActor(), "req-remove", "")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(store.movements) != 1 || store.movements[0].MovementType != domain.MovementWriteOff {
		t.Fatalf("remove movements = %+v, want WRITE_OFF", store.movements)
	}
	if _, err := svc.Restore(context.Background(), item.ID, removed.Item.ModAt, testActor(), "req-restore", ""); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(store.movements) != 2 || store.movements[1].MovementType != domain.MovementReceive {
		t.Fatalf("restore movements = %+v, want RECEIVE", store.movements)
	}
	if len(store.audits) != 2 || store.audits[1].EventType != "ITEM_RECEIVED" {
		t.Fatalf("audits = %+v, want ITEM_RECEIVED", store.audits)
	}
}

func TestAdminRestoreUsesReturnForWholeIssuedRow(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	item.DeletedFg = true
	item.GivenTo = "Alice"
	store.items[item.ID] = item
	svc := testAdminService(store, nil, testClock())

	result, err := svc.Restore(context.Background(), item.ID, item.ModAt, testActor(), "req-restore", "")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.Item.DeletedFg || result.Item.GivenTo != "" {
		t.Fatalf("result = %+v, want restored row without recipient", result.Item)
	}
	if len(store.movements) != 1 || store.movements[0].MovementType != domain.MovementReturn {
		t.Fatalf("movements = %+v, want RETURN", store.movements)
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "ITEM_RETURNED" {
		t.Fatalf("audits = %+v, want ITEM_RETURNED", store.audits)
	}
}

func TestAdminCreateRejectsGivenToOnActiveRow(t *testing.T) {
	store := newFakeAdmin()
	svc := testAdminService(store, nil, testClock())

	_, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeItem, EQUID: 100, Quantity: 1,
		GivenTo: optional("Alice"), Actor: testActor(),
	})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["givenTo"] == "" {
		t.Fatalf("err = %v, want givenTo validation", err)
	}
}

func TestAdminUpdateRejectsGivenToOnActiveRow(t *testing.T) {
	store := newFakeAdmin()
	item := issuedItem()
	store.items[item.ID] = item
	svc := testAdminService(store, nil, testClock())

	_, err := svc.Update(context.Background(), domain.UpdateItemInput{
		ID: item.ID, ExpectedModAt: item.ModAt, Quantity: 1,
		SourceLocation: optional("General-Slot"), GivenTo: optional("Alice"),
		DateOpen: domain.OptionalDate{Value: datePtr(2026, 11, 15), Set: true},
		Actor:    testActor(),
	})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["givenTo"] == "" {
		t.Fatalf("err = %v, want givenTo validation", err)
	}
}
