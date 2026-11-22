package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func testClock() time.Time { return time.Date(2026, 11, 22, 15, 4, 5, 0, time.UTC) }

func strPtr(value string) *string { return &value }

func TestAdminCreateItemWritesOneRowWithQuantity(t *testing.T) {
	store := newFakeAdmin()
	svc := testAdminService(store, nil, testClock())
	result, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeItem, EQUID: 100, Quantity: 3,
		SourceLocation: domain.OptionalString{Value: strPtr("General5-Slot1"), Set: true},
		GivenTo:        domain.OptionalString{Value: strPtr("Guild Bank"), Set: true},
		Actor:          testActor(), CorrelationID: "req-1", SourceIP: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result.CreatedCount != 1 || len(result.CreatedIDs) != 1 {
		t.Fatalf("created = %d ids %v, want 1/1", result.CreatedCount, result.CreatedIDs)
	}
	if result.Item.Quantity != 3 {
		t.Fatalf("item quantity = %d, want 3", result.Item.Quantity)
	}
	if got := result.Item.DateAdded.Format("2006-01-02"); got != "2026-11-22" {
		t.Fatalf("dateAdded = %s", got)
	}
	if got := result.Item.DateOpen.Format("2006-01-02"); got != "2026-12-06" {
		t.Fatalf("dateOpen = %s", got)
	}
	if got := result.Item.DateBazaar.Format("2006-01-02"); got != "2026-12-22" {
		t.Fatalf("dateBazaar = %s", got)
	}
	if result.Item.LocationGroup != "General 5" {
		t.Fatalf("location group = %q, want General 5", result.Item.LocationGroup)
	}
	if result.Item.LocationSort != "general\x010000000005" {
		t.Fatalf("location sort = %q", result.Item.LocationSort)
	}
	if result.Item.SourceLocation != "General5-Slot1" {
		t.Fatalf("raw location changed to %q", result.Item.SourceLocation)
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "ITEM_CREATED" {
		t.Fatalf("audits = %+v, want one ITEM_CREATED", store.audits)
	}
	if store.audits[0].RecordID != "1" || store.audits[0].CorrelationID != "req-1" {
		t.Fatalf("audit record/correlation = %s/%s", store.audits[0].RecordID, store.audits[0].CorrelationID)
	}
}

func TestAdminCreateSpellMergesLowestID(t *testing.T) {
	store := newFakeAdmin()
	base := domain.AdminInventoryItem{
		EQUID: 200, Name: "Clarity", ItemType: domain.ItemTypeSpell,
		DateAdded:  time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		DateOpen:   time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC),
		DateBazaar: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		ModAt:      time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}
	first := base
	first.ID, first.Quantity = 9, 2
	second := base
	second.ID, second.Quantity = 10, 4
	store.items[9] = first
	store.items[10] = second
	svc := testAdminService(store, nil, testClock())
	result, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeSpell, EQUID: 200, Quantity: 3,
		Actor: testActor(), CorrelationID: "req-2",
	})
	if err != nil {
		t.Fatalf("create spell: %v", err)
	}
	if result.CreatedCount != 0 || len(result.CreatedIDs) != 0 {
		t.Fatalf("spell merge created %d rows", result.CreatedCount)
	}
	if result.Item.ID != 9 || result.Item.Quantity != 5 {
		t.Fatalf("merge target = %d qty %d, want 9/5", result.Item.ID, result.Item.Quantity)
	}
	if store.items[10].Quantity != 4 {
		t.Fatalf("duplicate spell row changed to %d", store.items[10].Quantity)
	}
	var duplicateWarning bool
	for _, warning := range result.Warnings {
		if warning.Code == ItemWarningDuplicateSpellTarget {
			duplicateWarning = true
		}
	}
	if !duplicateWarning {
		t.Fatalf("missing duplicate_spell_target warning: %+v", result.Warnings)
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "ITEM_UPDATED" ||
		store.audits[0].Notes != "spell quantity merged" {
		t.Fatalf("audit = %+v", store.audits)
	}
}

func TestAdminCreateSpellMergeRejectsDateOverride(t *testing.T) {
	store := newFakeAdmin()
	store.items[5] = domain.AdminInventoryItem{
		ID: 5, EQUID: 200, ItemType: domain.ItemTypeSpell, Quantity: 1,
		DateAdded:  time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		DateOpen:   time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC),
		DateBazaar: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
	}
	dateOpen := time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)
	svc := testAdminService(store, nil, testClock())
	_, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeSpell, EQUID: 200, Quantity: 1,
		DateOpen: domain.OptionalDate{Value: &dateOpen, Set: true},
		Actor:    testActor(),
	})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["dateOpen"] == "" {
		t.Fatalf("err = %v, want dateOpen validation", err)
	}
	if len(store.audits) != 0 {
		t.Fatalf("audits written on rejected merge: %+v", store.audits)
	}
}
