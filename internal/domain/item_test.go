package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestUnresolvedEQUIDIsReservedPlaceholder(t *testing.T) {
	if UnresolvedEQUID != 4294967295 {
		t.Fatalf("UnresolvedEQUID = %d, want 4294967295 (INT UNSIGNED max)", UnresolvedEQUID)
	}
	if !IsUnresolvedEQUID(UnresolvedEQUID) {
		t.Fatal("IsUnresolvedEQUID(UnresolvedEQUID) = false")
	}
	for _, value := range []uint64{0, 1, 28813, 4294967294} {
		if IsUnresolvedEQUID(value) {
			t.Fatalf("IsUnresolvedEQUID(%d) = true, want false", value)
		}
	}
}

func TestItemNameAmbiguousErrorIsValidationAndNamesCandidates(t *testing.T) {
	err := &ItemNameAmbiguousError{Name: "Ring", Candidates: []Item{
		{EQUID: 10, Name: "Ring"},
		{EQUID: 11, Name: "Ring"},
	}}
	if !errors.Is(err, ErrValidation) {
		t.Fatal("ambiguous error does not unwrap to ErrValidation")
	}
	message := err.Error()
	for _, want := range []string{"Ring", "10", "11", "more than one"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q is missing %q", message, want)
		}
	}
}

func TestAdminInventoryItemHasUnresolvedIdentity(t *testing.T) {
	if !(AdminInventoryItem{EQUID: UnresolvedEQUID}).HasUnresolvedIdentity() {
		t.Fatal("placeholder row not reported as unresolved")
	}
	if (AdminInventoryItem{EQUID: 100}).HasUnresolvedIdentity() {
		t.Fatal("real EQUID reported as unresolved")
	}
}
