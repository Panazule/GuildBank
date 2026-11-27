package api

import (
	"context"
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
	"github.com/Panazule/guildbank/internal/web"
)

type requestTestCharacters struct {
	names []string
}

func (f requestTestCharacters) List(ctx context.Context, userID int64) ([]domain.Character, error) {
	characters := make([]domain.Character, 0, len(f.names))
	for i, name := range f.names {
		characters = append(characters, domain.Character{ID: int64(i + 1), UserID: userID, Name: name})
	}
	return characters, nil
}

func newRequestTestHandler(store *movementFakeStore) *requestHandler {
	viewSet, err := newViews(web.Templates)
	if err != nil {
		panic(err)
	}
	svc := service.NewRequestService(store,
		requestTestCharacters{names: []string{"Icefear", "Banktoon"}},
		func() time.Time { return time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC) },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &requestHandler{
		requests: svc, views: viewSet,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func newRequestTestContext(method, target, form string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(form))
	if form != "" {
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c.Params = params
	c.Set(requestIDKey, "req-test-0001")
	c.Set(principalKey, service.AuthenticatedSession{
		User: domain.User{ID: 7, UserName: "member1", AccessLevel: "Member"},
	})
	return c, recorder
}

func requestTestStoreLot(store *movementFakeStore) domain.AdminInventoryItem {
	lot := domain.AdminInventoryItem{
		ID: 1, EQUID: 100, Name: "Cloak of Flames", ItemType: domain.ItemTypeItem,
		Quantity: 5, SourceLocation: "Bank Tab 2",
		ModAt: time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC),
	}
	store.items[lot.ID] = lot
	return lot
}

func TestRequestCreateFormRendersCharacterSelect(t *testing.T) {
	store := newMovementFakeStore()
	lot := requestTestStoreLot(store)
	h := newRequestTestHandler(store)

	c, recorder := newRequestTestContext(http.MethodGet, "/request/new?item=1", "", nil)
	h.newForm(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`<select name="characterName"`, `>Icefear</option>`, `>Banktoon</option>`,
		`max="5"`, `value="` + lot.ModAt.Format(time.RFC3339Nano) + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("form missing %q\n%s", want, body)
		}
	}
}

func TestRequestCreateRedirectsToReceiptAndQueuesOneRow(t *testing.T) {
	store := newMovementFakeStore()
	lot := requestTestStoreLot(store)
	h := newRequestTestHandler(store)

	form := "item=1&quantity=2&characterName=Icefear&note=for+raid" +
		"&expectedModAt=" + lot.ModAt.Format(time.RFC3339Nano) + "&idempotencyKey=key-1"
	c, recorder := newRequestTestContext(http.MethodPost, "/request/new", form, nil)
	h.create(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/my/requests/1?notice=created" {
		t.Fatalf("Location = %q", location)
	}
	if len(store.requests) != 1 || store.requests[0].Status != domain.RequestPending {
		t.Fatalf("requests = %+v", store.requests)
	}
	if store.requests[0].CharacterName != "Icefear" || store.requests[0].RequestedQuantity != 2 {
		t.Fatalf("request = %+v", store.requests[0])
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "REQUEST_CREATED" {
		t.Fatalf("audits = %+v", store.audits)
	}
}

func TestRequestCreateDuplicateAndUnownedCharacterRerender(t *testing.T) {
	store := newMovementFakeStore()
	lot := requestTestStoreLot(store)
	h := newRequestTestHandler(store)
	base := "item=1&quantity=2&expectedModAt=" + lot.ModAt.Format(time.RFC3339Nano)

	first := base + "&characterName=Icefear&idempotencyKey=key-1"
	c, recorder := newRequestTestContext(http.MethodPost, "/request/new", first, nil)
	h.create(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("first status = %d body %s", recorder.Code, recorder.Body.String())
	}

	duplicate := base + "&characterName=Icefear&idempotencyKey=key-2"
	c, recorder = newRequestTestContext(http.MethodPost, "/request/new", duplicate, nil)
	h.create(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "already has a pending request for this item") {
		t.Fatalf("duplicate body = %s", recorder.Body.String())
	}

	unowned := base + "&characterName=Someoneelse&idempotencyKey=key-3"
	c, recorder = newRequestTestContext(http.MethodPost, "/request/new", unowned, nil)
	h.create(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unowned status = %d body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "That character is not on your profile.") {
		t.Fatalf("unowned body = %s", recorder.Body.String())
	}
	if len(store.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(store.requests))
	}
}

