package api

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
	"github.com/Panazule/guildbank/internal/web"
)

// newPageTestHandler wires the movement fake store into a handler that renders
// the embedded HTML templates.
func newPageTestHandler(t *testing.T, store *movementFakeStore) *adminInventoryHandler {
	t.Helper()
	h := newMovementTestHandler(store)
	v, err := newViews(web.Templates)
	if err != nil {
		t.Fatalf("newViews: %v", err)
	}
	h.views = v
	return h
}

type handlerResult struct {
	status   int
	body     string
	location string
}

// runMovementHandler invokes one HTML handler directly and flushes the gin
// response writer so a 303 See Other is observable without a running engine.
func runMovementHandler(method, target, form string, params gin.Params, fn func(*gin.Context)) handlerResult {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(form))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.Params = params
	c.Set(principalKey, service.AuthenticatedSession{
		User: domain.User{ID: 1, UserName: "admin", AccessLevel: domain.AccessAdmin},
	})
	fn(c)
	c.Writer.WriteHeaderNow()
	return handlerResult{
		status: recorder.Code, body: recorder.Body.String(),
		location: recorder.Header().Get("Location"),
	}
}

func movementFormBody(fields map[string]string) string {
	parts := make([]string, 0, len(fields))
	for key, value := range fields {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "&")
}

func TestIssuePageRendersOfficerForm(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 10)
	store.items[item.ID] = item
	h := newPageTestHandler(t, store)

	res := runMovementHandler(http.MethodGet, "/admin/inventory/1/issue", "", movementTestParams("1"), h.issuePage)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	for _, want := range []string{
		`action="/admin/inventory/1/issue"`, `name="csrf_token"`, `name="expectedModAt"`,
		`name="idempotencyKey"`, `name="quantity"`, `name="recipient"`, "On hand", "Partial issue",
	} {
		if !strings.Contains(res.body, want) {
			t.Fatalf("issue page missing %q", want)
		}
	}
}

func TestIssuePartialFormKeepsOneActiveLot(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 10)
	store.items[item.ID] = item
	h := newPageTestHandler(t, store)

	form := movementFormBody(map[string]string{
		"quantity": "5", "recipient": "Joe", "expectedModAt": "2026-11-22T10:00:00Z", "idempotencyKey": "form-key-1",
	})
	res := runMovementHandler(http.MethodPost, "/admin/inventory/1/issue", form, movementTestParams("1"), h.issueForm)
	if res.status != http.StatusSeeOther || res.location != "/admin/inventory/1?notice=issued" {
		t.Fatalf("status = %d location = %q body %s", res.status, res.location, res.body)
	}
	if len(store.items) != 1 || store.items[1].Quantity != 5 || store.items[1].GivenTo != "" {
		t.Fatalf("stored items = %+v", store.items)
	}
	if len(store.movements) != 1 || store.movements[0].MovementType != domain.MovementIssue ||
		store.movements[0].Recipient != "Joe" {
		t.Fatalf("movements = %+v", store.movements)
	}
}

func TestIssueFullStackFormRemovesRecord(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 5)
	store.items[item.ID] = item
	h := newPageTestHandler(t, store)

	form := movementFormBody(map[string]string{
		"quantity": "5", "recipient": "Joe", "expectedModAt": "2026-11-22T10:00:00Z", "idempotencyKey": "form-key-2",
	})
	res := runMovementHandler(http.MethodPost, "/admin/inventory/1/issue", form, movementTestParams("1"), h.issueForm)
	if res.status != http.StatusSeeOther {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	if !store.items[1].DeletedFg || store.items[1].GivenTo != "Joe" {
		t.Fatalf("item = %+v", store.items[1])
	}
}

