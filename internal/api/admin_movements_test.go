package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

// movementFakeStore is a minimal in-memory AdminStore used to exercise the
// movement handlers end to end without a database.
type movementFakeStore struct {
	items        map[int64]domain.AdminInventoryItem
	movements    []domain.InventoryMovement
	audits       []domain.AuditEntry
	stackability map[uint64]domain.Stackability
	nextItemID   int64
	nextMoveID   int64
}

func newMovementFakeStore() *movementFakeStore {
	return &movementFakeStore{
		items: map[int64]domain.AdminInventoryItem{}, nextItemID: 1, nextMoveID: 1,
	}
}

func (f *movementFakeStore) Ready() bool { return true }

func (f *movementFakeStore) ListAdminInventory(ctx context.Context, filter domain.AdminInventoryFilter) (domain.AdminInventoryPage, error) {
	records := make([]domain.AdminInventoryItem, 0, len(f.items))
	for _, item := range f.items {
		records = append(records, item)
	}
	return domain.AdminInventoryPage{
		Records: records, Total: len(records), Page: filter.Page, PageSize: filter.PageSize, Filter: filter,
	}, nil
}

func (f *movementFakeStore) GetAdminInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error) {
	item, ok := f.items[id]
	if !ok {
		return domain.AdminInventoryItem{}, domain.ErrNotFound
	}
	return item, nil
}

func (f *movementFakeStore) ListLocationGroups(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (f *movementFakeStore) SumActiveQuantity(ctx context.Context, filter domain.AdminInventoryFilter) (uint64, error) {
	var total uint64
	for _, item := range f.items {
		if !item.DeletedFg {
			total += uint64(item.Quantity)
		}
	}
	return total, nil
}

func (f *movementFakeStore) ListMovements(ctx context.Context, filter domain.MovementFilter) (domain.MovementPage, error) {
	matches := make([]domain.InventoryMovement, 0, len(f.movements))
	for _, movement := range f.movements {
		if filter.GuildBankItemID > 0 && movement.GuildBankItemID != filter.GuildBankItemID {
			continue
		}
		if filter.MovementType != "" && movement.MovementType != filter.MovementType {
			continue
		}
		if filter.Recipient != "" && movement.Recipient != filter.Recipient {
			continue
		}
		if filter.CorrelationID != "" && movement.CorrelationID != filter.CorrelationID {
			continue
		}
		matches = append(matches, movement)
	}
	page := domain.MovementPage{
		Movements: matches, Total: len(matches), Page: filter.Page, PageSize: filter.PageSize,
		Filter: filter,
	}
	if page.PageSize > 0 {
		page.TotalPages = (len(matches) + page.PageSize - 1) / page.PageSize
	}
	return page, nil
}

func (f *movementFakeStore) GetMovement(ctx context.Context, movementID int64) (domain.InventoryMovement, error) {
	for _, movement := range f.movements {
		if movement.MovementID == movementID {
			return movement, nil
		}
	}
	return domain.InventoryMovement{}, domain.ErrNotFound
}

func (f *movementFakeStore) movementOutstanding(movementID int64) uint32 {
	var issue *domain.InventoryMovement
	for i := range f.movements {
		if f.movements[i].MovementID == movementID {
			issue = &f.movements[i]
			break
		}
	}
	if issue == nil || issue.MovementType != domain.MovementIssue {
		return 0
	}
	outstanding := int64(issue.Quantity)
	for _, movement := range f.movements {
		if movement.MovementType == domain.MovementReturn && movement.RelatedMovementID != nil &&
			*movement.RelatedMovementID == movementID {
			outstanding -= int64(movement.Quantity)
		}
	}
	if outstanding <= 0 {
		return 0
	}
	return uint32(outstanding)
}

func (f *movementFakeStore) GetMovementOutstanding(ctx context.Context, movementID int64) (uint32, error) {
	for _, movement := range f.movements {
		if movement.MovementID == movementID {
			return f.movementOutstanding(movementID), nil
		}
	}
	return 0, domain.ErrNotFound
}

func (f *movementFakeStore) LookupStackability(ctx context.Context, equids []uint64) (map[uint64]domain.Stackability, error) {
	out := make(map[uint64]domain.Stackability, len(equids))
	for _, equid := range equids {
		if level, ok := f.stackability[equid]; ok {
			out[equid] = level
		}
	}
	return out, nil
}

func (f *movementFakeStore) BeginAdminWrite(ctx context.Context) (service.AdminWriteTx, error) {
	return &movementFakeTx{store: f}, nil
}

func (f *movementFakeStore) ListAudit(ctx context.Context, filter domain.AuditFilter) (domain.AuditPage, error) {
	return domain.AuditPage{}, nil
}

func (f *movementFakeStore) GetAudit(ctx context.Context, id int64) (domain.AuditEntry, error) {
	return domain.AuditEntry{}, domain.ErrNotFound
}

type movementFakeTx struct {
	store *movementFakeStore
}

func (t *movementFakeTx) LockInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error) {
	return t.store.GetAdminInventory(ctx, id)
}

