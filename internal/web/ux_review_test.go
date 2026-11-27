package web

import (
	"strings"
	"testing"
)

// TestRequestFormUXRoundOne pins the two request-page changes: the redundant
// quantity range is gone and the note field is labelled above a doubled box.
func TestRequestFormUXRoundOne(t *testing.T) {
	form := readEmbedded(t, "templates/request_form.html")
	if strings.Contains(form, "1–{{.Item.Quantity}}") {
		t.Fatal("the redundant quantity range hint is still rendered")
	}
	if !strings.Contains(form, `rows="8"`) {
		t.Fatal("the note textarea is not twice its former default height")
	}
	label := strings.Index(form, "Note (optional)")
	area := strings.Index(form, `<textarea name="note"`)
	if label < 0 || area < 0 || label > area {
		t.Fatal("the note label is not before the textarea")
	}
}

// TestRequestQueueRequesterAndFilterLayout pins the combined requester label
// and the deliberate two-line actionable filter.
func TestRequestQueueRequesterAndFilterLayout(t *testing.T) {
	queue := readEmbedded(t, "templates/admin_requests.html")
	for _, want := range []string{
		`{{.Character}}{{if and (eq .RequesterKind "Member") .RequesterName}} ({{.RequesterName}}){{end}}`,
		"Only actionable<br>(on hand ≥ 1)",
	} {
		if !strings.Contains(queue, want) {
			t.Fatalf("admin request queue is missing %q", want)
		}
	}
}

// TestAdminInventoryUsesTooltippedIconActions pins the compact action column.
// The screen-reader labels keep every action's name available to assistive
// technology and to existing text-based checks.
func TestAdminInventoryUsesTooltippedIconActions(t *testing.T) {
	list := readEmbedded(t, "templates/admin_inventory.html")
	for _, want := range []string{
		`class="action-links"`, `class="action-link"`, `class="sr-only">Edit</span>`,
		`title="Edit"`, `title="History"`, `title="Movements"`, `title="Issue"`,
		`title="Remove"`, `title="Restore"`, `title="Return"`,
		`{{template "actionIconEdit"}}`, `{{template "actionIconRemove"}}`,
	} {
		if !strings.Contains(list, want) {
			t.Fatalf("admin inventory action column is missing %q", want)
		}
	}
}

// TestImportReviewUXRoundOne pins the removed location copy and the compact
// grouped button layout. Every script hook keeps its original id.
func TestImportReviewUXRoundOne(t *testing.T) {
	detail := readEmbedded(t, "templates/import_detail.html")
	for _, forbidden := range []string{
		"The selector is built from normalized", "Default selection: container rows",
	} {
		if strings.Contains(detail, forbidden) {
			t.Fatalf("the location helper copy is still present: %q", forbidden)
		}
	}
	for _, want := range []string{
		`class="location-actions"`, `class="action-group"`, `class="button-group"`,
		`class="action-group-label">Locations`, `class="action-group-label">Scope`,
		`id="check-all-locations"`, `id="uncheck-all-locations"`, `id="select-all"`,
		`id="clear-all"`, `id="reset-defaults"`,
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("import location actions are missing %q", want)
		}
	}
}

// TestRaceAbbreviationTooltipAndAccountSpacing pins the remaining template
// changes that have no Go view model.
func TestRaceAbbreviationTooltipAndAccountSpacing(t *testing.T) {
	list := readEmbedded(t, "templates/inventory_list.html")
	if !strings.Contains(list, `{{if .RaceTitle}} title="{{.RaceTitle}}"{{end}}`) {
		t.Fatal("the race abbreviation has no full-name tooltip")
	}
	accounts := readEmbedded(t, "templates/admin_users.html")
	if !strings.Contains(accounts, `class="wizard-actions account-actions"`) {
		t.Fatal("the Create account action has no spacing hook")
	}
}

// TestUXRoundOneCSS pins the shared styles introduced by the review.
func TestUXRoundOneCSS(t *testing.T) {
	body, err := Static.ReadFile("static/app.css")
	if err != nil {
		t.Fatalf("read app.css: %v", err)
	}
	css := string(body)
	for _, want := range []string{
		"textarea {", "resize: vertical", ".action-link", ".action-icon-svg",
		"position: sticky", ".account-actions", ".location-actions",
		".action-group-label", ".button-group", ".checkbox-row span",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("app.css is missing %q", want)
		}
	}
}
