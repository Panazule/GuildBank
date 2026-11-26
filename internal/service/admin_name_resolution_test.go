package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// missingMetadata mirrors ItemMetadataService: an EQUID absent from Items with
// a name fallback is a non-fatal MetadataMissing result, not an error.
type missingMetadata struct{}

func (missingMetadata) Resolve(_ context.Context, ref domain.ItemReference) (ItemMetadata, error) {
	if strings.TrimSpace(ref.Name) == "" {
		return ItemMetadata{}, domain.NewValidationError("Name", "name is required")
	}
	return ItemMetadata{
		EQUID: ref.EQUID, Name: ref.Name, MetadataMissing: true,
		Warnings: []ItemWarning{{Code: ItemWarningMetadataMissing, Field: "EQUID", EQUID: ref.EQUID}},
	}, nil
}

func nameFirstService(store *fakeAdmin, reader ItemNameReader,
	metadata AdminMetadataResolver, now time.Time) *AdminInventoryService {
	return NewAdminInventoryService(store,
		fakeSettings{"GuildOpenDays": "14", "BazaarDays": "30"},
		metadata, NewItemNameResolver(reader), func() time.Time { return now }, nil)
}

func hasWarning(warnings []ItemWarning, code string) bool {
	for _, warning := range warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}

func TestCreateResolvesUniqueItemName(t *testing.T) {
	store := newFakeAdmin()
	reader := &nameReader{byName: map[string][]domain.Item{
		"Sword of Truth": {{EQUID: 100, Name: "Sword of Truth"}},
	}}
	svc := nameFirstService(store, reader, fakeMetadata{
		100: {EQUID: 100, Name: "Sword of Truth", ClassString: "WAR", RaceString: "ALL"},
	}, testClock())

	got, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeItem, Name: "Sword of Truth", Quantity: 2, Actor: testActor(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Item.EQUID != 100 {
		t.Fatalf("EQUID = %d, want the resolved 100", got.Item.EQUID)
	}
	if got.Item.Name != "Sword of Truth" {
		t.Fatalf("Name = %q, want the metadata name", got.Item.Name)
	}
	if got.Item.MetadataMissingFg {
		t.Fatal("resolved row marked as metadata missing")
	}
	if hasWarning(got.Warnings, ItemWarningNameMismatch) {
		t.Fatalf("resolved name raised a mismatch warning: %+v", got.Warnings)
	}
}

func TestCreateAmbiguousItemNameReturnsCandidates(t *testing.T) {
	store := newFakeAdmin()
	reader := &nameReader{byName: map[string][]domain.Item{
		"Ring": {{EQUID: 10, Name: "Ring"}, {EQUID: 11, Name: "Ring"}},
	}}
	svc := nameFirstService(store, reader, fakeMetadata{}, testClock())
	before := len(store.items)

	_, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeItem, Name: "Ring", Quantity: 1, Actor: testActor(),
	})
	var ambiguous *domain.ItemNameAmbiguousError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("Create error = %v, want ItemNameAmbiguousError", err)
	}
	if len(ambiguous.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(ambiguous.Candidates))
	}
	if len(store.items) != before {
		t.Fatal("an ambiguous name created a row")
	}
}

func TestCreateUnresolvedItemNameStoresPlaceholder(t *testing.T) {
	store := newFakeAdmin()
	svc := nameFirstService(store, &nameReader{}, missingMetadata{}, testClock())

	got, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeItem, Name: "Mystery Trinket", Quantity: 2, Actor: testActor(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Item.EQUID != domain.UnresolvedEQUID {
		t.Fatalf("EQUID = %d, want the unresolved placeholder %d", got.Item.EQUID, domain.UnresolvedEQUID)
	}
	if !got.Item.HasUnresolvedIdentity() {
		t.Fatal("row does not report an unresolved identity")
	}
	if got.Item.Name != "Mystery Trinket" {
		t.Fatalf("Name = %q, want the entered name to stay authoritative", got.Item.Name)
	}
	if !got.Item.MetadataMissingFg {
		t.Fatal("placeholder row must set MetadataMissingFg")
	}
	if !hasWarning(got.Warnings, ItemWarningIdentityUnresolved) {
		t.Fatalf("warnings = %+v, want %s", got.Warnings, ItemWarningIdentityUnresolved)
	}
	if hasWarning(got.Warnings, ItemWarningMetadataMissing) {
		t.Fatalf("placeholder row kept the generic metadata_missing warning: %+v", got.Warnings)
	}
}

func TestCreateRejectsReservedPlaceholderEQUID(t *testing.T) {
	store := newFakeAdmin()
	svc := nameFirstService(store, &nameReader{}, fakeMetadata{}, testClock())

	_, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeItem, EQUID: domain.UnresolvedEQUID,
		Name: "Sword", Quantity: 1, Actor: testActor(),
	})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("Create error = %v, want ValidationError", err)
	}
	if _, ok := validation.Fields["equid"]; !ok {
		t.Fatalf("validation fields = %+v, want an equid error", validation.Fields)
	}
	if len(store.items) != 0 {
		t.Fatal("a client-supplied placeholder EQUID created a row")
	}
}

func TestCreateRequiresNameOrEQUID(t *testing.T) {
	store := newFakeAdmin()
	svc := nameFirstService(store, &nameReader{}, fakeMetadata{}, testClock())

	_, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeItem, Quantity: 1, Actor: testActor(),
	})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("Create error = %v, want ValidationError", err)
	}
	if _, ok := validation.Fields["equid"]; !ok {
		t.Fatalf("validation fields = %+v, want an equid error", validation.Fields)
	}
	if _, ok := validation.Fields["name"]; !ok {
		t.Fatalf("validation fields = %+v, want a name error", validation.Fields)
	}
}

// An unresolved spell must never merge into another placeholder row: the
// placeholder is one identifier shared by different unknown items.
func TestCreateUnresolvedSpellDoesNotMerge(t *testing.T) {
	store := newFakeAdmin()
	store.items[7] = domain.AdminInventoryItem{
		ID: 7, EQUID: domain.UnresolvedEQUID, Name: "Known Mystery Spell",
		ItemType: domain.ItemTypeSpell, Quantity: 1,
		DateAdded: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}
	store.nextID = 8
	svc := nameFirstService(store, &nameReader{}, missingMetadata{}, testClock())

	got, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeSpell, Name: "Other Mystery Spell", Quantity: 2, Actor: testActor(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Item.ID == 7 {
		t.Fatal("the placeholder spell merged into an existing placeholder row")
	}
	if store.items[7].Quantity != 1 {
		t.Fatalf("existing placeholder quantity = %d, want it untouched at 1", store.items[7].Quantity)
	}
	if got.CreatedCount != 1 {
		t.Fatalf("createdCount = %d, want 1 new row", got.CreatedCount)
	}
}
