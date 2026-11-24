package web

import (
	"strings"
	"testing"
)

func readEmbedded(t *testing.T, path string) string {
	t.Helper()
	body, err := Templates.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

// TestPublicInventoryListUIAmendments pins the owner-approved presentation of
// PD5-34, PD5-36, PD5-37 and PD5-38.
func TestPublicInventoryListUIAmendments(t *testing.T) {
	list := readEmbedded(t, "templates/inventory_list.html")
	if strings.Contains(list, "EQUID") || strings.Contains(list, "item-identity") {
		t.Fatal("public inventory grid still prints the ID/EQUID identity line")
	}
	if !strings.Contains(list, `title="View on EQResource"`) {
		t.Fatal("EQResource link lost its accessible title")
	}
	if !strings.Contains(list, `rel="noopener noreferrer"`) || !strings.Contains(list, `target="_blank"`) {
		t.Fatal("EQResource link lost its new-tab protections")
	}
	if !strings.Contains(list, "external-link-icon") || !strings.Contains(list, "<svg") {
		t.Fatal("EQResource link is not icon-only")
	}
	if strings.Contains(list, ">EQResource<") {
		t.Fatal("the repeated EQResource text label is still present")
	}
	if !strings.Contains(list, "No EQResource link") {
		t.Fatal("missing the screen-reader text for a row without a link")
	}
	if !strings.Contains(list, "availability-{{.Availability}}") {
		t.Fatal("availability badge lost its state class")
	}
	if !strings.Contains(list, "cell-nowrap") {
		t.Fatal("density class cell-nowrap is missing")
	}
	for _, forbidden := range []string{"template.HTML", "<script", "onclick="} {
		if strings.Contains(list, forbidden) {
			t.Fatalf("public list contains %q", forbidden)
		}
	}
}

func TestPublicInventoryDetailShowsLifecycleDates(t *testing.T) {
	detail := readEmbedded(t, "templates/inventory_detail.html")
	for _, want := range []string{
		"EQUID", "Date added", "Open to personas", "Open to bazaar",
		`datetime="{{.Item.DateOpenISO}}"`, `datetime="{{.Item.DateBazaarISO}}"`,
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail page is missing %q", want)
		}
	}
}

func TestAdminTemplatesExistAndCarryCSRF(t *testing.T) {
	for _, name := range []string{
		"templates/admin_inventory.html",
		"templates/admin_inventory_form.html",
		"templates/admin_inventory_detail.html",
		"templates/admin_remove.html",
		"templates/admin_audit.html",
	} {
		body := readEmbedded(t, name)
		if !strings.Contains(body, `{{template "nav" .}}`) {
			t.Fatalf("%s does not use the shared nav", name)
		}
		if strings.Contains(body, "template.HTML") {
			t.Fatalf("%s uses template.HTML", name)
		}
	}
	form := readEmbedded(t, "templates/admin_inventory_form.html")
	for _, want := range []string{
		`name="csrf_token"`, `name="expectedModAt"`, `name="dateOpen"`, `name="dateBazaar"`,
		`name="quantity"`, "One record stores this quantity",
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("admin form is missing %q", want)
		}
	}
	if strings.Contains(form, "readonly") {
		t.Fatal("item quantity is editable under QTY-08")
	}
	audit := readEmbedded(t, "templates/admin_audit.html")
	for _, want := range []string{"name=\"eventType\"", "name=\"correlationId\"", "name=\"from\"", "name=\"to\""} {
		if !strings.Contains(audit, want) {
			t.Fatalf("audit page is missing %q", want)
		}
	}
	remove := readEmbedded(t, "templates/admin_remove.html")
	if !strings.Contains(remove, "Remove record") || !strings.Contains(remove, `name="csrf_token"`) {
		t.Fatal("remove confirmation page is incomplete")
	}
}

// TestOfficerMovementTemplatesExistAndCarryCSRF pins the Phase 5 officer
// workflows: the Issue, Return and Movement history pages are server-rendered,
// use the shared nav, carry a CSRF token and never reach for template.HTML.
func TestOfficerMovementTemplatesExistAndCarryCSRF(t *testing.T) {
	for _, name := range []string{
		"templates/admin_inventory_issue.html",
		"templates/admin_inventory_return.html",
		"templates/admin_inventory_history.html",
	} {
		body := readEmbedded(t, name)
		if !strings.Contains(body, `{{template "nav" .}}`) {
			t.Fatalf("%s does not use the shared nav", name)
		}
		if strings.Contains(body, "template.HTML") || strings.Contains(body, "<script") {
			t.Fatalf("%s contains unsafe rendering", name)
		}
	}

	issue := readEmbedded(t, "templates/admin_inventory_issue.html")
	for _, want := range []string{
		`name="csrf_token"`, `name="expectedModAt"`, `name="idempotencyKey"`,
		`name="quantity"`, `name="recipient"`, `action="/admin/inventory/{{.Item.ID}}/issue"`,
		"On hand",
	} {
		if !strings.Contains(issue, want) {
			t.Fatalf("issue form is missing %q", want)
		}
	}

	ret := readEmbedded(t, "templates/admin_inventory_return.html")
	for _, want := range []string{
		`name="csrf_token"`, `name="idempotencyKey"`, `action="/admin/inventory/{{.Item.ID}}/return"`,
	} {
		if !strings.Contains(ret, want) {
			t.Fatalf("return form is missing %q", want)
		}
	}

	history := readEmbedded(t, "templates/admin_inventory_history.html")
	for _, want := range []string{
		"Movement type", "Recipient", "Correlation ID", "opening balance", "Remaining",
	} {
		if !strings.Contains(history, want) {
			t.Fatalf("movement history page is missing %q", want)
		}
	}
}

// TestAdminDetailExposesMovementAccess pins the item-level stock visibility and
// movement access approved for Phase 5 (PD5-33..PD5-38 briefly, §7.1/§7.3).
func TestAdminDetailExposesMovementAccess(t *testing.T) {
	detail := readEmbedded(t, "templates/admin_inventory_detail.html")
	for _, want := range []string{
		`/admin/inventory/{{.Item.ID}}/issue`, `/admin/inventory/{{.Item.ID}}/return`,
		`/admin/inventory/{{.Item.ID}}/movements`, "Movement history",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("admin detail is missing %q", want)
		}
	}
	list := readEmbedded(t, "templates/admin_inventory.html")
	if !strings.Contains(list, `/admin/inventory/{{.ID}}/movements`) {
		t.Fatal("admin list is missing the movement history action")
	}
}

// TestInventoryCSSUIAmendments pins PD5-33, PD5-35 and PD5-38 in the stylesheet.
func TestInventoryCSSUIAmendments(t *testing.T) {
	body, err := Static.ReadFile("static/app.css")
	if err != nil {
		t.Fatalf("read app.css: %v", err)
	}
	css := string(body)
	for _, want := range []string{
		"min(120rem, calc(100% - 1.5rem))",
		".admin-filter-grid",
		"repeat(auto-fit, minmax(11rem, 1fr))",
		".external-link",
		"white-space: nowrap",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("app.css is missing %q", want)
		}
	}
	if strings.Contains(css, "min(80rem, calc(100% - 2rem))") {
		t.Fatal("the fixed 80 rem inventory cap is still present")
	}
}
