package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func fullUpdateInput(id int64, modAt time.Time) domain.UpdateItemInput {
	return domain.UpdateItemInput{
		ID: id, ExpectedModAt: modAt, Quantity: 1,
		SourceLocation: optional("Bank2-Slot4"), GivenTo: optional("Guild Bank"),
		DateOpen:   domain.OptionalDate{Value: datePtr(2026, 11, 15), Set: true},
		DateBazaar: domain.OptionalDate{Value: datePtr(2026, 12, 1), Set: true},
		Actor:      testActor(), CorrelationID: "req-1",
	}
}

func TestAdminUpdateAndRemoveShareOneTransaction(t *testing.T) {
	store := newFakeAdmin()
	store.items[1] = seededItem()
	svc := testAdminService(store, nil, testClock())

	in := fullUpdateInput(1, seededItem().ModAt)
	in.GivenTo = optional("Bazaar")
	in.Remove = true
	result, err := svc.Update(context.Background(), in)
	if err != nil {
		t.Fatalf("update+remove: %v", err)
	}
	if !result.Changed || !result.FieldsChanged || !result.Item.DeletedFg {
		t.Fatalf("result = %+v, want changed + fieldsChanged + deleted", result)
	}
	if store.committed != 1 || store.rolled != 0 {
		t.Fatalf("committed/rolled = %d/%d, want 1/0", store.committed, store.rolled)
	}
	if len(store.audits) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(store.audits))
	}
	if store.audits[0].EventType != "ITEM_UPDATED" || store.audits[1].EventType != "ITEM_REMOVED" {
		t.Fatalf("audit types = %s/%s", store.audits[0].EventType, store.audits[1].EventType)
	}
	if store.audits[0].CorrelationID != store.audits[1].CorrelationID ||
		store.audits[0].CorrelationID != "req-1" {
		t.Fatalf("correlation ids = %q/%q", store.audits[0].CorrelationID, store.audits[1].CorrelationID)
	}
	if !store.audits[0].EventDate.Equal(store.audits[1].EventDate) {
		t.Fatal("edit and removal audit rows do not share EventDate")
	}
	if !store.items[1].DeletedFg || store.items[1].GivenTo != "Bazaar" {
		t.Fatalf("stored item = %+v, want edited + removed", store.items[1])
	}
}

func TestAdminUpdateAndRemoveWithoutEditsWritesOnlyRemoval(t *testing.T) {
	store := newFakeAdmin()
	store.items[1] = seededItem()
	svc := testAdminService(store, nil, testClock())

	in := fullUpdateInput(1, seededItem().ModAt)
	in.Remove = true
	result, err := svc.Update(context.Background(), in)
	if err != nil {
		t.Fatalf("remove-only update: %v", err)
	}
	if !result.Changed || result.FieldsChanged || !result.Item.DeletedFg {
		t.Fatalf("result = %+v, want changed without field changes, deleted", result)
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "ITEM_REMOVED" {
		t.Fatalf("audits = %+v, want one ITEM_REMOVED", store.audits)
	}
}
func TestAdminRemoveManyIsAllOrNothing(t *testing.T) {
	store := newFakeAdmin()
	third := seededItem()
	third.ID = 3
	store.items[1] = seededItem()
	store.items[3] = third
	svc := testAdminService(store, nil, testClock())

	targets := []domain.RemoveItemTarget{
		{ID: 3, ExpectedModAt: third.ModAt},
		{ID: 1, ExpectedModAt: seededItem().ModAt},
	}
	result, err := svc.RemoveMany(context.Background(), targets, testActor(), "req-bulk", "")
	if err != nil {
		t.Fatalf("bulk remove: %v", err)
	}
	if result.Removed != 2 || len(result.Items) != 2 {
		t.Fatalf("result = %+v, want two removals", result)
	}
	if store.committed != 1 || store.rolled != 0 {
		t.Fatalf("committed/rolled = %d/%d, want 1/0", store.committed, store.rolled)
	}
	if len(store.audits) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(store.audits))
	}
	for _, entry := range store.audits {
		if entry.EventType != "ITEM_REMOVED" || entry.CorrelationID != "req-bulk" {
			t.Fatalf("audit = %+v", entry)
		}
	}
	if !store.items[1].DeletedFg || !store.items[3].DeletedFg {
		t.Fatal("one of the selected rows was not removed")
	}
}

func TestAdminRemoveManyStaleTokenRollsBackEverything(t *testing.T) {
	store := newFakeAdmin()
	third := seededItem()
	third.ID = 3
	store.items[1] = seededItem()
	store.items[3] = third
	svc := testAdminService(store, nil, testClock())

	targets := []domain.RemoveItemTarget{
		{ID: 1, ExpectedModAt: seededItem().ModAt},
		{ID: 3, ExpectedModAt: third.ModAt.Add(time.Second)},
	}
	_, err := svc.RemoveMany(context.Background(), targets, testActor(), "req-bulk", "")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if store.committed != 0 || store.rolled != 1 {
		t.Fatalf("committed/rolled = %d/%d, want 0/1", store.committed, store.rolled)
	}
	if store.items[1].DeletedFg || store.items[3].DeletedFg {
		t.Fatal("a rolled-back bulk removal left a row deleted")
	}
	if len(store.audits) != 0 {
		t.Fatalf("audit rows = %d, want 0 after rollback", len(store.audits))
	}
}

func TestAdminRemoveManyValidationAndPreview(t *testing.T) {
	store := newFakeAdmin()
	store.items[1] = seededItem()
	removed := seededItem()
	removed.ID = 2
	removed.DeletedFg = true
	store.items[2] = removed
	svc := testAdminService(store, nil, testClock())

	if _, err := svc.RemoveMany(context.Background(), nil, testActor(), "req", ""); err == nil {
		t.Fatal("empty bulk removal was accepted")
	}
	duplicates := []domain.RemoveItemTarget{
		{ID: 1, ExpectedModAt: seededItem().ModAt},
		{ID: 1, ExpectedModAt: seededItem().ModAt},
	}
	_, err := svc.RemoveMany(context.Background(), duplicates, testActor(), "req", "")
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["remove"] == "" {
		t.Fatalf("duplicate selection err = %v, want remove validation", err)
	}
	if _, err := svc.PreviewRemoveMany(context.Background(), []domain.RemoveItemTarget{
		{ID: 1, ExpectedModAt: seededItem().ModAt},
	}); err != nil {
		t.Fatalf("preview active row: %v", err)
	}
	if _, err := svc.PreviewRemoveMany(context.Background(), []domain.RemoveItemTarget{
		{ID: 2, ExpectedModAt: removed.ModAt},
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("preview removed row err = %v, want ErrConflict", err)
	}
	if _, err := svc.PreviewRemoveMany(context.Background(), []domain.RemoveItemTarget{
		{ID: 1, ExpectedModAt: seededItem().ModAt.Add(time.Second)},
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("preview stale row err = %v, want ErrConflict", err)
	}
}
