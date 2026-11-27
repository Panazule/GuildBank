package api

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/web"
)

// TestInventoryGridUsesRaceAbbreviations pins the owner-approved shorthand in
// the grid while the detail page and the filter lists keep full race names.
func TestInventoryGridUsesRaceAbbreviations(t *testing.T) {
	cases := map[string]string{
		"Barbarian": "BAR", "Dark Elf": "DEF", "Drakkin": "DRK", "Dwarf": "DWF",
		"Erudite": "ERU", "Froglok": "FRG", "Gnome": "GNM", "Half Elf": "HEF",
		"Halfling": "HFL", "Human": "HUM", "Iksar": "IKS", "Ogre": "OGR",
		"Troll": "TRL", "Vah Shir": "VAH", "Wood Elf": "ELF", "High Elf": "HIE",
	}
	for full, want := range cases {
		item := domain.PublicInventoryItem{RaceString: full}
		grid := inventoryItemViewOf(item, false)
		if grid.RaceText != want || grid.RaceTitle != full {
			t.Fatalf("grid race for %q = %q (title %q), want %q (title %q)",
				full, grid.RaceText, grid.RaceTitle, want, full)
		}
		detail := inventoryItemViewOf(item, true)
		if detail.RaceText != full || detail.RaceTitle != "" {
			t.Fatalf("detail race for %q = %q (title %q), want the full name",
				full, detail.RaceText, detail.RaceTitle)
		}
	}

	multiple := inventoryItemViewOf(domain.PublicInventoryItem{RaceString: "Human,Wood Elf"}, false)
	if multiple.RaceText != "HUM, ELF" || multiple.RaceTitle != "Human, Wood Elf" {
		t.Fatalf("multi-race grid = %q (title %q)", multiple.RaceText, multiple.RaceTitle)
	}
	all := inventoryItemViewOf(domain.PublicInventoryItem{RaceString: "ALL"}, false)
	if all.RaceText != "All" || all.RaceTitle != "All" {
		t.Fatalf("ALL grid = %q (title %q)", all.RaceText, all.RaceTitle)
	}
	unknown := inventoryItemViewOf(domain.PublicInventoryItem{RaceString: "Clockwork"}, false)
	if unknown.RaceText != "Clockwork" {
		t.Fatalf("unrecognized race = %q, want it preserved verbatim", unknown.RaceText)
	}
}

// TestSortOptionsUseAscendingWording replaces the awkward "natural"/"reverse
// natural" labels on both the public and administrative grids.
func TestSortOptionsUseAscendingWording(t *testing.T) {
	want := map[string]string{
		string(domain.InventorySortName):     "Name (ascending)",
		string(domain.InventorySortNameDesc): "Name (descending)",
	}
	public := inventoryListPageDataOf(domain.PublicInventoryPage{
		Filter: domain.InventoryFilter{Page: 1, PageSize: 50},
		Page:   1, PageSize: 50, TotalPages: 1, Sort: domain.InventorySortName,
	}, nil)
	for _, option := range public.SortOptions {
		if label, ok := want[option.Value]; ok && option.Label != label {
			t.Fatalf("public sort %q = %q, want %q", option.Value, option.Label, label)
		}
	}
	for _, option := range adminSortOptions(domain.AdminSortName) {
		if label, ok := want[option.Value]; ok && option.Label != label {
			t.Fatalf("admin sort %q = %q, want %q", option.Value, option.Label, label)
		}
	}
	if label := domainLabelFor(adminSortOptions(domain.AdminSortRecipient), string(domain.AdminSortRecipientDesc)); label != "Recipient (descending)" {
		t.Fatalf("recipient descending label = %q", label)
	}
}

func domainLabelFor(options []inventorySortOption, value string) string {
	for _, option := range options {
		if option.Value == value {
			return option.Label
		}
	}
	return ""
}

// TestAdminRequestQueueShowsCharacterWithUserName renders the queue and pins
// both the combined requester label and the two-line actionable filter.
func TestAdminRequestQueueShowsCharacterWithUserName(t *testing.T) {
	v, err := newViews(web.Templates)
	if err != nil {
		t.Fatalf("newViews: %v", err)
	}
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	page := domain.RequestPage{
		Requests: []domain.InventoryRequest{{
			RequestID: 1, GuildBankItemID: 9, ItemName: "Ant's Potion", RequestedQuantity: 1,
			CharacterName: "Franz", RequesterKind: domain.RequesterMember, RequesterUserName: "NotInGuild",
			Status: domain.RequestPending, CreatedAt: now.Add(-2 * time.Hour),
			CurrentOnHand: 4, LotActive: true,
		}},
		Total: 1, Page: 1, PageSize: 50, TotalPages: 1,
		Filter: domain.RequestFilter{Status: domain.RequestPending, Page: 1, PageSize: 50},
	}
	data := adminRequestsPageOf(page, 0, now)
	data.pageData = pageData{Title: "Requests"}
	var buf bytes.Buffer
	if err := v.templates.ExecuteTemplate(&buf, "admin_requests.html", data); err != nil {
		t.Fatalf("render admin requests: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		"Franz (NotInGuild)",
		"Only actionable<br>(on hand ≥ 1)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("request queue missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, `<span class="muted">NotInGuild</span>`) {
		t.Fatal("the username is still rendered as a separate muted value")
	}
}
