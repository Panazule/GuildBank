package service

import (
	"context"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

// TestAdminCreateNameMismatchIsAdvisoryWarning asserts the name/EQUID advisory:
// a typed name that disagrees with the resolved metadata name is a non-fatal
// warning, the metadata name is still stored, and a case-only difference is not
// reported.
func TestAdminCreateNameMismatchIsAdvisoryWarning(t *testing.T) {
	store := newFakeAdmin()
	svc := testAdminService(store, nil, testClock())
	result, err := svc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeItem, EQUID: 100, Quantity: 1, Name: "Sord", Actor: testActor(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result.Item.Name != "Sword" {
		t.Fatalf("stored name = %q, want the metadata name Sword", result.Item.Name)
	}
	if !warningsContain(result.Warnings, ItemWarningNameMismatch) {
		t.Fatalf("missing %s warning: %+v", ItemWarningNameMismatch, result.Warnings)
	}

	matching := newFakeAdmin()
	matchSvc := testAdminService(matching, nil, testClock())
	match, err := matchSvc.Create(context.Background(), domain.CreateItemInput{
		ItemType: domain.ItemTypeItem, EQUID: 100, Quantity: 1, Name: "sword", Actor: testActor(),
	})
	if err != nil {
		t.Fatalf("create matching name: %v", err)
	}
	if warningsContain(match.Warnings, ItemWarningNameMismatch) {
		t.Fatalf("case-only name difference raised a mismatch: %+v", match.Warnings)
	}
}

func warningsContain(warnings []ItemWarning, code string) bool {
	for _, warning := range warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}