func (t *movementFakeTx) FindActiveSpells(ctx context.Context, equid uint64) ([]domain.AdminInventoryItem, error) {
	return nil, nil
}

func (t *movementFakeTx) InsertInventory(ctx context.Context, rows []domain.GuildBankItem) ([]int64, error) {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		for {
			if _, taken := t.store.items[t.store.nextItemID]; !taken {
				break
			}
			t.store.nextItemID++
		}
		id := t.store.nextItemID
		t.store.nextItemID++
		item := domain.AdminInventoryItem{
			ID: id, EQUID: row.EQUID, Name: row.Name, ItemType: row.ItemType,
			Quantity: row.Quantity, SourceLocation: row.SourceLocation,
			LocationGroup: row.LocationGroup, LocationSort: row.LocationSort,
			GivenTo: row.GivenTo, DeletedFg: row.DeletedFg, NameSort: row.NameSort,
			DateAdded: row.DateAdded, DateOpen: row.DateOpen, DateBazaar: row.DateBazaar,
			ModAt: row.ModAt, ModBy: row.ModBy, ModByUserID: row.ModByUserID,
		}
		t.store.items[id] = item
		ids = append(ids, id)
	}
	return ids, nil
}

func (t *movementFakeTx) AddSpellQuantity(ctx context.Context, id int64, qty uint32, at time.Time, actor domain.Actor) error {
	return nil
}

func (t *movementFakeTx) UpdateInventory(ctx context.Context, in domain.AdminUpdateRow) error {
	item, ok := t.store.items[in.ID]
	if !ok {
		return domain.ErrNotFound
	}
	if in.Quantity != nil {
		item.Quantity = *in.Quantity
	}
	item.SourceLocation = in.SourceLocation
	item.LocationGroup = in.LocationGroup
	item.LocationSort = in.LocationSort
	item.GivenTo = in.GivenTo
	if in.DateOpen != nil {
		item.DateOpen = *in.DateOpen
	}
	if in.DateBazaar != nil {
		item.DateBazaar = *in.DateBazaar
	}
	item.ModAt = in.ModAt
	item.ModBy = in.Actor.UserName
	t.store.items[in.ID] = item
	return nil
}

func (t *movementFakeTx) SetDeleted(ctx context.Context, id int64, deleted bool, at time.Time, actor domain.Actor) error {
	item, ok := t.store.items[id]
	if !ok {
		return domain.ErrNotFound
	}
	item.DeletedFg = deleted
	item.ModAt = at
	item.ModBy = actor.UserName
	t.store.items[id] = item
	return nil
}

func (t *movementFakeTx) InsertMovement(ctx context.Context, movement domain.InventoryMovement) (int64, error) {
	if err := movement.Validate(); err != nil {
		return 0, err
	}
	if movement.IdempotencyKey != "" {
		for _, existing := range t.store.movements {
			if existing.IdempotencyKey == movement.IdempotencyKey {
				return 0, domain.ErrConflict
			}
		}
	}
	movement.MovementID = t.store.nextMoveID
	t.store.nextMoveID++
	t.store.movements = append(t.store.movements, movement)
	return movement.MovementID, nil
}

func (t *movementFakeTx) InsertAudit(ctx context.Context, entry domain.AuditEntry) error {
	t.store.audits = append(t.store.audits, entry)
	return nil
}

func (t *movementFakeTx) LockMovementOutstanding(ctx context.Context, movementID int64) (domain.InventoryMovement, uint32, error) {
	for _, movement := range t.store.movements {
		if movement.MovementID == movementID {
			return movement, t.store.movementOutstanding(movementID), nil
		}
	}
	return domain.InventoryMovement{}, 0, domain.ErrNotFound
}

func (t *movementFakeTx) EarliestOutstandingIssue(ctx context.Context, lotID int64) (int64, error) {
	best := int64(0)
	for _, movement := range t.store.movements {
		if movement.MovementType != domain.MovementIssue || movement.GuildBankItemID != lotID {
			continue
		}
		if t.store.movementOutstanding(movement.MovementID) == 0 {
			continue
		}
		if best == 0 || movement.MovementID < best {
			best = movement.MovementID
		}
	}
	return best, nil
}

