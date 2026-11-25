package mariadb

import (
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

// TestAdminInventoryWhereStatuses locks the mutually exclusive lifecycle
// predicates. Active, Issued and Removed must never overlap, and All must carry
// no status predicate.
func TestAdminInventoryWhereStatuses(t *testing.T) {
	cases := []struct {
		status domain.AdminInventoryStatus
		want   string
	}{
		{domain.AdminStatusActive, "DeletedFg = 0"},
		{domain.AdminStatusIssued, "DeletedFg = 1 AND GivenTo IS NOT NULL AND GivenTo <> ''"},
		{domain.AdminStatusRemoved, "DeletedFg = 1 AND (GivenTo IS NULL OR GivenTo = '')"},
		{domain.AdminStatusAll, "1 = 1"},
		{"", "1 = 1"},
	}
	for _, tc := range cases {
		where, args := adminInventoryWhere(domain.AdminInventoryFilter{Status: tc.status})
		if where != tc.want {
			t.Fatalf("status %q where = %q, want %q", tc.status, where, tc.want)
		}
		if len(args) != 0 {
			t.Fatalf("status %q args = %#v, want none", tc.status, args)
		}
	}
}

// TestAdminInventoryWhereIssuedKeepsRecipientPredicate proves the issued queue
// composes the status predicate with the parameterized recipient filter.
func TestAdminInventoryWhereIssuedKeepsRecipientPredicate(t *testing.T) {
	where, args := adminInventoryWhere(domain.AdminInventoryFilter{
		Status: domain.AdminStatusIssued, GivenTo: "Joe",
	})
	if !strings.Contains(where, "GivenTo = ?") {
		t.Fatalf("where = %q, want the recipient predicate", where)
	}
	if len(args) != 1 || args[0] != "Joe" {
		t.Fatalf("args = %#v, want [Joe]", args)
	}
}

// TestAdminInventoryOrderRecipient pins the recipient sort allowlist.
func TestAdminInventoryOrderRecipient(t *testing.T) {
	cases := map[domain.AdminInventorySort]string{
		domain.AdminSortRecipient:     "GivenTo IS NULL ASC, GivenTo ASC, NameSort ASC, GuildBankItemID ASC",
		domain.AdminSortRecipientDesc: "GivenTo IS NULL ASC, GivenTo DESC, NameSort ASC, GuildBankItemID ASC",
	}
	for sort, want := range cases {
		got, ok := adminInventoryOrder(sort)
		if !ok || got != want {
			t.Fatalf("adminInventoryOrder(%q) = %q, %v; want %q", sort, got, ok, want)
		}
	}
	if _, ok := adminInventoryOrder(domain.AdminInventorySort("GivenTo; DROP")); ok {
		t.Fatal("an unsafe sort must be rejected")
	}
}
