package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/repository/sqlite"
	"github.com/Panazule/guildbank/internal/service"
)

type fakePublicInventoryStore struct {
	ready      bool
	page       domain.InventoryRecordPage
	err        error
	lastFilter domain.InventoryFilter
}

func (f *fakePublicInventoryStore) Ready() bool { return f.ready }

func (f *fakePublicInventoryStore) ListPublicInventory(_ context.Context, filter domain.InventoryFilter) (domain.InventoryRecordPage, error) {
	f.lastFilter = filter
	if f.err != nil {
		return domain.InventoryRecordPage{}, f.err
	}
	if filter.Page > 1 {
		return domain.InventoryRecordPage{Records: []domain.InventoryRecord{}, Total: f.page.Total}, nil
	}
	return f.page, nil
}

func (f *fakePublicInventoryStore) GetPublicInventory(_ context.Context, id int64) (domain.InventoryRecord, error) {
	for _, record := range f.page.Records {
		if record.GuildBankItemID == id {
			return record, nil
		}
	}
	return domain.InventoryRecord{}, domain.ErrNotFound
}

type fakePublicSettings struct {
	base string
	err  error
}

func (f fakePublicSettings) Values(context.Context) (map[string]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return map[string]string{"EQResourceBaseUrl": f.base}, nil
}

func publicInventoryRecord() domain.InventoryRecord {
	return domain.InventoryRecord{
		GuildBankItemID: 7, EQUID: 28813, Name: "Blade of Carnage", ItemType: domain.ItemTypeItem,
		Quantity: 1, ClassString: "WAR,PAL", RaceString: "ALL",
		DateAdded:  time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC),
		DateOpen:   time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC),
		DateBazaar: time.Date(2026, 12, 21, 0, 0, 0, 0, time.UTC),
	}
}