func (t *movementFakeTx) Commit() error   { return nil }
func (t *movementFakeTx) Rollback() error { return nil }

type movementTestSettings map[string]string

func (s movementTestSettings) Values(ctx context.Context) (map[string]string, error) {
	return map[string]string(s), nil
}

func movementTestItem(id int64, quantity uint32) domain.AdminInventoryItem {
	return domain.AdminInventoryItem{
		ID: id, EQUID: 100, Name: "Ant's Potion", ItemType: domain.ItemTypeItem,
		Quantity: quantity, SourceLocation: "General1-Slot1", LocationGroup: "General 1",
		LocationSort: "general\x010000000001", NameSort: "ants potion",
		DateAdded:  time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		DateOpen:   time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC),
		DateBazaar: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		ModAt:      time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC),
		ModBy:      "admin",
	}
}

func newMovementTestHandler(store *movementFakeStore) *adminInventoryHandler {
	svc := service.NewAdminInventoryService(store,
		movementTestSettings{"GuildOpenDays": "14", "BazaarDays": "30"},
		nil, func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &adminInventoryHandler{
		admin: svc, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func newMovementTestContext(method, target, body string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	c.Params = params
	c.Set(principalKey, service.AuthenticatedSession{
		User: domain.User{ID: 1, UserName: "admin", AccessLevel: domain.AccessAdmin},
	})
	return c, recorder
}

func decodeMovementBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, recorder.Body.String())
	}
	return body
}

func TestMovementInputFieldsValidation(t *testing.T) {
	valid := movementRequest{
		Quantity:      decimalUint{Value: 5, Set: true},
		Recipient:     func() *string { s := " Joe "; return &s }(),
		ExpectedModAt: func() *string { s := "2026-11-22T10:00:00Z"; return &s }(),
	}
	quantity, recipient, _, _, fields := movementInputFields(valid, true)
	if len(fields) != 0 || quantity != 5 || recipient != "Joe" {
		t.Fatalf("valid request = quantity %d recipient %q fields %v", quantity, recipient, fields)
	}
	missing := movementRequest{ExpectedModAt: valid.ExpectedModAt}
	if _, _, _, _, fields := movementInputFields(missing, true); fields["quantity"] == "" || fields["recipient"] == "" {
		t.Fatalf("missing fields = %v", fields)
	}
	badVersion := movementRequest{Quantity: valid.Quantity, Recipient: valid.Recipient,
		ExpectedModAt: func() *string { s := "not-a-date"; return &s }()}
	if _, _, _, _, fields := movementInputFields(badVersion, true); fields["expectedModAt"] == "" {
		t.Fatalf("bad version fields = %v", fields)
	}
	immutable := movementRequest{Quantity: valid.Quantity, Recipient: valid.Recipient,
		ExpectedModAt: valid.ExpectedModAt, ID: func() *json.RawMessage { r := json.RawMessage(`1`); return &r }()}
	if _, _, _, _, fields := movementInputFields(immutable, true); fields["id"] == "" {
		t.Fatalf("immutable fields = %v", fields)
	}
}

func TestMovementFilterFromDefaultsAndValidation(t *testing.T) {
	c, _ := newMovementTestContext(http.MethodGet, "/api/v1/admin/items/5/movements", "", nil)
	filter, fields := movementFilterFrom(c, 5)
	if len(fields) != 0 || filter.GuildBankItemID != 5 || filter.Page != 1 ||
		filter.PageSize != service.AdminDefaultPageSize {
		t.Fatalf("default filter = %+v fields %v", filter, fields)
	}
	c, _ = newMovementTestContext(http.MethodGet,
		"/api/v1/admin/items/5/movements?page=0&pageSize=201&movementType=NOPE&from=bad", "", nil)
	_, fields = movementFilterFrom(c, 5)
	for _, field := range []string{"page", "pageSize", "movementType", "from"} {
		if fields[field] == "" {
			t.Fatalf("missing %s in %v", field, fields)
		}
	}
}

func TestMovementIdempotencyKeyTrimsOnly(t *testing.T) {
	c, _ := newMovementTestContext(http.MethodPost, "/api/v1/items/5/issue", "{}", nil)
	c.Request.Header.Set(movementIdempotencyHeader, "  key-1  ")
	if got := movementIdempotencyKey(c); got != "key-1" {
		t.Fatalf("key = %q", got)
	}
	// Over-long keys pass through so the service can return a named 422 rather
	// than silently truncating the retry key.
	c.Request.Header.Set(movementIdempotencyHeader, strings.Repeat("a", 80))
	if got := movementIdempotencyKey(c); len([]rune(got)) != 80 {
		t.Fatalf("key length = %d", len([]rune(got)))
	}
}

