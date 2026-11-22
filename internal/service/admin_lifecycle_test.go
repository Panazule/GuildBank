package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func seededItem() domain.AdminInventoryItem {
	return domain.AdminInventoryItem{
		ID: 1, EQUID: 100, Name: "Sword", ItemType: domain.ItemTypeItem, Quantity: 1,
		ClassString: "WAR", RaceString: "ALL", SourceLocation: "Bank2-Slot4",
		LocationGroup: "Bank 2", LocationSort: "bank\x010000000002", GivenTo: "Guild Bank",
		DateAdded:  time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		DateOpen:   time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC),
		DateBazaar: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		ModAt:      time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC),
		ModBy:      "admin",
	}
}

func optional(value string) domain.OptionalString {
	return domain.OptionalString{Value: strPtr(value), Set: true}
}

func datePtr(year int, month time.Month, day int) *time.Time {
	value := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return &value
}

func TestAdminCreateMissingMetadataRequiresName(t *testing.T) {
	store := newFakeAdmin()
	metadata := fakeMetadata{300: {
		EQUID: 300, MetadataMissing: true,
		Warnings: []ItemWarning{{Code: ItemWarningMetadataMissing, Field: "EQUID", EQUID: 300}},
	}}
	svc := testAdminService(store, metadata, testClock())
	_, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeItem, EQUID: 300, Quantity: 1, Actor: testActor(),
	})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["name"] == "" {
		t.Fatalf("err = %v, want name validation", err)
	}
	result, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeItem, EQUID: 300, Quantity: 1, Name: "Unknown Trinket", Actor: testActor(),
	})
	if err != nil {
		t.Fatalf("create with fallback: %v", err)
	}
	if !result.Item.MetadataMissingFg || result.Item.Name != "Unknown Trinket" ||
		result.Item.ClassString != "" || result.Item.RaceString != "" {
		t.Fatalf("missing-metadata row = %+v", result.Item)
	}
}