func newPublicInventoryHarness(t *testing.T, inventory service.InventoryStore, eqURL string) *harness {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "guildbank.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	if _, err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	hash, err := service.HashPassword("test-password-123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := store.DB().Exec(`
		INSERT INTO Users (UserName, PasswordHash, AccessLevel, ModAt, CreatedAt)
		VALUES ('admin', ?, 'Admin', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, hash); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	settings := service.NewSettingsService(store)
	setup := service.NewSetupService(store, store, setupDBStub{})
	auth := service.NewAuthService(store, store, settings)
	inventoryService := service.NewInventoryService(inventory, fakePublicSettings{base: eqURL},
		func() time.Time { return time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC) },
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
	router, err := NewRouter(Deps{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Health: fakeHealth{result: service.Readiness{Ready: true}},
		Setup:  setup, Auth: auth, Settings: settings,
		Imports:   service.NewImportService(nil, nil, settings, nil, nil),
		Inventory: inventoryService, Version: "test",
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	server := httptest.NewServer(router)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	t.Cleanup(func() { server.Close(); _ = store.Close() })
	return &harness{t: t, server: server, client: client, store: store}
}

func (h *harness) loginAdmin() {
	h.t.Helper()
	csrf := h.csrf()
	body, _ := json.Marshal(map[string]string{"userName": "admin", "password": "test-password-123"})
	resp, data := h.request(http.MethodPost, "/api/v1/auth/login", csrf, body, "application/json")
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("login status = %d: %s", resp.StatusCode, data)
	}
}

func jsonKeys(raw []byte, path ...string) map[string]any {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	current, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func assertForbiddenFieldsAbsent(t *testing.T, body []byte) {
	t.Helper()
	for _, forbidden := range []string{"deletedFg", "sourceLocation", "givenTo", "modAt", "modBy", "modByUserId", "nameSort"} {
		if bytes.Contains(body, []byte(forbidden)) {
			t.Fatalf("public response contains forbidden field %q", forbidden)
		}
	}
}
func TestPublicInventoryAPIContract(t *testing.T) {
	store := &fakePublicInventoryStore{ready: true, page: domain.InventoryRecordPage{Records: []domain.InventoryRecord{publicInventoryRecord()}, Total: 1}}
	h := newPublicInventoryHarness(t, store, "https://items.eqresource.com/items.php?id=")

	resp, body := h.request(http.MethodGet, "/api/v1/items", "", nil, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q", got)
	}
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers missing: %v", resp.Header)
	}
	assertForbiddenFieldsAbsent(t, body)

	var payload struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json: %v\n%s", err, body)
	}
	if len(payload.Data.Items) != 1 {
		t.Fatalf("items = %#v", payload.Data.Items)
	}
	item := payload.Data.Items[0]
	allowed := map[string]bool{
		"id": true, "equid": true, "name": true, "itemType": true, "quantity": true,
		"classString": true, "raceString": true, "metadataMissing": true,
		"dateAdded": true, "dateOpen": true, "dateBazaar": true,
		"availability": true, "eqResourceUrl": true,
	}
	if len(item) != len(allowed) {
		t.Fatalf("public item keys = %#v", item)
	}
	for key := range item {
		if !allowed[key] {
			t.Fatalf("unexpected public key %q", key)
		}
	}
	if item["availability"] != "upcoming" || item["dateOpen"] != "2026-12-05" {
		t.Fatalf("item = %#v", item)
	}
	if payload.Meta["page"] != float64(1) || payload.Meta["pageSize"] != float64(50) ||
		payload.Meta["total"] != float64(1) || payload.Meta["totalPages"] != float64(1) ||
		payload.Meta["sort"] != "name" {
		t.Fatalf("meta = %#v", payload.Meta)
	}
	if store.lastFilter.Sort != domain.InventorySortName || store.lastFilter.PageSize != 50 {
		t.Fatalf("service filter = %+v", store.lastFilter)
	}

	detail, detailBody := h.request(http.MethodGet, "/api/v1/items/7", "", nil, "application/json")
	if detail.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d: %s", detail.StatusCode, detailBody)
	}
	assertForbiddenFieldsAbsent(t, detailBody)
	if !bytes.Contains(detailBody, []byte(`"id":7`)) || !bytes.Contains(detailBody, []byte(`"eqResourceUrl":"https://items.eqresource.com/items.php?id=28813"`)) {
		t.Fatalf("detail body = %s", detailBody)
	}

	for _, path := range []string{"/api/v1/items/999", "/api/v1/items/0", "/api/v1/items/not-a-number"} {
		resp, body := h.request(http.MethodGet, path, "", nil, "application/json")
		if resp.StatusCode != http.StatusNotFound || !bytes.Contains(body, []byte(`"code":"not_found"`)) {
			t.Fatalf("%s = %d: %s", path, resp.StatusCode, body)
		}
	}
}

func TestPublicInventoryValidationAndMethods(t *testing.T) {
	store := &fakePublicInventoryStore{ready: true, page: domain.InventoryRecordPage{Records: []domain.InventoryRecord{publicInventoryRecord()}, Total: 1}}
	h := newPublicInventoryHarness(t, store, "https://items.eqresource.com/items.php?id=")
	tests := []struct {
		path  string
		field string
	}{
		{"/api/v1/items?class=XXX", "class"},
		{"/api/v1/items?race=Martian", "race"},
		{"/api/v1/items?itemType=Quest", "itemType"},
		{"/api/v1/items?sort=NameSort%3B%20DROP", "sort"},
		{"/api/v1/items?page=0", "page"},
		{"/api/v1/items?pageSize=101", "pageSize"},
	}
	for _, tc := range tests {
		resp, body := h.request(http.MethodGet, tc.path, "", nil, "application/json")
		if resp.StatusCode != http.StatusUnprocessableEntity || !bytes.Contains(body, []byte(`"`+tc.field+`"`)) {
			t.Fatalf("%s = %d: %s", tc.path, resp.StatusCode, body)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		resp, _ := h.request(method, "/api/v1/items", "bad", []byte(`{}`), "application/json")
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want 405", method, resp.StatusCode)
		}
	}
}

func TestPublicInventoryPageBeyondEnd(t *testing.T) {
	store := &fakePublicInventoryStore{ready: true, page: domain.InventoryRecordPage{Records: []domain.InventoryRecord{publicInventoryRecord()}, Total: 1}}
	h := newPublicInventoryHarness(t, store, "https://items.eqresource.com/items.php?id=")
	resp, body := h.request(http.MethodGet, "/api/v1/items?page=2&pageSize=1", "", nil, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	var payload struct {
		Data struct {
			Items []any `json:"items"`
		} `json:"data"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Items == nil || len(payload.Data.Items) != 0 {
		t.Fatalf("items = %#v", payload.Data.Items)
	}
	if payload.Meta["page"] != float64(2) || payload.Meta["totalPages"] != float64(1) {
		t.Fatalf("meta = %#v", payload.Meta)
	}
	htmlResp, htmlBody := h.request(http.MethodGet, "/?page=2&pageSize=1", "", nil, "text/html")
	if htmlResp.StatusCode != http.StatusOK || !bytes.Contains(htmlBody, []byte("beyond the available results")) {
		t.Fatalf("html beyond = %d: %s", htmlResp.StatusCode, htmlBody)
	}
}
func TestPublicInventoryHTMLRenderingAndSessionParity(t *testing.T) {
	store := &fakePublicInventoryStore{ready: true, page: domain.InventoryRecordPage{Records: []domain.InventoryRecord{publicInventoryRecord()}, Total: 1}}
	h := newPublicInventoryHarness(t, store, "https://items.eqresource.com/items.php?id=")

	resp, body := h.request(http.MethodGet, "/", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{
		`<html lang="en">`, `<h1>Guild inventory</h1>`, `<caption class="sr-only">`,
		`name="q"`, `name="class"`, `name="race"`, `name="itemType"`, `name="sort"`,
		`href="/inventory/7"`, `target="_blank"`, `rel="noopener noreferrer"`,
		`aria-label="Open Blade of Carnage on EQResource (new tab)"`,
		`All`, `Opens Dec 5, 2026`, `1 inventory record`,
	} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("HTML missing %q\n%s", want, body)
		}
	}
	if bytes.Contains(body, []byte("/admin/settings")) || bytes.Contains(body, []byte("SourceLocation")) {
		t.Fatalf("public HTML contains admin/internal content: %s", body)
	}

	h.loginAdmin()
	authResp, authBody := h.request(http.MethodGet, "/api/v1/items", "", nil, "application/json")
	if authResp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated API status = %d", authResp.StatusCode)
	}
	assertForbiddenFieldsAbsent(t, authBody)
	if !bytes.Contains(authBody, []byte(`"name":"Blade of Carnage"`)) {
		t.Fatalf("authenticated projection changed: %s", authBody)
	}
	adminPage, adminBody := h.request(http.MethodGet, "/", "", nil, "text/html")
	if adminPage.StatusCode != http.StatusOK || !bytes.Contains(adminBody, []byte(`href="/admin/settings"`)) {
		t.Fatalf("authenticated navigation missing: %s", adminBody)
	}

	invalidCookieReq, _ := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/items", nil)
	invalidCookieReq.Header.Set("Accept", "application/json")
	invalidCookieReq.AddCookie(&http.Cookie{Name: "guildbank_session", Value: "invalid"})
	invalidClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	invalidResp, err := invalidClient.Do(invalidCookieReq)
	if err != nil {
		t.Fatal(err)
	}
	invalidBody, _ := io.ReadAll(invalidResp.Body)
	_ = invalidResp.Body.Close()
	if invalidResp.StatusCode != http.StatusOK || !bytes.Contains(invalidBody, []byte(`"name":"Blade of Carnage"`)) {
		t.Fatalf("invalid public cookie changed payload: %d %s", invalidResp.StatusCode, invalidBody)
	}
}

