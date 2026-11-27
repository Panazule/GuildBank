package mariadb

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

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
		"r.GuildBankItemID = ?",
		"LOWER(r.CharacterName) LIKE LOWER(?) ESCAPE '!'",
		"LOWER(r.ItemName) LIKE LOWER(?) ESCAPE '!'",
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
	if got, want := args[4], "%Bob'; DROP TABLE InventoryRequest; --%"; got != want {
		t.Fatalf("character pattern = %q, want %q", got, want)
	}
	if got, want := args[5], "%Sword%"; got != want {
		t.Fatalf("item pattern = %q, want %q", got, want)
	}

	empty, emptyArgs := requestWhere(domain.RequestFilter{})
	if empty != "1 = 1" || len(emptyArgs) != 0 {
		t.Fatalf("empty where = %q %#v", empty, emptyArgs)
	}
}

func TestRequestLikePatternEscapesWildcards(t *testing.T) {
	for input, want := range map[string]string{
		"Blade":        "%Blade%",
		"100% Rune":    "%100!% Rune%",
		"Rune _ Stone": "%Rune !_ Stone%",
		"Exact!":       "%Exact!!%",
	} {
		if got := requestLikePattern(input); got != want {
			t.Fatalf("requestLikePattern(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestRequestWherePartialTextMatching executes the generated predicate against
// an in-memory database so the contains, case-fold and escape behaviour is
// asserted as SQL rather than only as a string. SQLite implements the same
// LOWER/LIKE/ESCAPE semantics used by MariaDB for these ASCII values.
func TestRequestWherePartialTextMatching(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE InventoryRequest (
		RequestID INTEGER PRIMARY KEY,
		CharacterName TEXT NOT NULL,
		ItemName TEXT NOT NULL,
		Status TEXT NOT NULL,
		RequesterKind TEXT NOT NULL,
		RequesterUserID INTEGER,
		GuildBankItemID INTEGER NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	seed := []struct {
		id        int64
		character string
		item      string
		status    string
		kind      string
	}{
		{1, "Franz", "Blade of Carnage", "Pending", "Member"},
		{2, "Frostreaver", "Blade of War", "Pending", "Member"},
		{3, "Frantic", "Blade of Tactics", "Completed", "Member"},
		{4, "Thorin", "Blade of Carnage", "Pending", "Public"},
		{5, "Percent", "100% Rune", "Pending", "Member"},
		{6, "Underscore", "Rune _ Stone", "Pending", "Member"},
	}
	for _, row := range seed {
		if _, err := db.Exec(`INSERT INTO InventoryRequest
			(RequestID, CharacterName, ItemName, Status, RequesterKind, RequesterUserID, GuildBankItemID)
			VALUES (?, ?, ?, ?, ?, 7, 1)`,
			row.id, row.character, row.item, row.status, row.kind); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name   string
		filter domain.RequestFilter
		want   []string
	}{
		{"partial character", domain.RequestFilter{CharacterName: "Fr"},
			[]string{"Franz", "Frostreaver", "Frantic"}},
		{"partial item", domain.RequestFilter{ItemName: "Blade"},
			[]string{"Franz", "Frostreaver", "Frantic", "Thorin"}},
		{"case-insensitive", domain.RequestFilter{CharacterName: "fRaNz", ItemName: "CARN"},
			[]string{"Franz"}},
		{"combined filters", domain.RequestFilter{
			Status: domain.RequestPending, RequesterKind: domain.RequesterMember,
			CharacterName: "fr", ItemName: "blade",
		}, []string{"Franz", "Frostreaver"}},
		{"percent is literal", domain.RequestFilter{ItemName: "%"}, []string{"Percent"}},
		{"underscore is literal", domain.RequestFilter{ItemName: "_"}, []string{"Underscore"}},
		{"no match", domain.RequestFilter{CharacterName: "zzz"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			where, args := requestWhere(tc.filter)
			rows, err := db.Query(`SELECT r.CharacterName FROM InventoryRequest r
				WHERE `+where+` ORDER BY r.RequestID`, args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var got []string
			for rows.Next() {
				var character string
				if err := rows.Scan(&character); err != nil {
					t.Fatal(err)
				}
				got = append(got, character)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("matches = %v, want %v (where %q args %#v)", got, tc.want, where, args)
			}
		})
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
