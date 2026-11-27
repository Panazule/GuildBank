package api

import (
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

var adminRequestTestNow = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

func newAdminRequestTestHandler(store *movementFakeStore) *adminRequestHandler {
	viewSet, err := newViews(web.Templates)
	if err != nil {
		panic(err)
	}
	svc := service.NewAdminInventoryService(store, nil, nil, nil,
		func() time.Time { return adminRequestTestNow },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return &adminRequestHandler{
		admin: svc, views: viewSet,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		now:    func() time.Time { return adminRequestTestNow },
	}
}

func newAdminRequestTestContext(method, target, form string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(form))
	if form != "" {
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c.Params = params
	c.Set(requestIDKey, "req-admin-0001")
	c.Set(principalKey, service.AuthenticatedSession{
		User: domain.User{ID: 2, UserName: "officer1", AccessLevel: "Officer"},
	})
	return c, recorder
}

func seedOfficerRequest(store *movementFakeStore, id int64, status domain.RequestStatus,
	requested uint32, createdAt time.Time) domain.InventoryRequest {
	requester := int64(7)
	request := domain.InventoryRequest{
		RequestID: id, GuildBankItemID: 1, EQUID: 100, ItemName: "Cloak of Flames",
		RequestedQuantity: requested, OnHandAtCreate: requested, CharacterName: "Icefear",
		RequesterKind: domain.RequesterMember, RequesterUserID: &requester,
		RequesterUserName: "member1", Note: "for raid", Status: status,
		CorrelationID: "corr-seed", CreatedAt: createdAt, CreatedByName: "member1",
	}
	store.requests = append(store.requests, request)
	if store.nextRequestID <= id {
		store.nextRequestID = id + 1
	}
	return request
}

func TestAdminRequestQueueDefaultsToPendingOldestFirst(t *testing.T) {
	store := newMovementFakeStore()
	requestTestStoreLot(store)
	seedOfficerRequest(store, 1, domain.RequestPending, 5, adminRequestTestNow.Add(-72*time.Hour))
	seedOfficerRequest(store, 2, domain.RequestPending, 5, adminRequestTestNow.Add(-time.Hour))
	seedOfficerRequest(store, 3, domain.RequestCompleted, 5, adminRequestTestNow)
	h := newAdminRequestTestHandler(store)

	c, recorder := newAdminRequestTestContext(http.MethodGet, "/admin/requests", "", nil)
	h.queue(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "/admin/requests/1") || !strings.Contains(body, "/admin/requests/2") {
		t.Fatalf("queue missing pending rows:\n%s", body)
	}
	if strings.Contains(body, `href="/admin/requests/3"`) {
		t.Fatalf("queue leaked a completed row under the default filter:\n%s", body)
	}
	if strings.Index(body, "#1") > strings.Index(body, "#2") {
		t.Fatalf("queue is not oldest-first:\n%s", body)
	}
	if !strings.Contains(body, "3d") || !strings.Contains(body, "age-warning") {
		t.Fatalf("queue missing age band:\n%s", body)
	}
}

func TestAdminRequestDetailCoveredShowsNoQuantityField(t *testing.T) {
	store := newMovementFakeStore()
	requestTestStoreLot(store)
	seedOfficerRequest(store, 1, domain.RequestPending, 5, adminRequestTestNow)
	h := newAdminRequestTestHandler(store)

	c, recorder := newAdminRequestTestContext(http.MethodGet, "/admin/requests/1", "",
		gin.Params{{Key: "id", Value: "1"}})
	h.detail(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Approve and issue 5 × Cloak of Flames") {
		t.Fatalf("approve button missing:\n%s", body)
	}
	if strings.Contains(body, `name="issuedQuantity"`) {
		t.Fatalf("covered request rendered a quantity field:\n%s", body)
	}
	if !strings.Contains(body, `name="reason"`) || !strings.Contains(body, "Deny this request") {
		t.Fatalf("deny form missing:\n%s", body)
	}
}

func TestAdminRequestDetailShortAndEmptyStockShapes(t *testing.T) {
	t.Run("short", func(t *testing.T) {
		store := newMovementFakeStore()
		requestTestStoreLot(store)
		store.items[1] = func() domain.AdminInventoryItem {
			item := store.items[1]
			item.Quantity = 2
			return item
		}()
		seedOfficerRequest(store, 1, domain.RequestPending, 5, adminRequestTestNow)
		h := newAdminRequestTestHandler(store)
		c, recorder := newAdminRequestTestContext(http.MethodGet, "/admin/requests/1", "",
			gin.Params{{Key: "id", Value: "1"}})
		h.detail(c)
		c.Writer.WriteHeaderNow()
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d", recorder.Code)
		}
		body := recorder.Body.String()
		if !strings.Contains(body, "Requested 5 · Available 2") {
			t.Fatalf("short warning missing:\n%s", body)
		}
		if !strings.Contains(body, `name="issuedQuantity" value="2"`) {
			t.Fatalf("short issue prefills the on-hand quantity:\n%s", body)
		}
	})

	t.Run("empty", func(t *testing.T) {
		store := newMovementFakeStore()
		requestTestStoreLot(store)
		store.items[1] = func() domain.AdminInventoryItem {
			item := store.items[1]
			item.Quantity = 0
			return item
		}()
		seedOfficerRequest(store, 1, domain.RequestPending, 5, adminRequestTestNow)
		h := newAdminRequestTestHandler(store)
		c, recorder := newAdminRequestTestContext(http.MethodGet, "/admin/requests/1", "",
			gin.Params{{Key: "id", Value: "1"}})
		h.detail(c)
		c.Writer.WriteHeaderNow()
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d", recorder.Code)
		}
		body := recorder.Body.String()
		if !strings.Contains(body, "can only be denied") {
			t.Fatalf("empty-stock warning missing:\n%s", body)
		}
		if strings.Contains(body, `/admin/requests/1/approve`) {
			t.Fatalf("empty-stock page rendered an approve form:\n%s", body)
		}
		if !strings.Contains(body, `/admin/requests/1/deny`) {
			t.Fatalf("empty-stock page must still allow denial:\n%s", body)
		}
	})
}

