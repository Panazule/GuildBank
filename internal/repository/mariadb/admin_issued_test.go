package mariadb

import (
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

// TestAdminIssuedWhereMatchesLotOrMovement locks decision D4: the id filter on
// the Issued projection matches both the lot id and the movement id.
func TestAdminIssuedWhereMatchesLotOrMovement(t *testing.T) {
	where, args := adminIssuedWhere(domain.AdminInventoryFilter{ID: 7})
	if !strings.Contains(where, "(m.GuildBankItemID = ? OR m.MovementID = ?)") {
		t.Fatalf("where = %q, want the lot-or-movement predicate", where)
	}
	if len(args) != 2 || args[0] != int64(7) || args[1] != int64(7) {
		t.Fatalf("args = %#v, want [7 7]", args)
	}
}

// TestAdminIssuedWhereEmptyAndRecipient proves an empty filter adds no
// predicate and the recipient filter is parameterized.
func TestAdminIssuedWhereEmptyAndRecipient(t *testing.T) {
	where, args := adminIssuedWhere(domain.AdminInventoryFilter{})
	if where != "" || len(args) != 0 {
		t.Fatalf("empty where = %q args = %#v", where, args)
	}
	where, args = adminIssuedWhere(domain.AdminInventoryFilter{GivenTo: "Joe"})
	if !strings.Contains(where, "m.Recipient = ?") {
		t.Fatalf("recipient where = %q", where)
	}
	if len(args) != 1 || args[0] != "Joe" {
		t.Fatalf("recipient args = %#v", args)
	}
}

// TestAdminIssuedSourceExcludesZeroOutstanding pins the projection predicate:
// only ISSUE movements with a strictly positive outstanding survive.
func TestAdminIssuedSourceExcludesZeroOutstanding(t *testing.T) {
	where, _ := adminIssuedWhere(domain.AdminInventoryFilter{})
	source := issuedSourceOnly(where)
	for _, want := range []string{
		"MovementType = 'ISSUE'",
		"RelatedMovementID",
		"m.Quantity > COALESCE(r.returned, 0)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("issued source is missing %q", want)
		}
	}
}

// TestAdminUnionOrderAllowlist pins the normalized ordering and rejects an
// unknown sort rather than interpolating it.
func TestAdminUnionOrderAllowlist(t *testing.T) {
	if _, ok := adminUnionOrder(domain.AdminInventorySort("GivenTo; DROP")); ok {
		t.Fatal("an unsafe sort must be rejected")
	}
	order, ok := adminUnionOrder(domain.AdminSortModifiedNewest)
	if !ok || !strings.Contains(order, "u.SortAt DESC") {
		t.Fatalf("order = %q ok = %v", order, ok)
	}
}