func TestPublicInventoryDetailHTMLAndUnsafeSetting(t *testing.T) {
	store := &fakePublicInventoryStore{ready: true, page: domain.InventoryRecordPage{Records: []domain.InventoryRecord{publicInventoryRecord()}, Total: 1}}
	h := newPublicInventoryHarness(t, store, "http://evil.example/items.php?id=")
	resp, body := h.request(http.MethodGet, "/inventory/7", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{
		"Blade of Carnage", "WAR, PAL", "All races", "Opens Dec 5, 2026",
		`<time datetime="2026-11-21">Nov 21, 2026</time>`,
		`<time datetime="2026-12-05">Dec 5, 2026</time>`,
		`<time datetime="2026-12-21">Dec 21, 2026</time>`,
		"Back to guild inventory",
	} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("detail missing %q\n%s", want, body)
		}
	}
	if bytes.Contains(body, []byte("evil.example")) || bytes.Contains(body, []byte("EQResource")) {
		t.Fatalf("invalid EQResource setting rendered a link: %s", body)
	}

	notFound, notFoundBody := h.request(http.MethodGet, "/inventory/999", "", nil, "text/html")
	if notFound.StatusCode != http.StatusNotFound || !bytes.Contains(notFoundBody, []byte("does not exist")) {
		t.Fatalf("missing detail = %d: %s", notFound.StatusCode, notFoundBody)
	}
}

func TestPublicInventoryEscapesStoredNames(t *testing.T) {
	record := publicInventoryRecord()
	record.Name = `<script>alert(1)</script>`
	store := &fakePublicInventoryStore{ready: true, page: domain.InventoryRecordPage{Records: []domain.InventoryRecord{record}, Total: 1}}
	h := newPublicInventoryHarness(t, store, "https://items.eqresource.com/items.php?id=")
	resp, body := h.request(http.MethodGet, "/", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if bytes.Contains(body, []byte(`<script>alert(1)</script>`)) {
		t.Fatal("stored XSS was not escaped")
	}
	if !bytes.Contains(body, []byte(`&lt;script&gt;alert(1)&lt;/script&gt;`)) {
		t.Fatalf("escaped name missing: %s", body)
	}
}

func TestPublicInventoryUnavailablePaths(t *testing.T) {
	h := newPublicInventoryHarness(t, nil, "")
	apiResp, apiBody := h.request(http.MethodGet, "/api/v1/items", "", nil, "application/json")
	if apiResp.StatusCode != http.StatusServiceUnavailable || !bytes.Contains(apiBody, []byte(`"code":"dependency_unavailable"`)) {
		t.Fatalf("API unavailable = %d: %s", apiResp.StatusCode, apiBody)
	}
	htmlResp, htmlBody := h.request(http.MethodGet, "/", "", nil, "text/html")
	if htmlResp.StatusCode != http.StatusServiceUnavailable || !bytes.Contains(htmlBody, []byte("temporarily unavailable")) {
		t.Fatalf("HTML unavailable = %d: %s", htmlResp.StatusCode, htmlBody)
	}
	if htmlResp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unavailable page cache header = %q", htmlResp.Header.Get("Cache-Control"))
	}
}
