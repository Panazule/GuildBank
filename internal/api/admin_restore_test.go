package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/web"
)

// removedItem is a plain administrative removal: soft deleted with no recipient
// and no consolidation marker. It is the only row the list may offer to restore.
func removedItem(id int64, quantity uint32) domain.AdminInventoryItem {
	item := movementTestItem(id, quantity)
	item.DeletedFg = true
	return item
}

// TestAdminItemViewRestorable covers the four eligibility combinations the list
// renders from. Restore is offered only for a plain administrative removal.
func TestAdminItemViewRestorable(t *testing.T) {
	active := adminItemViewOf(movementTestItem(1, 3))
	if active.Restorable {
		t.Fatal("an active row must not be restorable")
	}
	issued := adminItemViewOf(issuedItem(2, 3, "Joe"))
	if issued.Restorable {
		t.Fatal("an issued row must not be restorable")
	}
	removed := adminItemViewOf(removedItem(3, 3))
	if !removed.Restorable {
		t.Fatal("a plain removed row must be restorable")
	}
	consolidated := removedItem(4, 3)
	survivorID := int64(9)
	consolidated.ConsolidatedInto = &survivorID
	if adminItemViewOf(consolidated).Restorable {
		t.Fatal("a consolidated row must not be restorable")
	}
}

// TestAdminListRendersRestoreActionForPlainRemovedRow locks the list action:
// Restore appears only on a plain removed row and carries the filtered view.
func TestAdminListRendersRestoreActionForPlainRemovedRow(t *testing.T) {
	v, err := newViews(web.Templates)
	if err != nil {
		t.Fatalf("newViews: %v", err)
	}
	consolidated := removedItem(4, 1)
	survivorID := int64(9)
	consolidated.ConsolidatedInto = &survivorID
	page := domain.AdminInventoryPage{
		Records: []domain.AdminInventoryItem{
			removedItem(3, 1),
			issuedItem(2, 1, "Joe"),
			consolidated,
			movementTestItem(1, 1),
		},
		Total: 4, Page: 1, PageSize: 50, TotalPages: 1,
		Filter: domain.AdminInventoryFilter{
			Status: domain.AdminStatusAll, Sort: domain.AdminSortModifiedNewest, Page: 1, PageSize: 50,
		},
	}
	data := adminListPageOf(page, nil)
	data.pageData = pageData{Title: "Admin inventory", CSRF: "csrf-token"}
	var buf strings.Builder
	if err := v.templates.ExecuteTemplate(&buf, "admin_inventory.html", data); err != nil {
		t.Fatalf("render admin list: %v", err)
	}
	body := buf.String()
	if !strings.Contains(body, `href="/admin/inventory/3/restore?returnTo=%2Fadmin%2Finventory%3Fstatus%3Dall"`) {
		t.Fatalf("plain removed row is missing its Restore action\n%s", body)
	}
	for _, unexpected := range []string{
		`href="/admin/inventory/1/restore?`,
		`href="/admin/inventory/2/restore?`,
		`href="/admin/inventory/4/restore?`,
	} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("ineligible row rendered a Restore action %q\n%s", unexpected, body)
		}
	}
}

// TestRestorePageRendersConfirmation proves the GET page follows the Remove-page
// pattern: record summary, expectedModAt protection and the carried returnTo.
func TestRestorePageRendersConfirmation(t *testing.T) {
	store := newMovementFakeStore()
	store.items[1] = removedItem(1, 5)
	h := newPageTestHandler(t, store)

	res := runMovementHandler(http.MethodGet, "/admin/inventory/1/restore?returnTo=%2Fadmin%2Finventory%3Fq%3Dant", "",
		movementTestParams("1"), h.restorePage)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	for _, want := range []string{
		`action="/admin/inventory/1/restore"`,
		`name="csrf_token"`,
		`name="expectedModAt" value="2026-11-22T10:00:00Z"`,
		`name="returnTo" value="/admin/inventory?q=ant"`,
		`Restore record`,
	} {
		if !strings.Contains(res.body, want) {
			t.Fatalf("restore page missing %q\n%s", want, res.body)
		}
	}
}

// TestRestorePageRejectsIneligibleRows proves the confirmation page cannot be
// reached for an active, issued or consolidated row.
func TestRestorePageRejectsIneligibleRows(t *testing.T) {
	consolidated := removedItem(3, 1)
	survivorID := int64(9)
	consolidated.ConsolidatedInto = &survivorID
	cases := map[string]domain.AdminInventoryItem{
		"active":       movementTestItem(1, 1),
		"issued":       issuedItem(2, 1, "Joe"),
		"consolidated": consolidated,
	}
	for name, item := range cases {
		store := newMovementFakeStore()
		item.ID = 1
		store.items[item.ID] = item
		h := newPageTestHandler(t, store)
		res := runMovementHandler(http.MethodGet, "/admin/inventory/1/restore", "",
			movementTestParams("1"), h.restorePage)
		if res.status != http.StatusConflict {
			t.Fatalf("%s row: status = %d body %s", name, res.status, res.body)
		}
	}
}

// TestRestoreFormUsesExistingWorkflowAndReturnsToList proves the list action
// posts to the unchanged restore handler: the row is reactivated, one RECEIVE
// movement is written and the officer returns to the filtered list.
func TestRestoreFormUsesExistingWorkflowAndReturnsToList(t *testing.T) {
	store := newMovementFakeStore()
	store.items[1] = removedItem(1, 5)
	h := newPageTestHandler(t, store)

	form := url.Values{
		"expectedModAt": {"2026-11-22T10:00:00Z"},
		"returnTo":      {"/admin/inventory?q=ant"},
	}.Encode()
	res := runMovementHandler(http.MethodPost, "/admin/inventory/1/restore", form,
		movementTestParams("1"), h.restoreForm)
	if res.status != http.StatusSeeOther {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	if !strings.HasPrefix(res.location, "/admin/inventory?") ||
		!strings.Contains(res.location, "notice=restored") ||
		!strings.Contains(res.location, "q=ant") {
		t.Fatalf("location = %q, want the filtered list with the restored notice", res.location)
	}
	if store.items[1].DeletedFg {
		t.Fatalf("stored item = %+v, want the removal cleared", store.items[1])
	}
	if len(store.movements) != 1 || store.movements[0].MovementType != domain.MovementReceive {
		t.Fatalf("movements = %+v, want one RECEIVE movement", store.movements)
	}
}

// TestRestoreFormKeepsExpectedModAtProtection proves a stale version token is
// still rejected without changing state.
func TestRestoreFormKeepsExpectedModAtProtection(t *testing.T) {
	store := newMovementFakeStore()
	store.items[1] = removedItem(1, 5)
	h := newPageTestHandler(t, store)

	form := url.Values{"expectedModAt": {"2026-11-22T09:00:00Z"}}.Encode()
	res := runMovementHandler(http.MethodPost, "/admin/inventory/1/restore", form,
		movementTestParams("1"), h.restoreForm)
	if res.status != http.StatusConflict {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	if !store.items[1].DeletedFg || len(store.movements) != 0 {
		t.Fatalf("a rejected form must not change state: item=%+v movements=%+v", store.items[1], store.movements)
	}
}
