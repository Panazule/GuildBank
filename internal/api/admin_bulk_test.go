package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/web"
)

func bulkTestItem(id int64) domain.AdminInventoryItem {
	return domain.AdminInventoryItem{
		ID: id, EQUID: 100, Name: "Sword", ItemType: domain.ItemTypeItem, Quantity: 1,
		SourceLocation: "General1-Slot1", LocationGroup: "General 1",
		LocationSort: "general\x010000000001", GivenTo: "Guild Bank",
		DateAdded:  time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		DateOpen:   time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC),
		DateBazaar: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		ModAt:      time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC),
		ModBy:      "admin",
	}
}

func TestAdminInventoryPageRendersBulkSelectionAndOtherLabel(t *testing.T) {
	v, err := newViews(web.Templates)
	if err != nil {
		t.Fatalf("newViews: %v", err)
	}
	item := bulkTestItem(5)
	page := domain.AdminInventoryPage{
		Records: []domain.AdminInventoryItem{item}, Total: 1, Page: 1, PageSize: 50, TotalPages: 1,
		Filter: domain.AdminInventoryFilter{
			Status: domain.AdminStatusActive, Sort: domain.AdminSortModifiedNewest, Page: 1, PageSize: 50,
		},
	}
	data := adminListPageOf(page, []string{"General 1", "Unspecified"})
	data.pageData = pageData{Title: "Admin inventory", CSRF: "csrf-token"}
	var buf bytes.Buffer
	if err := v.templates.ExecuteTemplate(&buf, "admin_inventory.html", data); err != nil {
		t.Fatalf("render admin list: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`action="/admin/inventory/bulk-remove"`,
		`name="remove" value="5|2026-11-22T10:00:00Z"`,
		`<option value="Unspecified">Other</option>`,
		`Remove selected…`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin list missing %q", want)
		}
	}
}

func TestAdminBulkRemovePageRendersApplyForm(t *testing.T) {
	v, err := newViews(web.Templates)
	if err != nil {
		t.Fatalf("newViews: %v", err)
	}
	item := bulkTestItem(7)
	token := "7|2026-11-22T10:00:00Z"
	var buf bytes.Buffer
	data := adminBulkRemovePageData{
		pageData: pageData{Title: "Remove records", CSRF: "csrf-token"},
		Items:    []adminItemView{adminItemViewOf(item)},
		Targets:  []string{token},
	}
	if err := v.templates.ExecuteTemplate(&buf, "admin_bulk_remove.html", data); err != nil {
		t.Fatalf("render bulk preview: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`action="/admin/inventory/bulk-remove/apply"`,
		`name="remove" value="` + token + `"`,
		`name="csrf_token" value="csrf-token"`,
		`Remove 1 record`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("bulk preview missing %q", want)
		}
	}
}

func TestBulkTargetsFromValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	modAt := "2026-11-22T10:00:00Z"
	newContext := func(values url.Values) *gin.Context {
		req := httptest.NewRequest(http.MethodPost, "/admin/inventory/bulk-remove", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req
		return c
	}
	targets, tokens, fields := bulkTargetsFrom(newContext(url.Values{"remove": {"5|" + modAt, "6|" + modAt}}))
	if len(fields) != 0 || len(targets) != 2 || len(tokens) != 2 {
		t.Fatalf("targets=%d tokens=%d fields=%v", len(targets), len(tokens), fields)
	}
	if targets[0].ID != 5 || targets[1].ID != 6 || targets[0].ExpectedModAt.IsZero() {
		t.Fatalf("targets = %+v", targets)
	}
	if _, _, fields := bulkTargetsFrom(newContext(url.Values{})); fields["remove"] == "" {
		t.Fatal("empty selection did not report a remove field error")
	}
	if _, _, fields := bulkTargetsFrom(newContext(url.Values{"remove": {"5"}})); fields["remove"] == "" {
		t.Fatal("malformed token did not report a remove field error")
	}
	if _, _, fields := bulkTargetsFrom(newContext(url.Values{"remove": {"5|not-a-date"}})); fields["remove"] == "" {
		t.Fatal("invalid version token did not report a remove field error")
	}
	if _, _, fields := bulkTargetsFrom(newContext(url.Values{"remove": {"5|" + modAt, "5|" + modAt}})); fields["remove"] == "" {
		t.Fatal("duplicate selection did not report a remove field error")
	}
}