func TestAdminUpdateNoOpWritesNothing(t *testing.T) {
	store := newFakeAdmin()
	store.items[1] = seededItem()
	svc := testAdminService(store, nil, testClock())
	result, err := svc.Update(context.Background(), domain.UpdateItemInput{
		ID: 1, ExpectedModAt: seededItem().ModAt, Quantity: 1,
		SourceLocation: optional("Bank2-Slot4"), GivenTo: optional("Guild Bank"),
		DateOpen:   domain.OptionalDate{Value: datePtr(2026, 11, 15), Set: true},
		DateBazaar: domain.OptionalDate{Value: datePtr(2026, 12, 1), Set: true},
		Actor:      testActor(),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if result.Changed {
		t.Fatal("no-op update reported a change")
	}
	if len(store.audits) != 0 {
		t.Fatalf("no-op update wrote %d audit rows", len(store.audits))
	}
	if !store.items[1].ModAt.Equal(seededItem().ModAt) {
		t.Fatal("no-op update changed ModAt")
	}
}

func TestAdminUpdateRejectsItemQuantityAboveOne(t *testing.T) {
	store := newFakeAdmin()
	store.items[1] = seededItem()
	svc := testAdminService(store, nil, testClock())
	_, err := svc.Update(context.Background(), domain.UpdateItemInput{
		ID: 1, ExpectedModAt: seededItem().ModAt, Quantity: 2,
		SourceLocation: optional("Bank2-Slot4"), GivenTo: optional("Guild Bank"),
		DateOpen:   domain.OptionalDate{Value: datePtr(2026, 11, 15), Set: true},
		DateBazaar: domain.OptionalDate{Value: datePtr(2026, 12, 1), Set: true},
		Actor:      testActor(),
	})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["quantity"] == "" {
		t.Fatalf("err = %v, want quantity validation", err)
	}
}

func TestAdminUpdateStaleTokenConflicts(t *testing.T) {
	store := newFakeAdmin()
	store.items[1] = seededItem()
	svc := testAdminService(store, nil, testClock())
	_, err := svc.Update(context.Background(), domain.UpdateItemInput{
		ID: 1, ExpectedModAt: seededItem().ModAt.Add(time.Second), Quantity: 1,
		SourceLocation: optional("Bank2-Slot4"), GivenTo: optional("Guild Bank"),
		DateOpen:   domain.OptionalDate{Value: datePtr(2026, 11, 15), Set: true},
		DateBazaar: domain.OptionalDate{Value: datePtr(2026, 12, 1), Set: true},
		Actor:      testActor(),
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestAdminUpdateDateChangeIsAuditedAndDateAddedIsFixed(t *testing.T) {
	store := newFakeAdmin()
	store.items[1] = seededItem()
	svc := testAdminService(store, nil, testClock())
	result, err := svc.Update(context.Background(), domain.UpdateItemInput{
		ID: 1, ExpectedModAt: seededItem().ModAt, Quantity: 1,
		SourceLocation: optional("Bank2-Slot4"), GivenTo: optional("Guild Bank"),
		DateOpen:   domain.OptionalDate{Value: datePtr(2026, 11, 20), Set: true},
		DateBazaar: domain.OptionalDate{Value: datePtr(2026, 12, 1), Set: true},
		Actor:      testActor(),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !result.Changed || result.Item.DateOpen.Format("2006-01-02") != "2026-11-20" {
		t.Fatalf("dateOpen not updated: %+v", result.Item)
	}
	if !result.Item.DateAdded.Equal(seededItem().DateAdded) {
		t.Fatal("DateAdded changed")
	}
	if len(store.audits) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(store.audits))
	}
	var oldValues, newValues map[string]any
	if err := json.Unmarshal(store.audits[0].OldValues, &oldValues); err != nil {
		t.Fatalf("old values: %v", err)
	}
	if err := json.Unmarshal(store.audits[0].NewValues, &newValues); err != nil {
		t.Fatalf("new values: %v", err)
	}
	if len(oldValues) != 1 || oldValues["dateOpen"] != "2026-11-15" {
		t.Fatalf("old values = %v", oldValues)
	}
	if len(newValues) != 1 || newValues["dateOpen"] != "2026-11-20" {
		t.Fatalf("new values = %v", newValues)
	}
}

func TestAdminUpdateResetUsesCurrentSettingsOffset(t *testing.T) {
	store := newFakeAdmin()
	store.items[1] = seededItem()
	svc := testAdminService(store, nil, testClock())
	result, err := svc.Update(context.Background(), domain.UpdateItemInput{
		ID: 1, ExpectedModAt: seededItem().ModAt, Quantity: 1,
		SourceLocation: optional("Bank2-Slot4"), GivenTo: optional("Guild Bank"),
		DateOpen:   domain.OptionalDate{Set: true},
		DateBazaar: domain.OptionalDate{Value: datePtr(2026, 12, 1), Set: true},
		Actor:      testActor(),
	})
	if err != nil {
		t.Fatalf("reset update: %v", err)
	}
	if got := result.Item.DateOpen.Format("2006-01-02"); got != "2026-11-15" {
		t.Fatalf("reset dateOpen = %s, want DateAdded + 14 days", got)
	}
}

func TestAdminRemoveAndRestore(t *testing.T) {
	store := newFakeAdmin()
	store.items[1] = seededItem()
	svc := testAdminService(store, nil, testClock())
	removed, err := svc.Remove(context.Background(), 1, seededItem().ModAt, testActor(), "req-3", "")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !removed.Item.DeletedFg {
		t.Fatal("row was not marked removed")
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "ITEM_REMOVED" {
		t.Fatalf("audits = %+v", store.audits)
	}
	if _, err := svc.Remove(context.Background(), 1, removed.Item.ModAt, testActor(), "", ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second remove err = %v, want conflict", err)
	}
	restored, err := svc.Restore(context.Background(), 1, removed.Item.ModAt, testActor(), "", "")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Item.DeletedFg {
		t.Fatal("row was not restored")
	}
	if len(store.audits) != 2 || store.audits[1].EventType != "ITEM_UPDATED" || store.audits[1].Notes != "restored" {
		t.Fatalf("restore audit = %+v", store.audits)
	}
}