func seedRequest(store *movementFakeStore, id, userID int64, status domain.RequestStatus) domain.InventoryRequest {
	request := domain.InventoryRequest{
		RequestID: id, GuildBankItemID: 1, EQUID: 100, ItemName: "Cloak of Flames",
		RequestedQuantity: 5, OnHandAtCreate: 5, CharacterName: "Icefear",
		RequesterKind: domain.RequesterMember, Status: status,
		CorrelationID: "corr-seed", CreatedAt: time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC),
		CreatedByName: "member1",
	}
	requester := userID
	request.RequesterUserID = &requester
	store.requests = append(store.requests, request)
	return request
}

func TestRequestMineListsOnlyOwnRequests(t *testing.T) {
	store := newMovementFakeStore()
	requestTestStoreLot(store)
	seedRequest(store, 1, 7, domain.RequestPending)
	seedRequest(store, 2, 8, domain.RequestPending)
	h := newRequestTestHandler(store)

	c, recorder := newRequestTestContext(http.MethodGet, "/my/requests", "", nil)
	h.mine(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "/my/requests/1") || strings.Contains(body, "/my/requests/2") {
		t.Fatalf("list leaked another account's request:\n%s", body)
	}
	if !strings.Contains(body, "Pending") {
		t.Fatalf("list missing status badge:\n%s", body)
	}
}

func TestRequestMineDetailShowsShortIssueBadge(t *testing.T) {
	store := newMovementFakeStore()
	requestTestStoreLot(store)
	request := seedRequest(store, 1, 7, domain.RequestCompleted)
	issued, movement := uint32(3), int64(9)
	request.IssuedQuantity, request.CompletedMovementID = &issued, &movement
	store.requests[0] = request
	h := newRequestTestHandler(store)

	c, recorder := newRequestTestContext(http.MethodGet, "/my/requests/1", "", gin.Params{{Key: "id", Value: "1"}})
	h.mineDetail(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Completed · 3 of 5") || !strings.Contains(body, "Issued as movement #9") {
		t.Fatalf("detail body:\n%s", body)
	}
	if strings.Contains(body, "Cancel this request") {
		t.Fatalf("completed request offers a cancel form:\n%s", body)
	}
}

func TestRequestMineDetailOfAnotherAccountIsNotFound(t *testing.T) {
	store := newMovementFakeStore()
	requestTestStoreLot(store)
	seedRequest(store, 1, 8, domain.RequestPending)
	h := newRequestTestHandler(store)

	c, recorder := newRequestTestContext(http.MethodGet, "/my/requests/1", "", gin.Params{{Key: "id", Value: "1"}})
	h.mineDetail(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestRequestCancelByOwnerWritesAuditAndRedirects(t *testing.T) {
	store := newMovementFakeStore()
	requestTestStoreLot(store)
	seedRequest(store, 1, 7, domain.RequestPending)
	h := newRequestTestHandler(store)

	c, recorder := newRequestTestContext(http.MethodPost, "/my/requests/1/cancel",
		"reason=no+longer+needed", gin.Params{{Key: "id", Value: "1"}})
	h.cancel(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/my/requests/1?notice=cancelled" {
		t.Fatalf("Location = %q", location)
	}
	if store.requests[0].Status != domain.RequestCancelled || store.requests[0].DecisionNote != "no longer needed" {
		t.Fatalf("request = %+v", store.requests[0])
	}
	if len(store.audits) != 1 || store.audits[0].EventType != "REQUEST_CANCELLED" {
		t.Fatalf("audits = %+v", store.audits)
	}
}

func TestRequestCancelOfAnotherAccountIsNotFound(t *testing.T) {
	store := newMovementFakeStore()
	requestTestStoreLot(store)
	seedRequest(store, 1, 8, domain.RequestPending)
	h := newRequestTestHandler(store)

	c, recorder := newRequestTestContext(http.MethodPost, "/my/requests/1/cancel", "",
		gin.Params{{Key: "id", Value: "1"}})
	h.cancel(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	if store.requests[0].Status != domain.RequestPending {
		t.Fatalf("request changed = %+v", store.requests[0])
	}
}
