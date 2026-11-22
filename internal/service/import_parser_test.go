package service

import (
	"os"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestNaturalSortKeyTable(t *testing.T) {
	cases := []struct{ input, want string }{
		{"General 1", "general\x010000000001"},
		{"General 2", "general\x010000000002"},
		{"General 3", "general\x010000000003"},
		{"General 10", "general\x010000000010"},
		{"General 11", "general\x010000000011"},
		{"Bank 4", "bank\x010000000004"},
		{"Hoard", "hoard"},
		{"SharedBank 1", "sharedbank\x010000000001"},
		{"  Mixed   CASE  ", "mixed\x01case"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := NaturalSortKey(tc.input); got != tc.want {
			t.Fatalf("NaturalSortKey(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
	if !(NaturalSortKey("General 2") < NaturalSortKey("General 10")) {
		t.Fatal("natural sort key does not order General 2 before General 10")
	}
}

func TestNormalizeLocationUsesGroupAsSelector(t *testing.T) {
	cases := []struct{ raw, group, warning string }{
		{"General 1-Slot1", "General 1", ""},
		{"General 01", "General 1", ""},
		{"Bank 12-Slot3", "Bank 12", ""},
		{"SharedBank 2", "SharedBank 2", ""},
		{"Hoard 3", "Hoard", ""},
		{"Hoard3", "Hoard", ""},
		{"Hoard", "Hoard", ""},
		{"", "Unspecified", "location_blank"},
		{"Equipment", "Unspecified", ""},
		{"Possessions", "Unspecified", ""},
		{"Spell: Cleric", "Unspecified", ""},
		{"General Stuff", "Unspecified", "location_pattern_invalid"},
	}
	for _, tc := range cases {
		got := NormalizeLocation(tc.raw)
		if got.Group != tc.group {
			t.Fatalf("NormalizeLocation(%q).Group = %q, want %q", tc.raw, got.Group, tc.group)
		}
		warning := ""
		if len(got.Warnings) > 0 {
			warning = got.Warnings[0].Code
		}
		if warning != tc.warning {
			t.Fatalf("NormalizeLocation(%q) warning = %q, want %q", tc.raw, warning, tc.warning)
		}
	}
}

func TestClassifyItem(t *testing.T) {
	cases := map[string]domain.ItemType{
		"Spell: Minor Healing":  domain.ItemTypeSpell,
		"Song: Chant":           domain.ItemTypeSpell,
		"Ancient: Word":         domain.ItemTypeSpell,
		"spell : Case":          domain.ItemTypeSpell,
		"Ancient Burrower":      domain.ItemTypeItem,
		"Ancient Sword":         domain.ItemTypeItem,
		"Spellblade":            domain.ItemTypeItem,
		"An item with Spell: x": domain.ItemTypeItem,
	}
	for name, want := range cases {
		if got := ClassifyItem(name); got != want {
			t.Fatalf("ClassifyItem(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestParseImportBasicFieldCounts(t *testing.T) {
	body := []byte("Location\tName\tID\tCount\tSlots\n" +
		"General 1\tThree Field\t10\n" +
		"General 1\tFour Field\t11\tSTAT\n" +
		"General 1\tFive Field\t12\t3\t6\n" +
		"General 1\tEmpty\t0\t0\t0\n" +
		"General 1\t\t13\t1\t6\n")
	parsed, err := ParseImport(body)
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if parsed.SourceLines != 6 || parsed.StructuralRows != 1 || parsed.DiscardedRows != 2 || len(parsed.Rows) != 3 {
		t.Fatalf("counts = %+v", parsed)
	}
	if parsed.Rows[0].Quantity != 1 || parsed.Rows[1].Quantity != 1 || parsed.Rows[2].Quantity != 3 {
		t.Fatalf("quantities = %d/%d/%d", parsed.Rows[0].Quantity, parsed.Rows[1].Quantity, parsed.Rows[2].Quantity)
	}
}

func TestParseImportGoldenSample(t *testing.T) {
	body, err := os.ReadFile("../../Docs/Samples/InventoryExport-Sample.txt")
	if err != nil {
		t.Skipf("sample not available: %v", err)
	}
	parsed, err := ParseImport(body)
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if parsed.SourceLines != 548 {
		t.Fatalf("SourceLines = %d, want 548", parsed.SourceLines)
	}
	if len(parsed.Rows) != 197 {
		t.Fatalf("accepted rows = %d, want 197", len(parsed.Rows))
	}
	if parsed.DiscardedRows != 348 {
		t.Fatalf("DiscardedRows = %d, want 348", parsed.DiscardedRows)
	}
	if parsed.StructuralRows != 2 {
		t.Fatalf("StructuralRows = %d, want 2", parsed.StructuralRows)
	}
	if parsed.BlankRows != 1 {
		t.Fatalf("BlankRows = %d, want 1", parsed.BlankRows)
	}
	var itemRows, itemUnits, spellRows, spellUnits int
	groups := make(map[string]bool)
	for _, row := range parsed.Rows {
		group := NormalizeLocation(row.Location).Group
		groups[group] = true
		if ClassifyItem(row.Name) == domain.ItemTypeSpell {
			spellRows++
			spellUnits += int(row.Quantity)
		} else {
			itemRows++
			itemUnits += int(row.Quantity)
		}
	}
	if itemRows != 194 || itemUnits != 723 || spellRows != 3 || spellUnits != 3 {
		t.Fatalf("item rows/units = %d/%d, spells rows/units = %d/%d", itemRows, itemUnits, spellRows, spellUnits)
	}
	if len(groups) != 30 {
		t.Fatalf("normalized groups = %d, want 30", len(groups))
	}
	if _, ok := groups["Hoard"]; !ok {
		t.Fatalf("Hoard group missing from %v", groups)
	}
}

func TestParseImportFatalRules(t *testing.T) {
	header := "Location\tName\tID\tCount\tSlots\n"
	cases := []struct {
		name string
		body string
		code string
	}{
		{"empty", "", "file_empty"},
		{"header", "wrong\nGeneral 1\tX\t1", "invalid_header"},
		{"too few", header + "General 1\tX\n", "too_few_fields"},
		{"too many", header + "a\tb\tc\td\te\tf\n", "too_many_fields"},
		{"bad quantity", header + "General 1\tX\t1\t0\t6\n", "invalid_quantity"},
		{"bad id", header + "General 1\tX\t4294967296\t1\t6\n", "invalid_id"},
		{"duplicate header", header + header + "General 1\tX\t1\t1\t6\n", "duplicate_header"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseImport([]byte(tc.body))
			var parseErr *ParseError
			if err == nil || !asParseError(err, &parseErr) || parseErr.Code != tc.code {
				t.Fatalf("error = %v, want code %q", err, tc.code)
			}
		})
	}
}

func asParseError(err error, target **ParseError) bool {
	value, ok := err.(*ParseError)
	if ok {
		*target = value
	}
	return ok
}
