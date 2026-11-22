package mariadb

import (
	"os"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestPublicInventoryWhereUsesParameterizedPredicates(t *testing.T) {
	filter := domain.InventoryFilter{
		Search: "50%_off!", Class: "WAR", Race: "Wood Elf", ItemType: domain.ItemTypeSpell,
	}
	where, args := publicInventoryWhere(filter)
	for _, fragment := range []string{"DeletedFg = 0", `Name LIKE ? ESCAPE '!'`, "FIND_IN_SET(?, ClassString)", "FIND_IN_SET(?, RaceString)", "ItemType = ?"} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("where %q missing %q", where, fragment)
		}
	}
	wantArgs := []any{"%50!%!_off!!%", "WAR", "Wood Elf", "Spell"}
	if len(args) != len(wantArgs) {
		t.Fatalf("args = %#v", args)
	}
	for i := range wantArgs {
		if args[i] != wantArgs[i] {
			t.Fatalf("args[%d] = %#v, want %#v", i, args[i], wantArgs[i])
		}
	}
	if strings.Contains(where, "50%_off!") || strings.Contains(where, "NameSort") {
		t.Fatalf("caller text leaked into SQL: %q", where)
	}

	empty, emptyArgs := publicInventoryWhere(domain.InventoryFilter{})
	if empty != "DeletedFg = 0" || len(emptyArgs) != 0 {
		t.Fatalf("empty where = %q %#v", empty, emptyArgs)
	}
}

func TestPublicInventoryOrderAllowlist(t *testing.T) {
	want := map[domain.InventorySort]string{
		domain.InventorySortName:           "NameSort ASC, GuildBankItemID ASC",
		domain.InventorySortNameDesc:       "NameSort DESC, GuildBankItemID DESC",
		domain.InventorySortDateAdded:      "DateAdded ASC, GuildBankItemID ASC",
		domain.InventorySortDateAddedDesc:  "DateAdded DESC, GuildBankItemID DESC",
		domain.InventorySortDateOpen:       "DateOpen ASC, GuildBankItemID ASC",
		domain.InventorySortDateOpenDesc:   "DateOpen DESC, GuildBankItemID DESC",
		domain.InventorySortDateBazaar:     "DateBazaar ASC, GuildBankItemID ASC",
		domain.InventorySortDateBazaarDesc: "DateBazaar DESC, GuildBankItemID DESC",
	}
	for sort, expression := range want {
		got, ok := publicInventoryOrder(sort)
		if !ok || got != expression {
			t.Fatalf("publicInventoryOrder(%q) = %q, %v", sort, got, ok)
		}
	}
	if _, ok := publicInventoryOrder(domain.InventorySort("NameSort; DROP")); ok {
		t.Fatal("unsafe sort accepted")
	}
	if got := publicInventoryOrderWithNulls(domain.InventorySortName, "NameSort ASC, GuildBankItemID ASC", true); got != "NameSort IS NULL ASC, NameSort ASC, GuildBankItemID ASC" {
		t.Fatalf("nulls-last order = %q", got)
	}
	if got := publicInventoryOrderWithNulls(domain.InventorySortDateOpen, "DateOpen ASC, GuildBankItemID ASC", true); got != "DateOpen ASC, GuildBankItemID ASC" {
		t.Fatalf("date order changed for null NameSort = %q", got)
	}
}

func TestPublicInventorySourceHasExplicitProjection(t *testing.T) {
	body, err := os.ReadFile("inventory.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	if strings.Contains(source, "SELECT *") {
		t.Fatal("inventory repository contains SELECT *")
	}
	if !strings.Contains(source, "publicInventoryColumns = `GuildBankItemID") {
		t.Fatal("explicit public projection is missing")
	}
	for _, forbidden := range []string{"SourceLocation", "GivenTo", "ModAt", "ModBy", "NameSort"} {
		projection := source[strings.Index(source, "publicInventoryColumns"):strings.Index(source, "// ListPublicInventory")]
		if strings.Contains(projection, forbidden) {
			t.Fatalf("public projection contains %s", forbidden)
		}
	}
}
