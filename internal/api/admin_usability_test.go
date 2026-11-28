package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
	"github.com/Panazule/guildbank/internal/web"
)

// stubMetadata is a fixed AdminMetadataResolver used to exercise the name/EQUID
// mismatch advisory without a database.
type stubMetadata map[uint64]service.ItemMetadata

func (s stubMetadata) Resolve(_ context.Context, ref domain.ItemReference) (service.ItemMetadata, error) {
	meta, ok := s[ref.EQUID]
	if !ok {
		return service.ItemMetadata{}, domain.NewValidationError("EQUID", "missing metadata fixture")
	}
	return meta, nil
}

func newMetadataPageHandler(t *testing.T, store *movementFakeStore, metadata service.AdminMetadataResolver) *adminInventoryHandler {
	t.Helper()
	h := newPageTestHandler(t, store)
	h.admin = service.NewAdminInventoryService(store,
		movementTestSettings{"GuildOpenDays": "14", "BazaarDays": "30"},
		metadata, nil, func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return h
}

// TestAdminSafeReturnToRejectsUntrustedTargets locks the one-destination rule so
// the carried navigation context can never become an open redirect.
func TestAdminSafeReturnToRejectsUntrustedTargets(t *testing.T) {
	cases := map[string]string{
		"":                                     "",
		"/admin/inventory":                     "/admin/inventory",
		"/admin/inventory?q=ant&status=all":    "/admin/inventory?q=ant&status=all",
		"/admin/inventory?page=2":              "/admin/inventory?page=2",
		"https://evil.example/admin/inventory": "",
		"//evil.example/admin/inventory":       "",
		"/admin/audit":                         "",
		"/admin/inventory#frag":                "",
		"/admin/inventory/../audit":            "",
	}
	for raw, want := range cases {
		if got := adminSafeReturnTo(raw); got != want {
			t.Fatalf("adminSafeReturnTo(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestAdminItemViewStatusLabelDistinguishesIssued asserts the presentation-only
// ISSUED badge: a removed row that carries a recipient is Issued, a plain
// removal stays Removed, and an active row stays Active.
func TestAdminItemViewStatusLabelDistinguishesIssued(t *testing.T) {
	active := adminItemViewOf(movementTestItem(1, 3))
	if active.StatusLabel != "Active" || active.StatusClass != "status-Completed" {
		t.Fatalf("active status = %q/%q", active.StatusLabel, active.StatusClass)
	}
	issued := adminItemViewOf(issuedItem(2, 3, "Joe"))
	if issued.StatusLabel != "Issued" || issued.StatusClass != "status-Issued" {
		t.Fatalf("issued status = %q/%q", issued.StatusLabel, issued.StatusClass)
	}
	removedItem := movementTestItem(3, 3)
	removedItem.DeletedFg = true
	removed := adminItemViewOf(removedItem)
	if removed.StatusLabel != "Removed" || removed.StatusClass != "status-Failed" {
		t.Fatalf("removed status = %q/%q", removed.StatusLabel, removed.StatusClass)
	}
}

// TestAdminListRendersIssuedBadgeAndContextLinks renders the list template to
// prove the ISSUED badge appears and the row actions carry the filtered view.
func TestAdminListRendersIssuedBadgeAndContextLinks(t *testing.T) {
	v, err := newViews(web.Templates)
	if err != nil {
		t.Fatalf("newViews: %v", err)
	}
	page := domain.AdminInventoryPage{
		Records: []domain.AdminInventoryItem{issuedItem(5, 2, "Joe")},
		Total:   1, Page: 1, PageSize: 50, TotalPages: 1,
		Filter: domain.AdminInventoryFilter{
			Search: "ant", Status: domain.AdminStatusAll, Sort: domain.AdminSortModifiedNewest, Page: 1, PageSize: 50,
		},
	}
	data := adminListPageOf(page, nil)
	data.pageData = pageData{Title: "Admin inventory", CSRF: "csrf-token"}
	var buf bytes.Buffer
	if err := v.templates.ExecuteTemplate(&buf, "admin_inventory.html", data); err != nil {
		t.Fatalf("render admin list: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		`<a class="status-badge status-Issued" href="/admin/inventory?givenTo=Joe&amp;status=issued">Issued</a>`,
		`returnTo=%2Fadmin%2Finventory%3Fq%3Dant`,
		`name="returnTo" value="/admin/inventory?q=ant&amp;status=all"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin list missing %q\n%s", want, body)
		}
	}
}

// TestAdminListRendersAvailabilityAfterQuantity pins the shared availability
// presentation on the administrative grid: the column follows Qty, every badge
// carries the same state and label the public grid renders, and the persisted
// "Unspecified" group is presented as "Other".
func TestAdminListRendersAvailabilityAfterQuantity(t *testing.T) {
	v, err := newViews(web.Templates)
	if err != nil {
		t.Fatalf("newViews: %v", err)
	}
	other := movementTestItem(11, 4)
	other.LocationGroup = "Unspecified"
	other.LocationSort = "unspecified"
	other.DateOpen = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	other.DateBazaar = time.Date(2000, 2, 1, 0, 0, 0, 0, time.UTC)
	upcoming := movementTestItem(12, 2)
	upcoming.DateOpen = time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC)
	upcoming.DateBazaar = time.Date(2999, 2, 1, 0, 0, 0, 0, time.UTC)

	page := domain.AdminInventoryPage{
		Records: []domain.AdminInventoryItem{other, upcoming},
		Total:   2, Page: 1, PageSize: 50, TotalPages: 1,
		Filter: domain.AdminInventoryFilter{
			Status: domain.AdminStatusAll, Sort: domain.AdminSortModifiedNewest,
			LocationGroup: "Unspecified", Page: 1, PageSize: 50,
		},
	}
	data := adminListPageOf(page, []string{"General 1", "Unspecified"})
	data.pageData = pageData{Title: "Admin inventory", CSRF: "csrf-token"}
	var buf bytes.Buffer
	if err := v.templates.ExecuteTemplate(&buf, "admin_inventory.html", data); err != nil {
		t.Fatalf("render admin list: %v", err)
	}
	body := buf.String()

	// Availability is the column immediately after Qty.
	qtyHeader := `<th scope="col">Qty</th>`
	qty := strings.Index(body, qtyHeader)
	if qty < 0 {
		t.Fatalf("admin list has no Qty header\n%s", body)
	}
	rest := body[qty+len(qtyHeader):]
	next := strings.Index(rest, "<th ")
	if next < 0 || !strings.HasPrefix(rest[next:], `<th scope="col">Availability</th>`) {
		t.Fatalf("the column after Qty is not Availability\n%s", rest)
	}

	// Every badge is the public calculation and label mapping, not a copy.
	today := time.Now().UTC()
	for _, item := range []domain.AdminInventoryItem{other, upcoming} {
		state := service.Availability(today, item.DateOpen, item.DateBazaar)
		want := `<span class="availability-badge availability-` + string(state) + `">` +
			availabilityLabel(state) + `</span>`
		if !strings.Contains(body, want) {
			t.Fatalf("admin list missing %q\n%s", want, body)
		}
	}
	for _, want := range []string{
		`<span class="availability-badge availability-bazaar">Bazaar</span>`,
		`<span class="availability-badge availability-upcoming">Mains</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin list missing the public label %q\n%s", want, body)
		}
	}

	// Unspecified renders as Other in the group cell and in the filter, and the
	// long label is gone.
	if !strings.Contains(body, `<span class="group-badge">Other</span>`) {
		t.Fatalf("the Unspecified row is not labelled Other\n%s", body)
	}
	if !strings.Contains(body, `<option value="Unspecified" selected>Other</option>`) {
		t.Fatalf("the Unspecified filter option is not labelled Other\n%s", body)
	}
	if strings.Contains(body, "not a bank location") {
		t.Fatalf("the long group label is still rendered\n%s", body)
	}
}

// TestCreateFormRedirectsWithNameMismatchAdvisory asserts the advisory is
// surfaced after a successful create without blocking the write.
func TestCreateFormRedirectsWithNameMismatchAdvisory(t *testing.T) {
	store := newMovementFakeStore()
	metadata := stubMetadata{100: {EQUID: 100, Name: "Sword", ClassString: "WAR", RaceString: "ALL"}}
	h := newMetadataPageHandler(t, store, metadata)
	form := url.Values{
		"itemType": {"Item"}, "equid": {"100"}, "quantity": {"1"}, "name": {"Sord"},
	}.Encode()
	res := runMovementHandler(http.MethodPost, "/admin/inventory", form, nil, h.createForm)
	if res.status != http.StatusSeeOther {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	if !strings.HasPrefix(res.location, "/admin/inventory/1?notice=created") ||
		!strings.Contains(res.location, "nameWarning=1") {
		t.Fatalf("location = %q, want created notice with nameWarning", res.location)
	}
	if store.items[1].Name != "Sword" {
		t.Fatalf("stored name = %q, want Sword", store.items[1].Name)
	}

	// A matching name carries no advisory.
	matchStore := newMovementFakeStore()
	matchHandler := newMetadataPageHandler(t, matchStore, metadata)
	matchForm := url.Values{
		"itemType": {"Item"}, "equid": {"100"}, "quantity": {"1"}, "name": {"Sword"},
	}.Encode()
	res = runMovementHandler(http.MethodPost, "/admin/inventory", matchForm, nil, matchHandler.createForm)
	if strings.Contains(res.location, "nameWarning") {
		t.Fatalf("matching name produced an advisory: %q", res.location)
	}
}

// TestDetailPageShowsNameMismatchAdvisory asserts the advisory renders on the
// record page and explains which name is stored.
func TestDetailPageShowsNameMismatchAdvisory(t *testing.T) {
	store := newMovementFakeStore()
	store.items[1] = movementTestItem(1, 5)
	h := newPageTestHandler(t, store)
	res := runMovementHandler(http.MethodGet, "/admin/inventory/1?notice=created&nameWarning=1", "",
		movementTestParams("1"), h.detailPage)
	if res.status != http.StatusOK || !strings.Contains(res.body, "metadata name shown above") {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
}

// TestReturnPagePrefillsRecipientFromIssuedLot asserts the return form
// pre-populates the recipient with the recorded GivenTo while staying editable.
func TestReturnPagePrefillsRecipientFromIssuedLot(t *testing.T) {
	store := newMovementFakeStore()
	store.items[1] = issuedItem(1, 5, "Joe")
	h := newPageTestHandler(t, store)
	res := runMovementHandler(http.MethodGet, "/admin/inventory/1/return", "", movementTestParams("1"), h.returnPage)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	if !strings.Contains(res.body, `name="recipient" type="text" maxlength="128" value="Joe"`) {
		t.Fatalf("return form did not prefill the recipient\n%s", res.body)
	}
}

// TestIssueFormReturnsToPreservedListView asserts a carried list context sends
// the officer back to the same filtered view, while an untrusted value falls
// back to the record page.
func TestIssueFormReturnsToPreservedListView(t *testing.T) {
	store := newMovementFakeStore()
	store.items[1] = movementTestItem(1, 10)
	h := newPageTestHandler(t, store)

	form := url.Values{
		"quantity": {"5"}, "recipient": {"Joe"}, "expectedModAt": {"2026-11-22T10:00:00Z"},
		"idempotencyKey": {"ctx-key-1"}, "returnTo": {"/admin/inventory?q=ant&status=all"},
	}.Encode()
	res := runMovementHandler(http.MethodPost, "/admin/inventory/1/issue", form, movementTestParams("1"), h.issueForm)
	if res.status != http.StatusSeeOther {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	if !strings.HasPrefix(res.location, "/admin/inventory?") ||
		!strings.Contains(res.location, "q=ant") ||
		!strings.Contains(res.location, "status=all") ||
		!strings.Contains(res.location, "notice=issued") {
		t.Fatalf("location = %q, want the preserved filtered view", res.location)
	}

	// A stale version re-renders the form and keeps the carried context.
	bad := url.Values{
		"quantity": {"5"}, "recipient": {"Joe"}, "expectedModAt": {"2026-11-22T09:00:00Z"},
		"idempotencyKey": {"ctx-key-2"}, "returnTo": {"/admin/inventory?q=ant&status=all"},
	}.Encode()
	res = runMovementHandler(http.MethodPost, "/admin/inventory/1/issue", bad, movementTestParams("1"), h.issueForm)
	if res.status != http.StatusConflict ||
		!strings.Contains(res.body, `name="returnTo" value="/admin/inventory?q=ant&amp;status=all"`) {
		t.Fatalf("re-render lost the context: status %d body %s", res.status, res.body)
	}
}

func TestIssueFormRejectsUntrustedReturnTo(t *testing.T) {
	store := newMovementFakeStore()
	store.items[1] = movementTestItem(1, 10)
	h := newPageTestHandler(t, store)
	form := url.Values{
		"quantity": {"5"}, "recipient": {"Joe"}, "expectedModAt": {"2026-11-22T10:00:00Z"},
		"idempotencyKey": {"ctx-key-3"}, "returnTo": {"https://evil.example/"},
	}.Encode()
	res := runMovementHandler(http.MethodPost, "/admin/inventory/1/issue", form, movementTestParams("1"), h.issueForm)
	if res.status != http.StatusSeeOther || res.location != "/admin/inventory/1?notice=issued" {
		t.Fatalf("status = %d location = %q, want the record-page fallback", res.status, res.location)
	}
}

// TestHistoryBannerTracksLotState asserts the movement-history banner offers one
// canonical action for each lot state.
func TestHistoryBannerTracksLotState(t *testing.T) {
	active := adminHistoryBannerFor(movementTestItem(1, 5))
	if active.ActionLabel != "Issue" || active.ActionForm || active.ActionURL != "/admin/inventory/1/issue" {
		t.Fatalf("active banner = %+v", active)
	}
	issued := adminHistoryBannerFor(issuedItem(2, 5, "Joe"))
	if issued.ActionLabel != "Return" || issued.ActionForm ||
		!strings.Contains(issued.Text, "issued to Joe") {
		t.Fatalf("issued banner = %+v", issued)
	}
	removedItem := movementTestItem(3, 5)
	removedItem.DeletedFg = true
	removed := adminHistoryBannerFor(removedItem)
	if removed.ActionLabel != "Restore record" || !removed.ActionForm {
		t.Fatalf("removed banner = %+v", removed)
	}
}

func TestMovementsPageRendersIssuedBanner(t *testing.T) {
	store := newMovementFakeStore()
	store.items[1] = issuedItem(1, 5, "Joe")
	h := newPageTestHandler(t, store)
	res := runMovementHandler(http.MethodGet, "/admin/inventory/1/movements", "", movementTestParams("1"), h.movementsPage)
	if res.status != http.StatusOK || !strings.Contains(res.body, "This lot is currently issued to Joe.") {
		t.Fatalf("status = %d body %s", res.status, res.body)
	}
	if !strings.Contains(res.body, `href="/admin/inventory/1/return"`) {
		t.Fatalf("banner action missing\n%s", res.body)
	}
}
