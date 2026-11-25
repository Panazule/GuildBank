package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/web"
)

// TestAdminStatusOptionsIncludeIssued locks the four-way lifecycle selector.
func TestAdminStatusOptionsIncludeIssued(t *testing.T) {
	options := adminStatusOptions(string(domain.AdminStatusIssued))
	if len(options) != 4 {
		t.Fatalf("options = %d, want 4", len(options))
	}
	selected := 0
	for _, option := range options {
		if option.Selected {
			selected++
			if option.Value != string(domain.AdminStatusIssued) {
				t.Fatalf("selected = %q, want issued", option.Value)
			}
		}
	}
	if selected != 1 {
		t.Fatalf("selected options = %d, want 1", selected)
	}
}

// TestAdminStatusTabsPreserveFiltersAndMarkCurrent proves the tabs keep the
// officer's other filters and mark exactly one current state.
func TestAdminStatusTabsPreserveFiltersAndMarkCurrent(t *testing.T) {
	filter := domain.AdminInventoryFilter{
		Search: "ant", Status: domain.AdminStatusIssued, Sort: domain.AdminSortModifiedNewest,
		Page: 1, PageSize: 50,
	}
	tabs := adminStatusTabs(filter)
	if len(tabs) != len(domain.AdminInventoryStatuses) {
		t.Fatalf("tabs = %d, want %d", len(tabs), len(domain.AdminInventoryStatuses))
	}
	current := 0
	for _, tab := range tabs {
		if tab.Current {
			current++
			if tab.Value != string(domain.AdminStatusIssued) {
				t.Fatalf("current tab = %q, want issued", tab.Value)
			}
			if !strings.Contains(tab.URL, "q=ant") || !strings.Contains(tab.URL, "status=issued") {
				t.Fatalf("issued tab URL = %q, want the preserved filters", tab.URL)
			}
		}
		if tab.Value == string(domain.AdminStatusActive) && strings.Contains(tab.URL, "status=") {
			t.Fatalf("active tab URL = %q, want the default status omitted", tab.URL)
		}
	}
	if current != 1 {
		t.Fatalf("current tabs = %d, want 1", current)
	}
}

// TestAdminIssuedRecipientURL pins the single recipient-queue link shape.
func TestAdminIssuedRecipientURL(t *testing.T) {
	if got, want := adminIssuedRecipientURL("Joe"), "/admin/inventory?givenTo=Joe&status=issued"; got != want {
		t.Fatalf("adminIssuedRecipientURL(Joe) = %q, want %q", got, want)
	}
}

// TestAdminListRendersIssuedQueueLinks renders the list and proves an issued row
// links its recipient and badge to the issued queue while an active row does not.
func TestAdminListRendersIssuedQueueLinks(t *testing.T) {
	v, err := newViews(web.Templates)
	if err != nil {
		t.Fatalf("newViews: %v", err)
	}
	page := domain.AdminInventoryPage{
		Records: []domain.AdminInventoryItem{
			issuedItem(2, 1, "Joe"),
			movementTestItem(1, 1),
		},
		Total: 2, Page: 1, PageSize: 50, TotalPages: 1,
		Filter: domain.AdminInventoryFilter{
			Status: domain.AdminStatusIssued, Sort: domain.AdminSortModifiedNewest, Page: 1, PageSize: 50,
		},
	}
	data := adminListPageOf(page, nil)
	data.pageData = pageData{Title: "Admin inventory", CSRF: "csrf-token"}
	var buf strings.Builder
	if err := v.templates.ExecuteTemplate(&buf, "admin_inventory.html", data); err != nil {
		t.Fatalf("render admin list: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`class="status-tabs"`,
		`href="/admin/inventory?givenTo=Joe&amp;status=issued"`,
		`class="status-tab current"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin list missing %q\n%s", want, body)
		}
	}
	if strings.Count(body, `givenTo=Joe&amp;status=issued`) != 2 {
		t.Fatalf("issued recipient and badge links = %d, want 2\n%s",
			strings.Count(body, `givenTo=Joe&amp;status=issued`), body)
	}
}

// TestAdminListIssuedHintForActiveRecipient proves Active + recipient renders the
// empty-state hint that points at the issued queue instead of a dead end.
func TestAdminListIssuedHintForActiveRecipient(t *testing.T) {
	v, err := newViews(web.Templates)
	if err != nil {
		t.Fatalf("newViews: %v", err)
	}
	page := domain.AdminInventoryPage{
		Total: 0, Page: 1, PageSize: 50, TotalPages: 0,
		Filter: domain.AdminInventoryFilter{
			Status: domain.AdminStatusActive, GivenTo: "Joe", Sort: domain.AdminSortModifiedNewest,
			Page: 1, PageSize: 50,
		},
	}
	data := adminListPageOf(page, nil)
	if !data.IssuedHint {
		t.Fatal("active + recipient must set the issued hint")
	}
	data.pageData = pageData{Title: "Admin inventory", CSRF: "csrf-token"}
	var buf strings.Builder
	if err := v.templates.ExecuteTemplate(&buf, "admin_inventory.html", data); err != nil {
		t.Fatalf("render admin list: %v", err)
	}
	if !strings.Contains(buf.String(), "No active records carry this recipient") {
		t.Fatalf("empty-state hint missing\n%s", buf.String())
	}
}

// TestAdminDetailHidesRestoreForIssuedRow proves the detail page offers Return,
// not Restore, for an issued row.
func TestAdminDetailHidesRestoreForIssuedRow(t *testing.T) {
	v, err := newViews(web.Templates)
	if err != nil {
		t.Fatalf("newViews: %v", err)
	}
	data := adminDetailPageData{
		pageData: pageData{Title: "Ant's Potion", CSRF: "csrf-token"},
		Item:     adminItemViewOf(issuedItem(1, 5, "Joe")),
	}
	var buf strings.Builder
	if err := v.templates.ExecuteTemplate(&buf, "admin_inventory_detail.html", data); err != nil {
		t.Fatalf("render detail: %v", err)
	}
	body := buf.String()
	if strings.Contains(body, "/restore") {
		t.Fatalf("issued detail must not offer Restore\n%s", body)
	}
	if !strings.Contains(body, "/return") {
		t.Fatalf("issued detail must offer Return\n%s", body)
	}
}

// TestMovementsPagePreservesReturnTo proves the movement history page carries the
// inventory-list context into its back link and banner.
func TestMovementsPagePreservesReturnTo(t *testing.T) {
	store := newMovementFakeStore()
	store.items[1] = issuedItem(1, 5, "Joe")
	h := newPageTestHandler(t, store)
	res := runMovementHandler(http.MethodGet,
		"/admin/inventory/1/movements?returnTo=%2Fadmin%2Finventory%3Fstatus%3Dissued",
		"", movementTestParams("1"), h.movementsPage)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	if !strings.Contains(res.body, "returnTo=%2Fadmin%2Finventory%3Fstatus%3Dissued") {
		t.Fatalf("movement page dropped returnTo\n%s", res.body)
	}
}