func movementTestParams(id string) gin.Params {
	return gin.Params{{Key: "id", Value: id}}
}

func TestIssuePartialKeepsOneActiveLotAndRecordsRecipient(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 10)
	store.items[item.ID] = item
	h := newMovementTestHandler(store)

	body := `{"quantity":5,"recipient":"Joe","expectedModAt":"2026-11-22T10:00:00Z"}`
	c, recorder := newMovementTestContext(http.MethodPost, "/api/v1/items/1/issue", body, movementTestParams("1"))
	h.issue(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	data := decodeMovementBody(t, recorder)["data"].(map[string]any)
	itemBody := data["item"].(map[string]any)
	if itemBody["quantity"].(float64) != 5 || itemBody["deleted"].(bool) {
		t.Fatalf("item = %v", itemBody)
	}
	if itemBody["givenTo"] != nil {
		t.Fatalf("givenTo = %v, want null", itemBody["givenTo"])
	}
	if data["currentOnHand"].(float64) != 5 {
		t.Fatalf("currentOnHand = %v", data["currentOnHand"])
	}
	movement := data["movement"].(map[string]any)
	if movement["type"] != "ISSUE" || movement["recipient"] != "Joe" {
		t.Fatalf("movement = %v", movement)
	}
	if len(store.items) != 1 || store.items[item.ID].GivenTo != "" || store.items[item.ID].Quantity != 5 {
		t.Fatalf("stored items = %+v", store.items)
	}
}

func TestIssueFullStackSoftDeletesAndZeroOnHand(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 5)
	store.items[item.ID] = item
	h := newMovementTestHandler(store)

	body := `{"quantity":5,"recipient":"Joe","expectedModAt":"2026-11-22T10:00:00Z"}`
	c, recorder := newMovementTestContext(http.MethodPost, "/api/v1/items/1/issue", body, movementTestParams("1"))
	h.issue(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	data := decodeMovementBody(t, recorder)["data"].(map[string]any)
	if !data["item"].(map[string]any)["deleted"].(bool) || data["currentOnHand"].(float64) != 0 {
		t.Fatalf("data = %v", data)
	}
}

func TestIssuePartialAllowedByQuantityNotMetadata(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 10)
	store.items[item.ID] = item
	// A non-stackable classification must not block a partial issue: the rule is
	// the quantity on hand, which is 10 here.
	store.stackability = map[uint64]domain.Stackability{item.EQUID: domain.StackabilityNonStackable}
	h := newMovementTestHandler(store)

	body := `{"quantity":5,"recipient":"Joe","expectedModAt":"2026-11-22T10:00:00Z"}`
	c, recorder := newMovementTestContext(http.MethodPost, "/api/v1/items/1/issue", body, movementTestParams("1"))
	h.issue(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	data := decodeMovementBody(t, recorder)["data"].(map[string]any)
	itemBody := data["item"].(map[string]any)
	if itemBody["quantity"].(float64) != 5 || itemBody["deleted"].(bool) {
		t.Fatalf("item = %v", itemBody)
	}
	if itemBody["givenTo"] != nil {
		t.Fatalf("givenTo = %v, want null", itemBody["givenTo"])
	}
	if data["currentOnHand"].(float64) != 5 {
		t.Fatalf("currentOnHand = %v", data["currentOnHand"])
	}
}

