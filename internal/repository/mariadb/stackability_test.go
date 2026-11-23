package mariadb

import (
	"context"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestStackabilityAllowlistLookup(t *testing.T) {
	store := &Store{}
	if err := store.SetStackabilityAllowlist(map[uint64]domain.Stackability{
		25210: domain.StackabilityStackable,
		100:   domain.StackabilityNonStackable,
	}); err != nil {
		t.Fatalf("set allow-list: %v", err)
	}
	got, err := store.LookupStackability(context.Background(), []uint64{25210, 100, 999, 25210, 0})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("lookup size = %d, want 3: %#v", len(got), got)
	}
	if got[25210] != domain.StackabilityStackable {
		t.Fatalf("25210 = %q", got[25210])
	}
	if got[100] != domain.StackabilityNonStackable {
		t.Fatalf("100 = %q", got[100])
	}
	if got[999] != domain.StackabilityUnknown {
		t.Fatalf("absent EQUID = %q, want unknown", got[999])
	}
}

func TestStackabilityAllowlistRejectsInvalidInput(t *testing.T) {
	store := &Store{}
	if err := store.SetStackabilityAllowlist(map[uint64]domain.Stackability{0: domain.StackabilityStackable}); err == nil {
		t.Fatal("EQUID 0 accepted")
	}
	if err := store.SetStackabilityAllowlist(map[uint64]domain.Stackability{1: "maybe"}); err == nil {
		t.Fatal("invalid stackability accepted")
	}
}

func TestStackabilityLookupEmpty(t *testing.T) {
	store := &Store{}
	got, err := store.LookupStackability(context.Background(), nil)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty lookup = %#v", got)
	}
}
