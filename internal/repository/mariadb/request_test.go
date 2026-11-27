package mariadb

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestNormalizeRequestFilter(t *testing.T) {
	normalized, err := normalizeRequestFilter(domain.RequestFilter{})
	if err != nil {
		t.Fatalf("default filter rejected: %v", err)
	}
	if normalized.Page != 1 || normalized.PageSize != domain.RequestDefaultPageSize {
		t.Fatalf("defaults = page %d size %d", normalized.Page, normalized.PageSize)
	}

	from := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	normalized, err = normalizeRequestFilter(domain.RequestFilter{
		Status: domain.RequestPending, RequesterKind: domain.RequesterMember,
		RequesterUserID: 7, GuildBankItemID: 12,
		CharacterName: "  Bob  ", ItemName: "  Sword  ", From: from, To: to,
		Page: 2, PageSize: 25,
	})
	if err != nil {
		t.Fatalf("valid filter rejected: %v", err)
	}
	if normalized.CharacterName != "Bob" || normalized.ItemName != "Sword" {
		t.Fatalf("trim failed: %#v", normalized)
	}

	for name, filter := range map[string]domain.RequestFilter{
		"unknown status":     {Status: "Approved"},
		"unknown kind":       {RequesterKind: "Officer"},
		"negative requester": {RequesterUserID: -1},
		"negative lot":       {GuildBankItemID: -1},
		"oversized page":     {Page: domain.RequestMaxPage + 1},
		"oversized pageSize": {PageSize: domain.RequestMaxPageSize + 1},
		"reversed range":     {From: to, To: from},
	} {
		if _, err := normalizeRequestFilter(filter); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestRequestWhereUsesParameterizedPredicates(t *testing.T) {
	from := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	where, args := requestWhere(domain.RequestFilter{
		Status: domain.RequestPending, RequesterKind: domain.RequesterPublic,
		RequesterUserID: 7, GuildBankItemID: 12,
		CharacterName: "Bob'; DROP TABLE InventoryRequest; --",
		ItemName:      "Sword", From: from, To: to,
	})
	for _, fragment := range []string{
		"r.Status = ?", "r.RequesterKind = ?", "r.RequesterUserID = ?",
		"r.GuildBankItemID = ?", "r.CharacterName = ?", "r.ItemName = ?",
		"r.CreatedAt >= ?", "r.CreatedAt <= ?",
	} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("where %q missing %q", where, fragment)
		}
	}
	if len(args) != 8 {
		t.Fatalf("args = %#v", args)
	}
	if strings.Contains(where, "DROP TABLE") {
		t.Fatalf("caller text leaked into SQL: %q", where)
	}

	empty, emptyArgs := requestWhere(domain.RequestFilter{})
	if empty != "1 = 1" || len(emptyArgs) != 0 {
		t.Fatalf("empty where = %q %#v", empty, emptyArgs)
	}
}

// TestRequestProjectionsMatchScanner keeps requestReadColumns (the stored
// columns plus the two derived lot columns) aligned with scanRequest's
// destination list, so a column added to one projection cannot silently shift
// every scanned value.
func TestRequestProjectionsMatchScanner(t *testing.T) {
	stored := strings.Split(requestColumns, ",")
	if len(stored) != 24 {
		t.Fatalf("requestColumns has %d columns, want 24", len(stored))
	}
	// The COALESCE default carries its own comma, so neutralize it before
	// splitting on the projection separators.
	read := strings.Split(strings.ReplaceAll(requestReadColumns, "COALESCE(g.Quantity, 0)", "COALESCE"), ",")
	if len(read) != len(stored)+2 {
		t.Fatalf("requestReadColumns has %d columns, want %d", len(read), len(stored)+2)
	}
	for _, derived := range []string{"COALESCE(g.Quantity, 0)", "(g.GuildBankItemID IS NOT NULL AND g.DeletedFg = 0)"} {
		if !strings.Contains(requestReadColumns, derived) {
			t.Fatalf("requestReadColumns missing %q", derived)
		}
	}
	if strings.Contains(requestReadColumns, "SELECT *") {
		t.Fatal("request projection uses SELECT *")
	}
}

// TestRequestRepositoryGuardsTransitions pins the two properties the workflow
// depends on: the lock path must not join the lot (lock order remains
// request then lot), and both terminal transitions must be guarded by
// Status = 'Pending' so a concurrent decision reports ErrConflict.
func TestRequestRepositoryGuardsTransitions(t *testing.T) {
	body, err := os.ReadFile("request.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	lockStart := strings.Index(source, "func (c *adminWriteTx) LockRequest")
	if lockStart < 0 {
		t.Fatal("LockRequest not found")
	}
	lockBody := source[lockStart:]
	if end := strings.Index(lockBody, "\nfunc "); end >= 0 {
		lockBody = lockBody[:end]
	}
	if strings.Contains(lockBody, "JOIN") {
		t.Fatal("LockRequest joins the lot, breaking the request-then-lot lock order")
	}
	if !strings.Contains(lockBody, "FOR UPDATE") {
		t.Fatal("LockRequest does not lock the row")
	}
	if count := strings.Count(source, "WHERE RequestID = ? AND Status = 'Pending'"); count != 2 {
		t.Fatalf("guarded transitions = %d, want 2", count)
	}
	for _, required := range []string{"INSERT INTO InventoryRequest", "ORDER BY r.CreatedAt DESC, r.RequestID DESC"} {
		if !strings.Contains(source, required) {
			t.Fatalf("request repository is missing %q", required)
		}
	}
	if strings.Contains(source, "SELECT *") {
		t.Fatal("request repository contains SELECT *")
	}
}