func TestIssueFormValidationRerenders(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 10)
	store.items[item.ID] = item
	h := newPageTestHandler(t, store)

	form := movementFormBody(map[string]string{
		"quantity": "0", "recipient": "", "expectedModAt": "2026-11-22T10:00:00Z", "idempotencyKey": "form-key-3",
	})
	res := runMovementHandler(http.MethodPost, "/admin/inventory/1/issue", form, movementTestParams("1"), h.issueForm)
	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	if !strings.Contains(res.body, "Quantity must be at least 1") || !strings.Contains(res.body, "Recipient is required") {
		t.Fatalf("missing field errors in %s", res.body)
	}
	if len(store.movements) != 0 || store.items[1].Quantity != 10 {
		t.Fatal("a rejected form must not change state")
	}
}

func TestIssueFormPartialAllowedByQuantityNotMetadata(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 10)
	store.items[item.ID] = item
	// A non-stackable classification must not block a partial issue of a lot that
	// holds more than one unit on hand.
	store.stackability = map[uint64]domain.Stackability{item.EQUID: domain.StackabilityNonStackable}
	h := newPageTestHandler(t, store)

	form := movementFormBody(map[string]string{
		"quantity": "5", "recipient": "Joe", "expectedModAt": "2026-11-22T10:00:00Z", "idempotencyKey": "form-key-4",
	})
	res := runMovementHandler(http.MethodPost, "/admin/inventory/1/issue", form, movementTestParams("1"), h.issueForm)
	if res.status != http.StatusSeeOther || res.location != "/admin/inventory/1?notice=issued" {
		t.Fatalf("status = %d location = %q body %s", res.status, res.location, res.body)
	}
	if store.items[1].Quantity != 5 || store.items[1].GivenTo != "" || store.items[1].DeletedFg {
		t.Fatalf("stored item = %+v, want active quantity 5", store.items[1])
	}
	if len(store.movements) != 1 || store.movements[0].MovementType != domain.MovementIssue ||
		store.movements[0].Recipient != "Joe" {
		t.Fatalf("movements = %+v", store.movements)
	}
}

func issuedItem(id int64, quantity uint32, recipient string) domain.AdminInventoryItem {
	item := movementTestItem(id, quantity)
	item.DeletedFg = true
	item.GivenTo = recipient
	return item
}

func TestReturnPageRejectsAdministrativeRemoval(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 5)
	item.DeletedFg = true
	store.items[item.ID] = item
	h := newPageTestHandler(t, store)

	res := runMovementHandler(http.MethodGet, "/admin/inventory/1/return", "", movementTestParams("1"), h.returnPage)
	if res.status != http.StatusConflict {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
}

func TestReturnFormFullReturnRestoresRecord(t *testing.T) {
	store := newMovementFakeStore()
	item := issuedItem(1, 5, "Joe")
	store.items[item.ID] = item
	h := newPageTestHandler(t, store)

	form := movementFormBody(map[string]string{
		"quantity": "5", "expectedModAt": "2026-11-22T10:00:00Z", "idempotencyKey": "ret-key-1",
	})
	res := runMovementHandler(http.MethodPost, "/admin/inventory/1/return", form, movementTestParams("1"), h.returnForm)
	if res.status != http.StatusSeeOther || res.location != "/admin/inventory/1?notice=returned" {
		t.Fatalf("status = %d location = %q body %s", res.status, res.location, res.body)
	}
	if store.items[1].DeletedFg || store.items[1].GivenTo != "" {
		t.Fatalf("restored item = %+v", store.items[1])
	}
	if len(store.movements) != 1 || store.movements[0].MovementType != domain.MovementReturn {
		t.Fatalf("movements = %+v", store.movements)
	}
}

func TestReturnFormPartialReturnCreatesNewLot(t *testing.T) {
	store := newMovementFakeStore()
	item := issuedItem(1, 5, "Joe")
	store.items[item.ID] = item
	h := newPageTestHandler(t, store)

	form := movementFormBody(map[string]string{
		"quantity": "2", "expectedModAt": "2026-11-22T10:00:00Z", "idempotencyKey": "ret-key-2",
	})
	res := runMovementHandler(http.MethodPost, "/admin/inventory/1/return", form, movementTestParams("1"), h.returnForm)
	if res.status != http.StatusSeeOther || res.location != "/admin/inventory/2?notice=returned-new" {
		t.Fatalf("status = %d location = %q body %s", res.status, res.location, res.body)
	}
	if !store.items[1].DeletedFg || store.items[2].DeletedFg || store.items[2].Quantity != 2 {
		t.Fatalf("items = %+v", store.items)
	}
}