func TestAdminRequestDetailCompletedShowsIssuedQuantity(t *testing.T) {
	store := newMovementFakeStore()
	requestTestStoreLot(store)
	request := seedOfficerRequest(store, 1, domain.RequestCompleted, 5, adminRequestTestNow)
	issued, movementID := uint32(3), int64(9)
	request.IssuedQuantity, request.CompletedMovementID = &issued, &movementID
	request.DecidedByName = "officer1"
	store.requests[0] = request
	h := newAdminRequestTestHandler(store)

	c, recorder := newAdminRequestTestContext(http.MethodGet, "/admin/requests/1", "",
		gin.Params{{Key: "id", Value: "1"}})
	h.detail(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Issued") || !strings.Contains(body, "3 of 5") {
		t.Fatalf("completed page missing the issued comparison:\n%s", body)
	}
	if strings.Contains(body, `action="/admin/requests/1/approve"`) {
		t.Fatalf("completed page rendered an approve form:\n%s", body)
	}
}

func TestAdminRequestApproveIssuesThroughTheLedger(t *testing.T) {
	store := newMovementFakeStore()
	lot := requestTestStoreLot(store)
	seedOfficerRequest(store, 1, domain.RequestPending, 5, adminRequestTestNow)
	h := newAdminRequestTestHandler(store)

	form := "expectedModAt=" + lot.ModAt.Format(time.RFC3339Nano) + "&note=handled+at+raid"
	c, recorder := newAdminRequestTestContext(http.MethodPost, "/admin/requests/1/approve", form,
		gin.Params{{Key: "id", Value: "1"}})
	h.approve(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/admin/requests/1?notice=completed" {
		t.Fatalf("Location = %q", location)
	}
	if store.requests[0].Status != domain.RequestCompleted || store.requests[0].IssuedQuantity == nil ||
		*store.requests[0].IssuedQuantity != 5 {
		t.Fatalf("request = %+v", store.requests[0])
	}
	if len(store.movements) != 1 || store.movements[0].RequestID == nil ||
		*store.movements[0].RequestID != 1 || store.movements[0].Quantity != 5 {
		t.Fatalf("movements = %+v", store.movements)
	}
	if !store.items[lot.ID].DeletedFg || store.items[lot.ID].GivenTo != "Icefear" {
		t.Fatalf("lot = %+v", store.items[lot.ID])
	}
	if len(store.audits) != 2 || store.audits[1].EventType != "REQUEST_COMPLETED" {
		t.Fatalf("audits = %+v", store.audits)
	}
}

