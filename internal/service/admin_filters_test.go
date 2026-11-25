package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestNormalizeAdminInventoryFilter(t *testing.T) {
	filter, err := NormalizeAdminInventoryFilter(domain.AdminInventoryFilter{
		ItemType: "spell", Status: "", Sort: "", Page: 1, PageSize: 50, Metadata: "",
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if filter.ItemType != domain.ItemTypeSpell || filter.Status != domain.AdminStatusActive ||
		filter.Sort != domain.AdminSortModifiedNewest || filter.Metadata != domain.AdminMetadataAny {
		t.Fatalf("defaults = %+v", filter)
	}
	var validation *domain.ValidationError
	_, err = NormalizeAdminInventoryFilter(domain.AdminInventoryFilter{
		Status: "deleted", Page: 1, PageSize: 50,
	})
	if !errors.As(err, &validation) || validation.Fields["status"] == "" {
		t.Fatalf("err = %v, want status validation", err)
	}
	_, err = NormalizeAdminInventoryFilter(domain.AdminInventoryFilter{Page: 0, PageSize: 500})
	if !errors.As(err, &validation) {
		t.Fatalf("bounds err = %v", err)
	}
}

// TestNormalizeAdminInventoryFilterAcceptsIssued locks the mutually exclusive
// lifecycle contract: active, issued, removed and all are the only statuses.
func TestNormalizeAdminInventoryFilterAcceptsIssued(t *testing.T) {
	for _, status := range []domain.AdminInventoryStatus{
		domain.AdminStatusActive, domain.AdminStatusIssued,
		domain.AdminStatusRemoved, domain.AdminStatusAll,
	} {
		got, err := NormalizeAdminInventoryFilter(domain.AdminInventoryFilter{Status: status, Page: 1, PageSize: 50})
		if err != nil {
			t.Fatalf("status %q: %v", status, err)
		}
		if got.Status != status {
			t.Fatalf("status %q normalized to %q", status, got.Status)
		}
	}
	if _, err := NormalizeAdminInventoryFilter(domain.AdminInventoryFilter{Status: "issued ", Page: 1, PageSize: 50}); err == nil {
		t.Fatal("an unknown status must be rejected")
	}
}

func TestNormalizeAuditFilter(t *testing.T) {
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	_, err := NormalizeAuditFilter(domain.AuditFilter{
		EventType: "ITEM_CREATED", From: &from, To: &to, Page: 1, PageSize: 50,
	})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || !strings.Contains(validation.Fields["to"], "before") {
		t.Fatalf("err = %v, want to-before-from validation", err)
	}
}

func TestNormalizeAdminLocationWarnings(t *testing.T) {
	cases := []struct {
		raw        string
		group      string
		adminWarn  bool
		importWarn string
	}{
		{"General5-Slot1", "General 5", false, ""},
		{"bank04", "Bank 4", false, ""},
		{"SharedBank2-Slot6", "SharedBank 2", false, ""},
		{"Hoard17", "Hoard", false, ""},
		{"  ", "Unspecified", false, "location_blank"},
		{"Guild Hall", "Unspecified", true, ""},
		{"General", "Unspecified", true, "location_pattern_invalid"},
	}
	for _, c := range cases {
		admin := NormalizeAdminLocation(c.raw)
		imported := NormalizeLocation(c.raw)
		if admin.Group != c.group || imported.Group != c.group {
			t.Fatalf("%q groups = %q/%q, want %q", c.raw, admin.Group, imported.Group, c.group)
		}
		if admin.Sort != imported.Sort {
			t.Fatalf("%q sorts differ: %q vs %q", c.raw, admin.Sort, imported.Sort)
		}
		adminWarn := false
		for _, warning := range admin.Warnings {
			if warning.Code == ItemWarningLocationUnrecognized {
				adminWarn = true
			}
		}
		if adminWarn != c.adminWarn {
			t.Fatalf("%q admin warning = %v, want %v (%+v)", c.raw, adminWarn, c.adminWarn, admin.Warnings)
		}
		importCode := ""
		if len(imported.Warnings) > 0 {
			importCode = imported.Warnings[0].Code
		}
		if importCode != c.importWarn {
			t.Fatalf("%q import warning = %q, want %q", c.raw, importCode, c.importWarn)
		}
	}
}
