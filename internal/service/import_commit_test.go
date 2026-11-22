package service

import (
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestBuildCommitPlanExpandsItemsAndAggregatesSpells(t *testing.T) {
	now := time.Date(2026, 11, 21, 23, 59, 0, 0, time.UTC)
	rows := []domain.ImportRow{
		{ImportID: 1, ItemType: domain.ItemTypeItem, EQUID: 10, Name: "Sword", Quantity: 2, LocationRaw: "General 1"},
		{ImportID: 2, ItemType: domain.ItemTypeItem, EQUID: 11, Name: "Shield", Quantity: 1, LocationRaw: "Bank 2"},
		{ImportID: 3, ItemType: domain.ItemTypeSpell, EQUID: 20, Name: "Spell: Heal", Quantity: 1, LocationRaw: "Spell 1"},
		{ImportID: 4, ItemType: domain.ItemTypeSpell, EQUID: 20, Name: "Spell: Heal", Quantity: 2, LocationRaw: "Spell 2"},
		{ImportID: 5, ItemType: domain.ItemTypeSpell, EQUID: 21, Name: "Song: Chant", Quantity: 2, LocationRaw: "Spell 1"},
	}
	metadata := map[uint64]ItemMetadata{
		10: {EQUID: 10, Name: "Canonical Sword", ClassString: "WAR", RaceString: "ALL"},
		11: {EQUID: 11, Name: "Canonical Shield", ClassString: "CLR", RaceString: "Human"},
		21: {EQUID: 21, Name: "Song: Canonical Chant"},
	}
	active := []domain.GuildBankItem{{GuildBankItemID: 77, EQUID: 20, Quantity: 4, SourceLocation: "Old spell"}}
	plan, err := buildCommitPlan(rows, active, metadata, domain.Actor{UserID: 1, UserName: "admin"}, now, 14, 30)
	if err != nil {
		t.Fatalf("buildCommitPlan: %v", err)
	}
	if plan.itemCount != 3 || len(plan.items) != 3 {
		t.Fatalf("items = %d/%d, want 3", plan.itemCount, len(plan.items))
	}
	if plan.items[0].Name != "Canonical Sword" || plan.items[0].Quantity != 1 || plan.items[0].NameSort != "canonical\x01sword" {
		t.Fatalf("first item = %+v", plan.items[0])
	}
	if !plan.items[0].DateAdded.Equal(time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)) ||
		!plan.items[0].DateOpen.Equal(time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC)) ||
		!plan.items[0].DateBazaar.Equal(time.Date(2026, 12, 21, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("dates = %s / %s / %s", plan.items[0].DateAdded, plan.items[0].DateOpen, plan.items[0].DateBazaar)
	}
	if plan.newSpellCount != 1 || len(plan.newSpells) != 1 {
		t.Fatalf("new spells = %d/%d", plan.newSpellCount, len(plan.newSpells))
	}
	if plan.newSpells[0].EQUID != 21 || plan.newSpells[0].Quantity != 2 || plan.newSpells[0].MetadataMissingFg {
		t.Fatalf("new spell = %+v", plan.newSpells[0])
	}
	if len(plan.merges) != 1 || plan.merges[0].target.GuildBankItemID != 77 || plan.merges[0].quantity != 3 {
		t.Fatalf("merges = %+v", plan.merges)
	}
	if plan.spellQuantity != 5 || plan.itemCount+plan.newSpellCount+len(plan.merges) != 5 {
		t.Fatalf("spell quantity/counts = %d", plan.spellQuantity)
	}
}

func TestBuildCommitPlanUsesLowestSpellRowAndDuplicateTarget(t *testing.T) {
	rows := []domain.ImportRow{
		{ImportID: 9, ItemType: domain.ItemTypeSpell, EQUID: 5, Name: "Spell: First", Quantity: 1, LocationRaw: "A"},
		{ImportID: 3, ItemType: domain.ItemTypeSpell, EQUID: 5, Name: "Spell: First", Quantity: 1, LocationRaw: "A"},
	}
	active := []domain.GuildBankItem{
		{GuildBankItemID: 40, EQUID: 5, Quantity: 1},
		{GuildBankItemID: 12, EQUID: 5, Quantity: 2},
	}
	plan, err := buildCommitPlan(rows, active, map[uint64]ItemMetadata{
		5: {EQUID: 5, Name: "Spell: Canonical", MetadataMissing: false},
	}, domain.Actor{UserID: 1, UserName: "admin"}, time.Now(), 0, 0)
	if err != nil {
		t.Fatalf("buildCommitPlan: %v", err)
	}
	if len(plan.merges) != 1 || plan.merges[0].target.GuildBankItemID != 12 {
		t.Fatalf("merge target = %+v, want lowest active ID 12", plan.merges)
	}
	if len(plan.duplicates) != 1 || plan.duplicates[0].MergedInto != 12 {
		t.Fatalf("duplicates = %+v", plan.duplicates)
	}
	if len(plan.warnings) != 1 || plan.warnings[0].Code != "duplicate_spell_target" {
		t.Fatalf("warnings = %+v", plan.warnings)
	}
}

func TestBuildCommitPlanMissingMetadata(t *testing.T) {
	rows := []domain.ImportRow{
		{ImportID: 1, ItemType: domain.ItemTypeItem, EQUID: 99, Name: "Unresolved Item", Quantity: 1},
		{ImportID: 2, ItemType: domain.ItemTypeSpell, EQUID: 100, Name: "Spell: Unresolved", Quantity: 1},
	}
	plan, err := buildCommitPlan(rows, nil, nil, domain.Actor{UserID: 1, UserName: "admin"}, time.Now(), 0, 0)
	if err != nil {
		t.Fatalf("buildCommitPlan: %v", err)
	}
	if !plan.items[0].MetadataMissingFg || plan.items[0].Name != "Unresolved Item" || plan.items[0].ClassString != "" {
		t.Fatalf("item fallback = %+v", plan.items[0])
	}
	if !plan.newSpells[0].MetadataMissingFg || plan.newSpells[0].Name != "Spell: Unresolved" {
		t.Fatalf("spell fallback = %+v", plan.newSpells[0])
	}
}

func TestMarshalWarningsCapsAndPrioritizesMetadataMissing(t *testing.T) {
	warnings := make([]domain.ImportWarning, 0, 100)
	warnings = append(warnings, domain.ImportWarning{Code: "location_pattern_invalid", Detail: strings.Repeat("x", 200)})
	for i := 0; i < 100; i++ {
		warnings = append(warnings, domain.ImportWarning{Code: "undefined_high_bits", Detail: strings.Repeat("y", 100)})
	}
	warnings = append(warnings, domain.ImportWarning{Code: ItemWarningMetadataMissing, Field: "EQUID"})
	body, err := MarshalWarnings(warnings)
	if err != nil {
		t.Fatalf("MarshalWarnings: %v", err)
	}
	if len(body) > warningTextMax {
		t.Fatalf("warning JSON = %d bytes, want <= %d", len(body), warningTextMax)
	}
	if !strings.Contains(string(body), ItemWarningMetadataMissing) ||
		!strings.Contains(string(body), "warnings_truncated") {
		t.Fatalf("warning JSON lacks required codes: %s", body)
	}
	if got := ParseWarnings("not-json"); got != nil {
		t.Fatalf("ParseWarnings(malformed) = %+v", got)
	}
}
