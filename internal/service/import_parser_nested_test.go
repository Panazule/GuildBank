package service

import (
	"os"
	"testing"
)

func TestParseImportIgnoresPopulatedNestedSlots(t *testing.T) {
	body := []byte("Location\tName\tID\tCount\tSlots\n" +
		"General 1\tFrostpack\t900\t1\t40\n" +
		"General 1-Slot1\tBatwing Crunchies\t901\t2\t6\n" +
		"General 1-Slot2-Slot1\tWulfenite Gem\t902\t1\t6\n" +
		"General 1 - Slot 2 - Slot 3\tAnother Gem\t903\t1\t6\n" +
		"Equipment-Slot1-Slot2\tNot a bank prefix\t904\t1\t6\n")
	parsed, err := ParseImport(body)
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	if len(parsed.Rows) != 3 {
		t.Fatalf("accepted rows = %d, want 3: %+v", len(parsed.Rows), parsed.Rows)
	}
	if parsed.IgnoredRows != 2 {
		t.Fatalf("IgnoredRows = %d, want 2", parsed.IgnoredRows)
	}
	if parsed.DiscardedRows != 0 {
		t.Fatalf("DiscardedRows = %d, want 0", parsed.DiscardedRows)
	}
	for _, row := range parsed.Rows {
		if row.EQUID == 902 || row.EQUID == 903 {
			t.Fatalf("nested bank row %d was staged", row.EQUID)
		}
	}
	if got := NormalizeLocation(parsed.Rows[2].Location).Group; got != "Unspecified" {
		t.Fatalf("non-bank nested row group = %q, want Unspecified", got)
	}
}

func TestParseImportSampleHasNoIgnoredRows(t *testing.T) {
	body, err := os.ReadFile("../../Docs/Samples/InventoryExport-Sample.txt")
	if err != nil {
		t.Skipf("sample not available: %v", err)
	}
	parsed, err := ParseImport(body)
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	// Every nested sample row is an empty bag slot (ID 0), so it remains an
	// ordinary discard and the ignored counter stays zero (IMP-01).
	if parsed.IgnoredRows != 0 {
		t.Fatalf("IgnoredRows = %d, want 0", parsed.IgnoredRows)
	}
	if parsed.DiscardedRows != 348 {
		t.Fatalf("DiscardedRows = %d, want 348", parsed.DiscardedRows)
	}
}

func TestDefaultSelectionOnSample(t *testing.T) {
	body, err := os.ReadFile("../../Docs/Samples/InventoryExport-Sample.txt")
	if err != nil {
		t.Skipf("sample not available: %v", err)
	}
	parsed, err := ParseImport(body)
	if err != nil {
		t.Fatalf("ParseImport: %v", err)
	}
	selected, containers, hoard := 0, 0, 0
	for _, row := range parsed.Rows {
		group := NormalizeLocation(row.Location).Group
		container := IsContainerLocation(row.Location)
		if container {
			containers++
		}
		if group == HoardLocationGroup {
			hoard++
		}
		if DefaultImportSelection(group, container) {
			selected++
		}
	}
	// 49 General slot rows + 74 Bank + 12 SharedBank + 3 Hoard containers = 138.
	if selected != 138 {
		t.Fatalf("default-selected rows = %d, want 138", selected)
	}
	if containers != 60 || hoard != 3 {
		t.Fatalf("containers = %d (want 60), hoard rows = %d (want 3)", containers, hoard)
	}
	for _, row := range parsed.Rows {
		group := NormalizeLocation(row.Location).Group
		if group == "Unspecified" && DefaultImportSelection(group, IsContainerLocation(row.Location)) {
			t.Fatal("Unspecified row was selected by default")
		}
	}
}
