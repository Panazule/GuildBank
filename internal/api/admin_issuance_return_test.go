package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/web"
)

// TestAdminListRendersMovementIssuedRow proves an issued movement row renders
// the Return action, anchors on its RowKey and is not selectable for bulk
// removal (D5).
func TestAdminListRendersMovementIssuedRow(t *testing.T) {
	v, err := newViews(web.Templates)
	if err != nil {
		t.Fatalf("newViews: %v", err)
	}
	row := movementTestItem(5, 3)
	row.MovementID = 9
	row.IssuedQuantity = 3
	row.GivenTo = "Joe"
	row.IssuedAt = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	row.IssuedBy = "admin"
	page := domain.AdminInventoryPage{
		Records: []domain.AdminInventoryItem{row},
		Total:   1, Page: 1, PageSize: 50, TotalPages: 1,
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
		`id="m:9"`,
		`/admin/inventory/movements/9/return`,
		`Issued`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin list missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, `name="remove"`) {
		t.Fatalf("a movement-derived issued row must not be selectable for removal\n%s", body)
	}
}

// issuedMovementStore seeds one lot and one outstanding ISSUE movement.
func issuedMovementStore() *movementFakeStore {
	store := newMovementFakeStore()
	item := movementTestItem(1, 4)
	store.items[item.ID] = item
	store.movements = append(store.movements, domain.InventoryMovement{
		MovementID: 1, GuildBankItemID: item.ID, EQUID: item.EQUID,
		MovementType: domain.MovementIssue, Quantity: 2, Recipient: "Joe",
		ActorUserName: "admin", OccurredAt: item.ModAt, RemainingQuantity: 4,
	})
	return store
}

// TestReturnIssuancePageRendersMovementForm proves the new GET route renders the
// shared return form posting back to the movement route.
func TestReturnIssuancePageRendersMovementForm(t *testing.T) {
	store := issuedMovementStore()
	h := newPageTestHandler(t, store)
	params := gin.Params{{Key: "movementID", Value: "1"}}
	res := runMovementHandler(http.MethodGet, "/admin/inventory/movements/1/return", "", params, h.returnIssuancePage)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	for _, want := range []string{
		`action="/admin/inventory/movements/1/return"`,
		`name="csrf_token"`,
		`name="idempotencyKey"`,
		`Outstanding`,
	} {
		if !strings.Contains(res.body, want) {
			t.Fatalf("return form missing %q\n%s", want, res.body)
		}
	}
}

// TestReturnIssuanceFormRecordsAttributedReturn proves the POST route writes a
// RETURN attributed to the addressed ISSUE movement.
func TestReturnIssuanceFormRecordsAttributedReturn(t *testing.T) {
	store := issuedMovementStore()
	h := newPageTestHandler(t, store)
	params := gin.Params{{Key: "movementID", Value: "1"}}
	form := movementFormBody(map[string]string{
		"quantity": "2", "recipient": "Joe",
		"expectedModAt":  store.items[1].ModAt.UTC().Format(time.RFC3339Nano),
		"idempotencyKey": "issuance-1",
	})
	res := runMovementHandler(http.MethodPost, "/admin/inventory/movements/1/return", form, params, h.returnIssuanceForm)
	if res.status != http.StatusSeeOther {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	if len(store.movements) != 2 {
		t.Fatalf("movements = %d, want 2", len(store.movements))
	}
	ret := store.movements[1]
	if ret.MovementType != domain.MovementReturn || ret.RelatedMovementID == nil || *ret.RelatedMovementID != 1 {
		t.Fatalf("return movement = %+v", ret)
	}
	if store.movementOutstanding(1) != 0 {
		t.Fatalf("outstanding = %d, want 0", store.movementOutstanding(1))
	}
}