func TestAdminRequestApproveShortRedirectsWithShortNotice(t *testing.T) {
	store := newMovementFakeStore()
	lot := requestTestStoreLot(store)
	short := store.items[lot.ID]
	short.Quantity = 2
	store.items[lot.ID] = short
	seedOfficerRequest(store, 1, domain.RequestPending, 5, adminRequestTestNow)
	h := newAdminRequestTestHandler(store)

	form := "expectedModAt=" + lot.ModAt.Format(time.RFC3339Nano) + "&issuedQuantity=2"
	c, recorder := newAdminRequestTestContext(http.MethodPost, "/admin/requests/1/approve", form,
		gin.Params{{Key: "id", Value: "1"}})
	h.approve(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/admin/requests/1?notice=short-issued" {
		t.Fatalf("Location = %q", location)
	}
	if store.requests[0].IssuedQuantity == nil || *store.requests[0].IssuedQuantity != 2 {
		t.Fatalf("request = %+v", store.requests[0])
	}
}

func TestAdminRequestApproveCoveredFormRejectsSilentShortIssue(t *testing.T) {
	store := newMovementFakeStore()
	lot := requestTestStoreLot(store)
	short := store.items[lot.ID]
	short.Quantity = 2
	store.items[lot.ID] = short
	seedOfficerRequest(store, 1, domain.RequestPending, 5, adminRequestTestNow)
	h := newAdminRequestTestHandler(store)

	// The officer submitted the covered shape (no issuedQuantity), but the
	// lot now holds only 2. The approval must not silently issue 2.
	form := "expectedModAt=" + lot.ModAt.Format(time.RFC3339Nano)
	c, recorder := newAdminRequestTestContext(http.MethodPost, "/admin/requests/1/approve", form,
		gin.Params{{Key: "id", Value: "1"}})
	h.approve(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "The stock changed") || !strings.Contains(body, "Requested 5 · Available 2") {
		t.Fatalf("re-render did not switch to the short-stock shape:\n%s", body)
	}
	if !strings.Contains(body, `name="issuedQuantity" value="2"`) {
		t.Fatalf("short-stock re-render missing the prefilled quantity:\n%s", body)
	}
	if store.requests[0].Status != domain.RequestPending || len(store.movements) != 0 {
		t.Fatal("a covered-form submission issued stock without confirmation")
	}
}

func TestAdminRequestApproveStaleStockRerendersAtConflict(t *testing.T) {
	store := newMovementFakeStore()
	lot := requestTestStoreLot(store)
	seedOfficerRequest(store, 1, domain.RequestPending, 5, adminRequestTestNow)
	h := newAdminRequestTestHandler(store)

	form := "expectedModAt=" + lot.ModAt.Add(time.Minute).Format(time.RFC3339Nano)
	c, recorder := newAdminRequestTestContext(http.MethodPost, "/admin/requests/1/approve", form,
		gin.Params{{Key: "id", Value: "1"}})
	h.approve(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "item changed since this page was loaded") {
		t.Fatalf("stale body:\n%s", recorder.Body.String())
	}
	if store.requests[0].Status != domain.RequestPending || len(store.movements) != 0 {
		t.Fatal("a stale approval changed state")
	}
}

func TestAdminRequestDenyRequiresReasonAndSucceeds(t *testing.T) {
	store := newMovementFakeStore()
	requestTestStoreLot(store)
	seedOfficerRequest(store, 1, domain.RequestPending, 5, adminRequestTestNow)
	h := newAdminRequestTestHandler(store)

	c, recorder := newAdminRequestTestContext(http.MethodPost, "/admin/requests/1/deny", "",
		gin.Params{{Key: "id", Value: "1"}})
	h.deny(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank reason status = %d body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Reason is required") {
		t.Fatalf("blank reason body:\n%s", recorder.Body.String())
	}
	if store.requests[0].Status != domain.RequestPending {
		t.Fatal("blank reason decided the request")
	}

	c, recorder = newAdminRequestTestContext(http.MethodPost, "/admin/requests/1/deny",
		"reason=no+stock+this+lockout", gin.Params{{Key: "id", Value: "1"}})
	h.deny(c)
	c.Writer.WriteHeaderNow()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("deny status = %d body %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/admin/requests/1?notice=denied" {
		t.Fatalf("Location = %q", location)
	}
	if store.requests[0].Status != domain.RequestDenied ||
		store.requests[0].DecisionNote != "no stock this lockout" {
		t.Fatalf("request = %+v", store.requests[0])
	}
	if len(store.movements) != 0 || len(store.audits) != 1 ||
		store.audits[0].EventType != "REQUEST_DENIED" {
		t.Fatalf("movements=%d audits=%+v", len(store.movements), store.audits)
	}
}