func TestIssueUnknownLotAndStaleVersion(t *testing.T) {
	store := newMovementFakeStore()
	h := newMovementTestHandler(store)
	body := `{"quantity":1,"recipient":"Joe","expectedModAt":"2026-11-22T10:00:00Z"}`
	c, recorder := newMovementTestContext(http.MethodPost, "/api/v1/items/9/issue", body, movementTestParams("9"))
	h.issue(c)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown lot status = %d body %s", recorder.Code, recorder.Body.String())
	}

	item := movementTestItem(1, 10)
	store.items[item.ID] = item
	body = `{"quantity":1,"recipient":"Joe","expectedModAt":"2026-11-22T09:00:00Z"}`
	c, recorder = newMovementTestContext(http.MethodPost, "/api/v1/items/1/issue", body, movementTestParams("1"))
	h.issue(c)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("stale status = %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestIssueDuplicateIdempotencyKeyConflicts(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 10)
	store.items[item.ID] = item
	h := newMovementTestHandler(store)

	body := `{"quantity":2,"recipient":"Joe","expectedModAt":"2026-11-22T10:00:00Z"}`
	c, recorder := newMovementTestContext(http.MethodPost, "/api/v1/items/1/issue", body, movementTestParams("1"))
	c.Request.Header.Set(movementIdempotencyHeader, "same-key")
	h.issue(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("first status = %d body %s", recorder.Code, recorder.Body.String())
	}
	body = `{"quantity":2,"recipient":"Joe","expectedModAt":"2026-11-22T12:00:00Z"}`
	c, recorder = newMovementTestContext(http.MethodPost, "/api/v1/items/1/issue", body, movementTestParams("1"))
	c.Request.Header.Set(movementIdempotencyHeader, "same-key")
	h.issue(c)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestReturnToActiveIncreasesQuantity(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 5)
	store.items[item.ID] = item
	h := newMovementTestHandler(store)

	body := `{"quantity":2,"recipient":"Joe","expectedModAt":"2026-11-22T10:00:00Z"}`
	c, recorder := newMovementTestContext(http.MethodPost, "/api/v1/items/1/return", body, movementTestParams("1"))
	h.returnItem(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	data := decodeMovementBody(t, recorder)["data"].(map[string]any)
	if data["currentOnHand"].(float64) != 7 || data["movement"].(map[string]any)["type"] != "RETURN" {
		t.Fatalf("data = %v", data)
	}
}

func TestMovementHistoryAndDetail(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 10)
	store.items[item.ID] = item
	store.stackability = map[uint64]domain.Stackability{item.EQUID: domain.StackabilityStackable}
	h := newMovementTestHandler(store)
	body := `{"quantity":2,"recipient":"Joe","expectedModAt":"2026-11-22T10:00:00Z"}`
	c, _ := newMovementTestContext(http.MethodPost, "/api/v1/items/1/issue", body, movementTestParams("1"))
	h.issue(c)

	c, recorder := newMovementTestContext(http.MethodGet,
		"/api/v1/admin/items/1/movements?pageSize=1&movementType=ISSUE", "", movementTestParams("1"))
	h.movements(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("history status = %d body %s", recorder.Code, recorder.Body.String())
	}
	page := decodeMovementBody(t, recorder)
	if page["meta"].(map[string]any)["total"].(float64) != 1 ||
		len(page["data"].(map[string]any)["movements"].([]any)) != 1 {
		t.Fatalf("page = %v", page)
	}

	c, recorder = newMovementTestContext(http.MethodGet, "/api/v1/admin/movements/1", "",
		gin.Params{{Key: "movementID", Value: "1"}})
	h.movement(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("detail status = %d body %s", recorder.Code, recorder.Body.String())
	}

	c, recorder = newMovementTestContext(http.MethodGet, "/api/v1/admin/movements/99", "",
		gin.Params{{Key: "movementID", Value: "99"}})
	h.movement(c)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing detail status = %d", recorder.Code)
	}

	c, recorder = newMovementTestContext(http.MethodGet, "/api/v1/admin/items/9/movements", "",
		movementTestParams("9"))
	h.movements(c)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown lot history status = %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestUpdateRejectsQuantityAndAllowsOmittedGivenTo(t *testing.T) {
	store := newMovementFakeStore()
	item := movementTestItem(1, 10)
	store.items[item.ID] = item
	h := newMovementTestHandler(store)

	body := `{"expectedModAt":"2026-11-22T10:00:00Z","sourceLocation":"General2-Slot1","dateOpen":"2026-11-15","dateBazaar":"2026-12-01"}`
	c, recorder := newMovementTestContext(http.MethodPut, "/api/v1/items/1", body, movementTestParams("1"))
	h.update(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	updated := store.items[item.ID]
	if updated.Quantity != 10 || updated.GivenTo != "" || updated.SourceLocation != "General2-Slot1" {
		t.Fatalf("updated = %+v", updated)
	}

	body = `{"expectedModAt":"2026-11-22T12:00:00Z","sourceLocation":"General2-Slot1","dateOpen":"2026-11-15","dateBazaar":"2026-12-01","quantity":3}`
	c, recorder = newMovementTestContext(http.MethodPut, "/api/v1/items/1", body, movementTestParams("1"))
	h.update(c)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("quantity status = %d body %s", recorder.Code, recorder.Body.String())
	}
	errBody := decodeMovementBody(t, recorder)["error"].(map[string]any)
	fields := errBody["fields"].(map[string]any)
	if fields["quantity"] == nil {
		t.Fatalf("fields = %v", fields)
	}
}
