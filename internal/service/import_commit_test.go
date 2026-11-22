package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestBuildCommitPlanCreatesOneRowPerLineAndAggregatesSpells(t *testing.T) {
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
	if plan.itemCount != 2 || len(plan.items) != 2 {
		t.Fatalf("items = %d/%d, want 2 rows for 2 item lines", plan.itemCount, len(plan.items))
	}
	if plan.itemUnits != 3 {
		t.Fatalf("item units = %d, want 3", plan.itemUnits)
	}
	if plan.items[0].Name != "Canonical Sword" || plan.items[0].Quantity != 2 || plan.items[0].NameSort != "canonical\x01sword" {
		t.Fatalf("first item = %+v", plan.items[0])
	}
	if plan.items[1].Quantity != 1 || plan.items[1].EQUID != 11 {
		t.Fatalf("second item = %+v", plan.items[1])
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
	if plan.spellQuantity != 5 || plan.itemCount+plan.newSpellCount+len(plan.merges) != 4 {
		t.Fatalf("spell quantity/counts = %d", plan.spellQuantity)
	}
}

func TestBuildCommitPlanNeverMergesRepeatedItemEQUID(t *testing.T) {
	rows := []domain.ImportRow{
		{ImportID: 1, ItemType: domain.ItemTypeItem, EQUID: 10, Name: "Batwing Crunchies", Quantity: 55, LocationRaw: "Bank 1"},
	}
	plan, err := buildCommitPlan(rows, nil, nil, domain.Actor{UserID: 1, UserName: "admin"}, time.Now(), 0, 0)
	if err != nil {
		t.Fatalf("buildCommitPlan: %v", err)
	}
	if len(plan.items) != 1 || plan.items[0].Quantity != 55 {
		t.Fatalf("first import = %+v, want one row with quantity 55", plan.items)
	}
	// A later import of the same EQUID is a separate staged line, so it builds
	// a second row; the plan never looks up existing item rows.
	later := []domain.ImportRow{
		{ImportID: 2, ItemType: domain.ItemTypeItem, EQUID: 10, Name: "Batwing Crunchies", Quantity: 8, LocationRaw: "Bank 1"},
	}
	laterPlan, err := buildCommitPlan(later, nil, nil, domain.Actor{UserID: 1, UserName: "admin"}, time.Now(), 0, 0)
	if err != nil {
		t.Fatalf("buildCommitPlan later: %v", err)
	}
	if len(laterPlan.items) != 1 || laterPlan.items[0].Quantity != 8 {
		t.Fatalf("later import = %+v, want a second row with quantity 8", laterPlan.items)
	}
	if len(plan.merges) != 0 || len(laterPlan.merges) != 0 {
		t.Fatalf("item imports must not create spell merges")
	}
}

func TestCommitAuditEntriesSeparateUnitsAndRecords(t *testing.T) {
	plan := commitPlan{
		items: []domain.GuildBankItem{
			{EQUID: 10, Quantity: 55}, {EQUID: 11, Quantity: 8},
		},
		itemCount: 2, itemUnits: 63,
		newSpellCount: 1, spellQuantity: 3,
	}
	entries, err := commitAuditEntries(7, domain.Actor{UserID: 1, UserName: "admin"},
		plan, 3, "req", "", time.Now(), time.Second)
	if err != nil {
		t.Fatalf("commitAuditEntries: %v", err)
	}
	if len(entries) != 2 || entries[0].EventType != "IMPORT_COMMITTED" ||
		entries[1].EventType != "ITEM_CREATED" {
		t.Fatalf("entries = %+v", entries)
	}
	var summary map[string]any
	if err := json.Unmarshal(entries[1].NewValues, &summary); err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary["itemUnits"] != float64(63) || summary["itemRecords"] != float64(2) {
		t.Fatalf("summary units/records = %v/%v, want 63/2",
			summary["itemUnits"], summary["itemRecords"])
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

func TestBuildCommitPlanAppliesSpellMetadata(t *testing.T) {
	rows := []domain.ImportRow{
		{ImportID: 1, ItemType: domain.ItemTypeSpell, EQUID: 26622, Name: "Ancient: Lcea's Lament", Quantity: 1},
		{ImportID: 2, ItemType: domain.ItemTypeSpell, EQUID: 7701, Name: "Song: Magical Monologue", Quantity: 2},
	}
	metadata := map[uint64]ItemMetadata{
		26622: {EQUID: 26622, Name: "Ancient: Lcea's Lament", ClassString: "BRD", RaceString: "ALL"},
		7701:  {EQUID: 7701, Name: "Song: Canonical Monologue", ClassString: "BRD", RaceString: "ALL"},
	}
	plan, err := buildCommitPlan(rows, nil, metadata, domain.Actor{UserID: 1, UserName: "admin"}, time.Now(), 0, 0)
	if err != nil {
		t.Fatalf("buildCommitPlan: %v", err)
	}
	if len(plan.newSpells) != 2 {
		t.Fatalf("new spells = %d, want 2", len(plan.newSpells))
	}
	for _, spell := range plan.newSpells {
		if spell.MetadataMissingFg {
			t.Fatalf("spell %d flagged missing despite resolved metadata", spell.EQUID)
		}
		if spell.ClassString != "BRD" || spell.RaceString != "ALL" {
			t.Fatalf("spell %d metadata = (%q, %q), want (BRD, ALL)",
				spell.EQUID, spell.ClassString, spell.RaceString)
		}
	}
	if plan.newSpells[0].Name != "Ancient: Lcea's Lament" ||
		plan.newSpells[0].NameSort != NaturalSortKey("Ancient: Lcea's Lament") {
		t.Fatalf("first spell name = %q / %q", plan.newSpells[0].Name, plan.newSpells[0].NameSort)
	}
	// The resolved Items name is authoritative for spells, exactly as it is
	// for items (DEFECT-01).
	if plan.newSpells[1].Name != "Song: Canonical Monologue" {
		t.Fatalf("second spell name = %q, want the Items name", plan.newSpells[1].Name)
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