func TestMovementsPageRendersHistoryAndEmptyState(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 10)
	store.items[item.ID] = item
	store.movements = []domain.InventoryMovement{{
		MovementID: 7, GuildBankItemID: 1, EQUID: item.EQUID, MovementType: domain.MovementIssue,
		Quantity: 5, RemainingQuantity: 5, Recipient: "Joe", ActorUserName: "admin",
		OccurredAt: item.ModAt, CorrelationID: "corr-1",
	}}
	h := newPageTestHandler(t, store)

	res := runMovementHandler(http.MethodGet, "/admin/inventory/1/movements", "", movementTestParams("1"), h.movementsPage)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	for _, want := range []string{"ISSUE", "Joe", "corr-1", "Movement history"} {
		if !strings.Contains(res.body, want) {
			t.Fatalf("history page missing %q", want)
		}
	}

	empty := newMovementFakeStore()
	empty.items[item.ID] = item
	h = newPageTestHandler(t, empty)
	res = runMovementHandler(http.MethodGet, "/admin/inventory/1/movements", "", movementTestParams("1"), h.movementsPage)
	if res.status != http.StatusOK || !strings.Contains(res.body, "opening balance") {
		t.Fatalf("empty history page = %d body %s", res.status, res.body)
	}
}

func TestMovementsPageRejectsUnknownLot(t *testing.T) {
	store := newMovementFakeStore()
	h := newPageTestHandler(t, store)
	res := runMovementHandler(http.MethodGet, "/admin/inventory/9/movements", "", movementTestParams("9"), h.movementsPage)
	if res.status != http.StatusNotFound {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
}

func TestMovementIdempotencyKeyIsRandomHex(t *testing.T) {
	first := newMovementIdempotencyKey()
	second := newMovementIdempotencyKey()
	if len(first) != 32 || len(second) != 32 {
		t.Fatalf("lengths = %d, %d", len(first), len(second))
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("key is not hex: %v", err)
	}
	if first == second {
		t.Fatal("two renders produced the same idempotency key")
	}
}

func TestAdminMovementModeText(t *testing.T) {
	if got := adminIssueModeText(5); !strings.Contains(got, "more than one unit") {
		t.Fatalf("multi-unit issue text = %q", got)
	}
	if got := adminIssueModeText(1); !strings.Contains(got, "single unit") {
		t.Fatalf("single-unit issue text = %q", got)
	}

	active := movementTestItem(1, 5)
	if mode, ok := adminReturnModeText(active); !ok || !strings.Contains(mode, "active") {
		t.Fatalf("active return mode = %q ok=%v", mode, ok)
	}
	if _, ok := adminReturnModeText(issuedItem(1, 5, "Joe")); !ok {
		t.Fatal("whole-issued record must allow a return")
	}
	removed := movementTestItem(1, 5)
	removed.DeletedFg = true
	if mode, ok := adminReturnModeText(removed); ok || !strings.Contains(mode, "Restore it instead") {
		t.Fatalf("admin-removed return mode = %q ok=%v", mode, ok)
	}
}

// TestMovementFormRenderRetainsIdempotencyKey asserts a re-render after a
// validation failure keeps the officer's key so a retry stays idempotent.
func TestMovementFormRenderRetainsIdempotencyKey(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 10)
	store.items[item.ID] = item
	h := newPageTestHandler(t, store)

	form := movementFormBody(map[string]string{
		"quantity": "0", "recipient": "Joe", "expectedModAt": "2026-11-22T10:00:00Z", "idempotencyKey": "keep-me",
	})
	res := runMovementHandler(http.MethodPost, "/admin/inventory/1/issue", form, movementTestParams("1"), h.issueForm)
	if res.status != http.StatusUnprocessableEntity || !strings.Contains(res.body, "keep-me") {
		t.Fatalf("re-render lost the key: status %d body %s", res.status, res.body)
	}
}
